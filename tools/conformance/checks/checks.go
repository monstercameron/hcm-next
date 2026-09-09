// Package checks validates a parsed model.Workflow against the ten core
// plus three structural primitive vocabulary in
// planning/specs/workflow-runtime.md and the boundary rules in
// planning/workflows/_engine/workflow-context-contract.md.
//
// Every check returns PASS, FAIL, or UNKNOWN. UNKNOWN is a first-class,
// deliberate result: when a document's structure genuinely does not give
// the check anything to evaluate (for example, a structural-primitive
// gating check on a document that never mentions PARALLEL/JOIN/
// SUBWORKFLOW), the check says so instead of manufacturing a PASS that
// would look like verified conformance for a property nobody exercised.
package checks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

// Status is one of the three deterministic check outcomes.
type Status string

const (
	Pass    Status = "PASS"
	Fail    Status = "FAIL"
	Unknown Status = "UNKNOWN"
)

// Result is one check's outcome for one workflow.
type Result struct {
	ID     string
	Name   string
	Status Status
	Detail string
	Refs   []string
}

// Summary counts a workflow's check results by status.
type Summary struct {
	Pass    int
	Fail    int
	Unknown int
}

// Summarize tallies results by Status.
func Summarize(results []Result) Summary {
	var s Summary
	for _, r := range results {
		switch r.Status {
		case Pass:
			s.Pass++
		case Fail:
			s.Fail++
		default:
			s.Unknown++
		}
	}
	return s
}

var fiveCompletionKeys = []string{"RequestState", "ExecutionState", "BusinessState", "ConsistencyState", "ObligationState"}

func isFiveCompletionKey(k string) bool {
	for _, want := range fiveCompletionKeys {
		if k == want {
			return true
		}
	}
	return false
}

type checkFunc func(*model.Workflow, *vocab.Vocabulary) Result

// registry lists every check in stable CHK-NNN order. Evaluate re-sorts by
// ID regardless, so registry order is documentation, not a determinism
// dependency.
var registry = []checkFunc{
	chkRetiredPrimitiveNotUsedAsNodeType,
	chkStructuralPrimitiveGated,
	chkSafePointConvention,
	chkCapabilityPresent,
	chkApprovalResolverPresence,
	chkCompletionDimensions,
	chkScenariosPresent,
	chkDiagramStepsNonEmpty,
	chkPreconditions,
	chkNoMutableGlobalState,
	chkAtomicCoreSeparation,
}

