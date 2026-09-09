// Package intentcoverage builds the bidirectional coverage graph between
// accepted BusinessIntent contracts and the atomic todos that deliver them
// (GOV-026): forward accepted-intent -> contract-gap -> atomic-todo -> test
// -> evidence, and reverse todo -> exact-intent-or-set -> outcome.
//
// Every edge is labelled CONCEPTUAL, CATALOGUED, CONTRACTED, IMPLEMENTED or
// VERIFIED from reproducible, exact-identity evidence only: a Markdown link
// or a matching noun never promotes a status, and a claim can never exceed
// what the lower rungs of its own ladder have proven. Concretely, an
// intent's status is capped at CONCEPTUAL whenever its capability or
// contract-dimension owners are missing, no matter how complete the bound
// todo's own test/evidence trail looks - reporting partition or conceptual
// coverage as CONTRACTED or IMPLEMENTED is refused by construction, not by
// a follow-up check.
//
// Identities and maturity vocabulary are intentionally imported rather than
// re-derived: accepted intents and their contract dimensions come from
// tools/planning/intentmanifests (INTENT-009), todo/test/evidence structure
// comes from tools/planning/todogovernance and tools/planning/traceability
// (GOV-025/GOV-003/GOV-017), and workflow bindings come from
// tools/planning/corpus (GOV-024) - this graph shares those identities and
// maturity rules by import and never re-derives or copies them.
package intentcoverage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const schemaVersion = 1

// EdgeStatus is the maturity label attached to every graph edge.
type EdgeStatus string

const (
	Conceptual  EdgeStatus = "CONCEPTUAL"
	Catalogued  EdgeStatus = "CATALOGUED"
	Contracted  EdgeStatus = "CONTRACTED"
	Implemented EdgeStatus = "IMPLEMENTED"
	Verified    EdgeStatus = "VERIFIED"
)

var statusRank = map[EdgeStatus]int{
	Conceptual: 0, Catalogued: 1, Contracted: 2, Implemented: 3, Verified: 4,
}

func (s EdgeStatus) rank() int { return statusRank[s] }

// Namespace distinguishes the fourteen currently drafted BusinessIntent
// contracts (baseline) from any future funded-domain contract added on top
// of them (extension), so counts are always reported separately rather than
// blended into one number that would hide how much of the "coverage" is
// still the original fourteen-intent slice.
const (
	NamespaceBaseline  = "baseline"
	NamespaceExtension = "extension"
)

// Orphan kinds. Each names exactly one owner in the nine-owner list GOV-026
// requires (contract, model, property, capability, workflow-or-direct path,
// governance, test, evidence, deferment) plus the reverse-consumer and
// mutation-detection kinds.
const (
	KindIntentContract         = "intent_contract"
	KindIntentModel            = "intent_model"
	KindIntentProperty         = "intent_property"
	KindIntentGovernance       = "intent_governance"
	KindIntentCapability       = "intent_capability"
	KindIntentWorkflowOrDirect = "intent_workflow_or_direct"
	KindIntentTest             = "intent_test"
	KindIntentEvidence         = "intent_evidence"
	KindGapDeferment           = "gap_deferment"
	KindTodoDirectDangling     = "todo_direct_dangling"
	KindFalseMaturityClaim     = "false_maturity_claim"
)

// Intent is one accepted BusinessIntent contract reduced to the dimensions
// GOV-026 must prove are owned. Model/Property/Governance mirror
// intentmanifests.IntentDescriptor's Entities/Properties/Authority
// dimensions; intentcoverage re-checks their presence independently so a
// hand-built or future-funded-domain snapshot can never silently drop one
// and still claim CATALOGUED coverage.
type Intent struct {
	ID              string
	Namespace       string // NamespaceBaseline or NamespaceExtension
	ConformanceOnly bool
	Model           []string
	Property        []string
	Governance      []string
}

