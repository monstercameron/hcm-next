package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrDuplicateContract = errors.New("duplicate intent contract")
var ErrDuplicateTodo = errors.New("conflicting existing todo")

// IntentContract adds reviewed semantics not represented by IntentDescriptor.
// The descriptor remains the authoritative identity and conformance contract.
type IntentContract struct {
	Descriptor                                                  IntentDescriptor
	OwnerDomain, Intelligence                                   string
	InputProperties, ResultProperties, EntityRelations          []string
	Invariants, ConflictRules, ProposalRules, RevalidationRules []string
	CancellationRules, CompensationRules, GovernanceObligations []string
	CapabilityPaths, DirectPaths, WorkflowComposition           []string
	ObservationReconciliationRepair, RetentionRules             []string
	SLO, OperationsOwner                                        string
}
type IntentGap struct{ Intent, Dimension, Detail string }
type TodoCandidate struct {
	Key, ID, Intent, Dimension, Owner, Phase, Intelligence string
	Dependencies, References                               []string
	TestName, RedOracle, GreenOracle                       string
	NegativeClasses, FaultClasses, SecurityClasses         []string
}
type GapCompilerInput struct {
	Catalog       []IntentDescriptor
	Contracts     []IntentContract
	ExistingTodos []TodoCandidate
}
type GapCompilerResult struct {
	Gaps                      []IntentGap
	Candidates, NewCandidates []TodoCandidate
	Digest                    string
}

// CompileCatalogGaps is the production entry point for the existing descriptor
// manifest. It reports every semantic field that the current manifest cannot
// prove instead of manufacturing a complete parallel fixture model.
func CompileCatalogGaps(catalog []IntentDescriptor, existing []TodoCandidate) (GapCompilerResult, error) {
	contracts := make([]IntentContract, len(catalog))
	for i := range catalog {
		contracts[i].Descriptor = catalog[i]
	}
	return CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: contracts, ExistingTodos: existing})
}

