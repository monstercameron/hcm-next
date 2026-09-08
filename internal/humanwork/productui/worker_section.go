package productui

// sectionFact is one worker section fact field: label key,
// field key, and locale-aware record accessor, in page
// order. Accessors needing no locale ignore it.
type sectionFact struct {
	labelKey string
	name     string
	value    func(LocaleContext, Person) string
}

// resolveWorkerSection resolves one worker object page
// section with the page adapter's exact fact behavior:
// silent servers pass values through (unavailable for
// empty), governed records project each field through its
// verdict with HIDE omitting the row. Sections share this
// engine so fact order, labels, and verdict handling cannot
// drift between sections. The record is never mutated.
func resolveWorkerSection(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord, titleKey, descriptionKey string, fields []sectionFact) WorkerSection {
	silent := len(verdicts) == 0
	verdictFields := map[string]AuthorizedField{}
	if record, ok := verdicts[person.ID]; ok {
		verdictFields = record.Fields
	}
	facts := make([]WorkerFact, 0, len(fields))
	for _, field := range fields {
		raw := field.value(locale, person)
		if silent {
			facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: valueOrUnavailableFor(locale, raw)})
			continue
		}
		projected, admitted := ProjectField(locale, raw, verdictFields[field.name])
		if !admitted {
			continue
		}
		facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: projected.Text})
	}
	return WorkerSection{
		Title:       locale.Text(titleKey),
		Description: locale.Text(descriptionKey),
		Facts:       facts,
	}
}
