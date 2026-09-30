# Build OAuth 独立代理名单设计

## 目标

让 Grok Build 对 `auth.x.ai` 的 OAuth 换票走一份**独立于出口节点表**的付费 CONNECT 代理名单；推理流量继续走现有免费 `grok_build` 池（例如 ProxyScrape）。

成功标准：

- `POST https://auth.x.ai/oauth2/token`（以及同主机 Device Flow）在名单已配置时只使用该名单。
- `cli-chat-proxy.grok.com` 等推理请求仍只使用 `grok_build` 节点。
- 未配置 OAuth 名单时行为与今天完全一致。
- 已配置但拨号失败时**不得**改走 ProxyScrape / `ScopeBuild`。
- 不改 `egress_nodes` 的 scope CHECK、节点列表、账号绑定、自动分配、质量守护、运营固定回退。

## 背景

Build 的 OAuth 客户端和推理客户端共用同一个 `http.Client`。`SetEgress` 把它包成 `egressTransport`，`RoundTrip` 固定租约 `ScopeBuild`。

公开 HTTP 代理（ProxyScrape `:3129`）对 `CONNECT auth.x.ai:443` 立刻返回 `403`；对 `cli-chat-proxy.grok.com` / `console.x.ai` 仍可通。因此 token 有效时聊天正常，token 一刷新就 SOCKS `0x01`。

换票不是每次请求都发生：

- 上游 `expires_in` 通常约 3600 秒。
- 请求路径：`now + 3m < ExpiresAt` 则直接用现有 access token。
- 后台调度：到期前 5–8 分钟刷新。
- 因此付费 OAuth 池的调用量远低于推理池。

## 非目标

- 不新增 egress scope（否决 `grok_build_oauth`）。
- 不给现有节点加「可用于 OAuth」标记。
- 不改 Web SSO 转换（`sso_build.go` 仍走 `ScopeWeb`）。
- 不改 Console / Web 出口。
- 不按模型名分流。
- 不把 OAuth 代理暴露到账号「绑定代理」对话框。
- 不把 OAuth 失败回落到 ProxyScrape。

## 方案对比

### A. 独立表 `build_oauth_proxies` + RoundTrip 挂钩（采用）

新增一张只服务 `auth.x.ai` 的代理名单。`egressTransport.RoundTrip` 在命中该主机且名单非空时改走这份名单；其它请求仍 `AcquireIfConfigured(ScopeBuild)`。

- 优点：不碰节点 CHECK / 列表 / 分配 / 回退；上游合并时冲突面小；付费 URL 不会进入推理选路。
- 缺点：多一张表和一套 CRUD，冷却只在进程内存。

### B. 新作用域 `grok_build_oauth`（否决）

与 Web Asset 拆分同构，但要升级三个 CHECK、节点 UI、运营回退、审计白名单。对上游 fork 不友好。

### C. 运营配置钉死一个现有 `grok_build` 节点（否决）

该节点仍会被推理抽到，付费流量泄漏。

## 行为

### 主机匹配

仅看请求 URL 的 hostname（忽略端口、大小写）：

```text
auth.x.ai
```

命中且 OAuth 名单至少有一条**启用且有代理 URL** 的记录，则走独立池。未命中或名单未配置，则仍为 `grok_build`。

覆盖：

- `https://auth.x.ai/oauth2/token`
- `https://auth.x.ai/oauth2/device/code`
- Device 轮询同一 token URL

不覆盖：`accounts.x.ai`、`console.x.ai`、`cli-chat-proxy.grok.com`、`grok.com`。

### RoundTrip 挂钩

`SetEgress` 签名不变。额外 `SetOAuthPool`，必须在 `SetEgress` 之后调用（后者会替换 Transport）。

```text
if oauthPool != nil && oauthPool.Enabled() && IsBuildOAuthHost(request.URL.Host):
    return oauthPool.RoundTrip(request)   # fail closed
# 原路径
AcquireIfConfigured(ScopeBuild, affinity)
```

