// Package productui presents current-versus-proposed field
// comparisons. The presenter takes already-formatted
// current and proposed values — formatting stays with the
// values' owners, so this never becomes a second authority
// — and verdicts them exactly: equal text is unchanged,
// anything else is changed with the catalog's change
// marker. Whitespace counts; two unreported values are
// unchanged rather than a change.
package productui

// FieldComparison is the presented comparison of one
// field's current value against its proposed value. Marker
// carries the catalog's change copy when Changed and stays
// empty otherwise.
type FieldComparison struct {
	Label    string
	Current  string
	Proposed string
	Changed  bool
	Marker   string
}

// CompareFieldValue presents the comparison of one field's
// already-formatted current value against its proposed
// value. Comparison is exact.
func CompareFieldValue(locale LocaleContext, label, current, proposed string) FieldComparison {
	compared := FieldComparison{Label: label, Current: current, Proposed: proposed, Changed: current != proposed}
	if compared.Changed {
		compared.Marker = locale.Text("work.field_changed")
	}
	return compared
}