// CapabilityGap is one feature-intent-coverage.yaml row (INTENT-009) whose
// effective single bound intent is an accepted intent: the contract gap the
// intent's capability dimension must close. Disposition MERGED_INTO is the
// only disposition that satisfies the capability owner; every other
// disposition (DEFERRED_TO_INTENT, REVIEW, NON_MATERIAL) is retained
// honestly as conceptual-only coverage and can never be promoted past it.
// This is a narrower, graph-edge-shaped projection of that coverage row and
// is distinct from intentmanifests.ContractCompletenessGap, which validates
// a feature's own family/result-contract dimensions rather than whether an
// accepted intent owns delivery of it.
type CapabilityGap struct {
	FeatureID            string
	IntentID             string
	Disposition          string
	DispositionTarget    string
	DispositionRationale string
	Capability           string
	Governance           string
}

// WorkflowBinding names one catalogued workflow (planning/workflows/catalog.yaml,
// loaded the same way tools/planning/corpus loads it) that names an accepted
// intent, satisfying the workflow half of the workflow-or-direct-path owner.
type WorkflowBinding struct {
	WorkflowID string
	IntentID   string
}

// TodoBinding is the reverse consumer: one todo's INTENT CONTEXT and TDD
// contract projected onto this graph. Direct is the exact accepted-intent
// id(s) the todo's DIRECT field names ("none" and blanks are excluded);
// Sets are the todo's SETS domains, retained for the set-only reverse view.
// EvidenceTestNames are extracted from every Evidence-labelled line in the
// todo's block (not only the two hard-coded spellings
// todoregistry.Todo.Evidence recognizes), so a dated Evidence line is never
// silently dropped.
type TodoBinding struct {
	TodoID            string
	Direct            []string
	Sets              []string
	Test              string
	TestMatrix        map[string]string
	Done              bool
	EvidenceTestNames []string
	Retired           bool
}

// Snapshot is an in-memory, detached view of the four inputs this graph
// reconciles: the accepted intent catalog, the capability gaps that close
// against it, the workflow catalogue, and the todo backlog.
type Snapshot struct {
	Intents   []Intent
	Gaps      []CapabilityGap
	Workflows []WorkflowBinding
	Todos     []TodoBinding
}

// AllowlistEntry permits one exact, reviewed orphan for the current
// snapshot. A wildcard is intentionally not supported: new orphans must be
// visible rather than silently inheriting an old exception.
type AllowlistEntry struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
	ReviewDate string `json:"review_date"`
}

// Options controls reconciliation. TestNames is the executable-oracle name
// set (tools/planning/traceability.ScanTestNames); a nil or incomplete map
// simply means no TEST/Evidence name in it is treated as executable, which
// keeps Reconcile pure and fixture-friendly.
type Options struct {
	Allowlist []AllowlistEntry
	TestNames map[string]bool
}

// Orphan is a missing owner, a dangling reference, or a false maturity
// claim. Detail carries the complete path a reviewer needs to repair.
type Orphan struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

func (o Orphan) Key() string { return o.Kind + "|" + o.ID + "|" + o.Detail }

// AllowlistedOrphan preserves the exact orphan and its accountable owner.
type AllowlistedOrphan struct {
	Orphan
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
	ReviewDate string `json:"review_date"`
}

// GapEdge is one capability gap's projection onto the report.
type GapEdge struct {
	FeatureID   string     `json:"feature_id"`
	Disposition string     `json:"disposition"`
	Status      EdgeStatus `json:"status"`
}

// TodoEdge is one bound todo's projection onto an intent node. Status is
// already capped by the intent's own capability/dimension completeness.
type TodoEdge struct {
	TodoID string     `json:"todo_id"`
	Status EdgeStatus `json:"status"`
	Reason string     `json:"reason"`
}

// IntentNode is the forward-direction result for one accepted intent:
// accepted intent -> contract gaps -> atomic todos -> tests -> evidence,
// resolved into gap edges, todo edges, and one overall Status.
type IntentNode struct {
	IntentID  string     `json:"intent_id"`
	Namespace string     `json:"namespace"`
	Status    EdgeStatus `json:"status"`
	Reason    string     `json:"reason"`
	Gaps      []GapEdge  `json:"gaps"`
	Todos     []TodoEdge `json:"todos"`
}

