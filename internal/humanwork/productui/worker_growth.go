package productui

// workerGrowthFacts is the growth fact set. The worker
// record carries no growth facts and the launchable
// workflows are view-coupled, so the section resolves with
// zero facts over the shared engine instead of inventing
// ratings or goals: the constructor is the independent
// resolution point the authorized growth record will fill.
var workerGrowthFacts = []sectionFact{}

// ResolveWorkerGrowth resolves the worker growth section
// for one person record over the shared section engine.
// The record is never mutated.
func ResolveWorkerGrowth(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.growth", "person.growth_detail", workerGrowthFacts)
}
