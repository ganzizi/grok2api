#!/usr/bin/env python3
"""Convert a residential proxy dump into independent Mihomo listeners and node plans."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlparse


LINE_RE_USERINFO = re.compile(
    r"^(?P<user>[^:@\s/]+):(?P<password>[^@\s/]+)@(?P<host>[^:\s/]+):(?P<port>\d+)\s*$"
)
LINE_RE_HOSTFIRST = re.compile(
    r"^(?P<host>[^:\s/]+):(?P<port>\d+):(?P<user>[^:\s]+):(?P<password>\S+)\s*$"
)
SID_RE = re.compile(r"(?:^|[-_])sid[-_]?([A-Za-z0-9]+)", re.I)


def _split_name(raw: str) -> tuple[str, str]:
    text = raw.strip()
    if " | " in text:
        name, rest = text.split(" | ", 1)
        return name.strip(), rest.strip()
    return "", text


def parse_proxy_line(raw: str) -> dict[str, str] | None:
    """Parse one proxy line; comments and blank lines return None."""
    name, text = _split_name(raw)
    if not text or text.startswith("#") or text.startswith("//"):
        return None

    scheme = "http"
    username = password = host = ""
    port = 0

    try:
        if "://" in text:
            parsed = urlparse(text)
            scheme = (parsed.scheme or "http").lower()
            if scheme in {"socks5h", "socks5"}:
                scheme = "socks5"
            elif scheme in {"https", "http"}:
                scheme = "http"
            else:
                raise ValueError(f"unsupported proxy scheme: {scheme}")
            host = parsed.hostname or ""
            port = parsed.port or 0
            username = unquote(parsed.username or "")
            password = unquote(parsed.password or "")
        elif match := LINE_RE_USERINFO.match(text):
            username, password, host, port = (
                match.group("user"),
                match.group("password"),
                match.group("host"),
                int(match.group("port")),
            )
        elif match := LINE_RE_HOSTFIRST.match(text):
            host, port, username, password = (
                match.group("host"),
                int(match.group("port")),
                match.group("user"),
                match.group("password"),
            )
        else:
            raise ValueError(f"unrecognized proxy format: {text[:48]}")
    except ValueError as exc:
        raise ValueError(f"invalid proxy format: {text[:48]} ({exc})") from exc

    if not host or not port:
        raise ValueError(f"proxy is missing host or port: {text[:48]}")

    sid = ""
    if username:
        found = SID_RE.search(username)
        if found:
            sid = found.group(1)
    fingerprint = f"{scheme}://{username}@{host}:{port}"
    return {
        "name": name,
        "scheme": scheme,
        "host": host,
        "port": int(port),
        "username": username,
        "password": password,
        "sid": sid,
        "fingerprint": fingerprint,
    }


def parse_dump(text: str) -> list[dict[str, str]]:
    """Parse the complete dump and reject duplicate sessions."""
    sessions: list[dict[str, str]] = []
    seen: set[str] = set()
    for index, raw in enumerate(text.splitlines(), start=1):
        item = parse_proxy_line(raw)
        if item is None:
            continue
        if item["fingerprint"] in seen:
            raise ValueError(f"duplicate session on line {index}: {item['host']}:{item['port']}")
        seen.add(item["fingerprint"])
        sessions.append(item)
    if not sessions:
        raise ValueError("no residential sessions found")
    return sessions


def split_roles(count: int) -> tuple[int, int]:
    """Reserve roughly one quarter for registration when N >= 4."""
    if count >= 4:
        n_reg = max(1, count // 4)
        return count - n_reg, n_reg
    return count, 0


def guard_defaults(n_use: int) -> dict[str, object]:
    """Generate conservative quality-guard defaults from the use-side count."""
    lab_like = n_use >= 3
    return {
        "lab_like": lab_like,
        "mode": "passive",
        "soft_tps": 200 if lab_like else 500,
        "hard_tps": 1000,
        "fail_closed": lab_like,
        "min_healthy_nodes": 3 if n_use >= 4 else (2 if n_use == 3 else 1),
        "rank_scheduler_enabled": lab_like,
        "rank_dry_run": True,
        "request_retry_enabled": False,
        "warning": (
            None
            if lab_like
            else "fewer than 3 use-side sessions: smoke only, not lab-like"
        ),
    }


def assign_roles(sessions: list[dict[str, str]]) -> list[dict[str, object]]:
    """Assign a role, listener port, and unique names to every session."""
    n_use, _ = split_roles(len(sessions))
    out: list[dict[str, object]] = []
    for index, session in enumerate(sessions):
        if index < n_use:
            role = "use"
            seq = index + 1
            listen_port = 8300 + seq
        else:
            role = "reg"
            seq = index - n_use + 1
            listen_port = 8200 + seq
        label = session["name"] or f"{role}-{seq:02d}"
        if session["sid"] and not session["name"]:
            label = f"{role}-{session['sid'][:8]}"
        item = dict(session)
        item.update(
            {
                "role": role,
                "seq": seq,
                "listen_port": listen_port,
                "proxy_name": f"sticky-{role}-{seq:02d}",
                "listener_name": f"mixed-{role}-{seq:02d}",
                "node_name": label,
                "proxy_pool": False,
            }
        )
        out.append(item)
    return out


def _yaml_string(value: object) -> str:
    """Use JSON strings as YAML double-quoted scalars so credentials stay valid."""
    return json.dumps(str(value), ensure_ascii=False)


def render_mihomo(nodes: list[dict[str, object]]) -> str:
    """Render a Mihomo config with one independent listener per session."""
    lines = [
        "mixed-port: 0",
        "bind-address: 127.0.0.1",
        "allow-lan: false",
        "mode: rule",
        "log-level: info",
        "external-controller: 127.0.0.1:9090",
        "",
        "proxies:",
    ]
    for node in nodes:
        ptype = "socks5" if node["scheme"] == "socks5" else "http"
        lines.extend(
            [
                f"  - name: {_yaml_string(node['proxy_name'])}",
                f"    type: {ptype}",
                f"    server: {_yaml_string(node['host'])}",
                f"    port: {node['port']}",
            ]
        )
        if node["username"]:
            lines.append(f"    username: {_yaml_string(node['username'])}")
        if node["password"]:
            lines.append(f"    password: {_yaml_string(node['password'])}")
    lines.extend(["", "listeners:"])
    for node in nodes:
        lines.extend(
            [
                f"  - name: {_yaml_string(node['listener_name'])}",
                "    type: mixed",
                f"    port: {node['listen_port']}",
                "    listen: 127.0.0.1",
                f"    proxy: {_yaml_string(node['proxy_name'])}",
            ]
        )
    first = nodes[0]["proxy_name"]
    lines.extend(["", "rules:", f"  - MATCH,{first}", ""])
    return "\n".join(lines)


def public_nodes(nodes: list[dict[str, object]]) -> list[dict[str, object]]:
    """Render a node list without proxy credentials for Grok2API imports."""
    return [
        {
            "name": node["node_name"],
            "role": node["role"],
            "listen": f"http://127.0.0.1:{node['listen_port']}",
            "host_from_docker": f"http://host.docker.internal:{node['listen_port']}",
            "proxy_pool": False,
            "scheme": node["scheme"],
            "upstream_host": node["host"],
            "has_sid": bool(node["sid"]),
        }
        for node in nodes
    ]


def render_plan(nodes: list[dict[str, object]], guard: dict[str, object]) -> str:
    """Render the port allocation and quality-guard plan."""
    use = [n for n in nodes if n["role"] == "use"]
    reg = [n for n in nodes if n["role"] == "reg"]
    lines = [
        "# Residential split plan",
        "",
        f"- sessions: {len(nodes)}",
        f"- use-side nodes: {len(use)} (8301+)",
        f"- register-side nodes: {len(reg)} (8201+)",
        f"- lab-like: {guard['lab_like']}",
        "",
        "## Listeners (safe to print)",
        "",
    ]
    for node in nodes:
        lines.append(
            f"- {node['node_name']}: {node['role']} -> 127.0.0.1:{node['listen_port']}"
        )
    lines.extend(
        [
            "",
            "## Quality Guard defaults (lab-like when use-side >= 3)",
            "",
            "```yaml",
            "qualityGuard:",
            "  enabled: true",
            f"  mode: {guard['mode']}",
            f"  softTPS: {guard['soft_tps']}",
            f"  hardTPS: {guard['hard_tps']}",
            f"  failClosed: {str(guard['fail_closed']).lower()}",
            f"  minimumHealthyNodes: {guard['min_healthy_nodes']}",
            "```",
            "",
            f"- RANK_SCHEDULER_ENABLED={str(guard['rank_scheduler_enabled']).lower()}",
            f"- RANK_DRY_RUN={str(guard['rank_dry_run']).lower()}  # flip to false after one traffic day",
            "",
        ]
    )
    if guard["warning"]:
        lines.extend(["## Warning", "", str(guard["warning"]), ""])
    lines.extend(
        [
            "## Docker note",
            "",
            "If Grok2API is not on host network, do not put 127.0.0.1 in the node URL.",
            "Use host.docker.internal / the host gateway, or run Grok2API with network_mode: host.",
            "",
        ]
    )
    return "\n".join(lines)


def _write_private(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")
    path.chmod(0o600)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dump", nargs="?", help="one proxy per line; read stdin when omitted")
    parser.add_argument("--out-dir", default="egress-gen", help="output directory, default: egress-gen")
    args = parser.parse_args(argv)

    try:
        raw = Path(args.dump).read_text(encoding="utf-8") if args.dump else sys.stdin.read()
        sessions = parse_dump(raw)
    except (OSError, ValueError) as exc:
        parser.error(str(exc))

    nodes = assign_roles(sessions)
    guard = guard_defaults(sum(1 for node in nodes if node["role"] == "use"))
    out = Path(args.out_dir)
    out.mkdir(parents=True, exist_ok=True, mode=0o700)
    out.chmod(0o700)
    _write_private(out / "mihomo.yaml", render_mihomo(nodes))
    _write_private(
        out / "nodes.json",
        json.dumps(public_nodes(nodes), ensure_ascii=False, indent=2) + "\n",
    )
    _write_private(out / "plan.md", render_plan(nodes, guard))
    _write_private(out / "guard.json", json.dumps(guard, ensure_ascii=False, indent=2) + "\n")
    print(f"wrote {out}/mihomo.yaml {out}/nodes.json {out}/plan.md")
    print(
        f"sessions={len(nodes)} use={sum(1 for n in nodes if n['role'] == 'use')} "
        f"reg={sum(1 for n in nodes if n['role'] == 'reg')} lab_like={guard['lab_like']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
