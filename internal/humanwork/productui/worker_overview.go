package productui

// WorkerFact is one resolved section fact: the field key,
// its localized label, and its verdict-projected value.
type WorkerFact struct {
	Name  string
	Label string
	Value string
}

// WorkerSection is one independently resolved worker object
// page section: title, description, and facts.
type WorkerSection struct {
	Title       string
	Description string
	Facts       []WorkerFact
}

// workerOverviewFacts is the overview fact set, matching the
// page adapter's details list field for field.
var workerOverviewFacts = []sectionFact{
	{"person.worker_number", "worker_number", func(person Person) string { return person.WorkerNumber }},
	{"person.job_code", "job_code", func(person Person) string { return person.JobCode }},
	{"person.job_level", "job_level", func(person Person) string { return person.Grade }},
	{"person.hire_date", "hire_date", func(person Person) string { return person.HireDate }},
	{"person.employment_type", "employment_type", func(Person) string { return "" }},
	{"person.time_type", "time_type", func(Person) string { return "" }},
	{"person.record_source", "record_source", func(person Person) string { return person.Source }},
	{"person.record_created", "record_created", func(person Person) string { return person.CreatedAt }},
}

// ResolveWorkerOverview resolves the worker overview section
// for one person record over the shared section engine. The
// record is never mutated.
func ResolveWorkerOverview(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.employment_overview", "person.employment_overview_detail", workerOverviewFacts)
}
