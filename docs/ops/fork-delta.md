# 本 fork 相对上游的修改清单（升级对照）

- 仓库：`https://github.com/ganzizi/grok2api.git`（`origin`）
- 上游：`https://github.com/chenyme/grok2api.git`（`upstream`，`chenyme/grok2api` `main`）
- 子分支参考：`https://github.com/lij768423-svg/grok2api.git`（`child`，只挑提交，不整树合并）
- 线上实例：`ganzizi-grok2api-15353`（宿主机 15353）
- 本文目的：下次 `git merge upstream/main` 或跑 sync 工作流时，用文件和测试对行为，而不是靠猜

写本文时（2026-09-18）：

| 引用 | SHA |
| --- | --- |
| 本 fork `origin/main` | `6a23bfc4` `fix: skip quality hold for Claude Code compact via chat` |
| 与上游的 merge-base | `44a390b8` |
| 当时 `upstream/main`（尚未合入本 fork） | `906b9493` `chore: bump version to v3.1.6` |

`upstream/main` 会继续往前走。合入前重新跑：

```bash
git fetch upstream origin
git log --oneline HEAD..upstream/main
git merge-base HEAD upstream/main
```

自动化同步说明在 [`.github/UPSTREAM_CHILD_SYNC.md`](../../.github/UPSTREAM_CHILD_SYNC.md)，状态在 [`.github/child-sync-state.json`](../../.github/child-sync-state.json)。**不要整树 merge `child/main`。**

---

## 下次升级怎么做（先读再合）

1. 只快进或走 `automation/upstream-child-sync` PR，不要在 `main` 上强制覆盖。
2. 打开本文件「必须保留」表，按**文件**核对，不要按提交信息猜「上游是不是已经有了」。
3. `quality_retry.go` 若和上游撞车：本 fork 已有 vis&lt;8 / plaintext-ratio / fake-enc / cipher-drool。`child-sync-state.json` 里 `upstreamEquivalent` 已记官方质量重试等价于子分支 `62ae4347`。**不要再 cherry-pick 一遍子分支质量补丁。**
4. `service.go` 冲突时，三块都必须还在：
   - `CreateChatCompletion` / `CreateMessage` 调用 `applyTUICompactionQualitySkip`，且**不**把 `Operation` 改成 compaction
   - `CreateResponse` 对 TUI 压缩只设 `auditOperation` + `skipQualityHold`
   - 免费额度耗尽后 `releasePinnedOwnership`（日志 `pinned_account_quota_exhausted_failover`）
5. 跑文末回归测试。缺测试或测试被上游改掉签名，先补测试再合。
6. 合入后若审计又出现压缩路径 `503 quality_degraded`，先查 skip 接线，不要先改 `fail_closed`。

---

## 必须保留的行为差

下面是**本 fork 自己的产品逻辑**，不是 sync chore、不是文档本身。

