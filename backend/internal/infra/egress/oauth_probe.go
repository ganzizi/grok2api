package egress

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
)

const (
	oauthProbeTarget    = "https://auth.x.ai/oauth2/token"
	oauthProbeAccount   = "probe"
	oauthProbeIPTimeout = 8 * time.Second
)

var (
	newOAuthProbeClient = newBuildClient
	credentialInURL     = regexp.MustCompile(`://[^/@:]+:[^/@]+@`)
	oauthProbeIPTargets = []string{cloudflareIPv4ProbeEndpoint, egressIPv4ProbeEndpoint}
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
	if err != nil {
		result.Error = sanitizeOAuthProbeError(err, rendered)
		result.LatencyMS = elapsedMS(started)
		return result
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	result.Status = domain.ProbeStatusHealthy
	result.StatusCode = response.StatusCode
	result.ExitIP = lookupOAuthProbeExitIP(ctx, client)
	result.LatencyMS = elapsedMS(started)
	return result
}

func lookupOAuthProbeExitIP(ctx context.Context, client *http.Client) string {
	if client == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, oauthProbeIPTimeout)
	defer cancel()
	for _, target := range oauthProbeIPTargets {
		if ip := fetchOAuthProbeIP(ctx, client, target); ip != "" {
			return ip
		}
	}
	return ""
}

func fetchOAuthProbeIP(ctx context.Context, client *http.Client, target string) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("User-Agent", DefaultUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ""
	}
	raw, err := decodeProbeIP(body)
	if err != nil {
		return ""
	}
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || !address.IsValid() || address.IsUnspecified() {
		return ""
	}
	return address.String()
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
