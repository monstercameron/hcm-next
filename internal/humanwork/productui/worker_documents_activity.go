// Package productui exposes the worker record's two remaining
// exposure sections: the document section over the record's
// attachment facts and the activity section over its event
// facts. No authorized records project document or activity
// facts today, so both resolve over empty sets and rest on
// the shared worker-section engine; fallback locales carry
// the English copy.
package productui

var workerDocumentsFacts = []sectionFact{}

var workerActivityFacts = []sectionFact{}

// ResolveWorkerDocuments renders the document section for a
// worker record over the shared exposure engine.
func ResolveWorkerDocuments(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.documents", "person.documents_detail", workerDocumentsFacts)
}

// ResolveWorkerActivity renders the activity section for a
// worker record over the shared exposure engine.
func ResolveWorkerActivity(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.activity", "person.activity_detail", workerActivityFacts)
}
