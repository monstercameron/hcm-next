package benefits

// PlanYearRevision is the revision vocabulary used when a plan-year's rules
// are published independently. It intentionally has the same immutable
// contract as PlanRevision; the PlanYear field identifies the scoped year.
type PlanYearRevision = PlanRevision
