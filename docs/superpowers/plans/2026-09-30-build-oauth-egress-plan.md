# Build OAuth 独立代理名单 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (Native) or superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Build 对 `auth.x.ai` 的 OAuth 请求走独立表 `build_oauth_proxies`，推理仍走 `grok_build`。

**Architecture:** 新表存加密代理 URL。`egressTransport.RoundTrip` 在主机匹配且名单 `Enabled()` 时改走 `OAuthPool`，失败不回落 `ScopeBuild`。`SetEgress` 签名不变，另加 `SetOAuthPool`。传输复用 `newBuildClient`。

**Tech Stack:** Go、GORM AutoMigrate、现有 Cipher / NormalizeProxyURL、React 管理端、i18next。

**Spec:** `docs/superpowers/specs/2026-09-30-build-oauth-egress-design.md`

## Global Constraints

- 只匹配 hostname `auth.x.ai`（忽略端口与大小写）；其它主机仍走原 `ScopeBuild`。
- 不新增 egress scope，不改 `egress_nodes` CHECK，不改节点列表 / 绑定 / 分配 / 质量守护 / 运营回退。
- 未配置 OAuth 名单时行为与改前相同；已配置但拨号失败不得改走 `grok_build`。
- OAuth 传输使用 `newBuildClient`，禁止 browser client / Cloudflare Cookie / 节点 User-Agent / 流式空闲超时。
- `SetEgress` 签名不变；`SetOAuthPool` 必须在其后调用。
- 不提交代理密码、订阅 URL 或真实账号到测试夹具。

## Review Focus

- Hostname 带端口或大小写变体仍必须命中 OAuth 池。
- `cli-chat-proxy.grok.com` 与 `accounts.x.ai` 即使名单已配置也不得走 OAuth 池。
- OAuth 池 `RoundTrip` 返回错误时不得第二次 Acquire Build。
- 旧库启动后三个现有 CHECK 仍不含 `grok_build_oauth`，已有 Build 节点仍可读。
- 列表 API 不得返回明文代理 URL。

---

## 文件边界

- Create: `backend/internal/domain/egress/oauth_host.go` — `IsBuildOAuthHost`。
- Create: `backend/internal/domain/egress/oauth_host_test.go`。
- Modify: `backend/internal/domain/egress/egress.go` — `BuildOAuthProxy` / `PublicBuildOAuthProxy`。
- Modify: `backend/internal/repository/egress.go` — `BuildOAuthProxyRepository`。
- Modify: `backend/internal/infra/persistence/relational/models.go` — `buildOAuthProxyModel`。
- Modify: `backend/internal/infra/persistence/relational/schema.go` — `schemaModels`。
- Create: `backend/internal/infra/persistence/relational/build_oauth_proxy_repository.go`。
- Create: `backend/internal/infra/persistence/relational/build_oauth_proxy_repository_test.go`。
- Create: `backend/internal/application/egress/oauth_proxy.go` — 管理 CRUD。
- Create: `backend/internal/application/egress/oauth_proxy_test.go`。
- Create: `backend/internal/infra/egress/oauth_pool.go` — 运行时选路。
- Create: `backend/internal/infra/egress/oauth_pool_test.go`。
- Modify: `backend/internal/infra/provider/cli/egress.go` — RoundTrip 挂钩。
- Modify: `backend/internal/infra/provider/cli/egress_test.go`。
- Modify: `backend/internal/infra/provider/cli/adapter.go` — `SetOAuthPool`。
- Modify: `backend/internal/app/application.go` — 接线。
- Modify: `backend/internal/transport/http/egress/handler.go` — 管理路由。
- Create: `frontend/src/features/settings/build-oauth-proxies.tsx`。
- Modify: `frontend/src/features/settings/settings-api.ts`、`settings-page.tsx`、`frontend/src/shared/i18n/index.ts`。

