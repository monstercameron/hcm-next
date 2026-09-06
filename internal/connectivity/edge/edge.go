// Package edge captures concrete, qualified ingress and egress controls.
// Products and providers are adapters to this manifest; they do not define
// HCM Next authorization semantics.
package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const schemaVersion = 1

// Version reports the concrete edge manifest schema version.
func Version() int { return schemaVersion }

// Explain describes the fail-closed edge qualification contract.
func Explain() string {
	return "EDGE-010 v1: fail-closed pinned routes, TLS, DNS, WAF, east-west, logged egress, and replacement controls"
}

// Route is one exact externally reachable route.
type Route struct {
	ID                string   `json:"id"`
	Host              string   `json:"host"`
	PathPrefix        string   `json:"path_prefix"`
	Methods           []string `json:"methods"`
	Backend           string   `json:"backend"`
	TLSVersion        string   `json:"tls_version"`
	CertificateRef    string   `json:"certificate_ref"`
	CertificateDigest string   `json:"certificate_digest"`
	DNSName           string   `json:"dns_name"`
	DNSIPs            []string `json:"dns_ips"`
	WAFRef            string   `json:"waf_ref"`
	WAFVersion        string   `json:"waf_version"`
	OwnerRef          string   `json:"owner_ref"`
	MaxRPS            int      `json:"max_rps"`
	BodyLimitBytes    int      `json:"body_limit_bytes"`
	FailureAction     string   `json:"failure_action"`
}

// EastWestRule is an exact service-to-service allow edge.
type EastWestRule struct {
	FromService string `json:"from_service"`
	ToService   string `json:"to_service"`
	Protocol    string `json:"protocol"`
	Port        int    `json:"port"`
	OwnerRef    string `json:"owner_ref"`
	FailClosed  bool   `json:"fail_closed"`
}

// EgressRule is one exact external destination lease.
type EgressRule struct {
	ID            string `json:"id"`
	Destination   string `json:"destination"`
	Port          int    `json:"port"`
	Purpose       string `json:"purpose"`
	OwnerRef      string `json:"owner_ref"`
	Logged        bool   `json:"logged"`
	RedactionRef  string `json:"redaction_ref"`
	FailureAction string `json:"failure_action"`
}

// OutagePlan is the tested degraded and replacement behavior.
type OutagePlan struct {
	DetectionSignal string `json:"detection_signal"`
	DegradedAction  string `json:"degraded_action"`
	ReplacementRef  string `json:"replacement_ref"`
	MigrationPlan   string `json:"migration_plan"`
}

// Manifest is the complete concrete edge and egress qualification input.
type Manifest struct {
	SchemaVersion         int            `json:"schema_version"`
	QualificationID       string         `json:"qualification_id"`
	ImplementationRef     string         `json:"implementation_ref"`
	ImplementationVersion string         `json:"implementation_version"`
	Routes                []Route        `json:"routes"`
	EastWest              []EastWestRule `json:"east_west"`
	Egress                []EgressRule   `json:"egress"`
	Outage                OutagePlan     `json:"outage"`
}

// Violation is one deterministic admission finding.
type Violation struct{ Field, Code, Detail string }

// Evidence is the qualification result consumed by release admission.
type Evidence struct {
	QualificationID string   `json:"qualification_id"`
	ManifestDigest  string   `json:"manifest_digest"`
	Ready           bool     `json:"ready"`
	Status          string   `json:"status"`
	Controls        []string `json:"controls"`
	Blockers        []string `json:"blockers"`
	HumanInputs     []string `json:"human_inputs"`
}

