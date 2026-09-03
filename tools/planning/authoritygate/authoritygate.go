// Package authoritygate implements the GOV-010 authority-gate decision
// record: a typed, versioned schema for Gate A/B/C decisions plus a
// validator that rejects a decision with incomplete evidence and flags a
// gate that a completed GATE_* todo already depends on but that has never
// actually been decided.
//
// A decision is immutable and additive: this package exposes no update
// API, Load re-parses definitions/planning/authority-gate-decision.yaml
// fresh on every call, and superseding an earlier decision means appending
// a new dated Record for the same Gate, never mutating one in place.
package authoritygate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/tools/planning/todoregistry"
	"gopkg.in/yaml.v3"
)

// Gate is one of the three Phase 1 authority gates (execution-plan.md
// "Delivery Gates, Staffing Envelope, and Critical Path"). The spelling
// matches todoregistry's GATE_A/GATE_B/GATE_C phase tags exactly, so a
// todo's Phase field converts directly to a Gate.
type Gate string

const (
	GateA Gate = "GATE_A"
	GateB Gate = "GATE_B"
	GateC Gate = "GATE_C"
)

var validGates = map[Gate]bool{GateA: true, GateB: true, GateC: true}

// Decision is the exact, closed set of verdicts a signed gate decision may
// return (GOV-010 GREEN: "return exactly PROCEED, REMEDIATE, NARROW,
// RESELECT_WEDGE or STOP").
type Decision string

const (
	DecisionProceed       Decision = "PROCEED"
	DecisionRemediate     Decision = "REMEDIATE"
	DecisionNarrow        Decision = "NARROW"
	DecisionReselectWedge Decision = "RESELECT_WEDGE"
	DecisionStop          Decision = "STOP"
)

var validDecisions = map[Decision]bool{
	DecisionProceed:       true,
	DecisionRemediate:     true,
	DecisionNarrow:        true,
	DecisionReselectWedge: true,
	DecisionStop:          true,
}

// EvidenceClass is one of the seven evidence classes every decision must
// cover (planning/todos.md GOV-010 RED: "every missing security, privacy,
// correctness, reconciliation, recovery, governance and continuity class").
type EvidenceClass string

const (
	ClassSecurity       EvidenceClass = "security"
	ClassPrivacy        EvidenceClass = "privacy"
	ClassCorrectness    EvidenceClass = "correctness"
	ClassReconciliation EvidenceClass = "reconciliation"
	ClassRecovery       EvidenceClass = "recovery"
	ClassGovernance     EvidenceClass = "governance"
	ClassContinuity     EvidenceClass = "continuity"
)

// RequiredClasses is the fixed, ordered set every decision record's
// EvidenceRefs must cover. Order is stable so violation lists are
// deterministic.
var RequiredClasses = []EvidenceClass{
	ClassSecurity, ClassPrivacy, ClassCorrectness, ClassReconciliation,
	ClassRecovery, ClassGovernance, ClassContinuity,
}

// Signer is one role-bound signatory on a decision record ("signed" per
// GOV-010 GREEN).
type Signer struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
}

// Record is one authority-gate decision: which gate, what was decided, the
// proposal it is bound to, the evidence backing it by class, who signed it
// and when, and the date it must be revisited by.
type Record struct {
	Gate         Gate                       `yaml:"gate"`
	Decision     Decision                   `yaml:"decision"`
	ProposalRef  string                     `yaml:"proposal_ref"`
	EvidenceRefs map[EvidenceClass][]string `yaml:"evidence_refs"`
	Signers      []Signer                   `yaml:"signers"`
	Date         string                     `yaml:"date"`
	ReviewBy     string                     `yaml:"review_by"`
}

type recordsFile struct {
	Decisions []Record `yaml:"decisions"`
}

// Load reads and parses the authority-gate decision record file. A missing
// file is treated as zero decisions.
func Load(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("authoritygate: reading %s: %w", path, err)
	}
	var f recordsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("authoritygate: parsing %s: %w", path, err)
	}
	return f.Decisions, nil
}

// Violation names one rejected decision record or gate reference. Kind is
// a short, stable identifier ("undecided", "unsigned", "invalid_decision",
// "invalid_gate", "missing_proposal_ref", "missing_date", "missing_review_by",
// "missing_evidence", "missing_evidence_path", "evidence_path_escape") used
// to match a violation against a known-defects.yaml allow-list entry.
type Violation struct {
	Gate   Gate
	Kind   string
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Gate, v.Reason)
}

// MissingEvidenceClasses returns every RequiredClasses entry that is absent
// or empty in r.EvidenceRefs, in RequiredClasses order. It returns every
// missing class, never just the first (GOV-010 RED).
func MissingEvidenceClasses(r Record) []EvidenceClass {
	var missing []EvidenceClass
	for _, c := range RequiredClasses {
		if len(r.EvidenceRefs[c]) == 0 {
			missing = append(missing, c)
		}
	}
	return missing
}