不要修改：`egress-nodes.tsx`、`egress-operations.tsx`、`SupportsScope`、账号绑定、质量守护。

---

### Task 1: Domain — 主机匹配与名单类型

**Files:**

- Create: `backend/internal/domain/egress/oauth_host.go`
- Create: `backend/internal/domain/egress/oauth_host_test.go`
- Modify: `backend/internal/domain/egress/egress.go`

**Interfaces:**

- Consumes: 无。
- Produces: `func IsBuildOAuthHost(host string) bool`；`type BuildOAuthProxy struct`；`type PublicBuildOAuthProxy struct`。

- [ ] **Step 1: Write the failing tests**

Create `oauth_host_test.go`:

```go
package egress

import "testing"

func TestIsBuildOAuthHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "auth.x.ai", want: true},
		{host: "AUTH.X.AI", want: true},
		{host: "auth.x.ai:443", want: true},
		{host: "cli-chat-proxy.grok.com", want: false},
		{host: "accounts.x.ai", want: false},
		{host: "console.x.ai", want: false},
		{host: "", want: false},
	}
	for _, test := range tests {
		if got := IsBuildOAuthHost(test.host); got != test.want {
			t.Fatalf("IsBuildOAuthHost(%q) = %v, want %v", test.host, got, test.want)
		}
	}
}
```

- [ ] **Step 2: Run tests and confirm they fail to compile**

Run from `backend`: `go test ./internal/domain/egress -count=1 -run TestIsBuildOAuthHost`

Expected: undefined `IsBuildOAuthHost`.

- [ ] **Step 3: Implement the domain helpers**

`oauth_host.go`:

```go
package egress

import "strings"

func IsBuildOAuthHost(host string) bool {
	hostname, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(host)), ":")
	return hostname == "auth.x.ai"
}
```

IPv6 字面量不需要支持：OAuth 目标不是 IP。

In `egress.go` after `PublicProxyProfile` add:

```go
type BuildOAuthProxy struct {
	ID                uint64
	Name              string
	EncryptedProxyURL string
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PublicBuildOAuthProxy struct {
	ID                uint64
	Name              string
	Enabled           bool
	ProxyDisplay      string
	ProxyFingerprint  string
	AccountBoundProxy bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
```

- [ ] **Step 4: Re-run domain tests**

`go test ./internal/domain/egress -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/egress/oauth_host.go backend/internal/domain/egress/oauth_host_test.go backend/internal/domain/egress/egress.go
git commit -m "feat: match auth.x.ai for independent Build OAuth proxies"
```

---

### Task 2: 表与仓储

**Files:**

- Modify: `backend/internal/repository/egress.go`
- Modify: `backend/internal/infra/persistence/relational/models.go`
- Modify: `backend/internal/infra/persistence/relational/schema.go`
- Create: `backend/internal/infra/persistence/relational/build_oauth_proxy_repository.go`
- Create: `backend/internal/infra/persistence/relational/build_oauth_proxy_repository_test.go`

**Interfaces:**

- Consumes: `egress.BuildOAuthProxy`。
- Produces: `repository.BuildOAuthProxyRepository`；表 `build_oauth_proxies`。

- [ ] **Step 1: Write the failing repository test**

```go
func TestBuildOAuthProxyCRUD(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "oauth-proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewEgressRepository(database)
	created, err := repository.CreateBuildOAuthProxy(ctx, egress.BuildOAuthProxy{
		Name: "paid-us", EncryptedProxyURL: "encrypted-url", Enabled: true,
	})
	if err != nil || created.ID == 0 || created.Name != "paid-us" || !created.Enabled {
		t.Fatalf("create = %+v, err = %v", created, err)
	}
	listed, err := repository.ListBuildOAuthProxies(ctx)
	if err != nil || len(listed) != 1 || listed[0].EncryptedProxyURL != "encrypted-url" {
		t.Fatalf("list = %+v, err = %v", listed, err)
	}
	created.Enabled = false
	updated, err := repository.UpdateBuildOAuthProxy(ctx, created)
	if err != nil || updated.Enabled {
		t.Fatalf("update = %+v, err = %v", updated, err)
	}
	if err := repository.DeleteBuildOAuthProxy(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = repository.ListBuildOAuthProxies(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("after delete list = %+v, err = %v", listed, err)
	}
}
```

