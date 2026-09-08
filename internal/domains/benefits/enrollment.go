package benefits

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// WindowKind identifies the business event which grants an enrollment window.
type WindowKind string

const (
	WindowOpenEnrollment WindowKind = "OPEN_ENROLLMENT"
	WindowNewHire        WindowKind = "NEW_HIRE"
	WindowLifeEvent      WindowKind = "LIFE_EVENT"
	WindowCorrection     WindowKind = "CORRECTION"
)

// WindowStatus is deliberately not a boolean. Missing event facts remain
// conditional or unknown and never become an ambiently accepted election.
type WindowStatus string

const (
	WindowOpen        WindowStatus = "OPEN"
	WindowClosed      WindowStatus = "CLOSED"
	WindowConditional WindowStatus = "CONDITIONAL"
	WindowUnknown     WindowStatus = "UNKNOWN"
)

var (
	ErrInvalidEnrollmentWindow = errors.New("benefits: invalid enrollment window")
	ErrOverlappingWindows      = errors.New("benefits: overlapping enrollment windows")
	ErrBEN002Rejected          = errors.New("BEN_002_REJECTED")
)

// EnrollmentWindowRejection is the stable, side-effect-free BEN-002 refusal
// returned when a published set would make resolution ambiguous.
type EnrollmentWindowRejection struct {
	Code           string
	OffendingField string
	OffendingState string
	Version        string
	Cause          error
}

func (e *EnrollmentWindowRejection) Error() string {
	if e == nil {
		return "BEN_002_REJECTED"
	}
	return fmt.Sprintf("%s: field=%s state=%s version=%s", e.Code, e.OffendingField, e.OffendingState, e.Version)
}

func (e *EnrollmentWindowRejection) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *EnrollmentWindowRejection) Is(target error) bool {
	return target == ErrBEN002Rejected || errors.Is(e.Cause, target)
}

// EnrollmentWindow is an immutable, half-open calendar-date interval. The
// interval's calendar is part of the value so a replay cannot silently use a
// different holiday/calendar revision.
type EnrollmentWindow struct {
	ID           string
	Kind         WindowKind
	PlanID       values.EntityRef
	PlanRevision string
	Interval     values.EffectiveInterval
	Reason       string
}

func (w EnrollmentWindow) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	b, _ := canonicalbytes.New("hcmnext.domains.benefits.EnrollmentWindow", 1).
		String("id", w.ID).String("kind", string(w.Kind)).Value("plan", w.PlanID).
		String("plan_revision", w.PlanRevision).Value("interval", w.Interval).String("reason", w.Reason).Bytes()
	return b
}

func (w EnrollmentWindow) Validate() error {
	if strings.TrimSpace(w.ID) == "" || !validWindowKind(w.Kind) {
		return ErrInvalidEnrollmentWindow
	}
	if err := w.PlanID.Validate(); err != nil {
		return fmt.Errorf("%w: plan: %v", ErrInvalidEnrollmentWindow, err)
	}
	if strings.TrimSpace(w.PlanRevision) == "" || strings.TrimSpace(w.Reason) == "" {
		return ErrInvalidEnrollmentWindow
	}
	if err := w.Interval.Validate(); err != nil || w.Interval.Kind() != values.IntervalKindLocalDate || w.Interval.IsOpenEnded() {
		return fmt.Errorf("%w: interval must be a bounded local-date interval", ErrInvalidEnrollmentWindow)
	}
	return nil
}

func validWindowKind(k WindowKind) bool {
	switch k {
	case WindowOpenEnrollment, WindowNewHire, WindowLifeEvent, WindowCorrection:
		return true
	default:
		return false
	}
}

// EnrollmentWindowSet is the published set of windows for one plan revision.
type EnrollmentWindowSet struct{ Windows []EnrollmentWindow }

func NewEnrollmentWindowSet(windows []EnrollmentWindow) (EnrollmentWindowSet, error) {
	set := EnrollmentWindowSet{Windows: append([]EnrollmentWindow(nil), windows...)}
	if err := set.Validate(); err != nil {
		return EnrollmentWindowSet{}, err
	}
	return set, nil
}

func (s EnrollmentWindowSet) Validate() error {
	for i, w := range s.Windows {
		if err := w.Validate(); err != nil {
			return fmt.Errorf("window %d: %w", i, err)
		}
		for _, prior := range s.Windows[:i] {
			if prior.PlanID == w.PlanID && prior.PlanRevision == w.PlanRevision && prior.Kind == w.Kind && intervalsOverlap(prior.Interval, w.Interval) {
				return &EnrollmentWindowRejection{
					Code:           "BEN_002_REJECTED",
					OffendingField: "interval",
					OffendingState: prior.ID + "," + w.ID,
					Version:        w.PlanRevision,
					Cause:          ErrOverlappingWindows,
				}
			}
		}
	}
	return nil
}

func (s EnrollmentWindowSet) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.benefits.EnrollmentWindowSet", 1).Count("window", len(s.Windows))
	for _, item := range s.Windows {
		w.Value("window", item)
	}
	b, _ := w.Bytes()
	return b
}

// EnrollmentWindowRequest contains every fact needed to resolve one election.
// ElectionDate is the date the worker submitted; EventDate is required for
// new-hire/life-event windows. A zero date is an explicit unknown fact.
type EnrollmentWindowRequest struct {
	Set          EnrollmentWindowSet
	PlanID       values.EntityRef
	PlanRevision string
	Kind         WindowKind
	ElectionDate values.LocalDate
	EventDate    values.LocalDate
}

