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
