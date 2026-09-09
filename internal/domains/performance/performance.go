// Package performance owns the pure conformance vocabulary for performance
// cycles. A cycle is an immutable revision: lifecycle operations return a new
// value and never imply persistence, events, approvals, or provider calls.
package performance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const performanceCycleSchema = "hcmnext.domains.performance.PerformanceCycle"

// PerformanceCycleState is the closed lifecycle vocabulary for one cycle
// revision.
type PerformanceCycleState string

const (
	PerformanceCyclePlanned     PerformanceCycleState = "PLANNED"
	PerformanceCycleOpen        PerformanceCycleState = "OPEN"
	PerformanceCycleCalibrating PerformanceCycleState = "CALIBRATING"
	PerformanceCycleClosed      PerformanceCycleState = "CLOSED"
	PerformanceCycleLocked      PerformanceCycleState = "LOCKED"

	// Short names make lifecycle calls read naturally while retaining the
	// explicit type for exported fields.
	Planned     = PerformanceCyclePlanned
	Open        = PerformanceCycleOpen
	Calibrating = PerformanceCycleCalibrating
	Closed      = PerformanceCycleClosed
	Locked      = PerformanceCycleLocked
)

func (s PerformanceCycleState) Valid() bool {
	switch s {
	case PerformanceCyclePlanned, PerformanceCycleOpen, PerformanceCycleCalibrating,
		PerformanceCycleClosed, PerformanceCycleLocked:
		return true
	default:
		return false
	}
}

func (s PerformanceCycleState) String() string { return string(s) }

var (
	ErrInvalidPerformanceCycle = errors.New("performance: invalid performance cycle")
	ErrTransitionRejected      = errors.New("PERFORMANCE_001_REJECTED")
	ErrOutOfOrderTransition    = errors.New("performance: out-of-order transition")
	ErrLockWithoutClose        = errors.New("performance: lock requires close")
	ErrCalibrationEvidence     = errors.New("performance: calibration evidence is required")
)

// PopulationBindingRef identifies an exact frozen population snapshot.
type PopulationBindingRef struct {
	DefinitionID    string
	RevisionVersion string
	Digest          string
}

func (r PopulationBindingRef) Validate() error {
	if strings.TrimSpace(r.DefinitionID) == "" || strings.TrimSpace(r.RevisionVersion) == "" || strings.TrimSpace(r.Digest) == "" {
		return errors.New("population binding requires definition id, revision version and digest")
	}
	return nil
}

func (r PopulationBindingRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.performance.PopulationBindingRef", 1).
		String("definition_id", r.DefinitionID).
		String("revision_version", r.RevisionVersion).
		String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// PopulationRef is the concise spelling used by callers.
type PopulationRef = PopulationBindingRef

// CalendarBindingRef identifies the calendar definition and its immutable
// published version used by the cycle.
type CalendarBindingRef struct {
	Ref     string
	Version string
	Digest  string
}

func (r CalendarBindingRef) Validate() error {
	if strings.TrimSpace(r.Ref) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Digest) == "" {
		return errors.New("calendar binding requires ref, version and digest")
	}
	return nil
}

func (r CalendarBindingRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.performance.CalendarBindingRef", 1).
		String("ref", r.Ref).String("version", r.Version).String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CalendarRef is the concise spelling used by callers.
type CalendarRef = CalendarBindingRef

// RatingScaleVersionRef binds ratings to one immutable scale version.
type RatingScaleVersionRef struct {
	ID      string
	Version string
	Digest  string
}

func (r RatingScaleVersionRef) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Digest) == "" {
		return errors.New("rating scale requires id, version and digest")
	}
	return nil
}

func (r RatingScaleVersionRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.performance.RatingScaleVersionRef", 1).
		String("id", r.ID).String("version", r.Version).String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// RatingScaleRef is the concise spelling used by callers.
type RatingScaleRef = RatingScaleVersionRef

// EvidenceRef binds one calibration observation, decision, or retained
// artifact to its exact immutable revision.
type EvidenceRef struct {
	ID      string
	Version string
	Digest  string
}

func (r EvidenceRef) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Digest) == "" {
		return errors.New("calibration evidence requires id, version and digest")
	}
	return nil
}