// Evaluate runs every registered check against wf and returns the results
// sorted by check ID.
func Evaluate(wf *model.Workflow, v *vocab.Vocabulary) []Result {
	results := make([]Result, 0, len(registry))
	for _, fn := range registry {
		results = append(results, fn(wf, v))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

const runtimeSpecRef = "planning/specs/workflow-runtime.md#kernel-vocabulary"
const contextApprovalRef = "planning/workflows/_engine/workflow-context-contract.md#7-human-identity-approvals-and-work"
const contextCompletionRef = "planning/workflows/_engine/workflow-context-contract.md#10-completion-intervention-and-explainability"
const runtimeCoreArchRef = "planning/specs/workflow-runtime.md#authoritative-core-versus-downstream-effects"

// CHK-001: a retired step-type name (CHECKPOINT, RULE, AGENT, DOCUMENT)
// must never be used as if it were a node type the way the compiled-example
// syntax in workflow-runtime.md writes primitives (e.g. "CAPABILITY
// people.worker.snapshot"). Those four names are expressed as attributes or
// as a CAPABILITY/DECISION call instead; see the Retired-name table.
var retiredAsNodeTypeRegex = regexp.MustCompile(`(?m)^\s*(CHECKPOINT|RULE|AGENT|DOCUMENT)\b`)

func chkRetiredPrimitiveNotUsedAsNodeType(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-001", "RetiredPrimitiveNameNotUsedAsNodeType"
	m := retiredAsNodeTypeRegex.FindStringSubmatch(wf.RawText)
	if m == nil {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: "no retired node-type keyword (CHECKPOINT, RULE, AGENT, DOCUMENT) found at the start of a line",
			Refs:   []string{runtimeSpecRef}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: fmt.Sprintf("retired name %q is used as if it were a node type; the vocabulary expresses it as: %s", m[1], v.ExpressedAs(m[1])),
		Refs:   []string{runtimeSpecRef}}
}

// CHK-002: every literal mention of a structural primitive (PARALLEL,
// JOIN, SUBWORKFLOW) must carry a nearby P1B/gate annotation, since
// workflow-runtime.md gates all three "After P1B evidence" and the
// reference workflows are P1A-depth fixtures.
var gatingContextRegex = regexp.MustCompile(`(?i)p1b|after p1b evidence|gate`)

func chkStructuralPrimitiveGated(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-002", "StructuralPrimitiveGatedAnnotation"
	structural := map[string]bool{}
	for _, n := range v.StructuralNames() {
		structural[n] = true
	}
	var hits []model.PrimitiveHit
	for _, h := range wf.PrimitiveHits {
		if structural[h.Primitive] {
			hits = append(hits, h)
		}
	}
	if len(hits) == 0 {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "document does not mention any structural primitive (PARALLEL, JOIN, SUBWORKFLOW); check not applicable",
			Refs:   []string{runtimeSpecRef}}
	}
	for _, h := range hits {
		if !gatingContextRegex.MatchString(h.Context) {
			return Result{ID: id, Name: name, Status: Fail,
				Detail: fmt.Sprintf("line %d mentions structural primitive %s without a nearby P1B/gate annotation: %q", h.Line, h.Primitive, h.Context),
				Refs:   []string{runtimeSpecRef}}
		}
	}
	return Result{ID: id, Name: name, Status: Pass,
		Detail: fmt.Sprintf("%d structural-primitive mention(s) all carry a P1B/gate annotation", len(hits)),
		Refs:   []string{runtimeSpecRef}}
}

// CHK-003: when a document discusses safe points, it must use the
// "[safe_point ...]" node-attribute convention from the compiled-example
// syntax, not a bare CHECKPOINT keyword (the retired name for the same
// concept).
var safePointAttrRegex = regexp.MustCompile(`\[safe_point`)
var safePointWordRegex = regexp.MustCompile(`(?i)safe.point`)

func chkSafePointConvention(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-003", "SafePointAttributeConvention"
	if !safePointWordRegex.MatchString(wf.RawText) {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "document does not mention safe points; check not applicable",
			Refs:   []string{runtimeSpecRef}}
	}
	if safePointAttrRegex.MatchString(wf.RawText) {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: "safe point expressed as a [safe_point ...] node attribute, matching the retired CHECKPOINT -> safe_point:true mapping",
			Refs:   []string{runtimeSpecRef}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: "document discusses safe points but never in the [safe_point ...] attribute style",
		Refs:   []string{runtimeSpecRef}}
}

// CHK-004: a workflow must invoke at least one governed, dotted-namespace
// capability (the CAPABILITY primitive's whole reason to exist); a
// workflow with zero capability references is not modeling delegated,
// typed behavior at all.
func chkCapabilityPresent(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-004", "CapabilityInvocationPresent"
	if len(wf.Capabilities) == 0 {
		return Result{ID: id, Name: name, Status: Fail,
			Detail: "no dotted governed-capability reference (e.g. people.worker.read) found in any diagram",
			Refs:   []string{runtimeSpecRef}}
	}
	return Result{ID: id, Name: name, Status: Pass,
		Detail: fmt.Sprintf("%d distinct capability reference(s) found", len(wf.Capabilities)),
		Refs:   []string{runtimeSpecRef}}
}

// CHK-005: when a document has an approval-related section, it must name
// at least one resolver expression or cardinality keyword, per the
// context contract's "Resolvers may target a named principal, role,
// scoped role, relationship, group, ... or composition."
var approvalHeadingRegex = regexp.MustCompile(`(?i)approv`)

