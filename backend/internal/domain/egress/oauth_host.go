package egress

import "strings"

func IsBuildOAuthHost(host string) bool {
	hostname, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(host)), ":")
	return hostname == "auth.x.ai"
}