func (r EvidenceRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.performance.EvidenceRef", 1).
		String("id", r.ID).String("version", r.Version).String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CalibrationEvidenceRef is the descriptive spelling used by callers.
type CalibrationEvidenceRef = EvidenceRef

// TransitionRefusal carries the stable refusal code and the offending
// lifecycle state/version. It is returned before any successor value exists.
type TransitionRefusal struct {
	Code     string
	Field    string
	From     PerformanceCycleState
	To       PerformanceCycleState
	Revision uint64
	Reason   string
	Cause    error
}

// TransitionError is retained as a conventional name for typed transition
// errors; it is an alias, not a second error shape.
type TransitionError = TransitionRefusal

func (e *TransitionRefusal) Error() string {
	return fmt.Sprintf("%s: field=%s from=%s to=%s revision=%d: %s", e.Code, e.Field, e.From, e.To, e.Revision, e.Reason)
}

func (e *TransitionRefusal) Is(target error) bool {
	return target == ErrTransitionRejected || target == e.Cause
}

func transitionRefusal(c PerformanceCycle, to PerformanceCycleState, field, reason string, cause error) error {
	return &TransitionRefusal{Code: ErrTransitionRejected.Error(), Field: field, From: c.State, To: to, Revision: c.Revision, Reason: reason, Cause: cause}
}

// PerformanceCycle is one immutable revision of a performance cycle.
type PerformanceCycle struct {
	CycleID             string
	Revision            uint64
	State               PerformanceCycleState
	Population          PopulationBindingRef
	Calendar            CalendarBindingRef
	RatingScale         RatingScaleVersionRef
	CalibrationEvidence []EvidenceRef
	SupersedesRevision  uint64
	CanonicalDigest     string
}

// NewPerformanceCycle creates revision one in PLANNED state. It only
// constructs and validates a value.
func NewPerformanceCycle(cycleID string, population PopulationBindingRef, calendar CalendarBindingRef, ratingScale RatingScaleVersionRef) (PerformanceCycle, error) {
	cycle := PerformanceCycle{
		CycleID: cycleID, Revision: 1, State: PerformanceCyclePlanned,
		Population: population, Calendar: calendar, RatingScale: ratingScale,
	}
	cycle.CanonicalDigest = cycle.computedDigest()
	if err := cycle.Validate(); err != nil {
		return PerformanceCycle{}, err
	}
	return cycle, nil
}

// NewPlanned is an explicit lifecycle-oriented alias for NewPerformanceCycle.
func NewPlanned(cycleID string, population PopulationBindingRef, calendar CalendarBindingRef, ratingScale RatingScaleVersionRef) (PerformanceCycle, error) {
	return NewPerformanceCycle(cycleID, population, calendar, ratingScale)
}

// NewPlannedPerformanceCycle is a descriptive alias for callers that prefer
// the aggregate name in the constructor.
func NewPlannedPerformanceCycle(cycleID string, population PopulationBindingRef, calendar CalendarBindingRef, ratingScale RatingScaleVersionRef) (PerformanceCycle, error) {
	return NewPerformanceCycle(cycleID, population, calendar, ratingScale)
}

func (c PerformanceCycle) Validate() error {
	if strings.TrimSpace(c.CycleID) == "" {
		return fmt.Errorf("%w: cycle id is required", ErrInvalidPerformanceCycle)
	}
	if c.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidPerformanceCycle)
	}
	if !c.State.Valid() {
		return fmt.Errorf("%w: state %q is not declared", ErrInvalidPerformanceCycle, c.State)
	}
	for _, item := range []struct {
		name string
		ref  interface{ Validate() error }
	}{
		{"population", c.Population}, {"calendar", c.Calendar}, {"rating_scale", c.RatingScale},
	} {
		if err := item.ref.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidPerformanceCycle, item.name, err)
		}
	}
	if c.SupersedesRevision >= c.Revision {
		return fmt.Errorf("%w: supersedes revision must precede revision", ErrInvalidPerformanceCycle)
	}
	seen := make(map[string]struct{}, len(c.CalibrationEvidence))
	for _, evidence := range c.CalibrationEvidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("%w: calibration evidence: %v", ErrInvalidPerformanceCycle, err)
		}
		key := evidence.ID + "\x00" + evidence.Version + "\x00" + evidence.Digest
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate calibration evidence %s/%s", ErrInvalidPerformanceCycle, evidence.ID, evidence.Version)
		}
		seen[key] = struct{}{}
	}
	if requiresCalibrationEvidence(c.State) && len(c.CalibrationEvidence) == 0 {
		return fmt.Errorf("%w: %w", ErrInvalidPerformanceCycle, ErrCalibrationEvidence)
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidPerformanceCycle)
	}
	return nil
}

func (c PerformanceCycle) body() []byte {
	evidence := append([]EvidenceRef(nil), c.CalibrationEvidence...)
	sortEvidence(evidence)
	w := canonicalbytes.New(performanceCycleSchema, 1).
		String("cycle_id", c.CycleID).
		Int("revision", int64(c.Revision)).
		String("state", c.State.String()).
		Value("population", c.Population).
		Value("calendar", c.Calendar).
		Value("rating_scale", c.RatingScale).
		Int("supersedes_revision", int64(c.SupersedesRevision)).
		Count("calibration_evidence", len(evidence))
	for _, item := range evidence {
		w.Value("calibration_evidence", item)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func sortEvidence(evidence []EvidenceRef) {
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ID != evidence[j].ID {
			return evidence[i].ID < evidence[j].ID
		}
		if evidence[i].Version != evidence[j].Version {
			return evidence[i].Version < evidence[j].Version
		}
		return evidence[i].Digest < evidence[j].Digest
	})
}

func requiresCalibrationEvidence(state PerformanceCycleState) bool {
	switch state {
	case PerformanceCycleCalibrating, PerformanceCycleClosed, PerformanceCycleLocked:
		return true
	default:
		return false
	}
}