// Validate rejects abstract, wildcard, mutable, ownerless, or fail-open
// controls. A labelled placeholder is accepted as a captured human decision
// and causes Compile to return HUMAN_SELECTION_REQUIRED.
func Validate(m Manifest) []Violation {
	var out []Violation
	add := func(field, code, detail string) { out = append(out, Violation{field, code, detail}) }
	if m.SchemaVersion != schemaVersion {
		add("schema_version", "UNSUPPORTED_SCHEMA", "schema version must be 1")
	}
	if strings.TrimSpace(m.QualificationID) == "" {
		add("qualification_id", "MISSING_ID", "qualification id is required")
	}
	if strings.TrimSpace(m.ImplementationRef) == "" {
		add("implementation_ref", "ABSTRACT_IMPLEMENTATION", "a concrete implementation reference is required")
	}
	if abstractValue(m.ImplementationRef) {
		add("implementation_ref", "ABSTRACT_IMPLEMENTATION", "generic or abstract implementations cannot qualify")
	}
	if strings.TrimSpace(m.ImplementationVersion) == "" || strings.ContainsAny(m.ImplementationVersion, "*?") {
		add("implementation_version", "UNVERIFIED_VERSION", "implementation version must be pinned")
	}
	if !isPlaceholder(m.ImplementationVersion) && unpinnedVersion(m.ImplementationVersion) {
		add("implementation_version", "UNVERIFIED_VERSION", "latest or floating implementation versions cannot qualify")
	}
	if len(m.Routes) == 0 {
		add("routes", "MISSING_ROUTE", "at least one route is required")
	}
	seen := map[string]bool{}
	for i, r := range m.Routes {
		field := fmt.Sprintf("routes[%d]", i)
		if seen[r.ID] {
			add(field+".id", "DUPLICATE_ID", "route ids must be unique")
		}
		seen[r.ID] = true
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Host) == "" || strings.TrimSpace(r.PathPrefix) == "" || strings.TrimSpace(r.Backend) == "" || strings.TrimSpace(r.CertificateRef) == "" || strings.TrimSpace(r.DNSName) == "" || strings.TrimSpace(r.WAFRef) == "" || strings.TrimSpace(r.WAFVersion) == "" || strings.TrimSpace(r.OwnerRef) == "" {
			add(field, "INCOMPLETE_ROUTE", "route requires exact host, path, backend, certificate, DNS, WAF, and owner")
		}
		if wildcard(r.Host) || wildcard(r.PathPrefix) || wildcard(r.Backend) {
			add(field, "WILDCARD_ROUTE", "route control cannot be wildcard or default")
		}
		if r.TLSVersion != "TLS1.3" {
			add(field+".tls_version", "UNSAFE_TLS", "route must pin TLS1.3")
		}
		if !sha256Digest(r.CertificateDigest) {
			add(field+".certificate_digest", "UNPINNED_CERTIFICATE", "certificate must have a sha256 digest")
		}
		if len(r.DNSIPs) == 0 || duplicate(r.DNSIPs) {
			add(field+".dns_ips", "UNVERIFIED_DNS", "DNS must contain unique observed addresses")
		}
		if len(r.Methods) == 0 || duplicate(r.Methods) {
			add(field+".methods", "INCOMPLETE_METHODS", "route methods must be explicit and unique")
		}
		if r.MaxRPS <= 0 || r.BodyLimitBytes <= 0 {
			add(field+".limits", "MISSING_ABUSE_BUDGET", "rate and body budgets must be positive")
		}
		if strings.ToUpper(r.FailureAction) != "DENY" {
			add(field+".failure_action", "FAIL_OPEN", "route failures must deny")
		}
		if unpinnedVersion(r.WAFVersion) {
			add(field+".waf_version", "UNVERIFIED_VERSION", "WAF version must be pinned")
		}
	}
	for i, rule := range m.EastWest {
		field := fmt.Sprintf("east_west[%d]", i)
		if strings.TrimSpace(rule.FromService) == "" || strings.TrimSpace(rule.ToService) == "" || strings.TrimSpace(rule.Protocol) == "" || strings.TrimSpace(rule.OwnerRef) == "" || rule.Port <= 0 || rule.Port > 65535 {
			add(field, "INCOMPLETE_EAST_WEST", "east-west rule requires exact services, protocol, port, and owner")
		}
		if !rule.FailClosed {
			add(field, "FAIL_OPEN", "east-west failure behavior must deny")
		}
		if wildcard(rule.FromService) || wildcard(rule.ToService) {
			add(field, "WILDCARD_ROUTE", "east-west services cannot be wildcard")
		}
	}
	if len(m.Egress) == 0 {
		add("egress", "MISSING_EGRESS", "egress allowlist is required")
	}
	for i, rule := range m.Egress {
		field := fmt.Sprintf("egress[%d]", i)
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Destination) == "" || strings.TrimSpace(rule.Purpose) == "" || strings.TrimSpace(rule.OwnerRef) == "" || strings.TrimSpace(rule.RedactionRef) == "" || rule.Port <= 0 || rule.Port > 65535 {
			add(field, "INCOMPLETE_EGRESS", "egress requires exact destination, purpose, owner, redaction, and port")
		}
		if wildcard(rule.Destination) {
			add(field+".destination", "WILDCARD_EGRESS", "egress destination cannot be wildcard")
		}
		if !rule.Logged {
			add(field+".logged", "UNLOGGED_BYPASS", "egress must be logged")
		}
		if strings.ToUpper(rule.FailureAction) != "DENY" {
			add(field+".failure_action", "FAIL_OPEN", "egress failures must deny")
		}
	}
	if strings.TrimSpace(m.Outage.DetectionSignal) == "" || strings.TrimSpace(m.Outage.DegradedAction) == "" || strings.TrimSpace(m.Outage.ReplacementRef) == "" || strings.TrimSpace(m.Outage.MigrationPlan) == "" {
		add("outage", "INCOMPLETE_OUTAGE_PLAN", "detection, degradation, replacement, and migration behavior are required")
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Check returns the first manifest defect.
func Check(m Manifest) error {
	if v := Validate(m); len(v) > 0 {
		return fmt.Errorf("edge: %s %s: %s", v[0].Code, v[0].Field, v[0].Detail)
	}
	return nil
}

func normalized(m Manifest) Manifest {
	m.Routes = append([]Route(nil), m.Routes...)
	sort.Slice(m.Routes, func(i, j int) bool { return m.Routes[i].ID < m.Routes[j].ID })
	for i := range m.Routes {
		m.Routes[i].Methods = append([]string(nil), m.Routes[i].Methods...)
		sort.Strings(m.Routes[i].Methods)
		m.Routes[i].DNSIPs = append([]string(nil), m.Routes[i].DNSIPs...)
		sort.Strings(m.Routes[i].DNSIPs)
	}
	m.EastWest = append([]EastWestRule(nil), m.EastWest...)
	sort.Slice(m.EastWest, func(i, j int) bool {
		return m.EastWest[i].FromService+"/"+m.EastWest[i].ToService < m.EastWest[j].FromService+"/"+m.EastWest[j].ToService
	})
	m.Egress = append([]EgressRule(nil), m.Egress...)
	sort.Slice(m.Egress, func(i, j int) bool { return m.Egress[i].ID < m.Egress[j].ID })
	return m
}

// Digest returns the canonical identity of a valid manifest.
func Digest(m Manifest) (string, error) {
	if err := Check(m); err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized(m))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Compile validates and qualifies a concrete edge implementation.
func Compile(m Manifest) (Evidence, error) {
	digest, err := Digest(m)
	if err != nil {
		return Evidence{}, err
	}
	e := Evidence{QualificationID: m.QualificationID, ManifestDigest: digest, Ready: true, Status: "READY", Controls: []string{"INGRESS", "TLS", "DNS", "WAF", "EAST_WEST", "EGRESS", "OUTAGE"}}
	if inputs := placeholderInputs(m); len(inputs) > 0 {
		e.Ready = false
		e.Status = "HUMAN_SELECTION_REQUIRED"
		e.HumanInputs = inputs
		e.Blockers = []string{"one or more edge controls are explicit placeholders"}
	}
	return e, nil
}

func wildcard(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "*" || value == "any" || value == "default" || strings.Contains(value, "/*")
}
func isPlaceholder(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "placeholder:")
}

func abstractValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "abstract", "generic", "default", "any", "latest", "floating", "*":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "abstract:")
	}
}

func unpinnedVersion(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "latest", "current", "floating", "default", "*":
		return true
	default:
		return false
	}
}

func placeholderInputs(m Manifest) []string {
	var inputs []string
	add := func(field, value string) {
		if isPlaceholder(value) {
			inputs = append(inputs, field)
		}
	}
	add("implementation_ref", m.ImplementationRef)
	add("implementation_version", m.ImplementationVersion)
	for i, route := range m.Routes {
		prefix := fmt.Sprintf("routes[%d]", i)
		add(prefix+".host", route.Host)
		add(prefix+".certificate_ref", route.CertificateRef)
		add(prefix+".dns_name", route.DNSName)
		add(prefix+".waf_ref", route.WAFRef)
		add(prefix+".owner_ref", route.OwnerRef)
	}
	for i, rule := range m.EastWest {
		add(fmt.Sprintf("east_west[%d].owner_ref", i), rule.OwnerRef)
	}
	for i, rule := range m.Egress {
		prefix := fmt.Sprintf("egress[%d]", i)
		add(prefix+".destination", rule.Destination)
		add(prefix+".owner_ref", rule.OwnerRef)
		add(prefix+".redaction_ref", rule.RedactionRef)
	}
	add("outage.replacement_ref", m.Outage.ReplacementRef)
	sort.Strings(inputs)
	return inputs
}
func sha256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "sha256:") {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
func duplicate(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
