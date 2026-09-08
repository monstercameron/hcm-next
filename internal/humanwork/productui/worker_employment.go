package productui

// workerEmploymentFacts is the employment fact set, matching
// the page adapter's organization list field for field.
var workerEmploymentFacts = []sectionFact{
	{"person.organization_unit", "organization_unit", func(_ LocaleContext, person Person) string { return person.Team }},
	{"person.manager", "manager", func(_ LocaleContext, person Person) string { return person.Manager }},
	{"person.position_id", "position_id", func(_ LocaleContext, person Person) string { return person.PositionID }},
	{"person.work_location", "work_location", func(_ LocaleContext, person Person) string { return person.Location }},
	{"person.company", "company", func(LocaleContext, Person) string { return "" }},
	{"person.business_unit", "business_unit", func(LocaleContext, Person) string { return "" }},
	{"person.cost_center", "cost_center", func(LocaleContext, Person) string { return "" }},
	{"person.work_arrangement", "work_arrangement", func(LocaleContext, Person) string { return "" }},
}

// ResolveWorkerEmployment resolves the worker employment
// section for one person record over the shared section
// engine. The record is never mutated.
func ResolveWorkerEmployment(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.organization", "person.organization_detail", workerEmploymentFacts)
}