// ValidateRecord checks the structural GOV-010 GREEN invariants for one
// decision record: a known gate, a decision drawn from the exact
// five-member enum, at least one complete signer, a non-empty proposal
// binding, a date, a review-by date, and every evidence class present. It
// returns every violation found, not just the first.
func ValidateRecord(r Record) []Violation {
	var violations []Violation

	if !validGates[r.Gate] {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "invalid_gate",
			Reason: fmt.Sprintf("gate %q is not one of GATE_A, GATE_B, GATE_C", r.Gate)})
	}
	if !validDecisions[r.Decision] {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "invalid_decision",
			Reason: fmt.Sprintf("decision %q is not one of PROCEED, REMEDIATE, NARROW, RESELECT_WEDGE, STOP", r.Decision)})
	}
	if len(r.Signers) == 0 {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "unsigned",
			Reason: "decision is unsigned: at least one signer is required"})
	}
	for _, s := range r.Signers {
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Role) == "" {
			violations = append(violations, Violation{Gate: r.Gate, Kind: "unsigned",
				Reason: "signer is missing a name or role"})
		}
	}
	if strings.TrimSpace(r.ProposalRef) == "" {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "missing_proposal_ref",
			Reason: "decision is not proposal-bound: proposal_ref is empty"})
	}
	if strings.TrimSpace(r.Date) == "" {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "missing_date",
			Reason: "decision has no date"})
	}
	if strings.TrimSpace(r.ReviewBy) == "" {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "missing_review_by",
			Reason: "decision has no review-by date"})
	}
	for _, c := range MissingEvidenceClasses(r) {
		violations = append(violations, Violation{Gate: r.Gate, Kind: "missing_evidence",
			Reason: fmt.Sprintf("missing %s evidence", c)})
	}

	return violations
}

// CheckEvidencePaths returns a Violation for every evidence path cited in r
// that escapes repoRoot (an absolute path, or one that resolves outside
// repoRoot via "..") or that does not resolve to a real file under
// repoRoot. Escaping paths are reported without ever being stat'd, so a
// malicious or malformed ref cannot be used to probe the filesystem outside
// the repository.
func CheckEvidencePaths(r Record, repoRoot string) []Violation {
	var violations []Violation

	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return []Violation{{Gate: r.Gate, Kind: "missing_evidence_path",
			Reason: fmt.Sprintf("cannot resolve repository root %q: %v", repoRoot, err)}}
	}

	for _, class := range RequiredClasses {
		for _, p := range r.EvidenceRefs[class] {
			resolved, ok := resolveWithinRoot(absRoot, p)
			if !ok {
				violations = append(violations, Violation{Gate: r.Gate, Kind: "evidence_path_escape",
					Reason: fmt.Sprintf("evidence ref %q for %s escapes the repository root", p, class)})
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				violations = append(violations, Violation{Gate: r.Gate, Kind: "missing_evidence_path",
					Reason: fmt.Sprintf("evidence ref %q for %s does not exist: %v", p, class, err)})
			}
		}
	}

	return violations
}

// resolveWithinRoot resolves rel against absRoot and reports whether the
// result stays inside absRoot. An absolute rel, or one containing a ".."
// segment that walks above absRoot, is rejected before any filesystem
// access is attempted.
func resolveWithinRoot(absRoot, rel string) (string, bool) {
	slashed := filepath.ToSlash(rel)
	if filepath.IsAbs(rel) || strings.HasPrefix(slashed, "../") || slashed == ".." || strings.Contains(slashed, "/../") {
		return "", false
	}
	full := filepath.Join(absRoot, filepath.FromSlash(rel))
	resolved, err := filepath.Abs(full)
	if err != nil {
		return "", false
	}
	if resolved != absRoot && !strings.HasPrefix(resolved, absRoot+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}

// TickedGateCounts counts, per Gate, how many todos in planning/todos.md
// are done ("- [x]") and phased into that gate.
func TickedGateCounts(todos []todoregistry.Todo) map[Gate]int {
	counts := map[Gate]int{}
	for _, t := range todos {
		if !t.Done {
			continue
		}
		g := Gate(t.Phase)
		if validGates[g] {
			counts[g]++
		}
	}
	return counts
}

// CheckUndecidedGates returns a Violation for every gate with at least one
// completed GATE_* todo but no record naming a valid Decision for that
// gate: closed delivery work must never outrun a signed authority
// decision.
func CheckUndecidedGates(todos []todoregistry.Todo, records []Record) []Violation {
	decided := map[Gate]bool{}
	for _, r := range records {
		if validDecisions[r.Decision] {
			decided[r.Gate] = true
		}
	}

	var violations []Violation
	for gate, n := range TickedGateCounts(todos) {
		if decided[gate] {
			continue
		}
		violations = append(violations, Violation{Gate: gate, Kind: "undecided",
			Reason: fmt.Sprintf("undecided: %d completed %s todo(s) exist but no signed decision record names %s", n, gate, gate)})
	}

	sort.Slice(violations, func(i, j int) bool { return violations[i].Gate < violations[j].Gate })
	return violations
}

// KnownGap is an allow-listed authority-gate violation, recorded with its
// rationale in definitions/planning/known-defects.yaml.
type KnownGap struct {
	Gate   Gate   `yaml:"gate"`
	Kind   string `yaml:"kind"`
	Owner  string `yaml:"owner"`
	Expiry string `yaml:"expiry"`
	Reason string `yaml:"reason"`
}

type knownGapsFile struct {
	KnownGaps []KnownGap `yaml:"known_authority_gate_gaps"`
}

// LoadKnownGaps reads the known-authority-gate-gaps allow-list. A missing
// file is treated as an empty list.
func LoadKnownGaps(path string) ([]KnownGap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("authoritygate: reading %s: %w", path, err)
	}
	var f knownGapsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("authoritygate: parsing %s: %w", path, err)
	}
	return f.KnownGaps, nil
}
