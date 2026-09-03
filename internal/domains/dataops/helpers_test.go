package dataops_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Fields exercised by the DataOps tests. The vocabulary is open, so these are
// the tokens a tenant's authority matrix would use, not a closed Go enum.
const (
	fieldBase     dataops.FieldID = "compensation.base"
	fieldGrade    dataops.FieldID = "assignment.grade"
	fieldJobCode  dataops.FieldID = "assignment.job_code"
	fieldLegal    dataops.FieldID = "person.legal_name"
	fieldPrefName dataops.FieldID = "person.preferred_name"
)

const (
	localSystem    = "hcmnext"
	externalSystem = "incumbent-hris"
	authorityPol   = "authority.by_field/2026.1"
	policyVersion  = "authz.policy/2026.1"
	purpose        = "workforce_administration"
	sourceSchema   = "incumbent.worker/v3"
)

// instantAt parses an RFC3339 timestamp for a fixture.
func instantAt(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(parsed)
}

// parseInstant parses an RFC3339 timestamp without failing the test, for
// fuzz input that is allowed to be nonsense.
func parseInstant(text string) (values.Instant, error) {
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return values.Instant{}, err
	}
	return values.NewInstant(parsed), nil
}

// knownAt wraps an RFC3339 timestamp as a knowledge time.
func knownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("known at %q: %v", text, err)
	}
	return k
}

// recordedAt wraps an RFC3339 timestamp as a recorded time.
func recordedAt(t *testing.T, text string) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("recorded at %q: %v", text, err)
	}
	return r
}

// localDate parses an ISO date.
func localDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("parse date %q: %v", text, err)
	}
	return d
}

// calendar returns the fixture business calendar.
func calendar(t *testing.T) values.CalendarRef {
	t.Helper()
	cal, err := fixtures.Calendar()
	if err != nil {
		t.Fatalf("fixture calendar: %v", err)
	}
	return cal
}

// closedInterval builds a half-open date interval under the fixture calendar.
func closedInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewLocalDateInterval(localDate(t, start), localDate(t, end), calendar(t))
	if err != nil {
		t.Fatalf("interval [%s,%s): %v", start, end, err)
	}
	return iv
}

// openInterval builds an open-ended date interval under the fixture calendar.
func openInterval(t *testing.T, start string) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenLocalDateInterval(localDate(t, start), calendar(t))
	if err != nil {
		t.Fatalf("open interval [%s,): %v", start, err)
	}
	return iv
}

// revision builds a sequence revision on the worker stream.
func revision(t *testing.T, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision("worker", seq)
	if err != nil {
		t.Fatalf("revision %d: %v", seq, err)
	}
	return r
}

// subject returns the fixture worker reference.
func subject(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatalf("fixture worker: %v", err)
	}
	return ref
}

// otherSubject returns a second fixture worker reference.
func otherSubject(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixture worker: %v", err)
	}
	return ref
}

// localAuthority is the source authority of a locally mastered field.
func localAuthority() evidence.SourceAuthority {
	return evidence.SourceAuthority{
		Kind:      evidence.AuthorityLocal,
		System:    localSystem,
		PolicyRef: authorityPol,
	}
}

// externalAuthority is the source authority of an externally mastered field.
func externalAuthority() evidence.SourceAuthority {
	return evidence.SourceAuthority{
		Kind:      evidence.AuthorityExternalObservation,
		System:    externalSystem,
		PolicyRef: authorityPol,
	}
}

// derivedAuthorityKind is the authority of a computed value, which owns
// nothing and is therefore never mechanically repairable.
func derivedAuthorityKind() evidence.AuthorityKind { return evidence.AuthorityDerived }

// provenance builds a provenance record.
func provenance(t *testing.T, source, ref, at string) evidence.Provenance {
	t.Helper()
	return evidence.Provenance{Source: source, EvidenceRef: ref, RecordedAt: recordedAt(t, at)}
}

// memoryHistory is an in-memory dataops.FieldHistory over a fixed corpus. It
// applies the knowledge cut-off and the requested projection itself, so a test
// can prove the caller is not relying on a permissive port.
type memoryHistory struct {
	subject    values.EntityRef
	assertions []dataops.Assertion
	watermark  values.RevisionToken
	// Absent makes the store answer that the subject does not exist.
	Absent bool
	// Widen makes the store answer with a field nobody asked for, so the
	// caller's refusal can be exercised.
	Widen dataops.FieldID
	// Calls counts reads.
	Calls int
	// LastFields records the projection the store was actually asked for.
	LastFields []dataops.FieldID
}