同时断言 `InitializeSchema` 后 `egress_nodes` 的 SQL **不含** `grok_build_oauth`。

- [ ] **Step 2: Run the test**

`go test ./internal/infra/persistence/relational -count=1 -run TestBuildOAuthProxyCRUD`

Expected: FAIL undefined `CreateBuildOAuthProxy`。

- [ ] **Step 3: Implement model, schema, repository**

`models.go`:

```go
type buildOAuthProxyModel struct {
	ID                uint64    `gorm:"primaryKey;autoIncrement"`
	Name              string    `gorm:"size:160;not null;uniqueIndex;check:chk_build_oauth_proxies_name,length(trim(name)) BETWEEN 1 AND 160"`
	EncryptedProxyURL string    `gorm:"type:text;not null;check:chk_build_oauth_proxies_url,length(encrypted_proxy_url) BETWEEN 1 AND 65536"`
	Enabled           bool      `gorm:"not null;default:true"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

func (buildOAuthProxyModel) TableName() string { return "build_oauth_proxies" }
```

把 `&buildOAuthProxyModel{}` 追加进 `schemaModels`（放在 `egressOperationsConfigModel` 之后）。

`repository/egress.go`:

```go
type BuildOAuthProxyRepository interface {
	ListBuildOAuthProxies(context.Context) ([]egress.BuildOAuthProxy, error)
	GetBuildOAuthProxy(context.Context, uint64) (egress.BuildOAuthProxy, error)
	CreateBuildOAuthProxy(context.Context, egress.BuildOAuthProxy) (egress.BuildOAuthProxy, error)
	UpdateBuildOAuthProxy(context.Context, egress.BuildOAuthProxy) (egress.BuildOAuthProxy, error)
	DeleteBuildOAuthProxy(context.Context, uint64) error
}
```

仓储方法做在现有 `*EgressRepository` 上，镜像 `egress_proxy_profile_repository.go` 的 Create/Get/Update/Delete + `mapError`。List 按 `LOWER(name), id` 排序。

- [ ] **Step 4: Re-run**

`go test ./internal/infra/persistence/relational -count=1 -run TestBuildOAuthProxy`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/repository/egress.go backend/internal/infra/persistence/relational/models.go backend/internal/infra/persistence/relational/schema.go backend/internal/infra/persistence/relational/build_oauth_proxy_repository.go backend/internal/infra/persistence/relational/build_oauth_proxy_repository_test.go
git commit -m "feat: persist independent Build OAuth proxy list"
```

---

### Task 3: 管理 CRUD

**Files:**

- Create: `backend/internal/application/egress/oauth_proxy.go`
- Create: `backend/internal/application/egress/oauth_proxy_test.go`
- Modify: `backend/internal/application/egress/service.go` — `NewService` 类型断言新仓储；新增 `ErrBuildOAuthProxyNotFound`。

**Interfaces:**

- Consumes: `BuildOAuthProxyRepository`、`NormalizeProxyURL`、`Cipher`。
- Produces: `ListBuildOAuthProxies` / `CreateBuildOAuthProxy` / `UpdateBuildOAuthProxy` / `DeleteBuildOAuthProxy` / `BuildOAuthProxyURL`。

- [ ] **Step 1: Write failing service tests**

用内存 fake 仓储 + `security.NewCipher`。覆盖：空名称拒绝、空 URL 拒绝、`{account}` 不在用户名拒绝、列表不含明文、reveal 返回规范化 URL、更新时可省略 URL。

- [ ] **Step 2: Run tests**

