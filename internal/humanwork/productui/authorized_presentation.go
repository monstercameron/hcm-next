package productui

// PresentationEffect is presentation's display vocabulary for one
// authorization verdict on a field. It mirrors the domain's effect
// vocabulary (allow, redacted, denied, withheld) without importing the
// evaluator: verdicts arrive as server-composed data, and presentation
// renders them — it never evaluates policy, calls the simulator, or
// mints an effect of its own.
type PresentationEffect string

const (
	PresentationAllow    PresentationEffect = "allow"
	PresentationRedact   PresentationEffect = "redacted"
	PresentationDenied   PresentationEffect = "denied"
	PresentationWithheld PresentationEffect = "withheld"
)

// AuthorizedField is the server's verdict on one field: its effect and
// the server's redaction-safe reason, rendered only where the effect
// withholds a value the viewer asked to see.
type AuthorizedField struct {
	Effect PresentationEffect
	Reason string
}

// ProjectedValue is one field ready to render: display text plus the
// server's reason where the text stands in for a withheld value.
type ProjectedValue struct {
	Text   string
	Reason string
}

// ProjectAuthorizedValue maps one value through its verdict to localized
// display text. Allowed values render (blank reads as not-reported);
// redacted reads as redacted; denied reads as unavailable with the
// server's reason; withheld — and any unknown effect, including the zero
// value — reads as withheld. Unknown fails closed, never open.
func ProjectAuthorizedValue(locale LocaleContext, value string, field AuthorizedField) ProjectedValue {
	switch field.Effect {
	case PresentationAllow:
		if value == "" {
			return ProjectedValue{Text: locale.Text("common.not_reported")}
		}
		return ProjectedValue{Text: value}
	case PresentationRedact:
		return ProjectedValue{Text: locale.Text("provenance.value.redacted")}
	case PresentationDenied:
		return ProjectedValue{Text: locale.Text("provenance.value.unavailable"), Reason: field.Reason}
	default:
		return ProjectedValue{Text: locale.Text("provenance.value.withheld"), Reason: field.Reason}
	}
}

// AuthorizedRecord is the server's verdict on one record: whether the
// subject is disclosable at all, the safe denial reason when it is not,
// and the per-field verdicts. A nil Fields map withholds every field.
type AuthorizedRecord struct {
	ID           string
	Disclosable  bool
	DenialReason string
	Fields       map[string]AuthorizedField
}

// ProjectedRecord is one record ready to render: either values projected
// field by field, or uniform withholding — reason only, never values.
type ProjectedRecord struct {
	ID       string
	Withheld bool
	Reason   string
	Values   map[string]ProjectedValue
}

// ProjectAuthorizedRecord maps one record's values through its verdict. A
// nil verdict or a non-disclosable subject projects to uniform
// withholding: no values leak, and the denial reason (or the unavailable
// fallback when the server stays silent) is the entire record. Fields
// without a verdict fail closed to withheld. Inputs are never mutated.
func ProjectAuthorizedRecord(locale LocaleContext, id string, values map[string]string, verdict *AuthorizedRecord) ProjectedRecord {
	if verdict == nil || !verdict.Disclosable {
		reason := locale.Text("provenance.value.unavailable")
		if verdict != nil && verdict.DenialReason != "" {
			reason = verdict.DenialReason
		}
		return ProjectedRecord{ID: id, Withheld: true, Reason: reason}
	}
	projected := ProjectedRecord{ID: id, Values: make(map[string]ProjectedValue, len(values))}
	for name, value := range values {
		projected.Values[name] = ProjectAuthorizedValue(locale, value, verdict.Fields[name])
	}
	return projected
}
