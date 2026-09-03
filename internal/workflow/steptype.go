package workflow

import "sort"

// StepType names one runtime primitive. The kernel has ten core primitives and
// three structural ones; CHECKPOINT, RULE, AGENT and DOCUMENT are deliberately
// absent because they are attributes or capabilities, not node types
// (planning/specs/workflow-runtime.md, "Kernel Vocabulary").
type StepType string

// The ten core primitives.
const (
	StepCapability StepType = "CAPABILITY"
	StepDecision   StepType = "DECISION"
	StepTransform  StepType = "TRANSFORM"
	StepObserve    StepType = "OBSERVE"
	StepEnd        StepType = "END"
	StepApproval   StepType = "APPROVAL"
	StepTask       StepType = "TASK"
	StepWait       StepType = "WAIT"
	StepSignal     StepType = "SIGNAL"
	StepCompensate StepType = "COMPENSATE"
)

// The three structural primitives, gated behind P1B evidence.
const (
	StepParallel    StepType = "PARALLEL"
	StepJoin        StepType = "JOIN"
	StepSubworkflow StepType = "SUBWORKFLOW"
)

// Phase names the release in which a primitive is implemented.
type Phase string

// Phases, in increasing order of what they admit.
const (
	// PhaseP1A compiles CAPABILITY, DECISION, TRANSFORM, OBSERVE and END.
	PhaseP1A Phase = "P1A"
	// PhaseP1B additionally compiles APPROVAL, TASK, WAIT, SIGNAL and
	// COMPENSATE.
	PhaseP1B Phase = "P1B"
	// PhaseStructural additionally admits PARALLEL, JOIN and SUBWORKFLOW. It
	// exists so a conformance fixture can be compiled; it is not a release
	// claim.
	PhaseStructural Phase = "STRUCTURAL"
)

// Outcome is one explicit route key a step type can produce. Every outcome a
// node can produce needs an explicit edge; there is no implicit first edge.
type Outcome string

// Capability outcomes (WF-STEP-001 GREEN).
const (
	OutcomeSucceeded Outcome = "SUCCEEDED"
	OutcomeRejected  Outcome = "REJECTED"
	OutcomeUnknown   Outcome = "UNKNOWN"
	OutcomeAmbiguous Outcome = "AMBIGUOUS"
)

// Transform outcomes.
const (
	OutcomeFailed Outcome = "FAILED"
)

// Observation outcomes (WF-STEP-014 GREEN).
const (
	OutcomePass    Outcome = "PASS"
	OutcomeFail    Outcome = "FAIL"
	OutcomePartial Outcome = "PARTIAL"
)

// Conformance is the fixed contract of one step type: which outcomes it may
// produce, whether an author declares extra routes, whether it may carry an
// effect, and which reference it must bind. The compiler and the step
// conformance harness read this one table, so a step type cannot mean one
// thing to the compiler and another to a test.
type Conformance struct {
	Type StepType
	// Phase is the earliest release that implements the primitive.
	Phase Phase
	// Outcomes are the fixed route keys the step type always produces.
	Outcomes []Outcome
	// AuthorRoutes reports whether the author declares additional route keys
	// (DECISION does; nothing else does).
	AuthorRoutes bool
	// Terminal reports whether the step ends a path and therefore has no
	// outgoing edges.
	Terminal bool
	// RequiresCapability reports whether the node must bind a capability
	// version.
	RequiresCapability bool
	// MayCarryEffect reports whether the step type may carry a write effect
	// class at all. DECISION, TRANSFORM and END never may.
	MayCarryEffect bool
}

