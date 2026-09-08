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

// overviewFact is one overview fact field: label key, field
// key, and record accessor, in page order.
type overviewFact struct {
	labelKey string
	name     string
	value    func(Person) string
}

// workerOverviewFacts is the overview fact set, matching the
// page adapter's details list field for field.
var workerOverviewFacts = []overviewFact{
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
// for one person record with the page adapter's exact fact
// behavior: silent servers pass values through
// (unavailable-for-empty), governed records project each
// field through its verdict with HIDE omitting the row. The
// record is never mutated.
func ResolveWorkerOverview(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	silent := len(verdicts) == 0
	fields := map[string]AuthorizedField{}
	if record, ok := verdicts[person.ID]; ok {
		fields = record.Fields
	}
	facts := make([]WorkerFact, 0, len(workerOverviewFacts))
	for _, field := range workerOverviewFacts {
		raw := field.value(person)
		if silent {
			facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: valueOrUnavailableFor(locale, raw)})
			continue
		}
		projected, admitted := ProjectField(locale, raw, fields[field.name])
		if !admitted {
			continue
		}
		facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: projected.Text})
	}
	return WorkerSection{
		Title:       locale.Text("person.employment_overview"),
		Description: locale.Text("person.employment_overview_detail"),
		Facts:       facts,
	}
}
