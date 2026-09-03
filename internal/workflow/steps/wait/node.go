package wait

import (
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// WakeKind names which shape of wake condition a CompiledWaitNode declares.
type WakeKind string

// The declared wake-condition kinds.
const (
	// WakeAtInstant is a fixed, already-resolved instant. It carries no
	// timezone ambiguity of its own, but a TimerReference is still required
	// so the requirement preserves the calendar/tzdb identity in force when
	// it was minted (REFACTOR: preserve calendar/tzdb version and
	// calculation evidence).
	WakeAtInstant WakeKind = "AT_INSTANT"
	// WakeAtLocalDate fires at the start of a business local date in a zone.
	WakeAtLocalDate WakeKind = "AT_LOCAL_DATE"
	// WakeAtLocalDateTime fires at a specific local wall-clock time on a
	// business local date in a zone.
	WakeAtLocalDateTime WakeKind = "AT_LOCAL_DATETIME"
)

// CompiledWaitNode is the WF-STEP-005 view of a compiled WAIT node: the
// identity it belongs to, its declared wake condition, and the
// reference-update policy that governs what happens when the timezone or
// business-calendar dataset behind it is republished. See package doc for why
// this is this package's own type rather than a field on workflow.Node.
type CompiledWaitNode struct {
	WorkflowID      string
	WorkflowVersion uint32
	NodeID          string

	// WakeInstant, when set, is a fixed-instant wake condition. It is
	// mutually exclusive with WakeLocalDate.
	WakeInstant values.Instant

	// WakeLocalDate, when set, is a business wall-clock wake condition.
	// WakeLocalTime is optional; when unset the wake condition is the start
	// of WakeLocalDate in Zone.
	WakeLocalDate  values.LocalDate
	WakeLocalTime  values.LocalTime
	Disambiguation values.Disambiguation

	Zone     values.ZoneRef
	Calendar values.CalendarRef
	Policy   values.ReferenceUpdatePolicy
}

// Kind reports which wake-condition shape the node declares. It does not
// validate the node; call Validate first.
func (n CompiledWaitNode) Kind() WakeKind {
	if n.WakeInstant.IsSet() {
		return WakeAtInstant
	}
	if n.WakeLocalTime.IsSet() {
		return WakeAtLocalDateTime
	}
	return WakeAtLocalDate
}

// Validate reports whether the node declares a complete, unambiguous shape.
// It does not resolve the wake condition to an instant — that is
// ComputeTimerRequirement's job, because resolution can legitimately refuse
// (DST gap/fold) without the node itself being malformed.
func (n CompiledWaitNode) Validate() error {
	if n.WorkflowID == "" || n.NodeID == "" {
		return ErrWorkflowIdentityRequired
	}
	if n.WakeInstant.IsSet() && n.WakeLocalDate.IsSet() {
		return ErrConflictingWakeCondition
	}
	if !n.WakeInstant.IsSet() {
		if !n.WakeLocalDate.IsSet() {
			return ErrNoWakeCondition
		}
		if n.Disambiguation == values.DisambiguationUnspecified {
			return values.ErrDisambiguationRequired
		}
		if !n.Disambiguation.Valid() {
			return fmt.Errorf("wait: %w: %d", values.ErrDisambiguation, uint8(n.Disambiguation))
		}
	}
	return n.reference().Validate()
}

// reference is the dataset context the node's wake condition must be
// replayable against.
func (n CompiledWaitNode) reference() values.TimerReference {
	return values.TimerReference{Zone: n.Zone, Calendar: n.Calendar, Policy: n.Policy}
}

// resolver builds a values.TimerResolver that recomputes the node's wake
// condition against an arbitrary dataset version pair. A fixed instant is
// dataset-independent and is returned unchanged; a local wall-clock condition
// is re-resolved with the zone/calendar's version fields substituted, so PIN
// replay against the historical dataset and RECALCULATE against the current
// one both go through the same arithmetic.
func (n CompiledWaitNode) resolver() values.TimerResolver {
	return func(ds values.DatasetVersions) (values.Instant, error) {
		if n.WakeInstant.IsSet() {
			return n.WakeInstant, nil
		}
		zone := n.Zone
		zone.TzdbVersion = ds.TzdbVersion
		var zdt values.ZonedDateTime
		var err error
		if n.WakeLocalTime.IsSet() {
			zdt, err = values.NewZonedDateTime(n.WakeLocalDate, n.WakeLocalTime, zone, n.Disambiguation)
		} else {
			zdt, err = n.WakeLocalDate.AtStartOfDay(zone, n.Disambiguation)
		}
		if err != nil {
			return values.Instant{}, err
		}
		return zdt.Instant(), nil
	}
}