func (c PerformanceCycle) computedDigest() string {
	b := c.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Canonical returns the canonical revision encoding or nil when invalid.
func (c PerformanceCycle) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	return c.body()
}

// Digest returns the canonical digest of the revision.
func (c PerformanceCycle) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.computedDigest(), nil
}

// CalibrationEvidenceRefs returns a detached evidence slice.
func (c PerformanceCycle) CalibrationEvidenceRefs() []EvidenceRef {
	return append([]EvidenceRef(nil), c.CalibrationEvidence...)
}

func (c PerformanceCycle) transition(to PerformanceCycleState, evidence ...EvidenceRef) (PerformanceCycle, error) {
	if err := c.Validate(); err != nil {
		return PerformanceCycle{}, err
	}
	if !to.Valid() {
		return PerformanceCycle{}, transitionRefusal(c, to, "state", "target state is not declared", nil)
	}
	allowed := (c.State == PerformanceCyclePlanned && to == PerformanceCycleOpen) ||
		(c.State == PerformanceCycleOpen && to == PerformanceCycleCalibrating) ||
		(c.State == PerformanceCycleCalibrating && to == PerformanceCycleClosed) ||
		(c.State == PerformanceCycleClosed && to == PerformanceCycleLocked)
	if !allowed {
		cause := ErrOutOfOrderTransition
		if to == PerformanceCycleLocked && c.State != PerformanceCycleClosed {
			cause = ErrLockWithoutClose
		}
		return PerformanceCycle{}, transitionRefusal(c, to, "state", "transition is out of order", cause)
	}

	next := c
	next.Revision++
	next.SupersedesRevision = c.Revision
	next.CalibrationEvidence = append([]EvidenceRef(nil), c.CalibrationEvidence...)
	if len(evidence) > 0 {
		next.CalibrationEvidence = append([]EvidenceRef(nil), evidence...)
	}
	sortEvidence(next.CalibrationEvidence)
	next.State = to
	if requiresCalibrationEvidence(to) && len(next.CalibrationEvidence) == 0 {
		return PerformanceCycle{}, transitionRefusal(c, to, "calibration_evidence", "calibrating, closed, and locked revisions require evidence", ErrCalibrationEvidence)
	}
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return PerformanceCycle{}, err
	}
	return next, nil
}

// Transition returns the next lifecycle revision. Evidence supplied for a
// transition becomes the complete evidence set for that successor; omitted
// evidence is carried forward unchanged.
func (c PerformanceCycle) Transition(to PerformanceCycleState, evidence ...EvidenceRef) (PerformanceCycle, error) {
	return c.transition(to, evidence...)
}

func (c PerformanceCycle) Open() (PerformanceCycle, error) { return c.transition(PerformanceCycleOpen) }

func (c PerformanceCycle) BeginCalibration(evidence ...EvidenceRef) (PerformanceCycle, error) {
	return c.transition(PerformanceCycleCalibrating, evidence...)
}

func (c PerformanceCycle) Calibrate(evidence ...EvidenceRef) (PerformanceCycle, error) {
	return c.BeginCalibration(evidence...)
}

func (c PerformanceCycle) Close(evidence ...EvidenceRef) (PerformanceCycle, error) {
	return c.transition(PerformanceCycleClosed, evidence...)
}

func (c PerformanceCycle) Lock() (PerformanceCycle, error) {
	return c.transition(PerformanceCycleLocked)
}

// PerformanceCycleExplanation is a read-only lifecycle explanation.
type PerformanceCycleExplanation struct {
	CycleID             string
	Revision            uint64
	State               PerformanceCycleState
	SupersedesRevision  uint64
	CalibrationEvidence int
	Digest              string
	AllowedNext         []PerformanceCycleState
}

// Explain returns the lifecycle facts and legal next states for a cycle.
func (c PerformanceCycle) Explain() (PerformanceCycleExplanation, error) {
	if err := c.Validate(); err != nil {
		return PerformanceCycleExplanation{}, err
	}
	allowed := map[PerformanceCycleState][]PerformanceCycleState{
		PerformanceCyclePlanned:     {PerformanceCycleOpen},
		PerformanceCycleOpen:        {PerformanceCycleCalibrating},
		PerformanceCycleCalibrating: {PerformanceCycleClosed},
		PerformanceCycleClosed:      {PerformanceCycleLocked},
		PerformanceCycleLocked:      nil,
	}
	return PerformanceCycleExplanation{
		CycleID: c.CycleID, Revision: c.Revision, State: c.State,
		SupersedesRevision: c.SupersedesRevision, CalibrationEvidence: len(c.CalibrationEvidence),
		Digest: c.computedDigest(), AllowedNext: append([]PerformanceCycleState(nil), allowed[c.State]...),
	}, nil
}

// Explain is the package-level spelling for callers that do not need a method.
func Explain(c PerformanceCycle) (PerformanceCycleExplanation, error) { return c.Explain() }
