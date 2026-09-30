package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestShouldReportEgressFailure(t *testing.T) {
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineContext, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "no error", ctx: context.Background(), err: nil, want: false},
		{name: "canceled request context", ctx: canceledContext, err: errors.New("connection reset"), want: false},
		{name: "expired request context", ctx: deadlineContext, err: context.DeadlineExceeded, want: false},
		{name: "canceled transport", ctx: context.Background(), err: context.Canceled, want: false},
		{name: "wrapped canceled transport", ctx: context.Background(), err: fmt.Errorf("round trip: %w", context.Canceled), want: false},
		{name: "connection failure", ctx: context.Background(), err: errors.New("connection refused"), want: true},
		{name: "independent transport timeout", ctx: context.Background(), err: context.DeadlineExceeded, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldReportEgressFailure(test.ctx, test.err); got != test.want {
				t.Fatalf("shouldReportEgressFailure() = %v, want %v", got, test.want)
			}
		})
	}
}

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
	if !useBuildOAuthPool(pool, "AUTH.X.AI:443") {
		t.Fatal("expected oauth pool for host with port")
	}
	if useBuildOAuthPool(pool, "cli-chat-proxy.grok.com") {
		t.Fatal("inference must stay on Build")
	}
	if useBuildOAuthPool(pool, "accounts.x.ai") {
		t.Fatal("accounts.x.ai must stay on Build")
	}
	if useBuildOAuthPool(&recordingOAuthPool{enabled: false}, "auth.x.ai") {
		t.Fatal("unconfigured must stay on Build")
	}
	if useBuildOAuthPool(nil, "auth.x.ai") {
		t.Fatal("nil pool must stay on Build")
	}
}

type panicOnEnabledPool struct{}

func (panicOnEnabledPool) Enabled() bool {
	panic("Enabled must not run for non-oauth hosts")
}

func (panicOnEnabledPool) RoundTrip(*http.Request) (*http.Response, error) {
	panic("RoundTrip must not run for non-oauth hosts")
}

func TestUseBuildOAuthPoolSkipsEnabledForInferenceHost(t *testing.T) {
	if useBuildOAuthPool(panicOnEnabledPool{}, "cli-chat-proxy.grok.com") {
		t.Fatal("inference must stay on Build")
	}
	if useBuildOAuthPool(panicOnEnabledPool{}, "accounts.x.ai") {
		t.Fatal("accounts.x.ai must stay on Build")
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
