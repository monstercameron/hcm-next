// Package devprofile owns the deliberately insecure identity material and
// network boundary used by the local development profile. Keeping it small
// lets developer tools share the contract without importing the application
// composition root or any server adapters.
package devprofile

import (
	"net"
	"strings"
)

const (
	Name     = "local-dev"
	HMACKey  = "hcm-next-local-dev-profile-key-only"
	Issuer   = "https://issuer.local.hcm-next.invalid"
	Audience = "hcm-next-api"
	Tenant   = "harborcare-demo"
	Subject  = "local-developer"
	OrgScope = "org:harborcare-demo:people-ops"
	Roles    = "intent_author,comp_admin,promotion_operator"
	Purpose  = "compensation_review"
)

// IsLoopbackAddress reports whether addr is a host:port bound to loopback.
func IsLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && IsLoopbackHost(host)
}

// IsLoopbackHost accepts localhost and literal loopback IP addresses only.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
