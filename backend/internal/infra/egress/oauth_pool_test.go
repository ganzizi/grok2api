package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	application "github.com/chenyme/grok2api/backend/internal/application/egress"
	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type memoryOAuthRepo struct {
	mu      sync.Mutex
	records []domain.BuildOAuthProxy
	listErr error
}

func (m *memoryOAuthRepo) ListBuildOAuthProxies(context.Context) ([]domain.BuildOAuthProxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]domain.BuildOAuthProxy, len(m.records))
	copy(out, m.records)
	return out, nil
}
func (m *memoryOAuthRepo) GetBuildOAuthProxy(context.Context, uint64) (domain.BuildOAuthProxy, error) {
	return domain.BuildOAuthProxy{}, repository.ErrNotFound
}
func (m *memoryOAuthRepo) CreateBuildOAuthProxy(context.Context, domain.BuildOAuthProxy) (domain.BuildOAuthProxy, error) {
	return domain.BuildOAuthProxy{}, repository.ErrNotFound
}
func (m *memoryOAuthRepo) UpdateBuildOAuthProxy(context.Context, domain.BuildOAuthProxy) (domain.BuildOAuthProxy, error) {
	return domain.BuildOAuthProxy{}, repository.ErrNotFound
}
func (m *memoryOAuthRepo) DeleteBuildOAuthProxy(context.Context, uint64) error {
	return repository.ErrNotFound
}

func testPoolCipher(t *testing.T) *security.Cipher {
	t.Helper()
	cipher, err := security.NewCipher("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func mustEncrypt(t *testing.T, cipher *security.Cipher, value string) string {
	t.Helper()
	encrypted, err := cipher.Encrypt(value)
	if err != nil {
		t.Fatal(err)
	}
	return encrypted
}

func TestOAuthPoolEnabled(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{}
	pool := NewOAuthPool(repo, cipher)
	if pool.Enabled() {
		t.Fatal("empty list must be disabled")
	}
	repo.records = []domain.BuildOAuthProxy{{
		ID: 1, Name: "off", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://127.0.0.1:2260"), Enabled: false,
	}}
	pool.expiresAt = time.Time{}
	if pool.Enabled() {
		t.Fatal("disabled rows must not enable the pool")
	}
	repo.records = []domain.BuildOAuthProxy{{
		ID: 1, Name: "on", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://127.0.0.1:2260"), Enabled: true,
	}}
	pool.expiresAt = time.Time{}
	if !pool.Enabled() {
		t.Fatal("enabled row must enable the pool")
	}
}

func TestOAuthPoolStickyAndAccountPlaceholder(t *testing.T) {
	cipher := testPoolCipher(t)
	first := "socks5h://US.{account}:token@127.0.0.1:2260"
	second := "socks5h://HK.{account}:token@127.0.0.1:2260"
	repo := &memoryOAuthRepo{records: []domain.BuildOAuthProxy{
		{ID: 1, Name: "us", EncryptedProxyURL: mustEncrypt(t, cipher, first), Enabled: true},
		{ID: 2, Name: "hk", EncryptedProxyURL: mustEncrypt(t, cipher, second), Enabled: true},
	}}
	var seen []string
	pool := NewOAuthPool(repo, cipher)
	pool.newClient = func(proxyURL string, _ time.Duration) (*http.Client, error) {
		seen = append(seen, proxyURL)
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
		})}, nil
	}
	ctx := WithAccountIdentity(context.Background(), "acct-7")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://auth.x.ai/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != seen[1] {
		t.Fatalf("sticky proxy URLs = %#v", seen)
	}
	if !strings.Contains(seen[0], "acct-7") || strings.Contains(seen[0], application.ProxyAccountPlaceholder) {
		t.Fatalf("placeholder not rendered: %q", seen[0])
	}
}

func TestOAuthPoolSkipsCooledProxy(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{records: []domain.BuildOAuthProxy{
		{ID: 1, Name: "us", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://us:token@127.0.0.1:2260"), Enabled: true},
		{ID: 2, Name: "hk", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://hk:token@127.0.0.1:2260"), Enabled: true},
	}}
	var seen []string
	pool := NewOAuthPool(repo, cipher)
	pool.newClient = func(proxyURL string, _ time.Duration) (*http.Client, error) {
		seen = append(seen, proxyURL)
		if strings.Contains(proxyURL, "us:token") && len(seen) == 1 {
			return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("socks connect: general SOCKS server failure")
			})}, nil
		}
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		})}, nil
	}
	request, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := pool.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if len(seen) < 2 {
		t.Fatalf("expected failover, seen = %#v", seen)
	}
}

func TestOAuthPoolKeepsSnapshotWhenListFails(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{records: []domain.BuildOAuthProxy{{
		ID: 1, Name: "us", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://us:token@127.0.0.1:2260"), Enabled: true,
	}}}
	pool := NewOAuthPool(repo, cipher)
	if !pool.Enabled() {
		t.Fatal("expected configured pool")
	}
	repo.mu.Lock()
	repo.listErr = errors.New("db down")
	repo.mu.Unlock()
	pool.expiresAt = time.Time{}
	if !pool.Enabled() {
		t.Fatal("list failure must keep the last configured snapshot")
	}
	var seen int
	pool.newClient = func(string, time.Duration) (*http.Client, error) {
		seen++
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
		})}, nil
	}
	request, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatalf("stale snapshot round trips = %d", seen)
	}
}

func TestOAuthPoolListErrorWithoutSnapshotFailsClosed(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{listErr: errors.New("db down")}
	pool := NewOAuthPool(repo, cipher)
	if !pool.Enabled() {
		t.Fatal("unread list must not look unconfigured")
	}
	request, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.RoundTrip(request); err == nil {
		t.Fatal("expected round trip to fail closed")
	}
}

func TestOAuthPoolEmptySnapshotStaysDisabledAfterListError(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{}
	pool := NewOAuthPool(repo, cipher)
	if pool.Enabled() {
		t.Fatal("empty list must be disabled")
	}
	repo.mu.Lock()
	repo.listErr = errors.New("db down")
	repo.mu.Unlock()
	pool.expiresAt = time.Time{}
	if pool.Enabled() {
		t.Fatal("previously empty list must stay disabled")
	}
}

func TestOAuthPoolRetriesReplayRequestBody(t *testing.T) {
	cipher := testPoolCipher(t)
	repo := &memoryOAuthRepo{records: []domain.BuildOAuthProxy{
		{ID: 1, Name: "us", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://us:token@127.0.0.1:2260"), Enabled: true},
		{ID: 2, Name: "hk", EncryptedProxyURL: mustEncrypt(t, cipher, "socks5h://hk:token@127.0.0.1:2260"), Enabled: true},
	}}
	var bodies []string
	pool := NewOAuthPool(repo, cipher)
	pool.newClient = func(proxyURL string, _ time.Duration) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			payload, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			bodies = append(bodies, string(payload))
			if strings.Contains(proxyURL, "us:token") {
				return nil, errors.New("socks connect: general SOCKS server failure")
			}
			return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
		})}, nil
	}
	request, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/oauth2/token", io.NopCloser(strings.NewReader("grant_type=refresh_token")))
	if err != nil {
		t.Fatal(err)
	}
	request.GetBody = nil
	request.ContentLength = int64(len("grant_type=refresh_token"))
	response, err := pool.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if len(bodies) != 2 || bodies[0] != "grant_type=refresh_token" || bodies[1] != "grant_type=refresh_token" {
		t.Fatalf("replayed bodies = %#v", bodies)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
