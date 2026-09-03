package inspect

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// GapKind classifies why one rendered reference carries no value, closing
// the hole ADMIN-008's RED clause names: "an empty governance ref is
// indistinguishable from an unrecorded one."
//
// [Ref] alone can only say VALUE, ABSENT or REDACTED, and ABSENT already
// conflates two different facts: a datum that was legitimately recorded as
// having no value (a TASK work item has no approval requirement; most
// transitions carry no evidence) and a datum the projection expected but
// never received (an APPROVAL work item with no approval_requirement_ref; a
// work item with zero recorded transitions, which [Store.Create] never
// allows to happen honestly). GapKind is the third state: it is computed
// wherever the caller can name the invariant that makes emptiness abnormal,
// rather than guessed from the value alone.
type GapKind string

const (
	// GapKindNone reports a rendered value: there is no gap to report.
	GapKindNone GapKind = ""
	// GapKindAbsent reports a reference that was correctly recorded as
	// carrying no value. This is a normal, expected state, not a defect.
	GapKindAbsent GapKind = "ABSENT"
	// GapKindUnrecorded reports a reference the projection expected to find
	// populated -- by a declared invariant, not a guess -- and did not. It is
	// always also named in the enclosing [Completeness.Gaps].
	GapKindUnrecorded GapKind = "UNRECORDED"
	// GapKindRedacted reports a reference withheld from this caller by an
	// authorization decision.
	GapKindRedacted GapKind = "REDACTED"
)

// RefGapKind classifies an already-rendered [Ref]'s presence state as a
// GapKind. It can only ever answer VALUE/ABSENT/REDACTED, because a bare
// [Ref] carries no invariant of its own to compare against -- exactly the
// limitation this file exists to name. A caller who can state the invariant
// (see [WorkItemView]'s ApprovalRequirementGap) computes GapKindUnrecorded
// directly instead of asking a Ref for it.
func RefGapKind(r Ref) GapKind {
	switch r.State() {
	case values.PresenceValue:
		return GapKindNone
	case values.PresenceRedacted:
		return GapKindRedacted
	default:
		return GapKindAbsent
	}
}
