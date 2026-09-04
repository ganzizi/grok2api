#!/usr/bin/env python3
"""将家宽代理清单转换为独立的 Mihomo 监听器和节点规划。"""

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
    """解析一行代理，注释和空行返回 None。"""
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
                raise ValueError(f"不支持的代理协议: {scheme}")
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
            raise ValueError(f"无法识别代理格式: {text[:48]}")
    except ValueError as exc:
        raise ValueError(f"代理格式错误: {text[:48]} ({exc})") from exc

    if not host or not port:
        raise ValueError(f"代理缺少 host 或 port: {text[:48]}")

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
    """解析完整清单，并拒绝重复 session。"""
    sessions: list[dict[str, str]] = []
    seen: set[str] = set()
    for index, raw in enumerate(text.splitlines(), start=1):
        item = parse_proxy_line(raw)
        if item is None:
            continue
        if item["fingerprint"] in seen:
            raise ValueError(f"第 {index} 行重复 session: {item['host']}:{item['port']}")
        seen.add(item["fingerprint"])
        sessions.append(item)
    if not sessions:
        raise ValueError("没有找到家宽 session")
    return sessions


def split_roles(count: int) -> tuple[int, int]:
    """N >= 4 时预留约四分之一作为注册侧，其余作为使用侧。"""
    if count >= 4:
        n_reg = max(1, count // 4)
        return count - n_reg, n_reg
    return count, 0


def guard_defaults(n_use: int) -> dict[str, object]:
    """根据使用侧 session 数生成保守的质量守护默认值。"""
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
            else "使用侧少于 3 条 session：只能作为冒烟环境，不能称为 lab-like"
        ),
    }


def assign_roles(sessions: list[dict[str, str]]) -> list[dict[str, object]]:
    """为每条 session 分配角色、监听端口和不重复的名称。"""
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
    """使用 JSON 字符串作为 YAML 双引号标量，避免凭据破坏配置。"""
    return json.dumps(str(value), ensure_ascii=False)


def render_mihomo(nodes: list[dict[str, object]]) -> str:
    """生成每个 session 一个独立 listener 的 Mihomo 配置。"""
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
    """生成不包含代理账密的节点清单，便于导入 Grok2API。"""
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
    """生成端口分配和质量守护规划。"""
    use = [n for n in nodes if n["role"] == "use"]
    reg = [n for n in nodes if n["role"] == "reg"]
    lines = [
        "# 家宽拆分规划",
        "",
        f"- session 总数: {len(nodes)}",
        f"- 使用侧节点: {len(use)}（8301+）",
        f"- 注册侧节点: {len(reg)}（8201+）",
        f"- lab-like: {guard['lab_like']}",
        "",
        "## 监听器（可安全展示）",
        "",
    ]
    for node in nodes:
        lines.append(
            f"- {node['node_name']}: {node['role']} -> 127.0.0.1:{node['listen_port']}"
        )
    lines.extend(
        [
            "",
            "## Quality Guard 默认值（使用侧 >= 3 时接近 lab）",
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
            f"- RANK_DRY_RUN={str(guard['rank_dry_run']).lower()}（真实流量运行一天后再关闭）",
            "",
        ]
    )
    if guard["warning"]:
        lines.extend(["## 注意", "", str(guard["warning"]), ""])
    lines.extend(
        [
            "## Docker 注意事项",
            "",
            "Grok2API 不在 host 网络时，不要在节点 URL 中填写 127.0.0.1。",
            "应使用 host.docker.internal / 宿主机网关，或让 Grok2API 使用 network_mode: host。",
            "",
        ]
    )
    return "\n".join(lines)


def _write_private(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")
    path.chmod(0o600)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dump", nargs="?", help="每行一条代理，省略时从标准输入读取")
    parser.add_argument("--out-dir", default="egress-gen", help="生成目录，默认 egress-gen")
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
    print(f"已生成 {out}/mihomo.yaml {out}/nodes.json {out}/plan.md")
    print(
        f"sessions={len(nodes)} use={sum(1 for n in nodes if n['role'] == 'use')} "
        f"reg={sum(1 for n in nodes if n['role'] == 'reg')} lab_like={guard['lab_like']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
