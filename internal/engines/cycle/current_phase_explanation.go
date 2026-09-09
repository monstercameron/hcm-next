package cycle

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const (
	CurrentPhaseExplanationIntentType    = "hcmnext.cycle.explain_current_phase"
	CurrentPhaseExplanationIntentVersion = "v1"
	CurrentPhaseExplanationRulePack      = "cycle.explain_current_phase/2026.1"
)

var (
	ErrCurrentPhaseInvalid = errors.New("cycle: current phase explanation is invalid")
	ErrNoActivePhase       = errors.New("cycle: no phase is active at the requested instant")
	ErrCutoffExplanation   = errors.New("cycle: cutoff resolution is invalid for explanation")
)

// PhaseWindow is the exact half-open window in which a phase is active.
type PhaseWindow struct {
	PhaseID string
	Name    string
	Start   time.Time
	End     time.Time
}

// PhaseCapability records whether the active phase permits an operation. The
// list is closed over the standard cycle operations and the phase's declared
// operation tokens, so a caller sees both permission and refusal explicitly.
type PhaseCapability struct {
	Operation string
	Permitted bool
	Reason    string
}

// TransitionAvailability is the typed lifecycle answer for one possible
// transition from the current cycle state.
type TransitionAvailability struct {
	Operation TransitionOperation
	Available bool
	Reason    string
}

// CurrentPhaseExplanation is a pure, digested explanation of one governed
// cycle at one explicit instant. It contains no population members and no
// storage side effects; the revision digest and state are the only cycle
// identity disclosed.
type CurrentPhaseExplanation struct {
	CycleRevisionDigest  string
	CycleRevisionVersion string
	State                LifecycleState
	Sequence             int
	EffectiveAt          time.Time
	KnownAt              time.Time

	HasPhase             bool
	Phase                CompiledPhase
	Window               PhaseWindow
	Cutoffs              []CutoffResolution
	Capabilities         []PhaseCapability
	PermittedOperations  []string
	ForbiddenOperations  []string
	Transitions          []TransitionAvailability
	AvailableTransitions []TransitionOperation
	Locks                []string
	Exceptions           []string

	InputsDigest  string
	ResultDigest  string
	Rendering     string
	HumanReadable string
}

// CyclePhaseExplanation is a descriptive alias for callers that use the
// longer engine terminology.
type CyclePhaseExplanation = CurrentPhaseExplanation

var standardCycleOperations = []string{
	string(OperationOpen), string(OperationClose), string(OperationLock), string(OperationReopen),
	OperationRebindPopulation, OperationAdmitMember, OperationRemoveMember,
	OperationAdmitLateEntrant, OperationRemoveLateEntrant,
}

func cloneCompiledPhase(in CompiledPhase) CompiledPhase {
	out := in
	out.AllowedOperations = append([]string(nil), in.AllowedOperations...)
	out.EntryConditions = append([]string(nil), in.EntryConditions...)
	out.ExitConditions = append([]string(nil), in.ExitConditions...)
	out.Obligations = append([]string(nil), in.Obligations...)
	return out
}

func validateCutoffForExplanation(c CutoffResolution) error {
	if strings.TrimSpace(c.PhaseID) == "" {
		return fmt.Errorf("%w: phase id is required", ErrCutoffExplanation)
	}
	if err := c.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: cutoff %q effective date: %v", ErrCutoffExplanation, c.PhaseID, err)
	}
	switch c.Status {
	case CutoffResolved:
		if c.Instant.IsZero() {
			return fmt.Errorf("%w: resolved cutoff %q has no instant", ErrCutoffExplanation, c.PhaseID)
		}
	case CutoffReviewRequired:
		if c.Instant.IsZero() == false {
			return fmt.Errorf("%w: review cutoff %q must not carry an instant", ErrCutoffExplanation, c.PhaseID)
		}
	default:
		return fmt.Errorf("%w: unknown cutoff status %q", ErrCutoffExplanation, c.Status)
	}
	if c.Rule == "" {
		return fmt.Errorf("%w: cutoff %q has no deciding rule", ErrCutoffExplanation, c.PhaseID)
	}
	return nil
}

