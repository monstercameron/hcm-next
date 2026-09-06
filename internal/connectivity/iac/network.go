// Package iac contains the pure, provider-neutral cell network contract.
// Reachability is explicit and never implies service authority: callers must
// supply an ingress rule, dependency, DNS pin, or egress lease for each path.
package iac

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Version reports the IAC-004 network contract version.
func Version() int { return 1 }

// Explain describes the fail-closed network decision contract.
func Explain() string {
	return "IAC-004 v1: explicit cell ingress, east-west dependency, pinned DNS, and egress admission"
}

// IngressRule is an explicit TLS route into one cell service.
type IngressRule struct {
	Host                string
	PathPrefix          string
	Service             string
	CellID              string
	TLS                 bool
	TrustedProxyCIDRs   []string
	TrustedProxyHeaders map[string]string
}

// ServiceDependency grants one exact east-west service edge.
type ServiceDependency struct {
	FromCell    string
	FromService string
	ToCell      string
	ToService   string
}

// DNSPin binds a name to the exact addresses approved for a route.
type DNSPin struct {
	Name        string
	PinnedIPs   []string
	ObservedIPs []string
}

// EgressRule is one exact outbound destination lease.
type EgressRule struct {
	CellID      string
	TenantID    string
	Destination string
	Port        int
	Purpose     string
}

// Network is a complete cell boundary policy. Empty rule lists mean deny,
// not allow-all.
type Network struct {
	CellID       string
	TenantID     string
	Ingress      []IngressRule
	Dependencies []ServiceDependency
	DNSPins      []DNSPin
	Egress       []EgressRule
}

// Request is a read-only evaluation question at a network boundary.
type Request struct {
	FromCell     string
	FromService  string
	ToCell       string
	ToService    string
	Host         string
	Path         string
	TLS          bool
	ProxyAddress string
	ProxyHeaders map[string]string
	Destination  string
	Port         int
}

// Decision is an explainable allow or deny result.
type Decision struct {
	Allowed bool
	Code    string
	Detail  string
}

// Validate checks the structural network contract and returns every defect.
func Validate(n Network) []string {
	var defects []string
	if strings.TrimSpace(n.CellID) == "" {
		defects = append(defects, "missing cell id")
	}
	if strings.TrimSpace(n.TenantID) == "" {
		defects = append(defects, "missing tenant id")
	}
	for _, rule := range n.Ingress {
		if strings.TrimSpace(rule.Host) == "" || strings.TrimSpace(rule.Service) == "" || strings.TrimSpace(rule.CellID) == "" {
			defects = append(defects, "ingress requires host, service, and cell")
		}
		if !rule.TLS {
			defects = append(defects, "ingress must require TLS")
		}
		for _, cidr := range rule.TrustedProxyCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				defects = append(defects, "ingress has invalid trusted proxy CIDR "+cidr)
			}
		}
	}
	for _, pin := range n.DNSPins {
		if strings.TrimSpace(pin.Name) == "" || len(pin.PinnedIPs) == 0 {
			defects = append(defects, "DNS pin requires a name and at least one pinned address")
		}
		for _, address := range append(append([]string(nil), pin.PinnedIPs...), pin.ObservedIPs...) {
			if _, err := netip.ParseAddr(address); err != nil {
				defects = append(defects, "DNS pin has invalid address "+address)
			}
		}
		if !sameSet(pin.PinnedIPs, pin.ObservedIPs) {
			defects = append(defects, "DNS pin observed addresses differ from pinned addresses")
		}
	}
	for _, rule := range n.Egress {
		if strings.TrimSpace(rule.CellID) == "" || strings.TrimSpace(rule.TenantID) == "" || strings.TrimSpace(rule.Destination) == "" || strings.TrimSpace(rule.Purpose) == "" || rule.Port <= 0 || rule.Port > 65535 {
			defects = append(defects, "egress requires cell, tenant, destination, purpose, and valid port")
		}
		if wildcard(rule.Destination) {
			defects = append(defects, "egress destination cannot be wildcard")
		}
	}
	sort.Strings(defects)
	return defects
}

// Check returns the first structural defect, if any.
func Check(n Network) error {
	if defects := Validate(n); len(defects) != 0 {
		return fmt.Errorf("network: %s", defects[0])
	}
	return nil
}

// Evaluate answers one ingress, east-west, DNS, or egress question. It
// denies malformed requests and all paths absent from the exact policy.
func Evaluate(n Network, req Request) Decision {
	if err := Check(n); err != nil {
		return Decision{Code: "INVALID_POLICY", Detail: err.Error()}
	}
	if req.Destination != "" {
		for _, rule := range n.Egress {
			if rule.CellID == req.FromCell && rule.TenantID == n.TenantID && rule.Destination == req.Destination && rule.Port == req.Port && hasDNSPin(n, req.Destination) {
				return Decision{Allowed: true, Code: "EGRESS_ALLOWED", Detail: "exact destination lease matched"}
			}
		}
		if !hasDNSPin(n, req.Destination) {
			return Decision{Code: "DNS_NOT_PINNED", Detail: "destination has no verified DNS pin"}
		}
		return Decision{Code: "EGRESS_DENIED", Detail: "destination is not explicitly leased"}
	}
	if req.Host != "" {
		for _, rule := range n.Ingress {
			if rule.Host != req.Host || rule.CellID != req.ToCell || rule.Service != req.ToService || !strings.HasPrefix(req.Path, rule.PathPrefix) || rule.TLS != req.TLS {
				continue
			}
			if !trustedProxy(rule, req) {
				return Decision{Code: "UNTRUSTED_PROXY", Detail: "proxy identity or forwarded header is not trusted"}
			}
			if !hasDNSPin(n, req.Host) {
				return Decision{Code: "DNS_NOT_PINNED", Detail: "ingress host has no verified DNS pin"}
			}
			return Decision{Allowed: true, Code: "INGRESS_ALLOWED", Detail: "explicit ingress route matched"}
		}
		return Decision{Code: "INGRESS_DENIED", Detail: "no explicit ingress route matched"}
	}
	for _, dependency := range n.Dependencies {
		if dependency.FromCell == req.FromCell && dependency.FromService == req.FromService && dependency.ToCell == req.ToCell && dependency.ToService == req.ToService {
			return Decision{Allowed: true, Code: "EAST_WEST_ALLOWED", Detail: "exact service dependency matched"}
		}
	}
	return Decision{Code: "EAST_WEST_DENIED", Detail: "cross-cell or east-west route is not explicitly declared"}
}

func trustedProxy(rule IngressRule, req Request) bool {
	if len(rule.TrustedProxyHeaders) == 0 {
		return req.ProxyAddress == ""
	}
	address, err := netip.ParseAddr(req.ProxyAddress)
	if err != nil {
		return false
	}
	trusted := false
	for _, raw := range rule.TrustedProxyCIDRs {
		prefix, parseErr := netip.ParsePrefix(raw)
		if parseErr == nil && prefix.Contains(address) {
			trusted = true
			break
		}
	}
	if !trusted {
		return false
	}
	for name, value := range rule.TrustedProxyHeaders {
		if req.ProxyHeaders[name] != value {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func hasDNSPin(n Network, name string) bool {
	for _, pin := range n.DNSPins {
		if pin.Name == name {
			return true
		}
	}
	return false
}

func wildcard(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "*", "0.0.0.0/0", "::/0", "any":
		return true
	default:
		return false
	}
}
