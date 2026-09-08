package productui

// workerTimeLeaveFacts is the time and leave fact set. The
// worker record carries no time/leave facts, so the section
// resolves with zero facts over the shared engine instead of
// inventing balances: the constructor is the independent
// resolution point the authorized time record will fill.
var workerTimeLeaveFacts = []sectionFact{}

// ResolveWorkerTimeLeave resolves the worker time and leave
// section for one person record over the shared section
// engine. The record is never mutated.
func ResolveWorkerTimeLeave(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.time_leave", "person.time_leave_detail", workerTimeLeaveFacts)
}
