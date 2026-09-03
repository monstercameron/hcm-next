// Package decomposition governs evidence-before-decomposition decisions
// (ARCH-GO-019). A new module, process, or service is an architectural
// decision, not a consequence of package count, team boundaries, or a desired
// deployment shape. The decision must carry evidence for every required
// boundary and must preserve one semantic owner for each domain package.
package decomposition

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Evidence is the proof attached to a decomposition decision. Values are
// intentionally strings: the checker verifies that a decision names the
// measured result or contract, while the owning performance/security/release
// systems remain authoritative for the underlying measurements.
type Evidence struct {
	Scaling               string `json:"scaling"`
	FailureIsolation      string `json:"failure_isolation"`
	SecurityBoundary      string `json:"security_boundary"`
	Residency             string `json:"residency"`
	ReleaseBoundary       string `json:"release_boundary"`
	OwnershipBoundary     string `json:"ownership_boundary"`
	APIEventConsistency   string `json:"api_event_consistency"`
	DataAuthority         string `json:"data_authority"`
	FailureRepair         string `json:"failure_repair"`
	MigrationRollback     string `json:"migration_rollback"`
	OperationalCost       string `json:"operational_cost"`
	MonolithInsufficiency string `json:"modular_monolith_insufficiency"`
}

// Decision describes a proposed physical boundary. Processes lists the
// process roles that would own each listed domain package.
type Decision struct {
	ID             string              `json:"id"`
	Kind           string              `json:"kind"` // module, process, or service
	Target         string              `json:"target"`
	Justification  string              `json:"justification"`
	Evidence       Evidence            `json:"evidence"`
	DomainPackages []string            `json:"domain_packages"`
	Processes      map[string][]string `json:"processes"`
}

const (
	MissingEvidence      = "MISSING_EVIDENCE"
	TopologyOnly         = "TOPOLOGY_ONLY"
	DuplicateDomainOwner = "DUPLICATE_DOMAIN_OWNER"
	MissingKind          = "MISSING_KIND"
	MissingTarget        = "MISSING_TARGET"
)

// Finding is one ARCH-GO-019 contract violation.
type Finding struct {
	DecisionID string
	Code       string
	Field      string
	Detail     string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s (%s)", f.DecisionID, f.Field, f.Detail, f.Code)
}

// Validate returns all violations in stable contract order. An empty result
// means the proposal is eligible for architectural review.
func Validate(d Decision) []Finding {
	var findings []Finding
	add := func(code, field, detail string) { findings = append(findings, Finding{d.ID, code, field, detail}) }
	if strings.TrimSpace(d.Kind) == "" {
		add(MissingKind, "kind", "decomposition kind is required")
	}
	if strings.TrimSpace(d.Target) == "" {
		add(MissingTarget, "target", "decomposition target is required")
	}
	if strings.TrimSpace(d.Justification) == "" {
		add(TopologyOnly, "justification", "decision must explain a measured architectural constraint")
	}
	fields := []struct{ name, value string }{
		{"scaling", d.Evidence.Scaling}, {"failure_isolation", d.Evidence.FailureIsolation},
		{"security_boundary", d.Evidence.SecurityBoundary}, {"residency", d.Evidence.Residency},
		{"release_boundary", d.Evidence.ReleaseBoundary}, {"ownership_boundary", d.Evidence.OwnershipBoundary},
		{"api_event_consistency", d.Evidence.APIEventConsistency}, {"data_authority", d.Evidence.DataAuthority},
		{"failure_repair", d.Evidence.FailureRepair}, {"migration_rollback", d.Evidence.MigrationRollback},
		{"operational_cost", d.Evidence.OperationalCost}, {"modular_monolith_insufficiency", d.Evidence.MonolithInsufficiency},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			add(MissingEvidence, f.name, "required evidence is missing")
		}
	}
	if topologyOnly(d) {
		add(TopologyOnly, "justification", "team naming, domain count, or deployment fashion cannot justify decomposition")
	}
	owners := map[string]string{}
	processes := make([]string, 0, len(d.Processes))
	for process := range d.Processes {
		processes = append(processes, process)
	}
	sort.Strings(processes)
	for _, process := range processes {
		domains := d.Processes[process]
		for _, domain := range domains {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			if prior, ok := owners[domain]; ok && prior != process {
				add(DuplicateDomainOwner, "processes."+process, fmt.Sprintf("domain package %q is also owned by process %q", domain, prior))
			} else {
				owners[domain] = process
			}
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Field != findings[j].Field {
			return findings[i].Field < findings[j].Field
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
	return findings
}

// Check is an error-shaped convenience for callers admitting one decision.
func Check(d Decision) error {
	if v := Validate(d); len(v) != 0 {
		return errors.New(v[0].String())
	}
	return nil
}

// CheckFile parses and validates a JSON decision record.
func CheckFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("decomposition: decision path is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("decomposition: read decision: %w", err)
	}
	var d Decision
	if err := json.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("decomposition: parse decision: %w", err)
	}
	return Check(d)
}

func topologyOnly(d Decision) bool {
	s := strings.ToLower(strings.TrimSpace(d.Justification + " " + d.Target))
	if s == "" {
		return false
	}
	markers := []string{"team", "domain count", "deployment fashion", "deploy fashion", "topology", "one service per", "one process per"}
	found := false
	for _, m := range markers {
		if strings.Contains(s, m) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	// Any named measured constraint makes the justification non-topological.
	for _, v := range []string{d.Evidence.Scaling, d.Evidence.FailureIsolation, d.Evidence.SecurityBoundary, d.Evidence.Residency, d.Evidence.ReleaseBoundary, d.Evidence.OwnershipBoundary, d.Evidence.MonolithInsufficiency} {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