func phaseCapabilities(phase CompiledPhase) ([]PhaseCapability, []string, []string) {
	var permitted []string
	if len(phase.AllowedOperations) > 0 {
		permitted = make([]string, len(phase.AllowedOperations))
		copy(permitted, phase.AllowedOperations)
		sort.Strings(permitted)
	}
	seen := make(map[string]struct{}, len(permitted))
	for _, operation := range permitted {
		seen[operation] = struct{}{}
	}
	forbidden := make([]string, 0, len(standardCycleOperations))
	for _, operation := range standardCycleOperations {
		if _, ok := seen[operation]; !ok {
			forbidden = append(forbidden, operation)
		}
	}
	sort.Strings(forbidden)
	capabilities := make([]PhaseCapability, len(permitted)+len(forbidden))
	i := 0
	for _, operation := range permitted {
		capabilities[i] = PhaseCapability{Operation: operation, Permitted: true, Reason: "declared by active phase"}
		i++
	}
	for _, operation := range forbidden {
		capabilities[i] = PhaseCapability{Operation: operation, Reason: "not declared by active phase"}
		i++
	}
	return capabilities, permitted, forbidden
}

func transitionAvailability(state LifecycleState, phase CompiledPhase) ([]TransitionAvailability, []TransitionOperation) {
	transitions := []TransitionAvailability{
		{Operation: OperationOpen, Reason: "OPEN is valid only from UNOPENED"},
		{Operation: OperationClose, Reason: "CLOSE is valid only from OPEN"},
		{Operation: OperationLock, Reason: "LOCK is valid only from CLOSED"},
		{Operation: OperationReopen, Reason: "REOPEN is valid only from CLOSED and requires distinct approval"},
	}
	var available []TransitionOperation
	for i := range transitions {
		stateAllows := (state == StateUnopened && transitions[i].Operation == OperationOpen) ||
			(state == StateOpen && transitions[i].Operation == OperationClose) ||
			(state == StateClosed && (transitions[i].Operation == OperationLock || transitions[i].Operation == OperationReopen))
		if !stateAllows {
			continue
		}
		if !phaseAllows(phase, string(transitions[i].Operation)) {
			transitions[i].Reason = "active phase does not declare this transition"
			continue
		}
		transitions[i].Available = true
		transitions[i].Reason = "current state and active phase permit this transition"
		available = append(available, transitions[i].Operation)
	}
	return transitions, available
}

