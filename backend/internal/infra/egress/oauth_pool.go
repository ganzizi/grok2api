package egress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	application "github.com/chenyme/grok2api/backend/internal/application/egress"
	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

const (
	oauthProxyCooldown = 30 * time.Second
	oauthSnapshotTTL   = time.Second
	oauthDialTimeout   = 30 * time.Second
)

type oauthCandidate struct {
	ID       uint64
	ProxyURL string
}

type OAuthPool struct {
	repository repository.BuildOAuthProxyRepository
	cipher     *security.Cipher
	timeout    time.Duration
	newClient  func(proxyURL string, timeout time.Duration) (*http.Client, error)
	mu         sync.Mutex
	snapshot   []domain.BuildOAuthProxy
	expiresAt  time.Time
	loaded     bool
	cooldown   map[uint64]time.Time
	rr         atomic.Uint64
}

func NewOAuthPool(storage repository.BuildOAuthProxyRepository, cipher *security.Cipher) *OAuthPool {
	return &OAuthPool{
		repository: storage,
		cipher:     cipher,
		timeout:    oauthDialTimeout,
		newClient:  newBuildClient,
		cooldown:   map[uint64]time.Time{},
	}
}

func (p *OAuthPool) Enabled() bool {
	if p == nil || p.repository == nil {
		return false
	}
	now := time.Now().UTC()
	values, err := p.load(context.Background(), now)
	if err != nil {
		return true
	}
	for _, value := range values {
		if value.Enabled && value.EncryptedProxyURL != "" {
			return true
		}
	}
	return false
}

func (p *OAuthPool) RoundTrip(request *http.Request) (*http.Response, error) {
	if p == nil {
		return nil, errors.New("Build OAuth 代理池未配置")
	}
	ctx := context.Background()
	if request != nil && request.Context() != nil {
		ctx = request.Context()
	}
	request, err := rewindableOAuthRequest(request)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	candidates, err := p.candidates(ctx, now)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errors.New("Build OAuth 代理池未配置")
	}
	affinity := AccountFromContext(ctx)
	if affinity == "" {
		affinity = "bootstrap"
	}
	ready := p.ready(candidates, now)
	if len(ready) == 0 {
		ready = candidates
	}
	start := oauthStartIndex(affinity, len(ready), &p.rr)
	var lastErr error
	for offset := 0; offset < len(ready); offset++ {
		candidate := ready[(start+offset)%len(ready)]
		client, clientErr := p.newClient(candidate.ProxyURL, p.timeout)
		if clientErr != nil {
			p.markCooldown(candidate.ID, now)
			lastErr = clientErr
			continue
		}
		attempt, attemptErr := cloneOAuthRequest(request)
		if attemptErr != nil {
			p.markCooldown(candidate.ID, now)
			lastErr = attemptErr
			continue
		}
		response, tripErr := client.Do(attempt)
		if tripErr != nil {
			p.markCooldown(candidate.ID, now)
			lastErr = tripErr
			continue
		}
		return response, nil
	}
	if lastErr == nil {
		lastErr = errors.New("Build OAuth 代理全部不可用")
	}
	return nil, lastErr
}

func (p *OAuthPool) candidates(ctx context.Context, now time.Time) ([]oauthCandidate, error) {
	values, err := p.load(ctx, now)
	if err != nil {
		return nil, err
	}
	affinity := AccountFromContext(ctx)
	if affinity == "" {
		affinity = "bootstrap"
	}
	out := make([]oauthCandidate, 0, len(values))
	for _, value := range values {
		if !value.Enabled || value.EncryptedProxyURL == "" || p.cipher == nil {
			continue
		}
		proxyURL, decryptErr := p.cipher.Decrypt(value.EncryptedProxyURL)
		if decryptErr != nil {
			continue
		}
		proxyURL, decryptErr = application.NormalizeProxyURL(proxyURL)
		if decryptErr != nil || proxyURL == "" {
			continue
		}
		proxyURL, decryptErr = renderAccountProxyURL(proxyURL, affinity)
		if decryptErr != nil {
			continue
		}
		out = append(out, oauthCandidate{ID: value.ID, ProxyURL: proxyURL})
	}
	return out, nil
}

func (p *OAuthPool) ready(candidates []oauthCandidate, now time.Time) []oauthCandidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]oauthCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		until, cooling := p.cooldown[candidate.ID]
		if cooling && now.Before(until) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (p *OAuthPool) markCooldown(id uint64, now time.Time) {
	p.mu.Lock()
	p.cooldown[id] = now.Add(oauthProxyCooldown)
	p.mu.Unlock()
}

func (p *OAuthPool) load(ctx context.Context, now time.Time) ([]domain.BuildOAuthProxy, error) {
	p.mu.Lock()
	if now.Before(p.expiresAt) {
		values := p.snapshot
		p.mu.Unlock()
		return values, nil
	}
	stale := p.snapshot
	loaded := p.loaded
	p.mu.Unlock()
	if p.repository == nil {
		return nil, errors.New("Build OAuth 代理池未配置")
	}
	values, err := p.repository.ListBuildOAuthProxies(ctx)
	if err != nil {
		if loaded {
			return stale, nil
		}
		return nil, err
	}
	p.mu.Lock()
	p.snapshot = values
	p.loaded = true
	p.expiresAt = now.Add(oauthSnapshotTTL)
	p.mu.Unlock()
	return values, nil
}

func rewindableOAuthRequest(request *http.Request) (*http.Request, error) {
	if request == nil {
		return nil, errors.New("Build OAuth 请求为空")
	}
	if request.GetBody != nil || request.Body == nil || request.Body == http.NoBody {
		return request, nil
	}
	payload, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, err
	}
	clone := request.Clone(request.Context())
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	clone.Body = io.NopCloser(bytes.NewReader(payload))
	clone.ContentLength = int64(len(payload))
	return clone, nil
}

func cloneOAuthRequest(request *http.Request) (*http.Request, error) {
	if request == nil {
		return nil, errors.New("Build OAuth 请求为空")
	}
	clone := request.Clone(request.Context())
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		clone.Body = body
		clone.GetBody = request.GetBody
		return clone, nil
	}
	if request.Body == nil || request.Body == http.NoBody {
		clone.Body = http.NoBody
		return clone, nil
	}
	return clone, nil
}

func oauthStartIndex(affinity string, n int, rr *atomic.Uint64) int {
	if n <= 0 {
		return 0
	}
	if affinity != "" && affinity != "bootstrap" {
		hash := fnv.New64a()
		_, _ = fmt.Fprintf(hash, "%s", affinity)
		return int(hash.Sum64() % uint64(n))
	}
	return int((rr.Add(1) - 1) % uint64(n))
}
