# 免费额度 429、会话 pin 换号、质量探测噪音

- 日期：2026-09-12
- 实例：`ganzizi-grok2api-15353`（Compose 项目同名，宿主机端口 15353）
- 落地提交：`47f9e31` `fix: fail over previous_response_id pin after free-quota exhaustion`
- 源码：`/opt/ganzizi-grok2api-15353/source`，自动更新脚本 `auto-update.sh`

## 现象（容易混成一件事）

1. Grok Build 长任务做到一半停住，新开一轮又能跑，后台号池看起来还有额度。
2. 管理端 / 容器日志里 **一直有 429**，看起来像长任务在不停打爆号。
3. 改完 pin 换号之后，429 日志还在，长任务窗口也还是不往下走。

这三件事不是同一个根因。

## 结论对照

| 你看到的 | 实际是什么 | 要不要改 gateway |
| --- | --- | --- |
| 长会话下一轮失败，新提问又能跑 | `previous_response_id` 把请求钉在已耗尽的号 A 上；换到 B 后质量拦截 `fail_closed`，号池其余号不用 | 要。已落地为 A |
| 后台每隔几分钟一条红 429 | `[system] Egress Quality Guard` 约每 5 分钟探一次 `grok-4.6`，最终状态就是 `429 upstream_quota_exhausted`，**不走换号** | 不要回滚 A。要消噪音就停探测或改频率 |
| 你的 Key 请求日志里也有 429 | 免费 `grok_build` 号额度打光，上游返回 `subscription:free-usage-exhausted`；A 之后同一 `request_id` 对外经常是 **HTTP 200** | 429 行是换号前的上游信号，不是整单失败 |
| 某个 Grok 窗口一直转、不吐字也不工具 | 卡在 MCP（例如 `chrome-devtools take_snapshot`，超时 6000 秒），**当时没有模型请求**，A 帮不上 | 中断工具 / 新开一轮 |

## 根因 A：会话 pin + 质量拦截

Grok Build 多轮会带 `previous_response_id`。gateway 第一次走 `AcquirePinnedForKey`，钉死号 A。

1. 号 A 免费额度用完 → 上游 `429 subscription:free-usage-exhausted`，写入 `account_quota_recovery`（kind=`free`，约 24h 后再探），sticky 会删。候选过滤本身是对的。
2. 同一次请求还能选到号 B（现场常见：A 429 后 3–12 秒 B 上质量拦截）。
3. `canReplayQualityHoldAcrossAccounts` 要求 `ownership == nil`。pin 不清，流式 grok-4.6 的 `qualityGuard.requestRetry.onExhausted=fail_closed` 会让 `hasNextAccount` 为假，号 B 缺推理直接 `quality_degraded_rejected`，C 以后不用。
4. 钉死号在选号阶段已经 `SelectionQuotaExhausted` / cooling 时，旧代码 `break` 整段尝试循环，而不是释放 pin 再从号池选。

所以：后台「还有很多号」是真的；长任务下一轮仍像 429/503，是因为 **pin 比 sticky 活得更长**。新提问没有 `previous_response_id`，所以又能跑。

部署前 24h 量级（容器日志）：`subscription:free-usage-exhausted` 约 178 次 / 170+ 账号；`quality_degraded_rejected` 更多；同 `request_id` 上 `429(A) → reject(B)` 能对上十几次。

## 落地 A（不要回滚）

只改 `backend/internal/application/gateway/service.go` 尝试循环，不改 selector、不关 sticky。

- 钉死号选号失败且原因是额度耗尽 / 冷却 / 模型冷却：释放 in-request pin，`continue` 从号池选。
- 请求打到 `subscription:free-usage-exhausted` 并 `MarkFreeQuotaExhausted` 后：同样释放 pin，重算 `qualityCrossAccountReplay` 和 attempt 上限。
- 成功路径仍钉原号。账号禁用仍走原来的 pinned 503。hosted 工具仍不跨号重放质量 hold（`canReplayQualityHoldAcrossAccounts` 的 hosted 检查还在）。

测试：

- `TestGatewayPreviousResponseIDFailoversAfterFreeQuotaExhaustion`：A 返回免费额度 429 后打到 B 并 200。
- `TestGatewayPreviousResponseIDFailoversWhenPinnedAccountAlreadyExhausted`：A 已在 recovery，请求直接打 B。
- `TestPinnedSelectionAllowsPoolFailover` / `TestGatewayPinnedDisabledOwnerRecordsSpecific503`：禁用号不换。

本机与服务器 `golang:1.26-alpine` 均跑过上述测试。部署后二进制里有日志字段 `pinned_account_quota_exhausted_failover`。

**不要做 B：** 为了质量 hold 跨号重放而在任意 429 后清 ownership（hosted 工具可能已经有副作用）。A 只在额度/冷却这条线上 unpin。

## 根因 B：质量探测一直刷 429

`deploy/compose.yaml` 里 `egress-quality-guard` 默认 `QUALITY_GUARD_MODE=passive`，模型 `grok-4.6`。线上审计里客户端名是 `[system] Egress Quality Guard`，`request_id` 形如 `quality_*`。