func explanationBody(e CurrentPhaseExplanation) []byte {
	w := canonicalbytes.New("hcmnext.engines.cycle.CurrentPhaseExplanation", Version()).
		String("revision_digest", e.CycleRevisionDigest).
		String("revision_version", e.CycleRevisionVersion).
		String("state", string(e.State)).
		Int("sequence", int64(e.Sequence)).
		String("effective_at", e.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String("known_at", e.KnownAt.UTC().Format(time.RFC3339Nano)).
		Bool("has_phase", e.HasPhase)
	if e.HasPhase {
		w.String("phase.id", e.Phase.ID).
			String("phase.name", e.Phase.Name).
			String("window.start", e.Window.Start.UTC().Format(time.RFC3339Nano)).
			String("window.end", e.Window.End.UTC().Format(time.RFC3339Nano))
	}
	w.Count("cutoffs", len(e.Cutoffs))
	for _, cutoff := range e.Cutoffs {
		w.String("cutoff.phase", cutoff.PhaseID).
			String("cutoff.status", string(cutoff.Status)).
			Value("cutoff.effective_date", cutoff.EffectiveDate).
			String("cutoff.instant", cutoff.Instant.UTC().Format(time.RFC3339Nano)).
			String("cutoff.rule", cutoff.Rule)
	}
	w.Count("capabilities", len(e.Capabilities))
	for _, capability := range e.Capabilities {
		w.String("capability.operation", capability.Operation).
			Bool("capability.permitted", capability.Permitted).
			String("capability.reason", capability.Reason)
	}
	w.SortedStrings("permitted", e.PermittedOperations).
		SortedStrings("forbidden", e.ForbiddenOperations)
	w.Count("transitions", len(e.Transitions))
	for _, transition := range e.Transitions {
		w.String("transition.operation", string(transition.Operation)).
			Bool("transition.available", transition.Available).
			String("transition.reason", transition.Reason)
	}
	w.SortedStrings("available_transitions", transitionOperationStrings(e.AvailableTransitions)).
		SortedStrings("locks", e.Locks).
		SortedStrings("exceptions", e.Exceptions).
		String("rendering", e.Rendering)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func transitionOperationStrings(in []TransitionOperation) []string {
	out := make([]string, len(in))
	for i, operation := range in {
		out[i] = string(operation)
	}
	return out
}

func renderCurrentPhase(e CurrentPhaseExplanation) string {
	var b strings.Builder
	b.Grow(512)
	b.WriteString("cycle revision ")
	b.WriteString(e.CycleRevisionVersion)
	b.WriteString(" (")
	b.WriteString(e.CycleRevisionDigest)
	b.WriteString(") at effective ")
	writeTime(&b, e.EffectiveAt)
	b.WriteString(", known at ")
	writeTime(&b, e.KnownAt)
	b.WriteString("\nstate: ")
	b.WriteString(string(e.State))
	b.WriteString(" (sequence ")
	writeInt(&b, int64(e.Sequence))
	b.WriteByte(')')
	if e.HasPhase {
		b.WriteString("\nactive phase: ")
		b.WriteString(e.Phase.ID)
		b.WriteString(" (")
		b.WriteString(e.Phase.Name)
		b.WriteString("), window [")
		writeTime(&b, e.Window.Start)
		b.WriteString(", ")
		writeTime(&b, e.Window.End)
		b.WriteByte(')')
	} else {
		b.WriteString("\nactive phase: none")
	}
	b.WriteString("\npermitted operations: ")
	writeJoined(&b, e.PermittedOperations)
	b.WriteString("\nforbidden operations: ")
	writeJoined(&b, e.ForbiddenOperations)
	b.WriteString("\navailable transitions: ")
	for i, operation := range e.AvailableTransitions {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(string(operation))
	}
	if len(e.Cutoffs) == 0 {
		b.WriteString("\nbounding cutoffs: none supplied")
	} else {
		for _, cutoff := range e.Cutoffs {
			b.WriteString("\nbounding cutoff ")
			b.WriteString(cutoff.PhaseID)
			b.WriteString(": ")
			if cutoff.Status == CutoffResolved {
				writeTime(&b, cutoff.Instant)
			} else {
				b.WriteString("REVIEW_REQUIRED")
			}
		}
	}
	if len(e.Locks) > 0 {
		b.WriteString("\nlocks: ")
		writeJoined(&b, e.Locks)
	}
	if len(e.Exceptions) > 0 {
		b.WriteString("\nexceptions: ")
		writeJoined(&b, e.Exceptions)
	}
	return b.String()
}

func writeJoined(b *strings.Builder, values []string) {
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(value)
	}
}

func writeTime(b *strings.Builder, value time.Time) {
	var buf [len(time.RFC3339Nano)]byte
	b.Write(value.UTC().AppendFormat(buf[:0], time.RFC3339Nano))
}

func writeInt(b *strings.Builder, value int64) {
	var buf [20]byte
	b.Write(strconv.AppendInt(buf[:0], value, 10))
}

func finishCurrentPhaseExplanation(e CurrentPhaseExplanation) (CurrentPhaseExplanation, error) {
	e.Rendering = renderCurrentPhase(e)
	e.HumanReadable = e.Rendering
	body := explanationBody(e)
	if len(body) == 0 {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: result could not be canonically encoded", ErrCurrentPhaseInvalid)
	}
	e.ResultDigest = canonicalbytes.Digest(body)
	return e, nil
}

// Canonical returns the stable explanation body used for the result digest.
// The body intentionally excludes no semantic explanation fields; the digest
// strings themselves are derived metadata and are not recursively encoded.
func (e CurrentPhaseExplanation) Canonical() []byte {
	return explanationBody(e)
}

// ExplainCurrentPhase returns the phase active at at, with the cutoffs
// already resolved by ResolveCycleCutoffs. It is pure and uses at as both the
// explicit effective and known coordinate, preventing ambient-clock leakage.
func ExplainCurrentPhase(c GovernedCycle, graph PhaseGraph, cutoffs []CutoffResolution, at time.Time) (CurrentPhaseExplanation, error) {
	if c.Revision.Digest == "" || c.Revision.Version == "" {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: cycle revision is incomplete", ErrCurrentPhaseInvalid)
	}
	if c.State != StateUnopened && c.State != StateOpen && c.State != StateClosed && c.State != StateLocked {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: unknown lifecycle state %q", ErrCurrentPhaseInvalid, c.State)
	}
	if at.IsZero() {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: effective and known instant are required", ErrCurrentPhaseInvalid)
	}
	if !c.Revision.ValidAt(at) {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: revision %s is not effective at %s", ErrCurrentPhaseInvalid, c.Revision.Version, at.UTC().Format(time.RFC3339Nano))
	}
	phase, ok := graph.PhaseAt(at)
	if !ok {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: %s", ErrNoActivePhase, graph.Explain(at).Rule)
	}
	for _, cutoff := range cutoffs {
		if err := validateCutoffForExplanation(cutoff); err != nil {
			return CurrentPhaseExplanation{}, err
		}
	}
	var activeCutoffs []CutoffResolution
	if len(cutoffs) > 0 {
		activeCutoffs = make([]CutoffResolution, 0, len(cutoffs))
	}
	for _, cutoff := range cutoffs {
		if cutoff.PhaseID == phase.ID {
			activeCutoffs = append(activeCutoffs, cutoff)
		}
	}
	if len(activeCutoffs) > 0 {
		sort.SliceStable(activeCutoffs, func(i, j int) bool {
			if activeCutoffs[i].PhaseID != activeCutoffs[j].PhaseID {
				return activeCutoffs[i].PhaseID < activeCutoffs[j].PhaseID
			}
			if activeCutoffs[i].Status != activeCutoffs[j].Status {
				return activeCutoffs[i].Status < activeCutoffs[j].Status
			}
			return activeCutoffs[i].Instant.Before(activeCutoffs[j].Instant)
		})
	} else {
		activeCutoffs = nil
	}
	capabilities, permitted, forbidden := phaseCapabilities(phase)
	transitions, available := transitionAvailability(c.State, phase)
	locks := []string(nil)
	if c.State == StateLocked {
		locks = []string{"cycle_locked"}
	}
	exceptions := []string(nil)
	for _, cutoff := range activeCutoffs {
		if cutoff.Status == CutoffReviewRequired {
			exceptions = append(exceptions, "cutoff_review_required")
		}
	}
	if c.State == StateLocked {
		exceptions = append(exceptions, "all_lifecycle_transitions_forbidden")
	}
	e := CurrentPhaseExplanation{
		CycleRevisionDigest: c.Revision.Digest, CycleRevisionVersion: c.Revision.Version,
		State: c.State, Sequence: c.Sequence, EffectiveAt: at.UTC(), KnownAt: at.UTC(),
		HasPhase: true, Phase: cloneCompiledPhase(phase),
		Window:  PhaseWindow{PhaseID: phase.ID, Name: phase.Name, Start: phase.Start.UTC(), End: phase.End.UTC()},
		Cutoffs: activeCutoffs, Capabilities: capabilities,
		PermittedOperations: permitted, ForbiddenOperations: forbidden,
		Transitions: transitions, AvailableTransitions: available,
		Locks: locks, Exceptions: exceptions,
	}
	body := explanationBody(e)
	if len(body) == 0 {
		return CurrentPhaseExplanation{}, fmt.Errorf("%w: inputs could not be canonically encoded", ErrCurrentPhaseInvalid)
	}
	e.InputsDigest = canonicalbytes.Digest(body)
	return finishCurrentPhaseExplanation(e)
}

// Explain is the package-level ARCH-GO-009 explanation entry point for a
// current cycle phase. The more explicit ExplainCurrentPhase name is kept for
// callers that want the semantic operation in their code.
func Explain(c GovernedCycle, graph PhaseGraph, cutoffs []CutoffResolution, at time.Time) (CurrentPhaseExplanation, error) {
	return ExplainCurrentPhase(c, graph, cutoffs, at)
}

// PhaseActionDecision is the pure permission result for an operation named by
// a caller after obtaining a current-phase explanation.
type PhaseActionDecision struct {
	Code            string
	Operation       string
	State           LifecycleState
	RevisionVersion string
	Field           string
	Reason          string
}

const (
	Cycle009Accepted = "CYCLE_009_ACCEPTED"
	Cycle009Rejected = "CYCLE_009_REJECTED"
)

// DecideOperation refuses an operation that is not permitted by the explained
// phase or is not a legal transition from the explained lifecycle state. It
// only returns a typed decision; it cannot persist an event or any other row.
func (e CurrentPhaseExplanation) DecideOperation(operation string) PhaseActionDecision {
	decision := PhaseActionDecision{Code: Cycle009Rejected, Operation: operation, State: e.State, RevisionVersion: e.CycleRevisionVersion, Field: "operation"}
	if strings.TrimSpace(operation) == "" {
		decision.Reason = "operation is required"
		return decision
	}
	if !containsString(e.PermittedOperations, operation) {
		decision.Reason = "operation is outside the active phase window"
		return decision
	}
	for _, transition := range e.Transitions {
		if string(transition.Operation) == operation && !transition.Available {
			decision.Reason = transition.Reason
			return decision
		}
	}
	decision.Code = Cycle009Accepted
	decision.Field = ""
	decision.Reason = "operation is declared by the active phase"
	return decision
}

// DecideOperationAt evaluates an operation against the phase window at the
// supplied instant. An instant outside every pinned window is a typed
// CYCLE_009_REJECTED decision rather than a phase guess, and this helper has
// no persistence or effect boundary.
func DecideOperationAt(c GovernedCycle, graph PhaseGraph, cutoffs []CutoffResolution, operation string, at time.Time) PhaseActionDecision {
	explanation, err := ExplainCurrentPhase(c, graph, cutoffs, at)
	if err != nil {
		return PhaseActionDecision{
			Code: Cycle009Rejected, Operation: operation, State: c.State,
			RevisionVersion: c.Revision.Version, Field: "phase.window", Reason: err.Error(),
		}
	}
	return explanation.DecideOperation(operation)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