`go test ./internal/application/egress -count=1 -run BuildOAuth`

Expected: FAIL 缺方法。

- [ ] **Step 3: Implement**

`NewService`：

```go
if oauth, ok := storage.(repository.BuildOAuthProxyRepository); ok {
    service.oauthProxies = oauth
}
```

输入结构：

```go
type BuildOAuthProxyInput struct {
	Name     string
	ProxyURL *string
	Enabled  *bool
}
```

Create 必须有 ProxyURL；Update 的 ProxyURL 为 nil 表示不改。加密前走 `NormalizeProxyURL`。Public 映射复用 `proxyMetadata`。

- [ ] **Step 4: Re-run**

`go test ./internal/application/egress -count=1 -run BuildOAuth`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/application/egress/oauth_proxy.go backend/internal/application/egress/oauth_proxy_test.go backend/internal/application/egress/service.go
git commit -m "feat: administer Build OAuth proxy list"
```

---

### Task 4: 运行时 OAuth 池

**Files:**

- Create: `backend/internal/infra/egress/oauth_pool.go`
- Create: `backend/internal/infra/egress/oauth_pool_test.go`

**Interfaces:**

- Consumes: `BuildOAuthProxyRepository`、`Cipher`、`newBuildClient`、`renderAccountProxyURL`、`AccountFromContext`。
- Produces: `func NewOAuthPool(...) *OAuthPool`；`func (p *OAuthPool) Enabled() bool`；`func (p *OAuthPool) RoundTrip(*http.Request) (*http.Response, error)`。

- [ ] **Step 1: Write failing tests**

内存仓储两条启用代理：

- `Enabled()` 在空表 / 全停用为 false，有启用记录为 true。
- 同一 `WithAccountIdentity` 两次 `pick` 得到同一 ID。
- 第一条拨号失败后冷却，第二次选另一条。
- `{account}` 渲染进用户名，测试夹具用 `socks5h://US.{account}:token@127.0.0.1:2260`，不要用真实 token。
- `newClient` 可注入：记录收到的 proxyURL，返回 stub `RoundTripper`。

- [ ] **Step 2: Run tests**

`go test ./internal/infra/egress -count=1 -run OAuthPool`

Expected: FAIL 缺 `OAuthPool`。

- [ ] **Step 3: Implement**

```go
const oauthProxyCooldown = 30 * time.Second
const oauthSnapshotTTL = time.Second

type OAuthPool struct {
	repository repository.BuildOAuthProxyRepository
	cipher     *security.Cipher
	timeout    time.Duration
	newClient  func(proxyURL string, timeout time.Duration) (*http.Client, error)
	mu         sync.Mutex
	snapshot   []domain.BuildOAuthProxy
	expiresAt  time.Time
	cooldown   map[uint64]time.Time
	rr         atomic.Uint64
}

func (p *OAuthPool) Enabled() bool
func (p *OAuthPool) RoundTrip(request *http.Request) (*http.Response, error)
```

`Enabled` 看快照里是否有 `Enabled && EncryptedProxyURL != ""`。`RoundTrip`：从 context 取 affinity（空则 `bootstrap`），在未冷却候选上粘性或 RR，解密、渲染 `{account}`、`newBuildClient`、`client.Do`。传输错误则冷却并试下一条；全部失败返回最后错误。HTTP 响应（含 4xx）不算出口失败。快照 TTL 1 秒，与节点快照一致。

默认 `newClient = newBuildClient`。默认 timeout 30s。

- [ ] **Step 4: Re-run**