// ReverseEdge is the reverse-direction result for one todo: todo -> exact
// intent or set -> outcome. ExactMatch is true only for a DIRECT binding to
// a real accepted intent; a SETS-only binding is retained with
// ExactMatch=false and is always capped at CONCEPTUAL, since a domain-level
// set is not an exact BusinessIntent claim.
type ReverseEdge struct {
	TodoID     string     `json:"todo_id"`
	IntentID   string     `json:"intent_id,omitempty"`
	Sets       []string   `json:"sets,omitempty"`
	ExactMatch bool       `json:"exact_match"`
	Outcome    EdgeStatus `json:"outcome"`
	Reason     string     `json:"reason"`
}

// Report is the one graph result consumed by human reports and the command.
type Report struct {
	SchemaVersion        int                           `json:"schema_version"`
	BaselineIntentCount  int                           `json:"baseline_intent_count"`
	ExtensionIntentCount int                           `json:"extension_intent_count"`
	GapCount             int                           `json:"gap_count"`
	WorkflowBindingCount int                           `json:"workflow_binding_count"`
	TodoBindingCount     int                           `json:"todo_binding_count"`
	Intents              []IntentNode                  `json:"intents"`
	ReverseEdges         []ReverseEdge                 `json:"reverse_edges"`
	StatusCounts         map[string]map[EdgeStatus]int `json:"status_counts"`
	Orphans              []Orphan                      `json:"orphans"`
	Allowlisted          []AllowlistedOrphan           `json:"allowlisted_orphans"`
	NewOrphans           []Orphan                      `json:"new_orphans"`
	Digest               string                        `json:"digest"`
}

// Violations returns only unallowlisted orphan paths.
func (r Report) Violations() []Orphan { return append([]Orphan(nil), r.NewOrphans...) }

// JSON returns deterministic indented report JSON.
func (r Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal intentcoverage report: %w", err)
	}
	return append(b, '\n'), nil
}

func (r Report) JSONString() string { b, _ := r.JSON(); return string(b) }

// Reconcile builds the deterministic bidirectional graph from snap. It is a
// pure function: no I/O, no clock, no map-iteration-order leakage.
func Reconcile(snap Snapshot, options ...Options) Report {
	var opt Options
	if len(options) > 0 {
		opt = options[0]
	}
	snap = cloneSnapshot(snap)

	intentByID := make(map[string]Intent, len(snap.Intents))
	for _, in := range snap.Intents {
		intentByID[in.ID] = in
	}

	var orphans []Orphan

	gapsByIntent := make(map[string][]CapabilityGap)
	for _, g := range snap.Gaps {
		if _, ok := intentByID[g.IntentID]; !ok {
			orphans = append(orphans, Orphan{Kind: KindIntentContract, ID: g.FeatureID,
				Detail: fmt.Sprintf("capability gap %s is bound to %q, which is not an accepted intent", g.FeatureID, g.IntentID)})
			continue
		}
		gapsByIntent[g.IntentID] = append(gapsByIntent[g.IntentID], g)
	}

	workflowsByIntent := make(map[string][]string)
	for _, w := range snap.Workflows {
		if _, ok := intentByID[w.IntentID]; !ok {
			orphans = append(orphans, Orphan{Kind: KindIntentContract, ID: w.WorkflowID,
				Detail: fmt.Sprintf("workflow %s names %q, which is not an accepted intent", w.WorkflowID, w.IntentID)})
			continue
		}
		workflowsByIntent[w.IntentID] = append(workflowsByIntent[w.IntentID], w.WorkflowID)
	}

	todosByIntent := make(map[string][]TodoBinding)
	for _, t := range snap.Todos {
		for _, d := range t.Direct {
			if _, ok := intentByID[d]; !ok {
				orphans = append(orphans, Orphan{Kind: KindTodoDirectDangling, ID: t.TodoID,
					Detail: fmt.Sprintf("todo %s DIRECT names %q, which is not an accepted intent", t.TodoID, d)})
				continue
			}
			todosByIntent[d] = append(todosByIntent[d], t)
		}
	}

	// capUnlocked[id] is true once the intent's capability and dimension
	// owners are both present, which is the only condition that permits
	// this intent's status (in either direction) to rise past CONCEPTUAL.
	capUnlocked := make(map[string]bool, len(snap.Intents))
	for _, in := range snap.Intents {
		hasMerged := false
		for _, g := range gapsByIntent[in.ID] {
			if g.Disposition == "MERGED_INTO" {
				hasMerged = true
				break
			}
		}
		dimensionsComplete := len(in.Model) > 0 && len(in.Property) > 0 && len(in.Governance) > 0
		capUnlocked[in.ID] = hasMerged && dimensionsComplete
	}

	var nodes []IntentNode
	statusCounts := make(map[string]map[EdgeStatus]int)
	for _, in := range snap.Intents {
		node := buildIntentNode(in, gapsByIntent[in.ID], workflowsByIntent[in.ID], todosByIntent[in.ID], capUnlocked[in.ID], opt.TestNames, &orphans)
		nodes = append(nodes, node)
		if statusCounts[in.Namespace] == nil {
			statusCounts[in.Namespace] = map[EdgeStatus]int{}
		}
		statusCounts[in.Namespace][node.Status]++
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].IntentID < nodes[j].IntentID })

	reverse := buildReverseEdges(snap.Todos, intentByID, capUnlocked, opt.TestNames)

	report := Report{
		SchemaVersion:        schemaVersion,
		Intents:              nodes,
		ReverseEdges:         reverse,
		StatusCounts:         statusCounts,
		GapCount:             len(snap.Gaps),
		WorkflowBindingCount: len(snap.Workflows),
		TodoBindingCount:     len(snap.Todos),
	}
	for _, in := range snap.Intents {
		switch in.Namespace {
		case NamespaceExtension:
			report.ExtensionIntentCount++
		default:
			report.BaselineIntentCount++
		}
	}

	sortOrphans(orphans)
	report.Allowlisted, report.NewOrphans = classifyOrphans(orphans, opt.Allowlist)
	report.Orphans = orphans
	report.Digest = reportDigest(report)
	return report
}

