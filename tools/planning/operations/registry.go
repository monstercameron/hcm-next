// Package operations validates the OPS-007 service-ownership and dependency
// registries. The YAML files under definitions/operations are authored
// evidence, not executable configuration: this package gives planning and
// release checks one typed, deterministic readiness contract.
package operations

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultOwnershipPath is the checked-in OPS-007 ownership registry.
	DefaultOwnershipPath = "definitions/operations/service-ownership.yaml"
	// DefaultDependencyPath is the checked-in OPS-007 dependency registry.
	DefaultDependencyPath = "definitions/operations/service-dependencies.yaml"

	StatusReady    = "READY"
	StatusRejected = "OPS_007_REJECTED"
)

// Ownership is the accountable operating contract for one production service,
// workflow, connector, store or provider.
type Ownership struct {
	ID           string `yaml:"id"`
	Kind         string `yaml:"kind"`
	Version      string `yaml:"version"`
	PrimaryOwner string `yaml:"primary_owner"`
	BackupOwner  string `yaml:"backup_owner"`
	Tier         string `yaml:"tier"`
	SLO          string `yaml:"slo"`
	Escalation   string `yaml:"escalation"`
	Fallback     string `yaml:"fallback"`
	EvidenceRef  string `yaml:"evidence_ref"`
	VerifiedAt   string `yaml:"verified_at"`
	ExpiresAt    string `yaml:"expires_at"`
}

// Dependency is one logical dependency edge. Addresses are deliberately not
// part of this contract; endpoint resolution belongs to the connectivity
// plane.
type Dependency struct {
	Consumer         string `yaml:"consumer"`
	Dependency       string `yaml:"dependency"`
	Owner            string `yaml:"owner"`
	Criticality      string `yaml:"criticality"`
	TenantScope      string `yaml:"tenant_scope"`
	CellScope        string `yaml:"cell_scope"`
	WorkloadIdentity string `yaml:"workload_identity"`
	NetworkPath      string `yaml:"network_path"`
	Protocol         string `yaml:"protocol"`
	SchemaRange      string `yaml:"schema_range"`
	Timeout          string `yaml:"timeout"`
	Staleness        string `yaml:"staleness"`
	Fallback         string `yaml:"fallback"`
	SLO              string `yaml:"slo"`
	Version          string `yaml:"version"`
	EvidenceRef      string `yaml:"evidence_ref"`
	VerifiedAt       string `yaml:"verified_at"`
	ExpiresAt        string `yaml:"expires_at"`
}

// Registry is the common parsed shape of either OPS-007 YAML file. One file
// carries Ownership and the other carries Dependencies; sharing the shape
// keeps loading and validation symmetric and makes combining them explicit.
type Registry struct {
	Version      int          `yaml:"version"`
	Module       string       `yaml:"module"`
	EffectiveAt  string       `yaml:"effective_at"`
	Ownership    []Ownership  `yaml:"ownership"`
	Dependencies []Dependency `yaml:"dependencies"`
}

// Diagnostic identifies the exact rejected field/state/version. It is kept
// structured so callers can render a release report without parsing prose.
type Diagnostic struct {
	Registry string
	Entry    string
	Field    string
	State    string
	Version  string
	Reason   string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s: entry=%s field=%s state=%s version=%s: %s",
		d.Registry, d.Entry, d.Field, d.State, d.Version, d.Reason)
}

// Readiness is the side-effect-free OPS-007 admission result.
type Readiness struct {
	Status      string
	Diagnostics []Diagnostic
}

// Ready reports whether both registries satisfy the contract at now.
func (r Readiness) Ready() bool { return r.Status == StatusReady }

// Load reads either checked-in OPS-007 YAML registry.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("operations: reading %s: %w", path, err)
	}
	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("operations: parsing %s: %w", path, err)
	}
	return &r, nil
}

