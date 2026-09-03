package population_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// testTenant is the fixture tenant every test definition is scoped to.
const testTenant values.TenantId = "acme"

// workerID returns a distinct, canonically valid UUID for fixture subject n.
func workerID(n int) string {
	return fmt.Sprintf("3f2504e0-4f89-41d3-9a0c-%012x", n)
}

// worker returns a tenant-scoped EntityRef for fixture subject n.
func worker(n int) values.EntityRef {
	return values.EntityRef{Tenant: testTenant, Kind: "worker", Id: workerID(n)}
}

// testCatalog is the fixture field catalog every criteria-compiling test
// shares: grade and active are ordinary queryable string fields, salary is a
// sensitive queryable decimal field, and ssn is present but declared
// not-queryable, standing in for a field whose use in population membership
// would itself be an unauthorized inference.
func testCatalog() population.FieldCatalog {
	return population.FieldCatalog{
		"grade":  {Type: population.FieldTypeString, Queryable: true, IndexHint: "idx_grade"},
		"active": {Type: population.FieldTypeString, Queryable: true, IndexHint: "idx_active"},
		"salary": {Type: population.FieldTypeDecimal, Queryable: true, Sensitive: true, IndexHint: "idx_salary"},
		"hired":  {Type: population.FieldTypeDate, Queryable: true},
		"ssn":    {Type: population.FieldTypeString, Queryable: false},
	}
}

// fieldEquals builds a leaf EQUALS predicate.
func fieldEquals(field, value string) population.Predicate {
	return population.Predicate{Kind: population.PredicateEquals, Field: field, Values: []string{value}}
}

// and builds an AND composite.
func and(children ...population.Predicate) population.Predicate {
	return population.Predicate{Kind: population.PredicateAnd, Children: children}
}

// validDefinition returns a fully-populated, publishable definition: WORKER
// subjects, tenant/org/purpose scope, an explicit temporal and disclosure
// policy, and a typed two-leaf criteria (grade = P3 AND active = true).
func validDefinition() population.Definition {
	return population.Definition{
		ID:                "pop-def-p3-active",
		Owner:             "rewards",
		Subject:           population.SubjectWorker,
		Scope:             population.Scope{Tenant: testTenant, OrganizationScopeRef: "org:acme:root", Purpose: "reward_eligibility"},
		TemporalBasis:     population.TemporalBasisAsOfCaller,
		UnknownDisclosure: population.UnknownDisclosureExcludeAndReport,
		CountDisclosure:   population.CountDisclosureExact,
		Criteria: population.Criteria{Root: and(
			fieldEquals("grade", "P3"),
			fieldEquals("active", "true"),
		)},
	}
}

func mustPlan(t testing.TB, def population.Definition) population.CompiledPlan {
	t.Helper()
	plan, err := population.Compile(def.Criteria, testCatalog())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return plan
}

func mustInstant(t testing.TB, seconds int64) values.Instant {
	t.Helper()
	inst, err := values.NewInstantFromUnix(seconds, 0)
	if err != nil {
		t.Fatalf("instant: %v", err)
	}
	return inst
}

func mustKnownAt(t testing.TB, seconds int64) values.KnownAt {
	t.Helper()
	inst := mustInstant(t, seconds)
	k, err := values.NewKnownAt(inst)
	if err != nil {
		t.Fatalf("known-at: %v", err)
	}
	return k
}

func mustPolicyVersions() population.PolicyVersions {
	return population.PolicyVersions{
		AuthZVersion:        "authz.v1",
		PrivacyVersion:      "privacy.v1",
		OrganizationVersion: "org.v1",
		PurposeVersion:      "purpose.v1",
	}
}

// fakeReader is an in-memory FactReader fake: it holds one Fact per
// subject/field, a fixed subject enumeration and a fixed watermark, so a test
// can construct exactly the bitemporal scenario it wants to assert on.
type fakeReader struct {
	subjects     []values.EntityRef
	subjectsErr  error
	facts        map[string]map[string]population.Fact
	watermark    values.Instant
	watermarkErr error
	readErr      map[string]error
}

func newFakeReader() *fakeReader {
	return &fakeReader{facts: map[string]map[string]population.Fact{}, readErr: map[string]error{}}
}

func (f *fakeReader) withSubject(ref values.EntityRef) *fakeReader {
	f.subjects = append(f.subjects, ref)
	return f
}

func (f *fakeReader) withFact(ref values.EntityRef, field string, fact population.Fact) *fakeReader {
	m, ok := f.facts[ref.String()]
	if !ok {
		m = map[string]population.Fact{}
		f.facts[ref.String()] = m
	}
	m[field] = fact
	return f
}

func (f *fakeReader) withWatermark(at values.Instant) *fakeReader {
	f.watermark = at
	return f
}

func (f *fakeReader) withReadErr(ref values.EntityRef, field string, err error) *fakeReader {
	f.readErr[ref.String()+"|"+field] = err
	return f
}

func (f *fakeReader) Subjects(_ context.Context, _ population.SubjectKind, _ values.Instant) ([]values.EntityRef, error) {
	return f.subjects, f.subjectsErr
}

func (f *fakeReader) ReadFact(_ context.Context, subject values.EntityRef, field string, _ values.Instant, _ values.KnownAt) (population.Fact, error) {
	if err, ok := f.readErr[subject.String()+"|"+field]; ok {
		return population.Fact{}, err
	}
	if m, ok := f.facts[subject.String()]; ok {
		if fact, ok := m[field]; ok {
			return fact, nil
		}
	}
	return population.Fact{Presence: values.Unknown[string]("no fact recorded")}, nil
}

func (f *fakeReader) Watermark(_ context.Context, _ population.SubjectKind) (values.Instant, error) {
	return f.watermark, f.watermarkErr
}

// valueFact builds a present fact known at knownAt.
func valueFact(text string, knownAt values.KnownAt) population.Fact {
	return population.Fact{Presence: values.Value(text), KnownAt: knownAt}
}

// valueFactPresence builds a bare VALUE presence, for tests that set KnownAt
// separately on the surrounding Fact.
func valueFactPresence(text string) values.Presence[string] { return values.Value(text) }

// unavailableFact builds an UNAVAILABLE presence.
func unavailableFact() values.Presence[string] {
	return values.Unavailable[string]("source did not respond")
}

// redactedFact builds a REDACTED presence.
func redactedFact() values.Presence[string] {
	return values.Redacted[string]("caller is not authorized to read this field")
}

// invalidRef returns a syntactically well-typed but unresolvable EntityRef:
// its Id is not a canonical UUID or ULID, so Validate fails.
func invalidRef() values.EntityRef {
	return values.EntityRef{Tenant: testTenant, Kind: "worker", Id: "not-a-valid-identity"}
}

// testWatermarks returns a fixture watermark map for the WORKER subject kind,
// so snapshot tests can bind a time context without hand-building a map at
// every call site.
func testWatermarks(t testing.TB) map[population.SubjectKind]values.Instant {
	t.Helper()
	return map[population.SubjectKind]values.Instant{population.SubjectWorker: mustInstant(t, 1_000_000)}
}
