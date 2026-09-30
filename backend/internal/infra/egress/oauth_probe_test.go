package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
)

func TestProbeAuthXAITreatsHTTPResponseAsHealthy(t *testing.T) {
	var host string
	previous := newOAuthProbeClient
	newOAuthProbeClient = func(string, time.Duration) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "auth.x.ai" {
				host = request.URL.Host
			}
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("missing grant")), Header: make(http.Header)}, nil
		})}, nil
	}
	t.Cleanup(func() { newOAuthProbeClient = previous })

	result := ProbeAuthXAI(context.Background(), "socks5h://US.{account}:secret@127.0.0.1:2260")
	if result.Status != domain.ProbeStatusHealthy || result.StatusCode != 400 || host != "auth.x.ai" {
		t.Fatalf("result = %+v host = %q", result, host)
	}
}

func TestProbeAuthXAIReadsExitIP(t *testing.T) {
	previous := newOAuthProbeClient
	newOAuthProbeClient = func(string, time.Duration) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "auth.x.ai" {
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("missing grant")), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ip=203.0.113.10\n")), Header: make(http.Header)}, nil
		})}, nil
	}
	t.Cleanup(func() { newOAuthProbeClient = previous })

	result := ProbeAuthXAI(context.Background(), "socks5h://US.{account}:secret@127.0.0.1:2260")
	if result.Status != domain.ProbeStatusHealthy || result.ExitIP != "203.0.113.10" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeAuthXAITransportErrorIsUnhealthyAndRedactsSecret(t *testing.T) {
	previous := newOAuthProbeClient
	newOAuthProbeClient = func(string, time.Duration) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("socks connect: socks5h://US.probe:secret@127.0.0.1:2260 general SOCKS server failure")
		})}, nil
	}
	t.Cleanup(func() { newOAuthProbeClient = previous })

	result := ProbeAuthXAI(context.Background(), "socks5h://US.{account}:secret@127.0.0.1:2260")
	if result.Status != domain.ProbeStatusUnhealthy {
		t.Fatalf("status = %s", result.Status)
	}
	if strings.Contains(result.Error, "secret") || strings.Contains(result.Error, "US.probe") {
		t.Fatalf("error leaked proxy secret: %q", result.Error)
	}
}
