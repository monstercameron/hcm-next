package productui

// PromotionAvailabilityCode is the server-owned verdict on why a worker's
// promotion workflow is, or is not, offered right now. Every non-eligible
// code always carries a locale-resolved reason (PromotionAvailabilityReason)
// so a bare "no available workflows" fallback never renders unexplained.
type PromotionAvailabilityCode string

const (
	// PromotionEligible is offered normally; there is nothing to explain.
	PromotionEligible PromotionAvailabilityCode = "eligible"
	// PromotionIneligible means the job architecture publishes no next role
	// for this worker's current job and grade. It is a structural fact
	// about the ladder, not a judgment about the person.
	PromotionIneligible PromotionAvailabilityCode = "ineligible"
	// PromotionActiveConflict means this worker already has a nonterminal
	// promotion in flight; starting a second one would race it.
	PromotionActiveConflict PromotionAvailabilityCode = "active_conflict"
	// PromotionWithheld means the viewer does not hold the authority to
	// originate any promotion at all. It always wins over the other three
	// codes: an unauthorized viewer must see the same withheld verdict for
	// every worker regardless of that worker's real ladder or conflict
	// state, or the reason itself would leak which workers would otherwise
	// have been eligible, ineligible or conflicted.
	PromotionWithheld PromotionAvailabilityCode = "withheld"
)

// ResolvePromotionAvailability derives the exhaustive four-state verdict
// from facts the server has already read: whether the requesting viewer
// holds the authority to originate any promotion at all (authorized),
// whether the job architecture publishes a next role for this worker
// (hasPath), and whether a nonterminal promotion already exists for them
// (activeConflict).
//
// Authorization is checked first and wins outright, which is what keeps an
// unauthorized read from leaking anything: every worker collapses to the
// same PromotionWithheld code for such a viewer, so the reason text never
// varies with a worker's real ladder or conflict state. Every combination
// of the three inputs maps to exactly one of the four codes -- there is no
// permissive default and no combination this switch does not name.
func ResolvePromotionAvailability(authorized, hasPath, activeConflict bool) PromotionAvailabilityCode {
	switch {
	case !authorized:
		return PromotionWithheld
	case activeConflict:
		return PromotionActiveConflict
	case !hasPath:
		return PromotionIneligible
	default:
		return PromotionEligible
	}
}

// PromotionAvailabilityReason renders code's localized, viewer-safe
// explanation. PromotionEligible carries no reason because there is nothing
// to explain; every other known code always resolves to non-empty text, and
// an unrecognized code fails closed to the same generic, non-revealing
// reason PromotionWithheld uses rather than rendering blank.
func PromotionAvailabilityReason(locale LocaleContext, code PromotionAvailabilityCode) string {
	switch code {
	case PromotionEligible:
		return ""
	case PromotionIneligible:
		return locale.Text("workflow.no_promotion_path")
	case PromotionActiveConflict:
		return locale.Text("workflow.promotion_active_conflict")
	default:
		// PromotionWithheld and any code this build does not recognize
		// share one generic reason on purpose: recognizing a new code here
		// before its own review would otherwise be the moment an
		// unauthorized viewer starts seeing something more specific than
		// "withheld" for it.
		return locale.Text("workflow.promotion_withheld")
	}
}