// buildIntentNode resolves one accepted intent's forward chain: contract
// gaps -> atomic todos -> tests -> evidence.
func buildIntentNode(in Intent, gaps []CapabilityGap, workflowIDs []string, boundTodos []TodoBinding, unlocked bool, testNames map[string]bool, orphans *[]Orphan) IntentNode {
	node := IntentNode{IntentID: in.ID, Namespace: in.Namespace, Status: Conceptual, Reason: "accepted intent exists in the catalog"}

	if len(in.Model) == 0 {
		*orphans = append(*orphans, Orphan{Kind: KindIntentModel, ID: in.ID, Detail: "accepted intent has no model/entity owner"})
	}
	if len(in.Property) == 0 {
		*orphans = append(*orphans, Orphan{Kind: KindIntentProperty, ID: in.ID, Detail: "accepted intent has no property owner"})
	}
	if len(in.Governance) == 0 {
		*orphans = append(*orphans, Orphan{Kind: KindIntentGovernance, ID: in.ID, Detail: "accepted intent has no governance/authority owner"})
	}

	sortedGaps := append([]CapabilityGap(nil), gaps...)
	sort.Slice(sortedGaps, func(i, j int) bool { return sortedGaps[i].FeatureID < sortedGaps[j].FeatureID })
	hasMerged := false
	for _, g := range sortedGaps {
		edge := GapEdge{FeatureID: g.FeatureID, Disposition: g.Disposition}
		switch g.Disposition {
		case "MERGED_INTO":
			edge.Status = Catalogued
			hasMerged = true
		case "DEFERRED_TO_INTENT", "REVIEW", "NON_MATERIAL":
			edge.Status = Conceptual
			if strings.TrimSpace(g.DispositionTarget) == "" || strings.TrimSpace(g.DispositionRationale) == "" {
				*orphans = append(*orphans, Orphan{Kind: KindGapDeferment, ID: g.FeatureID,
					Detail: fmt.Sprintf("gap %s bound to %s has disposition %s without a recorded target and rationale", g.FeatureID, in.ID, g.Disposition)})
			}
		default:
			edge.Status = Conceptual
			*orphans = append(*orphans, Orphan{Kind: KindGapDeferment, ID: g.FeatureID,
				Detail: fmt.Sprintf("gap %s bound to %s has an unrecognized disposition %q", g.FeatureID, in.ID, g.Disposition)})
		}
		node.Gaps = append(node.Gaps, edge)
	}
	if !hasMerged {
		*orphans = append(*orphans, Orphan{Kind: KindIntentCapability, ID: in.ID, Detail: "accepted intent has no MERGED_INTO capability gap"})
	}

	hasDirect := len(boundTodos) > 0
	hasWorkflow := len(workflowIDs) > 0
	if !hasDirect && !hasWorkflow {
		*orphans = append(*orphans, Orphan{Kind: KindIntentWorkflowOrDirect, ID: in.ID,
			Detail: "accepted intent has no workflow binding and no todo names it directly"})
	}

	sortedTodos := append([]TodoBinding(nil), boundTodos...)
	sort.Slice(sortedTodos, func(i, j int) bool { return sortedTodos[i].TodoID < sortedTodos[j].TodoID })

	cap := Verified
	if !unlocked {
		cap = Conceptual
	}
	base := Conceptual
	if unlocked {
		base = Catalogued
	}
	best := base
	bestNaturalRank := -1
	for _, t := range sortedTodos {
		natural, reason := naturalTodoStatus(t, testNames)
		if natural.rank() > bestNaturalRank {
			bestNaturalRank = natural.rank()
		}
		displayed := natural
		if displayed.rank() > cap.rank() {
			displayed = cap
			reason = reason + " (capped: capability/dimension owner incomplete)"
		}
		node.Todos = append(node.Todos, TodoEdge{TodoID: t.TodoID, Status: displayed, Reason: reason})
		if displayed.rank() > best.rank() {
			best = displayed
			node.Reason = reason
		}
	}
	node.Status = best
	if len(sortedTodos) == 0 {
		if unlocked {
			node.Reason = "capability gap catalogued; no bound todo yet"
		} else {
			node.Reason = "accepted intent exists; capability or contract-dimension owner incomplete"
		}
	}

	if hasDirect && bestNaturalRank < Contracted.rank() {
		*orphans = append(*orphans, Orphan{Kind: KindIntentTest, ID: in.ID,
			Detail: "accepted intent has a directly-bound todo but none of its TEST fields is an executable oracle in the repository"})
	} else if hasDirect && bestNaturalRank < Implemented.rank() {
		*orphans = append(*orphans, Orphan{Kind: KindIntentEvidence, ID: in.ID,
			Detail: "accepted intent has a directly-bound, test-backed todo but none of them is done with evidence naming an existing test"})
	}

	return node
}