| 主题 | 落地提交 | 关键文件 | 现在的行为 | 合入时怎么认还在 | 禁止 |
| --- | --- | --- | --- | --- | --- |
| Claude Code /compact 跳过质量 hold（Messages） | `d024cd93` | `responses_compaction.go` `service.go` `claude_code_compact_hold_test.go` | 最后一条 user 同时含两句 Claude 压缩标记 → `auditOperation=compaction` 且 `skipQualityHold=true` | `CreateMessage` 里有 `applyTUICompactionQualitySkip(&input)`；分类器双标记仍是 **AND** | 不要改 `Operation`；不要改成「历史里任意 user 命中就算压缩」 |
| 同上，Chat 入口 | `6a23bfc4` | `service.go` | New API 把 Anthropic 压缩转到 `POST /v1/chat/completions`，`CreateChatCompletion` 同样 skip | `CreateChatCompletion` 里有同一 helper；测试 `TestCreateChatCompletionWiresTUICompactionQualitySkip` | 同上。漏接这一行，New API 压缩会再 503 |
| Grok TUI 压缩 skip | 随分类器，早于 Claude 补丁 | `CreateResponse` 分支 `responsesCompactionTUI` | TUI 标记「system-generated compaction prompt」同样 skip hold | `CreateResponse` 仍只改 audit + skip，不改 Responses 路由 | 不要把 TUI 压缩改成 `OperationCompaction`（会改 Provider 语义） |
| 会话 pin 在免费额度耗尽后换号 | `47f9e31` | `service.go` `service_test.go` | `previous_response_id` 钉死的号 A 免费额度 429 / 已冷却 → 释放 in-request pin，从号池选 B。成功路径仍钉原号。账号禁用仍 503 | 日志字段 `pinned_account_quota_exhausted_failover`；测试 `TestGatewayPreviousResponseIDFailoversAfterFreeQuotaExhaustion` | **不要回滚。** 不要对任意 429 清 ownership（hosted 工具会重放） |
| 质量守卫 fail_closed + Build-only | 子分支适配：`308d7a2c` `69e48e6f` `77ffed04` 等 | `quality_retry.go` `config.example.yaml` | 编码轮次缺推理扣留并重试，耗尽 `fail_closed` → 对外 503 `quality_degraded`。Web/Console 流式不走这套 | `shouldHoldQualityStream` 仍认 `skipQualityHold`；`OnExhausted` 仍是 fail_closed | **不要 fail_open**（压缩摘要会污染会话，表现为降智） |
| 假加密 / 短答倾倒扣留 | `77ffed04` `2d591039` `f341aaa2` | `quality_retry.go` `quality_retry_scan.go` | fake-enc、cipher-drool、floor 后 1s 短答倾倒仍 withhold | 与上游 `7f3f3d3c` 同类时走等价，保留 Fork 的 overflow-safe floor 和 provider 隔离 | 不要用子分支整文件覆盖本 fork 的 floor 实现 |
| 多节点账号绑定 | `2ced5658` 及后续 clamp/i18n | 账号 / egress / 前端绑定控件 | Console 绑定节点有上限、节点过滤 | 绑定 API 与 UI 还在 | 不要被上游没有该功能的页面盖掉 |
| FlareSolverr 拦截页拒绝 | `91b3828d` | 相关 provider/clearance | 被拦页面不当成登录成功 | 回归仍拒绝 blocked 页 | — |
| Codex MCP schema 简化 | `1852c605` | CLI adapter | Grok Build 拒掉的 MCP schema 已拍扁 | 与上游后续 schema flatten 并存时保留「能过 Grok Build」 | 不要回退到会被 Grok 拒的深层 oneOf |

压缩分类器细节、New API 路径、审计「200 · 错误」读法见 [2026-09-18 压缩 skip 与 200 错误](./2026-09-18-claude-code-compact-skip-and-200-error.md)。

pin 换号与探测 429 噪音见 [2026-09-12 免费额度 429](./2026-09-12-free-quota-429-pin-failover.md)。

---

## 热冲突文件

合入上游时优先打开这些文件。左边是本 fork 必须留下的符号。

### `backend/internal/application/gateway/service.go`

| 符号 / 位置 | 本 fork 要留下 |
| --- | --- |
| `Input.skipQualityHold` | 仅网关分类器可写 |
| `Input.auditOperation` | 只改审计展示，不改路由 |
| `CreateResponse` TUI 分支 | skip hold，不改 `Operation` |
| `CreateChatCompletion` | `applyTUICompactionQualitySkip(&input)` 在 `createResponseAt` 之前 |
| `CreateMessage` | 同上 |
| `releasePinnedOwnership` / `pinned_account_quota_exhausted_failover` | 仅免费额度耗尽 / 钉死号已冷却 |
| `recordQualityDegraded` 调用 | 扣留流另写 200 审计；不要改成改客户端 HTTP |

上游若重写 `CreateChatCompletion` 函数体，**最容易漏掉 chat skip**，New API 压缩会再 503。

### `backend/internal/application/gateway/responses_compaction.go`

| 符号 | 本 fork 要留下 |
| --- | --- |
| `claudeCodeCompactionPromptMarker` | Claude 压缩第一句 |
| `claudeCodeCompactionAnalysisMarker` | `wrap your analysis in <analysis> tags` |
| `looksLikeCompactionPrompt` | TUI 标记 **或** Claude 两句同时命中 |
| `lastItemLooksLikeCompactionPrompt` | **只看最后一条**，且 `role=user` |
| `applyTUICompactionQualitySkip` | 仅 `responsesCompactionTUI` 时 skip；`compaction_trigger` 不走这个 helper |