// FieldHistoryAt implements dataops.FieldHistory.
func (m *memoryHistory) FieldHistoryAt(_ context.Context, q dataops.HistoryQuery) (dataops.HistorySet, error) {
	if err := q.Validate(); err != nil {
		return dataops.HistorySet{}, err
	}
	m.Calls++
	m.LastFields = append([]dataops.FieldID(nil), q.Fields...)
	if m.Absent || q.Subject != m.subject {
		return dataops.HistorySet{Subject: q.Subject, Exists: false}, nil
	}
	wanted := make(map[dataops.FieldID]struct{}, len(q.Fields))
	for _, f := range q.Fields {
		wanted[f] = struct{}{}
	}
	set := dataops.HistorySet{Subject: q.Subject, Exists: true, Watermark: m.watermark}
	for _, a := range m.assertions {
		if _, ok := wanted[a.Field]; !ok && a.Field != m.Widen {
			continue
		}
		if a.KnownAt.Instant().After(q.KnownAt.Instant()) {
			continue
		}
		if a.Class.IsClaim() && !q.IncludeClaims {
			continue
		}
		set.Assertions = append(set.Assertions, a)
	}
	return set, nil
}

// corpus builds the shared bitemporal fixture.
//
// The compensation timeline is the one from the effective-date debugger
// specification: a value from January, a supersession in June, a correction to
// that June value recorded in August, and a future-dated September value.
func corpus(t *testing.T) *memoryHistory {
	t.Helper()
	who := subject(t)
	money := func(id, amount, from, to, known, recorded string, change dataops.ChangeKind, corrects string) dataops.Assertion {
		iv := openInterval(t, from)
		if to != "" {
			iv = closedInterval(t, from, to)
		}
		return dataops.Assertion{
			ID:         id,
			Field:      fieldBase,
			Kind:       fielddiff.KindMoney,
			Value:      values.Value(amount),
			Effective:  iv,
			KnownAt:    knownAt(t, known),
			Revision:   revision(t, 100+uint64(len(id))),
			Class:      dataops.ClassDomainFact,
			Change:     change,
			Corrects:   corrects,
			Authority:  localAuthority(),
			Provenance: provenance(t, localSystem, "evidence:comp-"+id, recorded),
		}
	}

	assertions := []dataops.Assertion{
		money("a1", "120000.00 USD", "2026-01-01", "2026-06-01",
			"2025-12-15T00:00:00Z", "2025-12-15T00:00:00Z", dataops.ChangeInitial, ""),
		money("a2", "130000.00 USD", "2026-06-01", "2026-09-01",
			"2026-05-20T00:00:00Z", "2026-05-20T00:00:00Z", dataops.ChangeSupersession, ""),
		money("a3", "135000.00 USD", "2026-06-01", "2026-09-01",
			"2026-08-14T00:00:00Z", "2026-08-14T00:00:00Z", dataops.ChangeCorrection, "a2"),
		money("a4", "145000.00 USD", "2026-09-01", "",
			"2026-08-20T00:00:00Z", "2026-08-20T00:00:00Z", dataops.ChangeSupersession, ""),
		{
			ID:         "g1",
			Field:      fieldGrade,
			Kind:       fielddiff.KindEnum,
			Value:      values.Value("P3"),
			Effective:  openInterval(t, "2026-01-01"),
			KnownAt:    knownAt(t, "2025-12-15T00:00:00Z"),
			Revision:   revision(t, 201),
			Class:      dataops.ClassDomainFact,
			Change:     dataops.ChangeInitial,
			Authority:  localAuthority(),
			Provenance: provenance(t, localSystem, "evidence:grade-g1", "2025-12-15T00:00:00Z"),
		},
		{
			ID:         "j1",
			Field:      fieldJobCode,
			Kind:       fielddiff.KindEnum,
			Value:      values.Value("ENG-3"),
			Effective:  openInterval(t, "2026-01-01"),
			KnownAt:    knownAt(t, "2026-01-02T00:00:00Z"),
			Revision:   revision(t, 202),
			Class:      dataops.ClassExternalObservation,
			Change:     dataops.ChangeInitial,
			Authority:  externalAuthority(),
			Provenance: provenance(t, externalSystem, "evidence:job-j1", "2026-01-02T00:00:00Z"),
		},
		{
			ID:         "n1",
			Field:      fieldLegal,
			Kind:       fielddiff.KindString,
			Value:      values.Value("Jane Q. Doe"),
			Effective:  openInterval(t, "2026-01-01"),
			KnownAt:    knownAt(t, "2025-12-15T00:00:00Z"),
			Revision:   revision(t, 203),
			Class:      dataops.ClassDomainFact,
			Change:     dataops.ChangeInitial,
			Authority:  localAuthority(),
			Provenance: provenance(t, localSystem, "evidence:name-n1", "2025-12-15T00:00:00Z"),
		},
		{
			ID:         "p1",
			Field:      fieldPrefName,
			Kind:       fielddiff.KindString,
			Value:      values.Value("Janey"),
			Effective:  openInterval(t, "2026-02-01"),
			KnownAt:    knownAt(t, "2026-02-01T00:00:00Z"),
			Revision:   revision(t, 204),
			Class:      dataops.ClassClaim,
			Change:     dataops.ChangeInitial,
			Authority:  localAuthority(),
			Provenance: provenance(t, "worker-self-service", "evidence:claim-p1", "2026-02-01T00:00:00Z"),
		},
	}
	return &memoryHistory{subject: who, assertions: assertions, watermark: revision(t, 999)}
}

