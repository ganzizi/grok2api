# Claude Code 压缩 skip 与审计「200 · 错误」

- 日期：2026-09-18
- 实例：`ganzizi-grok2api-15353`
- 落地提交：
  - `d024cd93` `fix: skip quality hold for Claude Code compact on messages`
  - `6a23bfc4` `fix: skip quality hold for Claude Code compact via chat`
- 部署：源码与 `deployed-commit` 均为 `6a23bfc4`，healthz 200（约 2026-09-18T08:56:50Z）
- 升级对照总表：[fork-delta.md](./fork-delta.md)

本文记两件事：压缩路径为什么只 skip hold、以及管理端「200 · 错误」多数不是客户端 HTTP 失败。下次合上游时先对这里的接线，不要靠猜。

---

## 拓扑（压缩怎么打进来）

```text
Claude Code → token.go-oai.com → New API :19393 → grok2api :15353
```

Claude Code `/compact` 与自动压缩走 Anthropic `POST /v1/messages`。New API 对 OpenAI 类型渠道会转成 grok2api `POST /v1/chat/completions`（`CreateChatCompletion`）。直连 Anthropic 渠道才走 `CreateMessage`。Grok TUI 压缩走 `POST /v1/responses`（`CreateResponse`）。

漏接 chat 入口时：压缩体仍被质量守卫当成编码轮次，缺推理 → `fail_closed` → New API 看到 503 `quality_degraded`。这就是 2026-09 压缩风暴的根因，不是 Nginx / Cloudflare / pin `47f9e31`。

---

## 分类器（必须保持窄）

文件：`backend/internal/application/gateway/responses_compaction.go`

只看 **最后一条** `role=user` 的文本：

| 命中 | 种类 | skip hold？ | 改 `Operation`？ |
| --- | --- | --- | --- |
| `input[].type = compaction_trigger` | `responsesCompactionTrigger` | 否（TUI helper 不处理） | `CreateResponse` 会改成 compaction；chat/messages helper **不改** |
| 含 TUI 句 `it is a system-generated compaction prompt, not a real user message` | `responsesCompactionTUI` | 是 | 否，只写 `auditOperation` |
| **同时**含 Claude 两句（AND） | 同上，走 TUI 分支 | 是 | 否 |
| 只命中其中一句、历史中间的压缩提示、assistant/system、最后一条是 `function_call_output` | 否 | 否 | 否 |

Claude 两句：

1. `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.`
2. `wrap your analysis in <analysis> tags`

接线：

```text
CreateResponse:        classify → TUI 则 auditOperation + skipQualityHold
CreateMessage:         Operation=messages 后 applyTUICompactionQualitySkip(&input)
CreateChatCompletion:  Operation=chat     后 applyTUICompactionQualitySkip(&input)
```

`applyTUICompactionQualitySkip` **禁止**写 `input.Operation`。`ConvertRequest` 按 `OperationChat` / `OperationMessages` 选协议；改成 compaction 会走错转换器。

---

## 修改后的影响清单

相对官方 `chenyme/grok2api`、相对本 fork 在 `d024cd93` 之前：

| 路径 | 改前 | 改后 |
| --- | --- | --- |
| Claude compact → `/v1/messages` | 质量 hold，常 503 `quality_degraded` | skip hold，审计 operation=compaction，HTTP 200（即使没有 reasoning tokens） |
| Claude compact → New API → `/v1/chat/completions` | 同上，且 `CreateChatCompletion` 完全没 skip | 与 messages 相同 |
| Grok TUI compact → `/v1/responses` | 已 skip（本补丁前就有） | 不变 |
| 普通编码轮次（最后一条不是压缩 user） | fail_closed | **不变** |
| 最后一条是 `function_call_output` / 普通 user | 503 缺推理 | **不变**（不是压缩） |
| `qualityGuard.requestRetry.onExhausted` | fail_closed | **不变** |
| `47f9e31` pin 换号 | 免费额度耗尽才 unpin | **不变** |
| `ClassifyQualityHold` / fake-enc / cipher-drool | withhold | **不变** |
| 压缩无 thinking 的摘要到达客户端 | 被 503 挡掉 | **会到达**。这是刻意的：压缩本身不需要思考链；fail_open 脏摘要才会降智 |

客户端提前关 SSE（探测读 2KB、用户取消、代理掐流）：HTTP 仍是 200，审计 `error_code=client_stream_interrupted`。这不是产品 503，不要把 skip 改回去。

---

## 合入时不要丢的测试

文件：`backend/internal/application/gateway/claude_code_compact_hold_test.go`（上游没有，合完必须还在）