### `backend/internal/application/gateway/claude_code_compact_hold_test.go`

本 fork 独有文件。上游没有。合入后文件必须还在，且 `CreateChatCompletion` 源码断言仍通过。

### `backend/internal/application/gateway/quality_retry.go`

`shouldHoldQualityStream` 必须继续：

- `input.skipQualityHold == true` → 不 hold
- 编码轮次缺推理 → hold / withhold
- `fail_closed` 耗尽 → 503，不是把脏摘要 fail_open 出去

### `config.example.yaml`

`qualityGuard.requestRetry.onExhausted: fail_closed`，holdTimeout 30s，maxAttempts 6。线上 `/opt/ganzizi-grok2api-15353/config.yaml` 同此，升级镜像**不要**顺手改成 fail_open。

---

## 合入后回归测试

在 `backend/`：

```bash
go test ./internal/application/gateway/ -count=1 \
  -run "TestApplyTUICompactionQualitySkip|TestClaudeCodeCompact(Messages|Chat)QualityHoldComposition|TestCreateChatCompletionWiresTUICompactionQualitySkip|TestIsResponsesCompactionRequest|TestGatewayPreviousResponseIDFailoversAfterFreeQuotaExhaustion|TestGatewayPreviousResponseIDFailoversWhenPinnedAccountAlreadyExhausted|TestPinnedSelectionAllowsPoolFailover"
```

本机若没有 gcc，不要强跑 `-race`（`-race requires cgo`）。

压缩分类器单测在 `TestIsResponsesCompactionRequest`：历史中的压缩提示不算；单标记不算；assistant/system 不算。

---

## 明确不要做的

- 回滚 `47f9e31`。探测 429、审计「200 · 错误」都不是回滚理由。
- `qualityGuard.requestRetry.onExhausted` 改成 `fail_open`。
- 把 chat/messages 的 `Input.Operation` 改成 `compaction`（ConvertRequest 按 Chat/Messages 分协议）。
- 分类器改成扫描整段历史。
- 整树合并 `child`。品牌、Compose tag、安装文案在 `child-sync-state.json` 的 `excluded` 里。
- 为「看起来像压缩」的普通编码轮次 skip hold。
- 未确认授权前操作 `104.236.196.133`。

---

## 审计里「200 · 错误」不要当成合入失败

前端 `AuditStatus`：HTTP 2xx **且** `errorCode` 非空 → 琥珀标签「200 · 错误」。SSE 先写 200 头，流失败或质量扣留另填 `errorCode`。

| 你看到的 | 多半是 | 要不要为了它改 skip / pin |
| --- | --- | --- |
| 大量 200 + `quality_degraded`，`attempt_count=0` | 扣留流的**影子审计**，同一 `request_id` 另有 503 或干净 200 | 不要。编码轮次缺推理的 fail_closed 仍要 |
| 200 + `client_stream_interrupted` + `operation=compaction` | 压缩已 skip，调用方提前关 SSE（探测读 2KB 也会这样） | 不要把 skip 改回去 |
| 200 + `client_stream_interrupted` + `operation=responses` | 用户取消 / 上游代理掐流 | 不要当成压缩 bug |
| 200 + `upstream_stream_*` | 握手后上游流死了或空转 | 不要改分类器 |

完整对照见 [2026-09-18 文档](./2026-09-18-claude-code-compact-skip-and-200-error.md)。

---

## 远程与线上更新

```text
origin    https://github.com/ganzizi/grok2api.git
upstream  https://github.com/chenyme/grok2api.git
child     https://github.com/lij768423-svg/grok2api.git
```

未点名不要 push `child`。线上源码目录 `/opt/ganzizi-grok2api-15353/source`，`auto-update.sh` 每小时整点快进 `origin/main` 并重建 `grok2api` 镜像。本机 stash `wip: sse stream keepalive before sync to live 3d0c444` 不要随便 pop。