// Validate combines and validates the ownership and dependency registries at
// now. It never writes to a store, emits a business event, queues work or
// calls a provider; a rejected readiness result is therefore intrinsically
// zero-effect.
func Validate(ownership, dependencies *Registry, now time.Time) Readiness {
	var out Readiness
	if ownership == nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{
			Registry: "ownership", Field: "registry", State: "MISSING", Reason: "ownership registry is required",
		})
	} else {
		out.Diagnostics = append(out.Diagnostics, validateOwnership(ownership, now)...)
	}
	if dependencies == nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{
			Registry: "dependencies", Field: "registry", State: "MISSING", Reason: "dependency registry is required",
		})
	} else {
		out.Diagnostics = append(out.Diagnostics, validateDependencies(dependencies, ownership, now)...)
	}
	if len(out.Diagnostics) == 0 {
		out.Status = StatusReady
	} else {
		out.Status = StatusRejected
	}
	return out
}

func validateOwnership(r *Registry, now time.Time) []Diagnostic {
	var ds []Diagnostic
	if r.Version != 1 {
		ds = append(ds, Diagnostic{Registry: "ownership", Field: "version", State: "UNSUPPORTED", Version: fmt.Sprint(r.Version), Reason: "registry version must be 1"})
	}
	if strings.TrimSpace(r.Module) == "" {
		ds = append(ds, Diagnostic{Registry: "ownership", Field: "module", State: "MISSING", Reason: "module is required"})
	}
	seen := make(map[string]bool, len(r.Ownership))
	for _, e := range r.Ownership {
		ds = append(ds, validateOwnershipEntry(e, seen, now)...)
	}
	if len(r.Ownership) == 0 {
		ds = append(ds, Diagnostic{Registry: "ownership", Field: "ownership", State: "MISSING", Reason: "at least one ownership row is required"})
	}
	return ds
}

func validateOwnershipEntry(e Ownership, seen map[string]bool, now time.Time) []Diagnostic {
	var ds []Diagnostic
	add := func(field, state, reason string) {
		ds = append(ds, Diagnostic{Registry: "ownership", Entry: e.ID, Field: field, State: state, Version: e.Version, Reason: reason})
	}
	if strings.TrimSpace(e.ID) == "" {
		add("id", "MISSING", "ownership id is required")
	} else if seen[e.ID] {
		add("id", "DUPLICATE", "ownership id is registered more than once")
	} else {
		seen[e.ID] = true
	}
	if !validKind(e.Kind) {
		add("kind", "INVALID", "kind must be SERVICE, WORKFLOW, CONNECTOR, STORE or PROVIDER")
	}
	for field, value := range map[string]string{
		"version": e.Version, "primary_owner": e.PrimaryOwner, "backup_owner": e.BackupOwner,
		"tier": e.Tier, "slo": e.SLO, "escalation": e.Escalation, "fallback": e.Fallback,
		"evidence_ref": e.EvidenceRef, "verified_at": e.VerifiedAt, "expires_at": e.ExpiresAt,
	} {
		if strings.TrimSpace(value) == "" {
			add(field, "MISSING", field+" is required")
		}
	}
	if e.PrimaryOwner != "" && e.PrimaryOwner == e.BackupOwner {
		add("backup_owner", "INVALID", "backup owner must be independent of primary owner")
	}
	if e.Tier != "" && e.Tier != "TIER_0" && e.Tier != "TIER_1" && e.Tier != "TIER_2" {
		add("tier", "INVALID", "tier must be TIER_0, TIER_1 or TIER_2")
	}
	ds = append(ds, validateEvidence("ownership", e.ID, e.Version, e.VerifiedAt, e.ExpiresAt, now)...)
	return ds
}