「未配置」= 没有任何启用记录，或启用记录都没有可解密的代理 URL。此时 `Enabled() == false`，换票继续走 `grok_build`。

「已配置但拨号失败」= 在名单内换下一条；全部失败则把错误返回给 OAuth 刷新，**禁止**第二次 `AcquireIfConfigured(ScopeBuild)`。

### 选路

启用记录组成一个池（多条即池模式，不必再加 scope）。

- 亲和性：`AccountFromContext`；空则 `"bootstrap"`（与现有 Build RoundTrip 一致）。
- 代理 URL 允许 `{account}`，且只能出现在用户名，复用 `NormalizeProxyURL` / `renderAccountProxyURL`。
- 粘性：用账号身份在当前候选上取模，同一账号尽量打同一条。
- 非粘性：原子计数 round-robin。
- 失败冷却：进程内存，按记录 ID，约 30 秒；不写 `egress_nodes.CooldownUntil`。
- 若全部在冷却，忽略冷却再试一轮，仍然 fail closed。
- 传输栈：`newBuildClient`（标准 Go HTTP/TLS + SOCKS）。不套浏览器指纹、不转发 Cloudflare Cookie、不覆盖 CLI User-Agent、不套流式空闲超时。

建议填写：

```text
socks5h://US.{account}:TOKEN@resin-host:2260
```

不要用 `Default.{account}`：Default 平台会抽到 ProxyScrape。

### 数据

新表 `build_oauth_proxies`，GORM AutoMigrate，不改现有 CHECK：

| 列 | 说明 |
| --- | --- |
| id | 主键 |
| name | 1–160，唯一 |
| encrypted_proxy_url | AES-GCM，必填 |
| enabled | 默认 true |
| created_at / updated_at | 时间戳 |

列表 API 只返回 `proxyDisplay` / `proxyFingerprint` / `accountBoundProxy`，不返回明文。完整 URL 走单独 reveal，与代理地址库相同。

### 管理端

Build 设置页增加独立区块「Build OAuth 出口」，不进入节点表、不进入运营回退表。

管理员可增删改启用状态。空名单 = 旧行为。

### 兼容

- 升级后若不建记录：换票继续走 `grok_build`。
- 已有 ProxyScrape `grok_build` 节点不用改。
- 质量守护仍打推理路径，不因此改成打 `auth.x.ai`。

## 错误处理

- CONNECT / SOCKS `0x01`：冷却该 OAuth 记录并换下一条；耗尽后原样返回，不污染 `grok_build` 节点健康。
- OAuth 永久错误（`invalid_grant` 等）仍走现有 `CredentialRefreshError`。
- 未配置时不新增错误类型。

## 测试

- `IsBuildOAuthHost`：带端口、大小写、其它 x.ai 主机。
- `useBuildOAuthPool`：未配置 / 已配置 + `auth.x.ai` / 已配置 + 推理主机。
- 已配置且 `auth.x.ai`：不调用 `AcquireIfConfigured`。
- 已配置但 `RoundTrip` 失败：仍不调用 `AcquireIfConfigured`。
- `{account}` 只出现在用户名；粘性同一账号选同一记录。
- Schema：AutoMigrate 后表存在，已有 Build 节点仍在，三个旧 CHECK **不含** `grok_build_oauth`。
- CRUD：名称冲突、空 URL、reveal 不进列表。

## 上线步骤

1. 部署（此时表空，行为不变）。
2. 在 Build 设置里添加能 CONNECT `auth.x.ai` 的 Resin 平台（`US` / `HK`），用户名含 `{account}`。
3. 手动刷新一个即将过期的 Build 账号，确认 Resin 日志里 `auth.x.ai` 不再落到 `003-proxyscrape*`。
4. 再打一条推理，确认 `cli-chat-proxy` 仍走 ProxyScrape。