func chkApprovalResolverPresence(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-005", "ApprovalResolverPresence"
	if wf.Kind == model.KindSuiteSection {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "suite-section workflows are parsed at scenario-list depth only; section-heading analysis is not performed for this kind",
			Refs:   []string{contextApprovalRef}}
	}
	hasApprovalHeading := false
	for _, s := range wf.Sections {
		if approvalHeadingRegex.MatchString(s.Heading) {
			hasApprovalHeading = true
			break
		}
	}
	if !hasApprovalHeading {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "document has no approval-related section heading; check not applicable",
			Refs:   []string{contextApprovalRef}}
	}
	if len(wf.Actors) == 0 && len(wf.Cardinalities) == 0 {
		return Result{ID: id, Name: name, Status: Fail,
			Detail: "document has an approval-related section but no resolver expression or cardinality keyword",
			Refs:   []string{contextApprovalRef}}
	}
	return Result{ID: id, Name: name, Status: Pass,
		Detail: fmt.Sprintf("%d resolver expression(s), %d cardinality keyword(s) found", len(wf.Actors), len(wf.Cardinalities)),
		Refs:   []string{contextApprovalRef}}
}

// CHK-006: a completion-state block must declare exactly the intent
// kernel's five dimensions (RequestState, ExecutionState, BusinessState,
// ConsistencyState, ObligationState) -- no fewer, and no invented sixth
// dimension, per workflow-context-contract.md section 10.
func chkCompletionDimensions(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-006", "CompletionFiveDimensionsDeclared"
	if len(wf.ExpectedOutcomes) == 0 {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "no completion-state block (RequestState/ExecutionState/...) found in this document",
			Refs:   []string{contextCompletionRef}}
	}
	seen := map[string]bool{}
	for _, kv := range wf.ExpectedOutcomes {
		seen[kv.Key] = true
	}
	var missing []string
	for _, k := range fiveCompletionKeys {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	var extra []string
	for k := range seen {
		if !isFiveCompletionKey(k) {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: "all five intent completion dimensions declared, with no undeclared sixth dimension",
			Refs:   []string{contextCompletionRef}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: fmt.Sprintf("missing=%v extra=%v", missing, extra),
		Refs:   []string{contextCompletionRef}}
}

// CHK-007: a workflow must declare its required conformance scenarios
// (CONF-001's own RED criterion: a fixture lacking expected events/states/
// effects must be visible, not silently accepted).
func chkScenariosPresent(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-007", "ScenarioSectionPresent"
	if len(wf.Scenarios) > 0 {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: fmt.Sprintf("%d conformance scenario(s) declared", len(wf.Scenarios)),
			Refs:   []string{"planning/todos.md#CONF-001"}}
	}
	if wf.Kind == model.KindSuiteSection {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "this suite section states its scenarios as prose rather than a structured list; automated extraction found none",
			Refs:   []string{"planning/todos.md#CONF-001"}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: "no 'Required conformance scenarios' numbered list found",
		Refs:   []string{"planning/todos.md#CONF-001"}}
}

// CHK-008: a workflow document should model an actual execution flow, not
// only prose -- its largest diagram should carry a handful of real steps.
func chkDiagramStepsNonEmpty(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-008", "DiagramStepsNonEmpty"
	total, best := 0, 0
	for _, d := range wf.Diagrams {
		total += len(d.Steps)
		if len(d.Steps) > best {
			best = len(d.Steps)
		}
	}
	if len(wf.Diagrams) == 0 {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "document has no fenced diagram blocks",
			Refs:   []string{runtimeSpecRef}}
	}
	if best >= 3 {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: fmt.Sprintf("largest diagram has %d step line(s) (%d total across %d diagram(s))", best, total, len(wf.Diagrams)),
			Refs:   []string{runtimeSpecRef}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: fmt.Sprintf("largest diagram has only %d step line(s); an execution-flow document should model at least 3", best),
		Refs:   []string{runtimeSpecRef}}
}

// CHK-009: a standalone workflow document must discuss execution-time
// revalidation somewhere, per the Phase 1 Acceptance Contract's "Exact
// proposal binding plus current approver-authority re-evaluation after
// relationship change."
var revalidatWordRegex = regexp.MustCompile(`(?i)revalidat`)