func validateDependencies(r *Registry, ownership *Registry, now time.Time) []Diagnostic {
	var ds []Diagnostic
	if r.Version != 1 {
		ds = append(ds, Diagnostic{Registry: "dependencies", Field: "version", State: "UNSUPPORTED", Version: fmt.Sprint(r.Version), Reason: "registry version must be 1"})
	}
	if strings.TrimSpace(r.Module) == "" {
		ds = append(ds, Diagnostic{Registry: "dependencies", Field: "module", State: "MISSING", Reason: "module is required"})
	}
	if len(r.Dependencies) == 0 {
		ds = append(ds, Diagnostic{Registry: "dependencies", Field: "dependencies", State: "MISSING", Reason: "at least one dependency row is required"})
	}
	owners := map[string]bool{}
	if ownership != nil {
		for _, e := range ownership.Ownership {
			owners[e.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, e := range r.Dependencies {
		ds = append(ds, validateDependencyEntry(e, owners, seen, now)...)
	}
	return ds
}

func validateDependencyEntry(e Dependency, owners, seen map[string]bool, now time.Time) []Diagnostic {
	var ds []Diagnostic
	add := func(field, state, reason string) {
		ds = append(ds, Diagnostic{Registry: "dependencies", Entry: e.Consumer + "->" + e.Dependency, Field: field, State: state, Version: e.Version, Reason: reason})
	}
	if strings.TrimSpace(e.Consumer) == "" {
		add("consumer", "MISSING", "consumer is required")
	} else if !owners[e.Consumer] {
		add("consumer", "UNKNOWN", "consumer has no service ownership row")
	}
	if strings.TrimSpace(e.Dependency) == "" {
		add("dependency", "MISSING", "dependency is required")
	} else if !owners[e.Dependency] {
		add("dependency", "UNKNOWN", "dependency has no service ownership row")
	}
	key := e.Consumer + "\x00" + e.Dependency
	if seen[key] {
		add("dependency", "DUPLICATE", "dependency edge is registered more than once")
	} else {
		seen[key] = true
	}
	for field, value := range map[string]string{
		"owner": e.Owner, "criticality": e.Criticality, "tenant_scope": e.TenantScope,
		"cell_scope": e.CellScope, "workload_identity": e.WorkloadIdentity, "network_path": e.NetworkPath,
		"protocol": e.Protocol, "schema_range": e.SchemaRange, "timeout": e.Timeout, "staleness": e.Staleness,
		"fallback": e.Fallback, "slo": e.SLO, "version": e.Version, "evidence_ref": e.EvidenceRef,
		"verified_at": e.VerifiedAt, "expires_at": e.ExpiresAt,
	} {
		if strings.TrimSpace(value) == "" {
			add(field, "MISSING", field+" is required")
		}
	}
	ds = append(ds, validateEvidence("dependencies", e.Consumer+"->"+e.Dependency, e.Version, e.VerifiedAt, e.ExpiresAt, now)...)
	return ds
}

func validateEvidence(registry, entry, version, verifiedAt, expiresAt string, now time.Time) []Diagnostic {
	var ds []Diagnostic
	add := func(field, state, reason string) {
		ds = append(ds, Diagnostic{Registry: registry, Entry: entry, Field: field, State: state, Version: version, Reason: reason})
	}
	verified, vErr := time.Parse(time.RFC3339, verifiedAt)
	expires, eErr := time.Parse(time.RFC3339, expiresAt)
	if vErr != nil && verifiedAt != "" {
		add("verified_at", "INVALID", "verified_at must be RFC3339")
	}
	if eErr != nil && expiresAt != "" {
		add("expires_at", "INVALID", "expires_at must be RFC3339")
	}
	if vErr == nil && verified.After(now) {
		add("verified_at", "FUTURE", "verification cannot be in the future")
	}
	if eErr == nil {
		if !expires.After(now) {
			add("expires_at", "EXPIRED", "operating evidence or ownership continuity has expired")
		}
		if vErr == nil && !expires.After(verified) {
			add("expires_at", "INVALID", "expires_at must be after verified_at")
		}
	}
	return ds
}

func validKind(kind string) bool {
	switch kind {
	case "SERVICE", "WORKFLOW", "CONNECTOR", "STORE", "PROVIDER":
		return true
	default:
		return false
	}
}

// OwnershipIDs returns sorted ownership IDs for deterministic reports.
func OwnershipIDs(r *Registry) []string {
	if r == nil {
		return nil
	}
	ids := make([]string, 0, len(r.Ownership))
	for _, e := range r.Ownership {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return ids
}