现场：约每 5 分钟一条，最终 HTTP **就是 429** `upstream_quota_exhausted`，`attempt_count=1`，常常没有换号。这是后台「一直都有 429」的节奏来源，与长任务是否卡死无关。

A 修的是用户 Key 的 pin 路径，探测带强制账号/节点，不走这套换号。不要因为探测还在 429 就回滚 A。

要消噪音：停该容器、拉长探测间隔、或改用不吃免费对话额度的探测模型。未改配置，只记在这里。

## 根因 C：Grok 窗口卡住 ≠ 429

2026-09-12 旁路长任务（Outlook 注册机控制台，会话 `01a09385`）：

- 最后一次模型吐字约 13:30（北京）。
- 随后 `chrome-devtools take_snapshot` 超时 6000 秒，MCP started/completed = 65/64，差这一次。
- `usage.json` 不再增长。gateway 没有对应的新模型请求。

A 只在「请求已经打到 grok2api」时生效。工具挂住时不会 429，也不会换号。处理：在那扇窗口中断工具，或新开一轮（新提问没有 pin）。

## 部署后怎么读日志（15353）

用户 Key（现场名 `my`）`grok-4.6` / `xhigh` / 流式：

- 内部：`upstream_request_failed` `status=429` `upstream_code=subscription:free-usage-exhausted`
- 成功换号：同一 `request_id` 的 `http_request` `POST /v1/responses` **200**
- pin 路径：还会有 `pinned_account_quota_exhausted_failover` 或 `pinned_account_unavailable_failover`

质量探测：

- `client_key_name` like `%Quality%`
- 最终 `status_code=429` `error_code=upstream_quota_exhausted`

抽样（部署后约 2 小时）：用户 Key 最终 429 为 0；探测 24 条最终 429。用户 Key 内部 429 后对外 200。

号池当时：`grok_build` 启用活跃约 1000，`account_quota_recovery` kind=`free` 一百多条。不是空池，是免费号被逐个打穿再换下一个。

## 下次怎么查

在宿主机（容器数据卷 SQLite）：

```bash
DB=/var/lib/docker/volumes/ganzizi-grok2api-15353-data/_data/backend.db

# 最近最终状态按客户端拆开
sqlite3 -header -column "$DB" "
SELECT client_key_name, status_code, error_code, count(*) n
FROM request_audits
WHERE created_at > datetime('now','-2 hours')
GROUP BY 1,2,3 ORDER BY n DESC;"

# 内部 429 尝试 vs 最终 HTTP
sqlite3 -header -column "$DB" "
SELECT a.created_at, a.request_id, a.client_key_name, a.status_code AS final_http,
       t.account_id AS fail_acct, a.account_id AS final_acct, t.upstream_status_code
FROM request_audit_attempts t
JOIN request_audits a ON a.id=t.audit_id
WHERE t.upstream_status_code=429
  AND a.created_at > datetime('now','-2 hours')
ORDER BY a.created_at DESC LIMIT 20;"
```

容器日志：

```bash
docker logs --since 2h ganzizi-grok2api-15353 2>&1 \
  | grep -E 'upstream_request_failed|quality_degraded_rejected|pinned_account_|http_request'
```

本机 Grok 会话是否在跑模型：看该 session 的 `events.jsonl` 最后 `first_token` / `tool_started`，以及 `usage.json` 是否还在涨。只有 MCP `started` 多于 `completed` 时，优先查工具挂死，不要先怪 429。

更新这台实例：源码快进 `origin/main` 后只重建 API 镜像即可（探测镜像未改）：

```bash
# 与 /opt/ganzizi-grok2api-15353/auto-update.sh 同一把锁
cd /opt/ganzizi-grok2api-15353/source
git fetch --prune origin main && git merge --ff-only origin/main
docker compose --profile flaresolverr -p ganzizi-grok2api-15353 \
  -f /opt/ganzizi-grok2api-15353/deploy/compose.yaml build grok2api
docker compose --profile flaresolverr -p ganzizi-grok2api-15353 \
  -f /opt/ganzizi-grok2api-15353/deploy/compose.yaml up -d grok2api
```

## 和本仓库无关、但同一次排障碰到的客户端配置

Grok Build 只读 `~/.grok/config.toml`。cc-switch 的 MCP 库（`mcp_servers` + `enabled_grokbuild`）在切供应商时写入 live；**不要把 `[mcp_servers]` 再写进 grok 供应商快照**。本 fork 的 `strip_grok_mcp_servers_from_settings` 就是这个分工。live 里出现 MCP 段是注入结果。内置 `web_search` 已关时，库里必须留着 `grok-search-rs` 的 Grok 开关，否则没有搜索。

## 明确不做的

- 回滚 `47f9e31`。探测还在 429 不是回滚理由。
- 为任意 429 清 pin（B）。hosted 工具有重复执行风险。
- 把「日志里有 429」当成任务失败。先看最终 `status_code` 和同一 `request_id` 的 `http_request`。