type EnrollmentWindowResolution struct {
	Status       WindowStatus
	WindowID     string
	Kind         WindowKind
	Start        values.LocalDate
	End          values.LocalDate
	Calendar     values.CalendarRef
	Reason       string
	PlanRevision string
	Explanation  string
}

func ResolveEnrollmentWindow(req EnrollmentWindowRequest) (EnrollmentWindowResolution, error) {
	if err := req.Set.Validate(); err != nil {
		return EnrollmentWindowResolution{}, err
	}
	if err := req.PlanID.Validate(); err != nil || strings.TrimSpace(req.PlanRevision) == "" || !validWindowKind(req.Kind) {
		return EnrollmentWindowResolution{}, ErrInvalidEnrollmentWindow
	}
	if err := req.ElectionDate.Validate(); err != nil {
		return EnrollmentWindowResolution{}, fmt.Errorf("%w: election date: %v", ErrInvalidEnrollmentWindow, err)
	}
	if (req.Kind == WindowNewHire || req.Kind == WindowLifeEvent) && isZeroDate(req.EventDate) {
		var published *EnrollmentWindow
		for _, w := range req.Set.Windows {
			if w.PlanID == req.PlanID && w.PlanRevision == req.PlanRevision && w.Kind == req.Kind {
				if published == nil {
					candidate := w
					published = &candidate
				}
				if electionInside, _ := w.Interval.ContainsDate(req.ElectionDate); !electionInside {
					continue
				}
				start, _ := w.Interval.StartDate()
				end, _ := w.Interval.EndDate()
				return EnrollmentWindowResolution{Status: WindowConditional, WindowID: w.ID, Kind: w.Kind, Start: start, End: end, Calendar: w.Interval.Calendar(), Reason: "event date is required", PlanRevision: w.PlanRevision, Explanation: fmt.Sprintf("CONDITIONAL: %s window %s-%s requires the qualifying event date", w.Kind, start, end)}, nil
			}
		}
		if published != nil {
			start, _ := published.Interval.StartDate()
			end, _ := published.Interval.EndDate()
			return EnrollmentWindowResolution{Status: WindowClosed, WindowID: published.ID, Kind: published.Kind, Start: start, End: end, Calendar: published.Interval.Calendar(), Reason: "election date is outside the declared window", PlanRevision: published.PlanRevision, Explanation: "CLOSED: no declared window contains the election date"}, nil
		}
		return EnrollmentWindowResolution{Status: WindowUnknown, Kind: req.Kind, PlanRevision: req.PlanRevision, Reason: "no matching window is published for this plan revision", Explanation: "UNKNOWN: no matching versioned window"}, nil
	}
	if !isZeroDate(req.EventDate) {
		if err := req.EventDate.Validate(); err != nil {
			return EnrollmentWindowResolution{Status: WindowUnknown, Kind: req.Kind, PlanRevision: req.PlanRevision, Reason: "event date is not a valid calendar date", Explanation: "UNKNOWN: qualifying event date cannot be evaluated"}, nil
		}
	}
	for _, w := range req.Set.Windows {
		if w.PlanID != req.PlanID || w.PlanRevision != req.PlanRevision || w.Kind != req.Kind {
			continue
		}
		start, _ := w.Interval.StartDate()
		end, _ := w.Interval.EndDate()
		if (req.Kind == WindowNewHire || req.Kind == WindowLifeEvent) && !isZeroDate(req.EventDate) {
			// EventDate is validated below; when supplied it must be within the
			// declared window, preventing a stale event from opening a window.
			if ok, _ := w.Interval.ContainsDate(req.EventDate); !ok {
				continue
			}
		}
		if ok, _ := w.Interval.ContainsDate(req.ElectionDate); !ok {
			continue
		}
		return EnrollmentWindowResolution{Status: WindowOpen, WindowID: w.ID, Kind: w.Kind, Start: start, End: end, Calendar: w.Interval.Calendar(), Reason: w.Reason, PlanRevision: w.PlanRevision, Explanation: fmt.Sprintf("%s window %s-%s open under calendar %s: %s", w.Kind, start, end, w.Interval.Calendar(), w.Reason)}, nil
	}
	for _, w := range req.Set.Windows {
		if w.PlanID == req.PlanID && w.PlanRevision == req.PlanRevision && w.Kind == req.Kind {
			start, _ := w.Interval.StartDate()
			end, _ := w.Interval.EndDate()
			reason := "election date is outside the declared window"
			explanation := "CLOSED: no declared window contains the election date"
			if req.Kind == WindowNewHire || req.Kind == WindowLifeEvent {
				if eventInside, _ := w.Interval.ContainsDate(req.EventDate); !eventInside {
					reason = "qualifying event date is outside the declared window"
					explanation = "CLOSED: qualifying event date does not match the declared window"
				}
			}
			return EnrollmentWindowResolution{Status: WindowClosed, WindowID: w.ID, Kind: w.Kind, Start: start, End: end, Calendar: w.Interval.Calendar(), Reason: reason, PlanRevision: w.PlanRevision, Explanation: explanation}, nil
		}
	}
	return EnrollmentWindowResolution{Status: WindowUnknown, Kind: req.Kind, PlanRevision: req.PlanRevision, Reason: "no matching window is published for this plan revision", Explanation: "UNKNOWN: no matching versioned window"}, nil
}

func isZeroDate(d values.LocalDate) bool { return d == (values.LocalDate{}) }
