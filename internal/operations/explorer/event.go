package explorer

import (
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// Gated field identifiers for one ledger event. A caller's authz.Decision
// must carry an ALLOW ruling for a field for that field's raw value to
// appear in an [EventView]; anything else - DENIED, REDACTED, WITHHELD, or
// simply absent - withholds it, matching authz.FieldDecision.Covers's own
// "a missing ruling is a refusal to answer" rule.
const (
	// FieldPayload gates the event's typed payload bytes.
	FieldPayload authz.FieldID = "operations.explorer.payload"
	// FieldProvenance gates who/what asserted the event (Authority,
	// SourceRef).
	FieldProvenance authz.FieldID = "operations.explorer.provenance"
)

// EventView is one ledger event as the explorer renders it: identity,
// timing and integrity metadata are always present, and Payload/Authority/
// SourceRef are present only when authorized.
type EventView struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	AssertionClass  string
	SchemaRef       string
	Digest          string
	DigestAlgorithm string
	OccurredAt      time.Time
	EffectiveAt     time.Time
	RecordedAt      time.Time
	CorrelationID   uuid.UUID
	IdempotencyKey  string

	// Authority/SourceRef are gated by FieldProvenance.
	Authority string
	SourceRef string

	// Payload/ArtifactRef are gated by FieldPayload. PayloadWithheld is the
	// typed unavailable state: it is true whenever Payload/ArtifactRef were
	// withheld, so an empty Payload can never be mistaken for "an event
	// with no payload" versus "a payload this caller may not see."
	Payload         []byte
	ArtifactRef     string
	PayloadWithheld bool

	// SubjectWithheld is true when the whole event was withheld because the
	// decision's subject was not disclosable: every gated field above is
	// then empty regardless of its individual field ruling.
	SubjectWithheld bool
}

// gateOpen reports whether field is affirmatively allowed by dec. A nil
// decision means "unrestricted" (an internal/system caller that has already
// authorized itself upstream).
func gateOpen(dec *authz.Decision, field authz.FieldID) bool {
	if dec == nil {
		return true
	}
	if !dec.SubjectDisclosable {
		return false
	}
	ruling, ok := dec.Fields[field]
	return ok && ruling.Effect == authz.EffectAllow
}

// redactEvent renders one ledger.EventRecord into an EventView, applying
// dec's field rulings. A nil dec is unrestricted.
func redactEvent(rec ledger.EventRecord, dec *authz.Decision) EventView {
	v := EventView{
		Tenant:          rec.Tenant,
		StreamKey:       rec.StreamKey,
		Sequence:        rec.Sequence,
		EventID:         rec.EventID,
		AssertionClass:  string(rec.AssertionClass),
		SchemaRef:       rec.SchemaRef,
		Digest:          rec.Digest,
		DigestAlgorithm: rec.DigestAlgorithm,
		OccurredAt:      rec.OccurredAt,
		EffectiveAt:     rec.EffectiveAt,
		RecordedAt:      rec.RecordedAt,
		CorrelationID:   rec.CorrelationID,
		IdempotencyKey:  rec.IdempotencyKey,
	}

	if dec != nil && !dec.SubjectDisclosable {
		v.SubjectWithheld = true
		v.PayloadWithheld = true
		return v
	}

	if gateOpen(dec, FieldProvenance) {
		v.Authority = rec.Authority
		v.SourceRef = rec.SourceRef
	}
	if gateOpen(dec, FieldPayload) {
		v.Payload = append([]byte(nil), rec.Payload...)
		v.ArtifactRef = rec.ArtifactRef
	} else {
		v.PayloadWithheld = true
	}
	return v
}
