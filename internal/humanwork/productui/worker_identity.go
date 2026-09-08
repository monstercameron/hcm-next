package productui

// WorkerIdentity is the governed worker header facts for one
// person record: the verdict-projected display name and
// role plus the pass-through avatar facts. It generalizes
// the exact pattern the search surface already follows, so
// every header projects denied names identically and no
// header invents its own rule.
type WorkerIdentity struct {
	Name     string
	Role     string
	Initials string
	PhotoURL string
}

// ResolveWorkerIdentity resolves one person record to its
// worker identity header facts. Name and role travel through
// the discovery projection; initials and photo pass through
// untouched. The record is never mutated.
func ResolveWorkerIdentity(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerIdentity {
	return WorkerIdentity{
		Name:     DiscoveryLabel(locale, person.ID, person.Name, "name", verdicts),
		Role:     DiscoveryLabel(locale, person.ID, person.Role, "role", verdicts),
		Initials: person.Initials,
		PhotoURL: person.PhotoURL,
	}
}