func CompileIntentGaps(in GapCompilerInput) (GapCompilerResult, error) {
	allowed := map[string]bool{}
	for _, d := range in.Catalog {
		allowed[intentRef(d)] = true
	}
	byKey := map[string]TodoCandidate{}
	for _, x := range in.ExistingTodos {
		normalizeCandidate(&x)
		if old, ok := byKey[x.Key]; ok && !same(old, x) {
			return GapCompilerResult{}, fmt.Errorf("%w: %s", ErrDuplicateTodo, x.Key)
		}
		byKey[x.Key] = x
	}
	seen := map[string]bool{}
	var gaps []IntentGap
	var added []TodoCandidate
	for _, c := range in.Contracts {
		ref := intentRef(c.Descriptor)
		if !allowed[ref] {
			continue
		}
		if seen[ref] {
			return GapCompilerResult{}, fmt.Errorf("%w: %s", ErrDuplicateContract, ref)
		}
		seen[ref] = true
		for _, d := range missingDimensions(c) {
			gaps = append(gaps, IntentGap{ref, d.name, d.detail})
			x := candidateFor(c, ref, d)
			normalizeCandidate(&x)
			if old, ok := byKey[x.Key]; ok {
				if !same(old, x) {
					return GapCompilerResult{}, fmt.Errorf("%w: %s", ErrDuplicateTodo, x.Key)
				}
				continue
			}
			byKey[x.Key] = x
			added = append(added, x)
		}
	}
	all := make([]TodoCandidate, 0, len(byKey))
	for _, x := range byKey {
		all = append(all, x)
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Intent != gaps[j].Intent {
			return gaps[i].Intent < gaps[j].Intent
		}
		return gaps[i].Dimension < gaps[j].Dimension
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	sort.Slice(added, func(i, j int) bool { return added[i].Key < added[j].Key })
	r := GapCompilerResult{Gaps: gaps, Candidates: all, NewCandidates: added}
	r.Digest = digestCandidates(all)
	return r, nil
}

type dimension struct {
	name, detail string
	present      bool
}

func missingDimensions(c IntentContract) []dimension {
	d := c.Descriptor
	checks := []dimension{
		{"input_properties", "required input properties", len(c.InputProperties) > 0}, {"result_properties", "required result properties", len(c.ResultProperties) > 0}, {"entity_relations", "entity relations", len(c.EntityRelations) > 0},
		{"authority_source", "authority/source rules", len(d.Authority) > 0}, {"temporal", "temporal rules", len(d.Time) > 0}, {"lifecycle", "five lifecycle dimensions", d.Lifecycle.Request != "" && d.Lifecycle.Execution != "" && d.Lifecycle.Business != "" && d.Lifecycle.Consistency != "" && d.Lifecycle.Obligation != ""},
		{"invariants", "invariants", len(c.Invariants) > 0}, {"conflict", "conflict rules", len(c.ConflictRules) > 0}, {"proposal", "proposal rules", len(c.ProposalRules) > 0}, {"revalidation", "revalidation rules", len(c.RevalidationRules) > 0}, {"cancellation", "cancellation rules", len(c.CancellationRules) > 0}, {"compensation", "compensation rules", len(c.CompensationRules) > 0},
		{"governance", "governance obligations", len(c.GovernanceObligations) > 0}, {"capability", "capability path", len(c.CapabilityPaths) > 0}, {"direct_path", "direct path", len(c.DirectPaths) > 0}, {"workflow", "workflow composition", len(c.WorkflowComposition) > 0}, {"transaction_effect", "transaction/effect contract", len(d.Writes) > 0 && len(d.Effects) > 0},
		{"observation_reconciliation_repair", "observation/reconciliation/repair", len(c.ObservationReconciliationRepair) > 0}, {"evidence", "evidence rule", len(d.Evidence) > 0}, {"retention", "retention rule", len(c.RetentionRules) > 0}, {"negative_dimensions", "negative/fault/security dimensions", len(d.NegativePolicy) > 0}, {"slo", "SLO", strings.TrimSpace(c.SLO) != ""}, {"operations_owner", "operations owner", strings.TrimSpace(c.OperationsOwner) != ""}}
	var out []dimension
	for _, x := range checks {
		if !x.present {
			out = append(out, x)
		}
	}
	return out
}
func candidateFor(c IntentContract, ref string, d dimension) TodoCandidate {
	owner := strings.TrimSpace(c.OwnerDomain)
	if owner == "" {
		owner = "intent-governance"
	}
	phase := strings.TrimSpace(c.Descriptor.Phase)
	if phase == "" {
		phase = "DESIGN"
	}
	intel := strings.TrimSpace(c.Intelligence)
	if intel == "" {
		intel = "SOL_HIGH"
	}
	key := ref + "::" + d.name
	test := "TestIntentGap_" + safeTestPart(ref) + "_" + safeTestPart(d.name)
	return TodoCandidate{Key: key, ID: key, Intent: ref, Dimension: d.name, Owner: owner, Phase: phase, Intelligence: intel, References: []string{"planning/specs/business-intent-catalog.md#required-definition-fields", "planning/data/models/adversarial-model-audit-2026-08-14.md#negative-dimensions-required-by-every-future-binding"}, TestName: test, RedOracle: fmt.Sprintf("%s reports unresolved %s and zero persisted, ledger, outbox, human-work, provider, or external effects", test, d.name), GreenOracle: fmt.Sprintf("%s is contracted and %s emits no %s gap", d.detail, ref, d.name), NegativeClasses: []string{"missing", "unknown", "stale", "redacted", "unavailable", "duplicate", "replay"}, FaultClasses: []string{"retry_exhaustion", "ambiguous_commit", "partial_success", "provider_outage"}, SecurityClasses: []string{"authorization_deny", "tenant_boundary", "dlp_egress"}}
}
func intentRef(d IntentDescriptor) string {
	if strings.TrimSpace(d.IntentTypeID) == "" || d.Version < 1 {
		return ""
	}
	return fmt.Sprintf("%s/v%d", strings.TrimSpace(d.IntentTypeID), d.Version)
}
func normalizeCandidate(c *TodoCandidate) {
	c.Key = strings.TrimSpace(c.Key)
	c.Dependencies = uniqueSorted(c.Dependencies)
	c.References = uniqueSorted(c.References)
	c.NegativeClasses = uniqueSorted(c.NegativeClasses)
	c.FaultClasses = uniqueSorted(c.FaultClasses)
	c.SecurityClasses = uniqueSorted(c.SecurityClasses)
}
func same(a, b TodoCandidate) bool {
	normalizeCandidate(&a)
	normalizeCandidate(&b)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func digestCandidates(c []TodoCandidate) string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func safeTestPart(s string) string {
	return strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(s)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