`go test ./internal/infra/egress -count=1 -run OAuthPool`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/infra/egress/oauth_pool.go backend/internal/infra/egress/oauth_pool_test.go
git commit -m "feat: route Build OAuth through independent proxy pool"
```

---

### Task 5: RoundTrip 挂钩

**Files:**

- Modify: `backend/internal/infra/provider/cli/egress.go`
- Modify: `backend/internal/infra/provider/cli/egress_test.go`
- Modify: `backend/internal/infra/provider/cli/adapter.go`
- Modify: `backend/internal/app/application.go`

**Interfaces:**

- Consumes: `IsBuildOAuthHost`、`OAuthPool.Enabled` / `RoundTrip`。
- Produces: `useBuildOAuthPool`；`Adapter.SetOAuthPool`。

- [ ] **Step 1: Write failing tests**

```go
type recordingOAuthPool struct {
	enabled bool
	calls   int
	err     error
}

func (p *recordingOAuthPool) Enabled() bool { return p.enabled }
func (p *recordingOAuthPool) RoundTrip(*http.Request) (*http.Response, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
}

func TestUseBuildOAuthPool(t *testing.T) {
	pool := &recordingOAuthPool{enabled: true}
	if !useBuildOAuthPool(pool, "auth.x.ai") {
		t.Fatal("expected oauth pool")
	}
	if useBuildOAuthPool(pool, "cli-chat-proxy.grok.com") {
		t.Fatal("inference must stay on Build")
	}
	if useBuildOAuthPool(&recordingOAuthPool{enabled: false}, "auth.x.ai") {
		t.Fatal("unconfigured must stay on Build")
	}
	if useBuildOAuthPool(nil, "auth.x.ai") {
		t.Fatal("nil pool must stay on Build")
	}
}

func TestEgressTransportUsesOAuthPoolForAuthHost(t *testing.T) {
	pool := &recordingOAuthPool{enabled: true, err: errors.New("socks connect: general SOCKS server failure")}
	transport := &egressTransport{oauthPool: pool, fallback: http.DefaultTransport}
	request, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if err == nil || pool.calls != 1 {
		t.Fatalf("calls = %d, err = %v", pool.calls, err)
	}
}
```

已配置失败时 `manager` 保持 nil：若代码回落到 `AcquireIfConfigured` 会 panic，测试即失败。

推理 URL + 已配置池只测 `useBuildOAuthPool`，不测完整 RoundTrip。

- [ ] **Step 2: Run tests**

`go test ./internal/infra/provider/cli -count=1 -run "OAuthPool|UseBuildOAuth"`

Expected: FAIL 缺 `useBuildOAuthPool`。

- [ ] **Step 3: Implement**

In `egress.go`:

```go
type oauthRoundTripper interface {
	Enabled() bool
	RoundTrip(*http.Request) (*http.Response, error)
}

type egressTransport struct {
	manager   *infraegress.Manager
	oauthPool oauthRoundTripper
	fallback  http.RoundTripper
}

func useBuildOAuthPool(pool oauthRoundTripper, host string) bool {
	return pool != nil && pool.Enabled() && domainegress.IsBuildOAuthHost(host)
}