- `TestApplyTUICompactionQualitySkip`：messages/chat skip 且 Operation 不变；普通编码不 skip；单标记不 skip；`compaction_trigger` 不经 helper skip
- `TestClaudeCodeCompactMessagesQualityHoldComposition` / `...Chat...`：skip 前后与 `shouldHoldQualityStream` 组合
- `TestCreateChatCompletionWiresTUICompactionQualitySkip`：源码断言 `CreateChatCompletion` 调用 helper 且不写 `OperationCompaction`

分类器原有：`TestIsResponsesCompactionRequest`（Claude 双标记、历史命中不算）。

---

## 「200 · 错误」根因清单（先读再改）

共享机制，不是五种独立 HTTP 实现：

1. 流式先 `c.Status(200)` 再 `copyStream`。
2. 流失败后 `errorCode = classifyCopyError(...)`，`Finalize` **不改** HTTP 状态。
3. 前端 `hasError = Boolean(audit.errorCode)`；2xx + errorCode 显示琥珀「200 · 错误」，tooltip 是 errorCode。
4. 筛「2xx · 成功」只看 HTTP 类，不看 errorCode 是否为空。
5. `auditRequestSucceeded` = 2xx **且** errorCode 为空。注释写明：2xx 头之后的流失败不算成功请求。

现场量（`request_audits`，UTC，2026-09-18 约 09:16 查询）：

| 窗口 | 总行 | 200+error | 其中 `quality_degraded` |
| --- | --- | --- | --- |
| 7d | 21938 | 5108 | 4822 |
| 24h | 4363 | 1259 | 1220（约 97%） |
| 部署 `6a23bfc4` 之后 | 128 | 5 | 2（后变为 3），503 = 0 |

### 1. 影子审计 `quality_degraded`（24h 主因）

`recordQualityDegraded` 给**被丢弃的 hold 流**另写一行：新 `event_id`，同一 `request_id`，`status_code=200`，`error_code=quality_degraded`，`attempt_count` 经常是 0。

客户端看到的是兄弟行：

| 兄弟行（24h） | 行数 | 含义 |
| --- | --- | --- |
| sibling 503 `quality_degraded` | 1097 | 编码轮次缺推理，fail_closed 对外 503 |
| sibling 干净 200 | 122 | 后续 thinking 流交付成功 |
| sibling 其他 200+error | 2 | 少见 |

24h：影子行 1221、去重 `request_id` 321；客户端 503 去重 249。所以管理端「200 · 错误」刷屏 **不等于** New API 也在 200 失败，更不是压缩 skip 写坏了状态码。

不要为了清掉这些 200 行去 fail_open，也不要回滚 pin。

### 2. 压缩路径 `client_stream_interrupted`

skip 生效后压缩是 200。调用方若在终态事件前关 SSE，审计仍记 interrupted。

24h compaction：干净 200 约 24 条（`/v1/responses` 23 + `/v1/messages` 1，平均约 74s / 5545 out）；200+interrupted 4 条，其中 3 条是部署探测（约 1.3s、0 token）。不是产品 503。

### 3. 非压缩 `client_stream_interrupted`

用户取消、浏览器关页、New API / 反代掐流。24h 约 32 条（responses 为主）。与压缩分类器无关。

### 4. 上游流在 200 之后死掉

`upstream_stream_error` / `upstream_stream_interrupted` / `upstream_stream_idle_timeout`。24h 各约 1 条。abort trailer 仍在已发出的 200 上补失败事件。

### 5. 近 7 天几乎为 0 的码

`upstream_stream_incomplete`、`upstream_response_empty`、`upstream_output_loop`、`stream_interrupted`、`stream_closed`、`response_too_large`。`quality_probe_stream_interrupted` 约 1。不要按这些码改 compact skip。

---

## 部署后怎么判断 skip 还在

审计 `operation=compaction` 且路径 `/v1/chat/completions` 或 `/v1/messages`：应是 200，不应是 503 `quality_degraded`。

仍允许：

- 普通 `/v1/responses` 编码轮次 503 `quality_degraded`（最后一条不是压缩 user）
- 影子 200 `quality_degraded` 挂在同一 `request_id` 上
- 压缩 200 + `client_stream_interrupted`（调用方提前断开）

sqlite 在宿主机（重建后容器内没有 `sqlite3`）：

```text
/var/lib/docker/volumes/ganzizi-grok2api-15353-data/_data/backend.db
表 request_audits，列 request_path（不是 path），created_at UTC
Authorization 已脱敏，不能拿来重放
```

---

## 明确不要做的（本条笔记范围内）

- 回滚 `47f9e31` 或把 compact skip 从 `CreateChatCompletion` 拿掉
- `fail_open`
- 分类器改成扫描整段历史
- chat/messages 把 `Operation` 改成 compaction
- 为影子 200 `quality_degraded` 改 HTTP 状态或停写审计
- 未点名就改 Nginx gzip、Cloudflare、`STREAMING_TIMEOUT`、bind `127.0.0.1`
