package egress

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type memoryBuildOAuthProxyRepository struct {
	mu      sync.Mutex
	nextID  uint64
	records map[uint64]domain.BuildOAuthProxy
}

func (m *memoryBuildOAuthProxyRepository) ListBuildOAuthProxies(context.Context) ([]domain.BuildOAuthProxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]domain.BuildOAuthProxy, 0, len(m.records))
	for _, value := range m.records {
		values = append(values, value)
	}
	return values, nil
}

func (m *memoryBuildOAuthProxyRepository) GetBuildOAuthProxy(_ context.Context, id uint64) (domain.BuildOAuthProxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.records[id]
	if !ok {
		return domain.BuildOAuthProxy{}, repository.ErrNotFound
	}
	return value, nil
}

func (m *memoryBuildOAuthProxyRepository) CreateBuildOAuthProxy(_ context.Context, value domain.BuildOAuthProxy) (domain.BuildOAuthProxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[uint64]domain.BuildOAuthProxy{}
	}
	for _, existing := range m.records {
		if existing.Name == value.Name {
			return domain.BuildOAuthProxy{}, repository.ErrConflict
		}
	}
	m.nextID++
	value.ID = m.nextID
	m.records[value.ID] = value
	return value, nil
}

func (m *memoryBuildOAuthProxyRepository) UpdateBuildOAuthProxy(_ context.Context, value domain.BuildOAuthProxy) (domain.BuildOAuthProxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[value.ID]; !ok {
		return domain.BuildOAuthProxy{}, repository.ErrNotFound
	}
	m.records[value.ID] = value
	return value, nil
}

func (m *memoryBuildOAuthProxyRepository) DeleteBuildOAuthProxy(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.records, id)
	return nil
}

func testBuildOAuthCipher(t *testing.T) *security.Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cipher, err := security.NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestCreateBuildOAuthProxyRejectsInvalidInput(t *testing.T) {
	cipher := testBuildOAuthCipher(t)
	service := &Service{cipher: cipher, oauthProxies: &memoryBuildOAuthProxyRepository{}}
	ctx := context.Background()
	proxyURL := "socks5h://US.{account}:token@127.0.0.1:2260"
	if _, err := service.CreateBuildOAuthProxy(ctx, BuildOAuthProxyInput{Name: "", ProxyURL: &proxyURL}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name error = %v", err)
	}
	empty := ""
	if _, err := service.CreateBuildOAuthProxy(ctx, BuildOAuthProxyInput{Name: "paid-us", ProxyURL: &empty}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty url error = %v", err)
	}
	badPlaceholder := "socks5h://127.0.0.1:2260/{account}"
	if _, err := service.CreateBuildOAuthProxy(ctx, BuildOAuthProxyInput{Name: "paid-us", ProxyURL: &badPlaceholder}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("placeholder path error = %v", err)
	}
}

func TestBuildOAuthProxyCRUDHidesPlaintext(t *testing.T) {
	cipher := testBuildOAuthCipher(t)
	service := &Service{cipher: cipher, oauthProxies: &memoryBuildOAuthProxyRepository{}}
	ctx := context.Background()
	proxyURL := "socks5h://US.{account}:token@127.0.0.1:2260"
	created, err := service.CreateBuildOAuthProxy(ctx, BuildOAuthProxyInput{Name: "paid-us", ProxyURL: &proxyURL})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || !created.Enabled || created.ProxyFingerprint == "" || !created.AccountBoundProxy {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(created.ProxyDisplay, "token") {
		t.Fatalf("list leaked proxy password: %q", created.ProxyDisplay)
	}
	listed, err := service.ListBuildOAuthProxies(ctx)
	if err != nil || len(listed) != 1 || listed[0].ProxyDisplay != created.ProxyDisplay {
		t.Fatalf("list = %+v, err = %v", listed, err)
	}
	revealed, err := service.BuildOAuthProxyURL(ctx, created.ID)
	if err != nil || revealed != proxyURL {
		t.Fatalf("reveal = %q, err = %v", revealed, err)
	}
	disabled := false
	updated, err := service.UpdateBuildOAuthProxy(ctx, created.ID, BuildOAuthProxyInput{Name: "paid-us", Enabled: &disabled})
	if err != nil || updated.Enabled {
		t.Fatalf("update = %+v, err = %v", updated, err)
	}
	if err := service.DeleteBuildOAuthProxy(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BuildOAuthProxyURL(ctx, created.ID); !errors.Is(err, ErrBuildOAuthProxyNotFound) {
		t.Fatalf("deleted reveal error = %v", err)
	}
}

func TestTestBuildOAuthProxyURLRejectsEmpty(t *testing.T) {
	service := &Service{cipher: testBuildOAuthCipher(t), oauthProxies: &memoryBuildOAuthProxyRepository{}}
	if _, err := service.TestBuildOAuthProxyURL(context.Background(), "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty url error = %v", err)
	}
}

func TestTestBuildOAuthProxyRequiresProber(t *testing.T) {
	cipher := testBuildOAuthCipher(t)
	service := &Service{cipher: cipher, oauthProxies: &memoryBuildOAuthProxyRepository{}}
	proxyURL := "socks5h://US.{account}:token@127.0.0.1:2260"
	created, err := service.CreateBuildOAuthProxy(context.Background(), BuildOAuthProxyInput{Name: "paid-us", ProxyURL: &proxyURL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.TestBuildOAuthProxy(context.Background(), created.ID); !errors.Is(err, ErrBuildOAuthProxyUnavailable) {
		t.Fatalf("missing prober error = %v", err)
	}
	listed, err := service.ListBuildOAuthProxies(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ProbeStatus != domain.ProbeStatusUnknown {
		t.Fatalf("probe must not persist without a prober: %+v, err = %v", listed, err)
	}
}

func TestTestBuildOAuthProxyPersistsResult(t *testing.T) {
	cipher := testBuildOAuthCipher(t)
	repo := &memoryBuildOAuthProxyRepository{}
	service := &Service{cipher: cipher, oauthProxies: repo}
	var seen string
	service.SetBuildOAuthProber(func(_ context.Context, proxyURL string) domain.BuildOAuthProbeResult {
		seen = proxyURL
		return domain.BuildOAuthProbeResult{
			Status: domain.ProbeStatusHealthy, TestedAt: time.Unix(1700000000, 0).UTC(),
			LatencyMS: 321, StatusCode: 400, Target: "auth.x.ai:443",
		}
	})
	proxyURL := "socks5h://US.{account}:token@127.0.0.1:2260"
	created, err := service.CreateBuildOAuthProxy(context.Background(), BuildOAuthProxyInput{Name: "paid-us", ProxyURL: &proxyURL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.TestBuildOAuthProxy(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.ProbeStatusHealthy || result.StatusCode != 400 || result.LatencyMS != 321 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(seen, "US.probe") || strings.Contains(seen, "{account}") {
		t.Fatalf("placeholder not rendered: %q", seen)
	}
	listed, err := service.ListBuildOAuthProxies(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ProbeStatus != domain.ProbeStatusHealthy || listed[0].ProbeLatencyMS != 321 {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
}
