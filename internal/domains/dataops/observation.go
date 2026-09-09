package dataops

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	observedFieldSchema  = "hcmnext.domains.dataops.ObservedField"
	observedRecordSchema = "hcmnext.domains.dataops.ObservedRecord"
	observationPageSchma = "hcmnext.domains.dataops.ObservationPage"
)

// Observation errors. All are matchable with errors.Is.
var (
	// ErrObservationIncomplete is returned when a page or record cannot say
	// where it came from, when it was retrieved, or what it decoded under. An
	// observation without those three is an assertion of unknown age from an
	// unknown system, which is worse than no observation at all.
	ErrObservationIncomplete = errors.New("dataops: observation is missing source, retrieval time, schema version or digest")
	// ErrObservationDuplicate is returned for two records about one subject in
	// one page, or two entries for one field in one record.
	ErrObservationDuplicate = errors.New("dataops: observation contains a duplicate subject or field")
	// ErrObservationOutOfTenant is returned when a page carries a record about
	// a subject outside the queried tenant.
	ErrObservationOutOfTenant = errors.New("dataops: observed subject is outside the queried tenant")
)

// MaxObservationPageRecords bounds one page. A comparison that pages without a
// bound is a comparison that can be made to allocate without limit by whatever
// is on the other side of the connector.
const MaxObservationPageRecords = 1000

// ObservedField is one field as an external system reported it.
//
// UpdatedAt is optional and its absence is load-bearing: a source that cannot
// say when it last changed a field cannot be used to decide which side is
// ahead, and the comparison degrades to CONFLICT rather than guessing.
type ObservedField struct {
	Field FieldID
	Kind  fielddiff.ValueKind
	// Value is the canonicalised value after the source's versioned
	// transformation, or the reason there is none.
	Value values.Presence[string]
	// UpdatedAt is when the source says it last changed this field.
	UpdatedAt values.Instant
}

// Validate reports whether the observed field is well formed.
func (f ObservedField) Validate() error {
	if err := f.Field.Validate(); err != nil {
		return err
	}
	if !f.Kind.Valid() {
		return fmt.Errorf("%w: field %s declares no value kind", ErrObservationIncomplete, f.Field)
	}
	if err := f.Value.Validate(); err != nil {
		return fmt.Errorf("dataops: observed field %s: %w", f.Field, err)
	}
	if f.UpdatedAt.IsSet() {
		if err := f.UpdatedAt.Validate(); err != nil {
			return fmt.Errorf("dataops: observed field %s updated_at: %w", f.Field, err)
		}
	}
	return nil
}

