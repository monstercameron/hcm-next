package productui

// FieldDisposition is presentation's display vocabulary for the spec
// field dispositions (SHOW, MASK, REDACT, HIDE, SUMMARY_ONLY,
// DERIVED_ONLY). Like PresentationEffect it mirrors server-composed
// verdict data: dispositions arrive on AuthorizedField, and
// presentation renders them — it never evaluates policy, decides which
// disposition a field deserves, or invents masking and summary text.
// The masking and summary/derived stand-ins are composed by the server
// into AuthorizedField.StandIn; presentation only chooses what may
// occupy a row.
type FieldDisposition string

const (
	FieldShow        FieldDisposition = "SHOW"
	FieldMask        FieldDisposition = "MASK"
	FieldRedact      FieldDisposition = "REDACT"
	FieldHide        FieldDisposition = "HIDE"
	FieldSummaryOnly FieldDisposition = "SUMMARY_ONLY"
	FieldDerivedOnly FieldDisposition = "DERIVED_ONLY"
)

// ProjectField renders one field honoring its disposition when the
// server sets one, else the legacy effect projection. The second result
// reports whether the field may occupy a row at all.
//
//   - SHOW renders the value (blank reads as not-reported).
//   - MASK, SUMMARY_ONLY, and DERIVED_ONLY render the server's
//     stand-in instead of the raw value; the semantic difference
//     between a mask, a summary, and a derived form is composed into
//     the stand-in server-side. A blank stand-in fails closed to
//     withheld: there is nothing safe to show.
//   - REDACT reads as redacted.
//   - HIDE omits the row entirely: no text and no reason, so the
//     field's existence stays out of the markup.
//   - Unknown dispositions, including the zero value handled by the
//     legacy path, fail closed to withheld, never open.
func ProjectField(locale LocaleContext, value string, field AuthorizedField) (ProjectedValue, bool) {
	if field.Disposition == "" {
		return ProjectAuthorizedValue(locale, value, field), true
	}
	switch field.Disposition {
	case FieldShow:
		if value == "" {
			return ProjectedValue{Text: locale.Text("common.not_reported")}, true
		}
		return ProjectedValue{Text: value}, true
	case FieldMask, FieldSummaryOnly, FieldDerivedOnly:
		if field.StandIn == "" {
			return ProjectedValue{Text: locale.Text("provenance.value.withheld"), Reason: field.Reason}, true
		}
		return ProjectedValue{Text: field.StandIn}, true
	case FieldRedact:
		return ProjectedValue{Text: locale.Text("provenance.value.redacted")}, true
	case FieldHide:
		return ProjectedValue{}, false
	default:
		return ProjectedValue{Text: locale.Text("provenance.value.withheld"), Reason: field.Reason}, true
	}
}
