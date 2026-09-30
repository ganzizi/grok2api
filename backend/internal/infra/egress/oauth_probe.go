package egress

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
)

const (
	oauthProbeTarget  = "https://auth.x.ai/oauth2/token"
	oauthProbeAccount = "probe"
)

var (
	newOAuthProbeClient = newBuildClient
	credentialInURL     = regexp.MustCompile(`://[^/@:]+:[^/@]+@`)
)

func ProbeAuthXAI(ctx context.Context, proxyURL string) domain.BuildOAuthProbeResult {
	started := time.Now().UTC()
	result := domain.BuildOAuthProbeResult{
		Status:   domain.ProbeStatusUnhealthy,
		TestedAt: started,
		Target:   "auth.x.ai:443",
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, oauthDialTimeout)
	defer cancel()

	rendered, err := renderAccountProxyURL(strings.TrimSpace(proxyURL), oauthProbeAccount)
	if err != nil {
		result.Error = sanitizeOAuthProbeError(err, proxyURL)
		result.LatencyMS = elapsedMS(started)
		return result
	}
	client, err := newOAuthProbeClient(rendered, oauthDialTimeout)
	if err != nil {
		result.Error = sanitizeOAuthProbeError(err, rendered)
		result.LatencyMS = elapsedMS(started)
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthProbeTarget, nil)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMS = elapsedMS(started)
		return result
	}
	response, err := client.Do(request)
	result.LatencyMS = elapsedMS(started)
	if err != nil {
		result.Error = sanitizeOAuthProbeError(err, rendered)
		return result
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	result.Status = domain.ProbeStatusHealthy
	result.StatusCode = response.StatusCode
	return result
}

func elapsedMS(started time.Time) int {
	ms := int(time.Since(started).Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
}

func sanitizeOAuthProbeError(err error, proxyURL string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if parsed, parseErr := url.Parse(proxyURL); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "***")
		}
		if user := parsed.User.Username(); user != "" {
			message = strings.ReplaceAll(message, user, "***")
		}
	}
	return credentialInURL.ReplaceAllString(message, "://***:***@")
}