// allowAll builds an authorization decision that allows every listed field.
func allowAll(fields ...dataops.FieldID) dataops.Authorization {
	d := dataops.Authorization{
		PolicyVersion:      policyVersion,
		Purpose:            purpose,
		SubjectDisclosable: true,
		Fields:             map[dataops.FieldID]dataops.Ruling{},
	}
	for _, f := range fields {
		d.Fields[f] = dataops.Ruling{Effect: dataops.EffectAllow}
	}
	return d
}

// denyField turns one field of a decision into a denial.
func denyField(d dataops.Authorization, field dataops.FieldID, reason string) dataops.Authorization {
	out := d
	out.Fields = map[dataops.FieldID]dataops.Ruling{}
	for k, v := range d.Fields {
		out.Fields[k] = v
	}
	out.Fields[field] = dataops.Ruling{Effect: dataops.EffectDeny, Reason: reason}
	return out
}

// withheldSubject turns a decision into a non-disclosable subject.
func withheldSubject(d dataops.Authorization, reason string) dataops.Authorization {
	out := d
	out.SubjectDisclosable = false
	out.SubjectDenialReason = reason
	return out
}

// freshness is the fixture observation-age policy: one day.
func freshness() dataops.FreshnessPolicy {
	return dataops.FreshnessPolicy{Version: "freshness.policy/1.0.0", MaxAgeSeconds: 86400}
}

// memoryObservations is an in-memory dataops.ObservationReader that serves a
// fixed set of pages.
type memoryObservations struct {
	pages []dataops.ObservationPage
	// Calls counts reads, and LastQuery records the last query seen.
	Calls     int
	LastQuery dataops.ObservationQuery
}

// ObservationsAt implements dataops.ObservationReader.
func (m *memoryObservations) ObservationsAt(
	_ context.Context,
	q dataops.ObservationQuery,
) (dataops.ObservationPage, error) {
	if err := q.Validate(); err != nil {
		return dataops.ObservationPage{}, err
	}
	m.Calls++
	m.LastQuery = q
	for _, p := range m.pages {
		if p.Cursor == q.Cursor {
			return p, nil
		}
	}
	return dataops.ObservationPage{}, fmt.Errorf("no page at cursor %q", q.Cursor)
}

// observedField builds one observed field.
func observedField(
	t *testing.T,
	field dataops.FieldID,
	kind fielddiff.ValueKind,
	value values.Presence[string],
	updated string,
) dataops.ObservedField {
	t.Helper()
	f := dataops.ObservedField{Field: field, Kind: kind, Value: value}
	if updated != "" {
		f.UpdatedAt = instantAt(t, updated)
	}
	return f
}

// observationPage builds a single-record page.
func observationPage(
	t *testing.T,
	retrieved string,
	record dataops.ObservedRecord,
) dataops.ObservationPage {
	t.Helper()
	return dataops.ObservationPage{
		Source:        externalSystem,
		SchemaVersion: sourceSchema,
		RetrievedAt:   recordedAt(t, retrieved),
		Digest:        "sha256:page-" + retrieved,
		Records:       []dataops.ObservedRecord{record},
	}
}

// fieldsOf returns the field identifiers of a set of findings, in order.
func fieldsOf(findings []dataops.FieldFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, string(f.Field))
	}
	return out
}

// sortedCopy returns a sorted copy of a string slice.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