// naturalTodoStatus computes one todo's uncapped ladder position: bound
// (CATALOGUED) -> executable oracle (CONTRACTED) -> done with evidence
// naming a real test (IMPLEMENTED) -> that test registered in the todo's
// own TEST MATRIX (VERIFIED). It never applies the intent-level capability
// cap, so intent_test/intent_evidence orphan detection is not muddied by a
// separate, already-reported capability gap.
func naturalTodoStatus(t TodoBinding, testNames map[string]bool) (EdgeStatus, string) {
	if strings.TrimSpace(t.Test) == "" {
		return Catalogued, "todo names the intent directly but has no TEST field"
	}
	if !testNames[t.Test] {
		return Catalogued, fmt.Sprintf("todo TEST %q is not an executable oracle in the scanned tree", t.Test)
	}
	if !t.Done {
		return Contracted, "todo TEST is an executable oracle; todo is not yet done"
	}
	named := firstExistingTest(t.EvidenceTestNames, testNames)
	if named == "" {
		return Contracted, "todo is done but its Evidence names no test that exists in the repository"
	}
	if inMatrix(named, t.TestMatrix) {
		return Verified, fmt.Sprintf("todo is done and Evidence test %s is registered in its TEST MATRIX", named)
	}
	return Implemented, fmt.Sprintf("todo is done and Evidence names existing test %s, which is not in its TEST MATRIX", named)
}

func firstExistingTest(names []string, testNames map[string]bool) string {
	for _, n := range names {
		if testNames[n] {
			return n
		}
	}
	return ""
}

func inMatrix(name string, matrix map[string]string) bool {
	for _, v := range matrix {
		if v == name {
			return true
		}
	}
	return false
}