var conformanceTable = map[StepType]Conformance{
	StepCapability: {
		Type: StepCapability, Phase: PhaseP1A,
		Outcomes:           []Outcome{OutcomeSucceeded, OutcomeRejected, OutcomeUnknown, OutcomeAmbiguous},
		RequiresCapability: true, MayCarryEffect: true,
	},
	StepDecision: {
		Type: StepDecision, Phase: PhaseP1A,
		Outcomes:     []Outcome{OutcomeUnknown},
		AuthorRoutes: true,
	},
	StepTransform: {
		Type: StepTransform, Phase: PhaseP1A,
		Outcomes: []Outcome{OutcomeSucceeded, OutcomeFailed},
	},
	StepObserve: {
		Type: StepObserve, Phase: PhaseP1A,
		Outcomes:           []Outcome{OutcomePass, OutcomeFail, OutcomeUnknown, OutcomePartial},
		RequiresCapability: true,
	},
	StepEnd: {
		Type: StepEnd, Phase: PhaseP1A,
		Terminal: true,
	},
	StepApproval:   {Type: StepApproval, Phase: PhaseP1B, Outcomes: []Outcome{"APPROVED", OutcomeRejected, "INVALIDATED", "EXPIRED", "CANCELLED"}},
	StepTask:       {Type: StepTask, Phase: PhaseP1B, Outcomes: []Outcome{OutcomeSucceeded, OutcomeRejected, "EXPIRED", "CANCELLED"}},
	StepWait:       {Type: StepWait, Phase: PhaseP1B, Outcomes: []Outcome{OutcomeSucceeded, "LATE", "CANCELLED"}},
	StepSignal:     {Type: StepSignal, Phase: PhaseP1B, Outcomes: []Outcome{OutcomeSucceeded, "TIMED_OUT", "CANCELLED"}},
	StepCompensate: {Type: StepCompensate, Phase: PhaseP1B, Outcomes: []Outcome{"COMPENSATED", OutcomePartial, OutcomeFailed, "REPAIR_REQUIRED"}, RequiresCapability: true, MayCarryEffect: true},
	StepParallel:   {Type: StepParallel, Phase: PhaseStructural, Outcomes: []Outcome{OutcomeSucceeded, OutcomeFailed}},
	StepJoin:       {Type: StepJoin, Phase: PhaseStructural, Outcomes: []Outcome{OutcomeSucceeded, OutcomePartial, OutcomeFailed}},
	StepSubworkflow: {
		Type: StepSubworkflow, Phase: PhaseStructural,
		Outcomes:       []Outcome{OutcomeSucceeded, OutcomeRejected, "CANCELLED", "COMPENSATED", "ALREADY_COMPLETED", "CANNOT_CANCEL"},
		MayCarryEffect: true,
	},
}

// ConformanceFor returns the fixed contract of a step type.
func ConformanceFor(t StepType) (Conformance, bool) {
	c, ok := conformanceTable[t]
	if !ok {
		return Conformance{}, false
	}
	c.Outcomes = append([]Outcome(nil), c.Outcomes...)
	return c, true
}

// StepTypes lists every declared step type, sorted, so a conformance suite can
// enumerate the vocabulary instead of hard-coding it.
func StepTypes() []StepType {
	out := make([]StepType, 0, len(conformanceTable))
	for t := range conformanceTable {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Valid reports whether t names a declared step type.
func (t StepType) Valid() bool { _, ok := conformanceTable[t]; return ok }

// admits reports whether a compiler running in phase p implements step type t.
func (p Phase) admits(t StepType) bool {
	c, ok := conformanceTable[t]
	if !ok {
		return false
	}
	return p.rank() >= c.Phase.rank()
}

func (p Phase) rank() int {
	switch p {
	case PhaseP1A:
		return 0
	case PhaseP1B:
		return 1
	case PhaseStructural:
		return 2
	default:
		return -1
	}
}

// ExecutionMode is the mode an execution declares. P1A ships SIMULATE only.
type ExecutionMode string

// The five declared execution modes.
const (
	ModeSimulate ExecutionMode = "SIMULATE"
	ModeExecute  ExecutionMode = "EXECUTE"
	ModeReplay   ExecutionMode = "REPLAY"
	ModeRepair   ExecutionMode = "REPAIR"
	ModeShadow   ExecutionMode = "SHADOW"
)

// Valid reports whether m names a declared execution mode.
func (m ExecutionMode) Valid() bool {
	switch m {
	case ModeSimulate, ModeExecute, ModeReplay, ModeRepair, ModeShadow:
		return true
	default:
		return false
	}
}