func chkPreconditions(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-009", "PreconditionsOrRevalidationDeclared"
	if wf.Kind == model.KindSuiteSection {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "suite-section workflows are conceptual summaries; explicit revalidation content is not expected at this depth",
			Refs:   []string{"planning/specs/workflow-runtime.md#phase-1-acceptance-contract"}}
	}
	if len(wf.Preconditions) > 0 {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: fmt.Sprintf("%d explicit revalidation precondition line(s) extracted", len(wf.Preconditions)),
			Refs:   []string{"planning/specs/workflow-runtime.md#phase-1-acceptance-contract"}}
	}
	if revalidatWordRegex.MatchString(wf.RawText) {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: "document discusses revalidation in prose even though no dedicated revalidation diagram was found",
			Refs:   []string{"planning/specs/workflow-runtime.md#phase-1-acceptance-contract"}}
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: "document never discusses execution-time revalidation",
		Refs:   []string{"planning/specs/workflow-runtime.md#phase-1-acceptance-contract"}}
}

// CHK-010: workflow-runtime.md is explicit that "the runtime does not
// expose a global mutable map[string]any"; a document that describes such
// a structure without negating it would contradict that rule.
var mapStringAnyRegex = regexp.MustCompile(`map\[string\]`)
var negationNearbyRegex = regexp.MustCompile(`(?i)\b(not|never|does not|doesn't)\b`)

func chkNoMutableGlobalState(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-010", "NoUndeclaredMutableGlobalState"
	for i, line := range strings.Split(wf.RawText, "\n") {
		if mapStringAnyRegex.MatchString(line) && !negationNearbyRegex.MatchString(line) {
			return Result{ID: id, Name: name, Status: Fail,
				Detail: fmt.Sprintf("line %d describes a map[string]... structure without an explicit negation, contradicting the no-global-mutable-map rule", i+1),
				Refs:   []string{runtimeSpecRef}}
		}
	}
	return Result{ID: id, Name: name, Status: Pass,
		Detail: "no undeclared global mutable map[string]... pattern found",
		Refs:   []string{runtimeSpecRef}}
}

// CHK-011: when a workflow describes a downstream external effect
// (payroll, IAM, access, learning, messaging, benefits), it must
// separately name an atomic/commit step and a later observe/reconcile
// step, per the AUTHORITATIVE_CORE vs. DOWNSTREAM_EFFECT classification.
var externalEffectKeywordRegex = regexp.MustCompile(`(?i)payroll|\biam\b|access|learning|messaging|benefits`)
var commitKeywordRegex = regexp.MustCompile(`(?i)acid|commit`)
var observeReconcileKeywordRegex = regexp.MustCompile(`(?i)observe|reconcil`)

func chkAtomicCoreSeparation(wf *model.Workflow, v *vocab.Vocabulary) Result {
	const id, name = "CHK-011", "AtomicCoreVsDownstreamEffectSeparation"
	if !externalEffectKeywordRegex.MatchString(wf.RawText) {
		return Result{ID: id, Name: name, Status: Unknown,
			Detail: "document does not describe any downstream external effect (payroll, IAM, access, learning, messaging, benefits)",
			Refs:   []string{runtimeCoreArchRef}}
	}
	hasCommit := commitKeywordRegex.MatchString(wf.RawText)
	hasObserve := observeReconcileKeywordRegex.MatchString(wf.RawText)
	if hasCommit && hasObserve {
		return Result{ID: id, Name: name, Status: Pass,
			Detail: "document separates an atomic/commit step from a subsequent observe/reconcile step for its external effects",
			Refs:   []string{runtimeCoreArchRef}}
	}
	var missing []string
	if !hasCommit {
		missing = append(missing, "atomic/commit language")
	}
	if !hasObserve {
		missing = append(missing, "observe/reconcile language")
	}
	return Result{ID: id, Name: name, Status: Fail,
		Detail: fmt.Sprintf("document describes external effects but is missing: %s", strings.Join(missing, ", ")),
		Refs:   []string{runtimeCoreArchRef}}
}