// buildReverseEdges resolves the reverse direction: todo -> exact intent or
// set -> outcome. A SETS-only binding is always capped at CONCEPTUAL; an
// exact DIRECT binding reuses the same capability/dimension cap and natural
// todo status as the forward direction, so the two views can never disagree
// about what a given (intent, todo) pair has actually proven.
func buildReverseEdges(todos []TodoBinding, intentByID map[string]Intent, capUnlocked map[string]bool, testNames map[string]bool) []ReverseEdge {
	var out []ReverseEdge
	for _, t := range todos {
		if len(t.Direct) == 0 {
			out = append(out, ReverseEdge{TodoID: t.TodoID, Sets: append([]string(nil), t.Sets...), ExactMatch: false,
				Outcome: Conceptual, Reason: "todo binds only a domain SETS classification, not an exact accepted intent"})
			continue
		}
		directs := append([]string(nil), t.Direct...)
		sort.Strings(directs)
		for _, d := range directs {
			if _, ok := intentByID[d]; !ok {
				continue // already recorded as KindTodoDirectDangling
			}
			natural, reason := naturalTodoStatus(t, testNames)
			outcome := natural
			if !capUnlocked[d] && outcome.rank() > Conceptual.rank() {
				outcome = Conceptual
				reason = reason + " (capped: capability/dimension owner incomplete)"
			}
			out = append(out, ReverseEdge{TodoID: t.TodoID, IntentID: d, ExactMatch: true, Outcome: outcome, Reason: reason})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TodoID != out[j].TodoID {
			return out[i].TodoID < out[j].TodoID
		}
		return out[i].IntentID < out[j].IntentID
	})
	return out
}

// Validate re-derives the report from snap and returns an orphan for every
// reported status (forward or reverse) that claims MORE than the raw
// evidence supports. Because Reconcile always derives status bottom-up, a
// report built by Reconcile itself can never fail Validate; it exists so a
// report that was hand-tampered (or produced by a future caller that
// bypasses Reconcile) is still caught, proving the false-maturity guard is
// enforced on the data, not merely by convention in one code path.
func Validate(snap Snapshot, report Report, options ...Options) []Orphan {
	fresh := Reconcile(snap, options...)

	freshByIntent := make(map[string]EdgeStatus, len(fresh.Intents))
	for _, n := range fresh.Intents {
		freshByIntent[n.IntentID] = n.Status
	}
	freshByPair := make(map[string]EdgeStatus, len(fresh.ReverseEdges))
	for _, e := range fresh.ReverseEdges {
		if e.ExactMatch {
			freshByPair[e.TodoID+"|"+e.IntentID] = e.Outcome
		}
	}

	var out []Orphan
	for _, n := range report.Intents {
		if n.Status.rank() > freshByIntent[n.IntentID].rank() {
			out = append(out, Orphan{Kind: KindFalseMaturityClaim, ID: n.IntentID,
				Detail: fmt.Sprintf("report claims intent status %s but raw evidence supports at most %s", n.Status, freshByIntent[n.IntentID])})
		}
	}
	for _, e := range report.ReverseEdges {
		if !e.ExactMatch {
			continue
		}
		key := e.TodoID + "|" + e.IntentID
		if e.Outcome.rank() > freshByPair[key].rank() {
			out = append(out, Orphan{Kind: KindFalseMaturityClaim, ID: key,
				Detail: fmt.Sprintf("reverse edge claims outcome %s but raw evidence supports at most %s", e.Outcome, freshByPair[key])})
		}
	}
	sortOrphans(out)
	return out
}

