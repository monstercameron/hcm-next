package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Impact actions: the closed required-action vocabulary.
const (
	ActionRecompute  = "recompute"
	ActionNotice     = "notice"
	ActionReapproval = "reapproval"
	ActionHold       = "hold"
)

// RuleChange is one regulatory change assessed before activation.
type RuleChange struct {
	RuleID        string
	OldRelease    string
	NewRelease    string
	EffectiveDate int
	Watermark     int
}

// Scope is one tenant scope running pinned rule releases with a business
// effective date. Assessment reads the business date, never the
// execution date.
type Scope struct {
	Tenant           string
	BusinessDate     int
	Releases         map[string]string
	Calculations     []string
	Workflows        []string
	Notices          []string
	PendingApprovals []string
}

// AffectedScope is one impacted scope with its required actions. Old
// outcomes retain their original rule release: the report never rewrites
// history.
type AffectedScope struct {
	Tenant       string
	OldRelease   string
	Actions      []string
	Calculations []string
	Workflows    []string
	Notices      []string
	Approvals    []string
}

// ImpactReport is the deterministic read-only assessment.
type ImpactReport struct {
	RuleID     string
	OldRelease string
	NewRelease string
	Watermark  int
	Affected   []AffectedScope
	Unaffected int
	Digest     string
}

func impactDigest(change RuleChange, affected []AffectedScope, unaffected int) string {
	ordered := append([]AffectedScope(nil), affected...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Tenant < ordered[j].Tenant })
	parts := []string{"legal-impact", change.RuleID, change.OldRelease, change.NewRelease, fmt.Sprint(change.EffectiveDate, change.Watermark, unaffected)}
	for _, scope := range ordered {
		actions := append([]string(nil), scope.Actions...)
		sort.Strings(actions)
		parts = append(parts, scope.Tenant+"="+scope.OldRelease+"="+strings.Join(actions, ","))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AssessImpact computes the affected scopes for one rule change. The
// query is read-only and watermark-bound: changes newer than the query
// watermark refuse, business effective dates decide (never execution
// dates), and old outcomes keep their original release.
func AssessImpact(change RuleChange, queryWatermark int, scopes []Scope) (ImpactReport, error) {
	if strings.TrimSpace(change.RuleID) == "" || strings.TrimSpace(change.OldRelease) == "" || strings.TrimSpace(change.NewRelease) == "" {
		return ImpactReport{}, fmt.Errorf("legal: rule change names its rule and releases")
	}
	if change.OldRelease == change.NewRelease {
		return ImpactReport{}, fmt.Errorf("legal: rule change must advance the release")
	}
	if change.Watermark > queryWatermark {
		return ImpactReport{}, fmt.Errorf("legal: impact query is bound at watermark %d", queryWatermark)
	}
	report := ImpactReport{RuleID: change.RuleID, OldRelease: change.OldRelease, NewRelease: change.NewRelease, Watermark: queryWatermark}
	for _, scope := range scopes {
		release, pinned := scope.Releases[change.RuleID]
		if !pinned || release != change.OldRelease || scope.BusinessDate < change.EffectiveDate {
			report.Unaffected++
			continue
		}
		affected := AffectedScope{Tenant: scope.Tenant, OldRelease: release}
		if len(scope.Calculations) > 0 {
			affected.Actions = append(affected.Actions, ActionRecompute)
			affected.Calculations = append([]string(nil), scope.Calculations...)
		}
		if len(scope.Notices) > 0 {
			affected.Actions = append(affected.Actions, ActionNotice)
			affected.Notices = append([]string(nil), scope.Notices...)
		}
		if len(scope.PendingApprovals) > 0 {
			affected.Actions = append(affected.Actions, ActionReapproval)
			affected.Approvals = append([]string(nil), scope.PendingApprovals...)
		}
		if len(scope.Workflows) > 0 {
			affected.Actions = append(affected.Actions, ActionHold)
			affected.Workflows = append([]string(nil), scope.Workflows...)
		}
		sort.Strings(affected.Actions)
		report.Affected = append(report.Affected, affected)
	}
	sort.Slice(report.Affected, func(i, j int) bool { return report.Affected[i].Tenant < report.Affected[j].Tenant })
	report.Digest = impactDigest(change, report.Affected, report.Unaffected)
	return report, nil
}

// Verify recomputes the report seal.
func (report ImpactReport) Verify(change RuleChange) error {
	if report.Digest == "" || impactDigest(change, report.Affected, report.Unaffected) != report.Digest {
		return fmt.Errorf("legal: impact seal is broken")
	}
	return nil
}

// ScopeRegistry guards scopes for concurrent assessment.
type ScopeRegistry struct {
	mu     sync.Mutex
	scopes map[string]Scope
}

// NewScopeRegistry starts an empty registry.
func NewScopeRegistry() *ScopeRegistry {
	return &ScopeRegistry{scopes: make(map[string]Scope)}
}

// Register publishes one scope. Duplicates refuse.
func (registry *ScopeRegistry) Register(scope Scope) error {
	if registry == nil {
		return fmt.Errorf("legal: nil scope registry")
	}
	if strings.TrimSpace(scope.Tenant) == "" {
		return fmt.Errorf("legal: scope tenant is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.scopes[scope.Tenant]; dup {
		return fmt.Errorf("legal: scope %s is already registered", scope.Tenant)
	}
	registry.scopes[scope.Tenant] = scope
	return nil
}

// Snapshot lists registered scopes in order.
func (registry *ScopeRegistry) Snapshot() []Scope {
	if registry == nil {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	out := make([]Scope, 0, len(registry.scopes))
	for _, scope := range registry.scopes {
		releases := make(map[string]string, len(scope.Releases))
		for rule, release := range scope.Releases {
			releases[rule] = release
		}
		scope.Releases = releases
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tenant < out[j].Tenant })
	return out
}