func (t *egressTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if useBuildOAuthPool(t.oauthPool, request.URL.Host) {
		return t.oauthPool.RoundTrip(request)
	}
	// existing body unchanged
}
```

`adapter.go`：

```go
func (a *Adapter) SetOAuthPool(pool *infraegress.OAuthPool) {
	if pool == nil {
		return
	}
	if transport, ok := a.http.Transport.(*egressTransport); ok {
		transport.oauthPool = pool
	}
}
```

`application.go` 在 `cliAdapter.SetEgress(egressManager)` 之后：

```go
oauthPool := infraegress.NewOAuthPool(egressRepo, cipher)
cliAdapter.SetOAuthPool(oauthPool)
```

- [ ] **Step 4: Re-run cli tests**

`go test ./internal/infra/provider/cli -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/infra/provider/cli/egress.go backend/internal/infra/provider/cli/egress_test.go backend/internal/infra/provider/cli/adapter.go backend/internal/app/application.go
git commit -m "feat: hook auth.x.ai Build traffic to OAuth proxy pool"
```

---

### Task 6: 管理 API

**Files:**

- Modify: `backend/internal/transport/http/egress/handler.go`

**Interfaces:**

- Consumes: application CRUD。
- Produces: `/egress-build-oauth-proxies` REST。

- [ ] **Step 1: Register routes**

```go
router.GET("/egress-build-oauth-proxies", h.listBuildOAuthProxies)
router.POST("/egress-build-oauth-proxies", h.createBuildOAuthProxy)
router.PUT("/egress-build-oauth-proxies/:id", h.updateBuildOAuthProxy)
router.DELETE("/egress-build-oauth-proxies/:id", h.deleteBuildOAuthProxy)
router.POST("/egress-build-oauth-proxies/:id/proxy-url/reveal", h.buildOAuthProxyURL)
```

不要挂到 `RegisterQualityGuard`。

- [ ] **Step 2: Handlers**

镜像 `proxyProfileRequest`：`name`、`proxyURL`、`enabled`。响应 `id,string`、`name`、`enabled`、`proxyDisplay`、`proxyFingerprint`、`accountBoundProxy`、时间戳。列表返回 `{items}`。

`writeError` 增加 `ErrBuildOAuthProxyNotFound` → 404。

- [ ] **Step 3: Compile**

`go test ./internal/transport/http/egress -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/transport/http/egress/handler.go
git commit -m "feat: expose Build OAuth proxy admin API"
```

---

### Task 7: 管理端 UI

**Files:**

- Create: `frontend/src/features/settings/build-oauth-proxies.tsx`
- Modify: `frontend/src/features/settings/settings-api.ts`
- Modify: `frontend/src/features/settings/settings-page.tsx`
- Modify: `frontend/src/shared/i18n/index.ts`

**Interfaces:**

- Consumes: `/api/admin/v1/egress-build-oauth-proxies`。
- Produces: Build 设置页独立区块。

- [ ] **Step 1: API client**

类型与 `EgressProxyProfileDTO` 类似，另加 `enabled`、`accountBoundProxy`。函数：`listBuildOAuthProxies`、`create`、`update`、`delete`、`reveal`。

- [ ] **Step 2: Panel**

仿 `egress-proxy-profiles.tsx` 的表格 + 新建/编辑对话框，内嵌在页面。列：名称、代理展示、启用开关、`{account}` 标记、操作。

在 `settings-page.tsx` 的 Build `SettingsPane` 里、`SettingsSection` 之后插入 `<BuildOAuthProxies />`。不要改 `EgressNodes`。

- [ ] **Step 3: i18n**

zh-CN / en 增加 `buildOAuthProxies`：

- title: `Build OAuth 出口` / `Build OAuth egress`
- description: `仅用于 auth.x.ai 换票。未配置时换票仍走 Build 出口；已配置失败不会回落到免费池。` / `Used only for auth.x.ai token refresh. If unset, refresh keeps using the Build pool. A configured failure does not fall back to the free pool.`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/features/settings/build-oauth-proxies.tsx frontend/src/features/settings/settings-api.ts frontend/src/features/settings/settings-page.tsx frontend/src/shared/i18n/index.ts
git commit -m "feat: add Build OAuth egress panel"
```

---

### Task 8: 回归

- [ ] **Step 1: Backend tests**

```bash
cd backend
go test ./internal/domain/egress ./internal/infra/egress ./internal/infra/provider/cli ./internal/application/egress ./internal/infra/persistence/relational ./internal/transport/http/egress -count=1
```

Expected: PASS.

- [ ] **Step 2: Host helper**

`go test ./internal/domain/egress -count=1 -run OAuth`

Expected: PASS.

- [ ] **Step 3: Deploy note**

部署后表为空则行为不变。在 Build 设置添加 `socks5h://US.{account}:TOKEN@resin:2260`。手动刷新一个 Build 账号，确认 Resin 日志里 `auth.x.ai` 不再落到 `003-proxyscrape*`；再打一条推理，确认 `cli-chat-proxy` 仍走免费池。
