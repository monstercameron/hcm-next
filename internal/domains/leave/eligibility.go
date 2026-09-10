package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Program eligibility results: the closed per-program vocabulary.
const (
	ProgramEligible    = "ELIGIBLE"
	ProgramIneligible  = "INELIGIBLE"
	ProgramConditional = "CONDITIONAL"
	ProgramUnknown     = "UNKNOWN"
)

// EligibilityRule is one independent program rule. Rules read facts and
// return verdicts; they never approve, mutate or execute.
type EligibilityRule interface {
	RuleID() string
	Version() string
	Evaluate(facts map[string]string) (string, []string, error)
}

// ProgramQuery binds one program to its rule and required facts.
type ProgramQuery struct {
	ProgramID    string
	Authority    string
	Release      string
	Rule         EligibilityRule
	RequireFacts []string
	Obligations  []string
}

// ProgramResult is one program's independent eligibility truth.
type ProgramResult struct {
	ProgramID   string
	Authority   string
	Release     string
	RuleID      string
	RuleVersion string
	Facts       []string
	Result      string
	Blockers    []string
	Obligations []string
}

// EligibilityResolution is the immutable composition input: every program
// keeps its own result, authority, blockers and obligations with zero
// human approval and zero domain mutation.
type EligibilityResolution struct {
	Programs []ProgramResult
	Digest   string
}

func resolutionDigest(results []ProgramResult) string {
	ordered := append([]ProgramResult(nil), results...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ProgramID < ordered[j].ProgramID })
	parts := []string{"leave-eligibility"}
	for _, result := range ordered {
		facts := append([]string(nil), result.Facts...)
		sort.Strings(facts)
		blockers := append([]string(nil), result.Blockers...)
		sort.Strings(blockers)
		parts = append(parts, strings.Join([]string{result.ProgramID, result.Authority, result.Release, result.RuleID, result.RuleVersion, strings.Join(facts, ","), result.Result, strings.Join(blockers, ","), strings.Join(result.Obligations, ",")}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// approvalFacts never determine statutory entitlement: they are stripped
// before statutory rules evaluate.
var approvalFacts = map[string]bool{"manager.approved": true, "reviewer.approved": true}

// ResolveEligibility evaluates every program independently of approval.
// Missing facts resolve UNKNOWN, never denial; ineligible and unknown
// programs stay listed with their traces; statutory rules never see human
// approval facts.
func ResolveEligibility(queries []ProgramQuery, facts map[string]string) (EligibilityResolution, error) {
	if len(queries) == 0 {
		return EligibilityResolution{}, fmt.Errorf("leave: eligibility resolution requires at least one program")
	}
	seen := make(map[string]bool, len(queries))
	resolution := EligibilityResolution{}
	for _, query := range queries {
		if strings.TrimSpace(query.ProgramID) == "" || seen[query.ProgramID] {
			return EligibilityResolution{}, fmt.Errorf("leave: program identities must be unique and non-empty")
		}
		seen[query.ProgramID] = true
		if query.Rule == nil {
			return EligibilityResolution{}, fmt.Errorf("leave: program %s has no rule", query.ProgramID)
		}
		result := ProgramResult{
			ProgramID: query.ProgramID, Authority: query.Authority, Release: query.Release,
			RuleID: query.Rule.RuleID(), RuleVersion: query.Rule.Version(),
			Obligations: append([]string(nil), query.Obligations...),
		}
		visible := make(map[string]string, len(facts))
		for key, value := range facts {
			if query.Authority == AuthorityStatutory && approvalFacts[key] {
				continue
			}
			visible[key] = value
		}
		missing := false
		for _, required := range query.RequireFacts {
			value, ok := visible[required]
			if !ok || strings.TrimSpace(value) == "" {
				missing = true
				result.Blockers = append(result.Blockers, "missing-fact:"+required)
			} else {
				result.Facts = append(result.Facts, required+"="+value)
			}
		}
		sort.Strings(result.Facts)
		sort.Strings(result.Blockers)
		if missing {
			result.Result = ProgramUnknown
			resolution.Programs = append(resolution.Programs, result)
			continue
		}
		verdict, blockers, err := query.Rule.Evaluate(visible)
		if err != nil {
			result.Result = ProgramUnknown
			result.Blockers = append(result.Blockers, "rule-error")
			resolution.Programs = append(resolution.Programs, result)
			continue
		}
		switch verdict {
		case ProgramEligible, ProgramIneligible, ProgramConditional:
			result.Result = verdict
			result.Blockers = append(result.Blockers, blockers...)
		default:
			result.Result = ProgramUnknown
			result.Blockers = append(result.Blockers, "rule-unknown")
		}
		sort.Strings(result.Blockers)
		resolution.Programs = append(resolution.Programs, result)
	}
	sort.Slice(resolution.Programs, func(i, j int) bool { return resolution.Programs[i].ProgramID < resolution.Programs[j].ProgramID })
	resolution.Digest = resolutionDigest(resolution.Programs)
	return resolution, nil
}

// Verify recomputes the resolution seal.
func (resolution EligibilityResolution) Verify() error {
	if resolution.Digest == "" || resolutionDigest(resolution.Programs) != resolution.Digest {
		return fmt.Errorf("leave: eligibility resolution seal is broken")
	}
	return nil
}

// AuthorityStatutory names the statutory authority in leave queries.
const AuthorityStatutory = "statutory"

// RuleRegistry guards rules for concurrent resolution.
type RuleRegistry struct {
	mu    sync.Mutex
	rules map[string]EligibilityRule
}

// NewRuleRegistry starts an empty registry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{rules: make(map[string]EligibilityRule)}
}

// Register publishes one rule. Duplicates refuse.
func (registry *RuleRegistry) Register(rule EligibilityRule) error {
	if registry == nil || rule == nil {
		return fmt.Errorf("leave: rule registry and rule are required")
	}
	if strings.TrimSpace(rule.RuleID()) == "" {
		return fmt.Errorf("leave: rule id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.rules[rule.RuleID()]; dup {
		return fmt.Errorf("leave: rule %s is already registered", rule.RuleID())
	}
	registry.rules[rule.RuleID()] = rule
	return nil
}

// Lookup resolves one registered rule.
func (registry *RuleRegistry) Lookup(id string) (EligibilityRule, bool) {
	if registry == nil {
		return nil, false
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	rule, ok := registry.rules[id]
	return rule, ok
}