// side returns the comparison side for this observation.
func (f ObservedField) side() fielddiff.Side {
	return fielddiff.Side{Kind: f.Kind, Value: f.Value, UpdatedAt: f.UpdatedAt}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (f ObservedField) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	encoded, err := values.MarshalPresence(f.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	w := canonicalbytes.New(observedFieldSchema, dataopsSchemaVer).
		String("field", string(f.Field)).
		String("value_kind", f.Kind.String()).
		Field("value", encoded).
		Bool("updated_at?", f.UpdatedAt.IsSet())
	if f.UpdatedAt.IsSet() {
		w.Value("updated_at", f.UpdatedAt)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ObservedRecord is one external system's view of one subject.
type ObservedRecord struct {
	// Subject is the Human Capital Management Suite entity the external record was matched to.
	Subject values.EntityRef
	// ExternalID is the identifier the source uses, retained so a mismatch can
	// be traced back without re-running the match.
	ExternalID string
	// Exists reports whether the source has a record for this subject at all.
	// A subject the source has never heard of is not a subject with empty
	// fields.
	Exists bool
	Fields []ObservedField
	// Digest is the source-side digest of the record as retrieved.
	Digest string
}

// Validate reports whether the record is well formed.
func (r ObservedRecord) Validate() error {
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("dataops: observed record subject: %w", err)
	}
	if !r.Exists {
		if len(r.Fields) != 0 {
			return fmt.Errorf("dataops: observed record says the subject is absent but carries %d fields",
				len(r.Fields))
		}
		return nil
	}
	if r.ExternalID == "" || r.Digest == "" {
		return fmt.Errorf("%w: record for %s has external id %q digest %q",
			ErrObservationIncomplete, r.Subject, r.ExternalID, r.Digest)
	}
	seen := make(map[FieldID]struct{}, len(r.Fields))
	for _, f := range r.Fields {
		if err := f.Validate(); err != nil {
			return err
		}
		if _, dup := seen[f.Field]; dup {
			return fmt.Errorf("%w: field %s", ErrObservationDuplicate, f.Field)
		}
		seen[f.Field] = struct{}{}
	}
	return nil
}

// Lookup returns the observed field, if the record carries one.
func (r ObservedRecord) Lookup(field FieldID) (ObservedField, bool) {
	for _, f := range r.Fields {
		if f.Field == field {
			return f, true
		}
	}
	return ObservedField{}, false
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r ObservedRecord) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	sorted := append([]ObservedField(nil), r.Fields...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })
	w := canonicalbytes.New(observedRecordSchema, dataopsSchemaVer).
		Value("subject", r.Subject).
		String("external_id", r.ExternalID).
		Bool("exists", r.Exists).
		String("digest", r.Digest).
		Count("fields", len(sorted))
	for _, f := range sorted {
		w.Value("field", f)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ObservationPage is one page of external observations together with the
// provenance a comparison needs in order to be honest about it: which system
// produced it, under which schema version it was decoded, when it was
// retrieved, where the cursor stood, and the digest of what was retrieved.
//
// This is the port shape the connectivity plane will adapt to. It is defined
// here, in the consumer, so that the diagnostic states what it needs rather
// than inheriting whatever a connector happens to return.
type ObservationPage struct {
	// Source is the observing system identifier.
	Source string
	// SchemaVersion is the source contract version the page was decoded under.
	SchemaVersion string
	// RetrievedAt is when the page was retrieved from the source. Freshness is
	// judged against this, never against a local clock read at compare time.
	RetrievedAt values.RecordedAt
	// Cursor is the position this page was read at.
	Cursor string
	// NextCursor is the position of the following page, empty when the
	// observation set is exhausted.
	NextCursor string
	// Digest is the digest of the page as retrieved.
	Digest  string
	Records []ObservedRecord
}

// Validate reports whether the page is well formed.
func (p ObservationPage) Validate() error {
	if p.Source == "" || p.SchemaVersion == "" || p.Digest == "" {
		return fmt.Errorf("%w: source %q schema %q digest %q",
			ErrObservationIncomplete, p.Source, p.SchemaVersion, p.Digest)
	}
	if p.RetrievedAt.Canonical() == nil {
		return fmt.Errorf("%w: page has no retrieval time", ErrObservationIncomplete)
	}
	if len(p.Records) > MaxObservationPageRecords {
		return fmt.Errorf("dataops: observation page carries %d records, bound is %d",
			len(p.Records), MaxObservationPageRecords)
	}
	seen := make(map[values.EntityRef]struct{}, len(p.Records))
	for _, r := range p.Records {
		if err := r.Validate(); err != nil {
			return err
		}
		if _, dup := seen[r.Subject]; dup {
			return fmt.Errorf("%w: subject %s", ErrObservationDuplicate, r.Subject)
		}
		seen[r.Subject] = struct{}{}
	}
	return nil
}

// Lookup returns the record for a subject, if the page carries one.
func (p ObservationPage) Lookup(subject values.EntityRef) (ObservedRecord, bool) {
	for _, r := range p.Records {
		if r.Subject == subject {
			return r, true
		}
	}
	return ObservedRecord{}, false
}

// Watermark is the page's provenance without its records: the part a report
// cites so a reader can tell exactly which observation the finding rests on.
func (p ObservationPage) Watermark() ObservationWatermark {
	return ObservationWatermark{
		Source:        p.Source,
		SchemaVersion: p.SchemaVersion,
		RetrievedAt:   p.RetrievedAt,
		Cursor:        p.Cursor,
		Digest:        p.Digest,
		Records:       len(p.Records),
	}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p ObservationPage) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	sorted := append([]ObservedRecord(nil), p.Records...)
	sort.Slice(sorted, func(i, j int) bool {
		return string(sorted[i].Subject.Canonical()) < string(sorted[j].Subject.Canonical())
	})
	w := canonicalbytes.New(observationPageSchma, dataopsSchemaVer).
		String("source", p.Source).
		String("schema_version", p.SchemaVersion).
		Value("retrieved_at", p.RetrievedAt).
		String("cursor", p.Cursor).
		String("next_cursor", p.NextCursor).
		String("digest", p.Digest).
		Count("records", len(sorted))
	for _, r := range sorted {
		w.Value("record", r)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ObservationWatermark is the provenance of one page, cited by a finding or a
// report without copying the observed values into it.
type ObservationWatermark struct {
	Source        string
	SchemaVersion string
	RetrievedAt   values.RecordedAt
	Cursor        string
	Digest        string
	Records       int
}

// Validate reports whether the watermark is complete.
func (w ObservationWatermark) Validate() error {
	if w.Source == "" || w.SchemaVersion == "" || w.Digest == "" {
		return fmt.Errorf("%w: watermark source %q schema %q digest %q",
			ErrObservationIncomplete, w.Source, w.SchemaVersion, w.Digest)
	}
	if w.RetrievedAt.Canonical() == nil {
		return fmt.Errorf("%w: watermark has no retrieval time", ErrObservationIncomplete)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (w ObservationWatermark) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.ObservationWatermark", dataopsSchemaVer).
		String("source", w.Source).
		String("schema_version", w.SchemaVersion).
		Value("retrieved_at", w.RetrievedAt).
		String("cursor", w.Cursor).
		String("digest", w.Digest).
		Int("records", int64(w.Records)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ObservationQuery is what the observation port is asked for.
type ObservationQuery struct {
	Tenant values.TenantId
	// Source is the observing system to read from.
	Source string
	// Subjects is the exact population to observe. The port must not widen it.
	Subjects []values.EntityRef
	// Fields is the exact projection to observe.
	Fields []FieldID
	// Cursor is the page position to resume from, empty for the first page.
	Cursor string
	// Limit bounds the page size.
	Limit int
}

// Validate reports whether the query is well formed.
func (q ObservationQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("dataops: observation query tenant: %w", err)
	}
	if q.Source == "" {
		return fmt.Errorf("%w: observation query names no source", ErrObservationIncomplete)
	}
	if len(q.Subjects) == 0 {
		return fmt.Errorf("dataops: observation query names no subjects")
	}
	seen := make(map[values.EntityRef]struct{}, len(q.Subjects))
	for _, s := range q.Subjects {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("dataops: observation query subject: %w", err)
		}
		if s.Tenant != q.Tenant {
			return fmt.Errorf("%w: %s", ErrObservationOutOfTenant, s)
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("%w: subject %s", ErrObservationDuplicate, s)
		}
		seen[s] = struct{}{}
	}
	if q.Limit <= 0 || q.Limit > MaxObservationPageRecords {
		return fmt.Errorf("dataops: observation query limit %d is outside 1..%d",
			q.Limit, MaxObservationPageRecords)
	}
	_, err := normalizeFields(q.Fields)
	return err
}

// ObservationReader is the read port for external observations.
//
// It is a read port and nothing else: there is no write, redrive or refresh
// method, because a diagnostic that could ask a connector to go and fetch
// something is a diagnostic that can cause a provider call. The connectivity
// plane owns retrieval; this package consumes what was already retrieved.
type ObservationReader interface {
	// ObservationsAt returns one page of observations for the queried
	// population. A subject the source has no record for is returned as an
	// ObservedRecord with Exists false, or omitted; both are read as "the
	// source said nothing", never as "the source said empty".
	ObservationsAt(ctx context.Context, q ObservationQuery) (ObservationPage, error)
}