// BuildAllowlist creates a reviewed baseline for a named snapshot. Owners
// are assigned by orphan kind; every entry remains exact. Intended for
// deliberate baseline updates via `-update-allowlist`, never for normal CI.
func BuildAllowlist(orphans []Orphan, reviewDate string) []AllowlistEntry {
	owners := map[string]string{
		KindIntentContract:         "intent-governance",
		KindIntentModel:            "model-governance",
		KindIntentProperty:         "model-governance",
		KindIntentGovernance:       "intent-governance",
		KindIntentCapability:       "intake-governance",
		KindIntentWorkflowOrDirect: "backlog-governance",
		KindIntentTest:             "test-governance",
		KindIntentEvidence:         "test-governance",
		KindGapDeferment:           "intake-governance",
		KindTodoDirectDangling:     "backlog-governance",
	}
	reasons := map[string]string{
		KindIntentContract:         "capability gap or workflow references an intent not yet in the accepted catalog",
		KindIntentModel:            "accepted intent is missing its model/entity dimension pending catalog repair",
		KindIntentProperty:         "accepted intent is missing its property dimension pending catalog repair",
		KindIntentGovernance:       "accepted intent is missing its governance/authority dimension pending catalog repair",
		KindIntentCapability:       "accepted intent has no MERGED_INTO capability yet; intake/implementation is pending",
		KindIntentWorkflowOrDirect: "accepted intent has no workflow or directly-bound todo yet; delivery is pending",
		KindIntentTest:             "accepted intent's bound todo(s) have no executable oracle yet; implementation is pending",
		KindIntentEvidence:         "accepted intent's bound todo(s) are not yet done with resolvable evidence",
		KindGapDeferment:           "capability gap disposition requires a recorded target/rationale repair",
		KindTodoDirectDangling:     "todo DIRECT field requires repair to an accepted intent id",
	}
	seen := make(map[string]bool, len(orphans))
	var entries []AllowlistEntry
	for _, o := range orphans {
		if seen[o.Key()] {
			continue
		}
		seen[o.Key()] = true
		owner := owners[o.Kind]
		if owner == "" {
			owner = "intentcoverage-governance"
		}
		entries = append(entries, AllowlistEntry{Kind: o.Kind, ID: o.ID, Owner: owner, Reason: reasons[o.Kind], ReviewDate: reviewDate})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}

func classifyOrphans(orphans []Orphan, allowlist []AllowlistEntry) ([]AllowlistedOrphan, []Orphan) {
	allow := make(map[string]AllowlistEntry, len(allowlist))
	for _, item := range allowlist {
		allow[item.Kind+"|"+item.ID] = item
	}
	var accepted []AllowlistedOrphan
	var fresh []Orphan
	for _, o := range orphans {
		if item, ok := allow[o.Kind+"|"+o.ID]; ok {
			accepted = append(accepted, AllowlistedOrphan{Orphan: o, Owner: item.Owner, Reason: item.Reason, ReviewDate: item.ReviewDate})
		} else {
			fresh = append(fresh, o)
		}
	}
	return accepted, fresh
}

func sortOrphans(orphans []Orphan) {
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Kind != orphans[j].Kind {
			return orphans[i].Kind < orphans[j].Kind
		}
		if orphans[i].ID != orphans[j].ID {
			return orphans[i].ID < orphans[j].ID
		}
		return orphans[i].Detail < orphans[j].Detail
	})
}

func reportDigest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	return canonicalbytes.Digest(b)
}

func cloneSnapshot(s Snapshot) Snapshot {
	out := Snapshot{
		Intents:   append([]Intent(nil), s.Intents...),
		Gaps:      append([]CapabilityGap(nil), s.Gaps...),
		Workflows: append([]WorkflowBinding(nil), s.Workflows...),
		Todos:     make([]TodoBinding, len(s.Todos)),
	}
	for i, t := range s.Todos {
		out.Todos[i] = TodoBinding{
			TodoID:            t.TodoID,
			Direct:            append([]string(nil), t.Direct...),
			Sets:              append([]string(nil), t.Sets...),
			Test:              t.Test,
			TestMatrix:        t.TestMatrix,
			Done:              t.Done,
			EvidenceTestNames: append([]string(nil), t.EvidenceTestNames...),
			Retired:           t.Retired,
		}
	}
	sort.Slice(out.Intents, func(i, j int) bool { return out.Intents[i].ID < out.Intents[j].ID })
	sort.Slice(out.Gaps, func(i, j int) bool { return out.Gaps[i].FeatureID < out.Gaps[j].FeatureID })
	sort.Slice(out.Workflows, func(i, j int) bool { return out.Workflows[i].WorkflowID < out.Workflows[j].WorkflowID })
	sort.Slice(out.Todos, func(i, j int) bool { return out.Todos[i].TodoID < out.Todos[j].TodoID })
	return out
}
