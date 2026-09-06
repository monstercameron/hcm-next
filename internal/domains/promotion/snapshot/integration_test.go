package snapshot_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/domains/budget"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/org"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// The integration fixture's own coordinates. The subject and the manager are
// the corpus's two people-ops workers; the target position is the one the
// second of them occupies, which is a real job_position row rather than an
// identifier this test made up.
const (
	integrationSubjectKey  = "omar-reyes"
	integrationManagerKey  = "noor-haddad"
	integrationPositionKey = "POS-HRBP-205"
	integrationBandKey     = "BAND-OPS-P3-USEAST"
)

// TestTodo_PROMO_001_Integration builds the Promotion input snapshot for the
// seeded harborcare-demo fixture through the real aggregate stores, and holds
// the two stability claims that make a snapshot worth binding to a proposal:
// the same tenant at the same coordinate builds byte-identical snapshots, and
// a write that touches none of the bound inputs does not move the digest.
func TestTodo_PROMO_001_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertHarborcareTenant(t, db)

	var loaded *aggregates.LoadedFixtures
	inTx(t, db, func(tx dbport.Tx) error {
		var err error
		loaded, err = aggregates.LoadFixtures(ctx, tx, tenantID)
		return err
	})
	if len(loaded.WorkerID) == 0 {
		t.Fatal("LoadFixtures seeded no workers")
	}

	readers, req := integrationCase(t, db, loaded)

	first, err := promosnapshot.Build(ctx, readers, req)
	if err != nil {
		t.Fatalf("Build over the real stores: %v", err)
	}
	if first.Completeness.Overall != "SATISFIED" {
		t.Fatalf("the seeded fixture is not complete:\n%s", first.Explain())
	}

	t.Run("two builds over the same stores are byte-identical", func(t *testing.T) {
		second, err := promosnapshot.Build(ctx, readers, req)
		if err != nil {
			t.Fatalf("second Build: %v", err)
		}
		assertSnapshotsIdentical(t, first, second)
	})

	t.Run("an unrelated write leaves the snapshot unchanged", func(t *testing.T) {
		// A whole new person in the same tenant: a real, committed write that
		// no bound input reads. If it moved the digest, the snapshot would be
		// binding something other than the inputs it names, and every proposal
		// in the tenant would be invalidated by every unrelated change.
		writeUnrelatedPerson(t, db, tenantID)

		after, err := promosnapshot.Build(ctx, readers, req)
		if err != nil {
			t.Fatalf("Build after an unrelated write: %v", err)
		}
		assertSnapshotsIdentical(t, first, after)
	})

	t.Run("a material write does move the digest", func(t *testing.T) {
		// The counterpart claim: the snapshot is stable because nothing it
		// reads changed, not because it is insensitive. Superseding the
		// subject's base pay is a change to a bound input, and it must show.
		writeNewBasePay(t, db, tenantID, loaded)

		after, err := promosnapshot.Build(ctx, readers, req)
		if err != nil {
			t.Fatalf("Build after a material write: %v", err)
		}
		if after.Digest == first.Digest {
			t.Fatalf("superseding the subject's base pay left the digest at %s", first.Digest)
		}
	})
}

// assertSnapshotsIdentical compares everything two builds are supposed to
// agree on: both digests, and every input's descriptors and disclosed text.
// Comparing the digests alone would pass a snapshot that agreed by accident.
func assertSnapshotsIdentical(t *testing.T, want, got promosnapshot.PromotionInputSnapshot) {
	t.Helper()
	if got.Digest != want.Digest {
		t.Fatalf("material digest %s, want %s", got.Digest, want.Digest)
	}
	if got.Reads.Digest != want.Reads.Digest {
		t.Fatalf("read digest %s, want %s", got.Reads.Digest, want.Reads.Digest)
	}
	if got.Completeness.Digest != want.Completeness.Digest {
		t.Fatalf("completeness digest %s, want %s", got.Completeness.Digest, want.Completeness.Digest)
	}
	wantInputs, gotInputs := want.Inputs(), got.Inputs()
	if len(gotInputs) != len(wantInputs) {
		t.Fatalf("bound %d input(s), want %d", len(gotInputs), len(wantInputs))
	}
	for i := range wantInputs {
		// Input carries a ResourceKey, which holds a slice and is therefore
		// not comparable with ==. Rendering both sides compares every field,
		// the key's segments included, which is what "byte-identical" means.
		got, want := fmt.Sprintf("%+v", gotInputs[i]), fmt.Sprintf("%+v", wantInputs[i])
		if got != want {
			t.Fatalf("input %s differs:\n want %s\n  got %s", wantInputs[i].Name, want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Database helpers
// ---------------------------------------------------------------------------

// insertHarborcareTenant registers the demo tenant under the key the fixture
// corpus and this package's request both name.
func insertHarborcareTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, string(fixtures.Tenant), "HarborCare Demo")
	return id
}

// inTx runs fn in one transaction on the migration connection and commits it.
func inTx(t *testing.T, db *pgtest.DB, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// writeUnrelatedPerson commits a person no bound input reads.
func writeUnrelatedPerson(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	inTx(t, db, func(tx dbport.Tx) error {
		person, err := aggregates.NewPerson(tenantID, uuid.New(),
			time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), nil,
			time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			"ACTIVE", "Unrelated Person", "Unrelated")
		if err != nil {
			return err
		}
		_, err = aggregates.PeopleStore{}.PutPerson(ctx, tx, person)
		return err
	})
}

// writeNewBasePay supersedes the subject's BASE_PAY component with a new
// amount, which is a change to a bound input.
func writeNewBasePay(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, loaded *aggregates.LoadedFixtures) {
	t.Helper()
	ctx := context.Background()
	amount, err := values.NewMoney("95000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	inTx(t, db, func(tx dbport.Tx) error {
		component, err := aggregates.NewCompensationComponent(tenantID, loaded.ComponentID, loaded.PackageID,
			time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC), nil, time.Now().UTC(),
			"BASE_PAY", amount, "ANNUAL")
		if err != nil {
			return err
		}
		_, err = aggregates.CompensationStore{}.PutCompensationComponent(ctx, tx, component)
		return err
	})
}

// ---------------------------------------------------------------------------
// Store-backed read ports
// ---------------------------------------------------------------------------

// integrationCase composes the six store-backed ports and the request that
// reads through them.
//
// Every port below reads its own aggregate table through the same Current*
// accessor a composed cell would use, and derives its revision, authority and
// provenance from the row's own bitemporal envelope -- its entity id, its
// recorded-at and its content digest -- rather than from anything this test
// invents. That is what makes the stability claims above claims about the
// stores, not about a fixture.
func integrationCase(
	t *testing.T, db *pgtest.DB, loaded *aggregates.LoadedFixtures,
) (promosnapshot.Readers, promosnapshot.Request) {
	t.Helper()
	subject := values.EntityRef{
		Tenant: fixtures.Tenant, Kind: people.KindWorker,
		Id: loaded.WorkerID[integrationSubjectKey].String(),
	}
	manager := values.EntityRef{
		Tenant: fixtures.Tenant, Kind: people.KindWorker,
		Id: loaded.WorkerID[integrationManagerKey].String(),
	}
	target := values.EntityRef{
		Tenant: fixtures.Tenant, Kind: position.KindPosition,
		Id: loaded.PositionID[integrationPositionKey].String(),
	}

	// The loader stamps the catalog rows with its own wall clock, so the
	// knowledge horizon has to sit after them. It is read once and reused by
	// every build, which is what makes two builds comparable at all.
	horizon := mustKnownAtInstant(t, time.Now().UTC().Add(time.Hour))
	source := &aggregateSource{db: db, tenant: loaded.Tenant, loaded: loaded, subject: subject, manager: manager}

	readers := promosnapshot.Readers{
		Worker:       source,
		Org:          aggregateOrgSource{source},
		Position:     source,
		Compensation: source,
		Bands:        source,
		Budget:       source,
	}
	req := fixtureRequest(t)
	req.Subject = subject
	req.TargetPosition = target
	req.KnownAt = horizon
	req.BudgetScope = fixtureTargetJob + "/" + fixtureTargetGrade
	req.BudgetPeriod = "legacy-scenario-cycle"
	req.SourceConnection = "pgtest"
	return readers, req
}

func mustKnownAtInstant(t *testing.T, at time.Time) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return known
}

// aggregateSource implements all six read ports over the real aggregate
// stores. It is one type rather than six because every port needs the same
// connection, tenant and identifier map, and splitting it would only duplicate
// that wiring.
type aggregateSource struct {
	db      *pgtest.DB
	tenant  uuid.UUID
	loaded  *aggregates.LoadedFixtures
	subject values.EntityRef
	manager values.EntityRef
}

// businessAt is the instant the Current* accessors are asked at. It is far
// enough forward to cover the loader's own wall-clock catalog rows and the
// fixture's 2026-06-01 compensation effective date alike.
func (s *aggregateSource) businessAt() time.Time { return time.Now().UTC().Add(24 * time.Hour) }

// rowRevision turns an aggregate row's envelope into a revision token: the
// entity's own stream, addressed by the row's content digest. Two reads of an
// unchanged row therefore produce the same token, and a superseding write
// produces a different one.
func rowRevision(kind string, entityID uuid.UUID, digest string) (values.RevisionToken, error) {
	return values.NewOpaqueRevision("aggregate."+kind+"."+entityID.String(), []byte(digest))
}

func rowAuthority(system string) evidence.SourceAuthority {
	return evidence.SourceAuthority{
		Kind: evidence.AuthorityLocal, System: system, PolicyRef: "harborcare.aggregates/2026.1",
	}
}

func rowProvenance(system, digest string, recordedAt time.Time) (evidence.Provenance, error) {
	recorded, err := values.NewRecordedAt(values.NewInstant(recordedAt.UTC()))
	if err != nil {
		return evidence.Provenance{}, err
	}
	return evidence.Provenance{Source: system, EvidenceRef: digest, RecordedAt: recorded}, nil
}

// WorkerFactsAt implements people.WorkerFacts over worker, employment,
// assignment, organization_unit and job_position.
func (s *aggregateSource) WorkerFactsAt(ctx context.Context, q people.FactQuery) (people.FactSet, error) {
	if q.Worker != s.subject {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}
	at := s.businessAt()
	store := aggregates.PeopleStore{}
	orgStore := aggregates.OrganizationStore{}

	worker, err := store.CurrentWorker(ctx, s.db.Conn, s.tenant, s.loaded.WorkerID[integrationSubjectKey], at)
	if err != nil {
		return people.FactSet{}, err
	}
	employment, err := store.CurrentEmployment(ctx, s.db.Conn, s.tenant, s.loaded.EmploymentID[integrationSubjectKey], at)
	if err != nil {
		return people.FactSet{}, err
	}
	assignment, err := store.CurrentAssignment(ctx, s.db.Conn, s.tenant, s.loaded.AssignmentID[integrationSubjectKey], at)
	if err != nil {
		return people.FactSet{}, err
	}
	unit, err := orgStore.CurrentOrganizationUnit(ctx, s.db.Conn, s.tenant, *assignment.OrganizationRef, at)
	if err != nil {
		return people.FactSet{}, err
	}
	jobPosition, err := orgStore.CurrentJobPosition(ctx, s.db.Conn, s.tenant, *assignment.PositionRef, at)
	if err != nil {
		return people.FactSet{}, err
	}

	hireDate := ""
	if employment.HireDate != nil {
		hireDate = employment.HireDate.UTC().Format(time.DateOnly)
	}
	sources := []struct {
		field    people.FieldID
		value    string
		envelope aggregates.Envelope
	}{
		{people.FieldLifecycleStatus, worker.LifecycleStatus, worker.Envelope},
		{people.FieldEmploymentStatus, employment.EmploymentStatus, employment.Envelope},
		{people.FieldHireDate, hireDate, employment.Envelope},
		{people.FieldJobCode, assignment.JobCode, assignment.Envelope},
		{people.FieldGrade, assignment.Grade, assignment.Envelope},
		{people.FieldOrgUnit, unit.Code, assignment.Envelope},
		{people.FieldPositionID, jobPosition.PositionCode, assignment.Envelope},
		{people.FieldPayZone, assignment.PayZone, assignment.Envelope},
		{people.FieldManagerRelation, assignment.ManagerRelationshipRef, assignment.Envelope},
	}

	requested := map[people.FieldID]bool{}
	for _, field := range q.Fields {
		requested[field] = true
	}
	facts := make([]people.Fact, 0, len(q.Fields))
	for _, src := range sources {
		if !requested[src.field] {
			continue
		}
		fact, err := s.factFrom(src.field, src.value, src.envelope)
		if err != nil {
			return people.FactSet{}, err
		}
		facts = append(facts, fact)
	}
	watermark, err := rowRevision("assignment", assignment.EntityID, assignment.Digest)
	if err != nil {
		return people.FactSet{}, err
	}
	return people.FactSet{Worker: q.Worker, Exists: true, Facts: facts, Watermark: watermark}, nil
}

// factFrom turns one column of one aggregate row into a governed fact,
// carrying the row's own bitemporal and evidence coordinates.
func (s *aggregateSource) factFrom(
	field people.FieldID, value string, env aggregates.Envelope,
) (people.Fact, error) {
	effective, err := values.NewOpenInstantInterval(values.NewInstant(env.EffectiveFrom.UTC()))
	if err != nil {
		return people.Fact{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(env.RecordedAt.UTC()))
	if err != nil {
		return people.Fact{}, err
	}
	revision, err := rowRevision(string(env.Kind), env.EntityID, env.Digest)
	if err != nil {
		return people.Fact{}, err
	}
	provenance, err := rowProvenance("hcmnext.people", env.Digest, env.RecordedAt)
	if err != nil {
		return people.Fact{}, err
	}
	presence := values.Value(value)
	if value == "" {
		presence = values.Absent[string]()
	}
	return people.Fact{
		Field:      field,
		Value:      presence,
		Effective:  effective,
		KnownAt:    knownAt,
		Revision:   revision,
		Authority:  rowAuthority("hcmnext.people"),
		Provenance: provenance,
	}, nil
}

// aggregateOrgSource is the org read port. It is a separate type only because
// people.WorkerFacts and org.WorkerFacts both declare a WorkerFactsAt method
// with different signatures, so one Go type cannot satisfy both.
type aggregateOrgSource struct{ *aggregateSource }

// WorkerFactsAt implements org.WorkerFacts. The relationship is the
// assignment row's own manager_relationship_ref; the physical fixture masters
// no manager edge, so the endpoint is the tenant's other people-ops worker,
// which is the one coordinate here that the store does not supply.
func (s aggregateOrgSource) WorkerFactsAt(ctx context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	at := s.businessAt()
	assignment, err := aggregates.PeopleStore{}.CurrentAssignment(
		ctx, s.db.Conn, s.tenant, s.loaded.AssignmentID[integrationSubjectKey], at)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	watermark, err := rowRevision("org.graph", s.tenant, assignment.Digest)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	if q.Worker != s.subject {
		return org.WorkerFactSet{
			Worker: q.Worker, Exists: false, Watermark: watermark, PolicyVersion: "harborcare.aggregates/2026.1",
		}, nil
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(assignment.EffectiveFrom.UTC()))
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(assignment.RecordedAt.UTC()))
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	revision, err := rowRevision("assignment", assignment.EntityID, assignment.Digest)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	provenance, err := rowProvenance("hcmnext.org", assignment.Digest, assignment.RecordedAt)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	return org.WorkerFactSet{
		Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: "harborcare.aggregates/2026.1",
		Relationships: []org.ManagerRelationshipFact{{
			RelationshipID: assignment.ManagerRelationshipRef,
			Type:           org.RelationshipDirectManager,
			Worker:         s.subject,
			Manager:        s.manager,
			AssignmentID:   assignment.EntityID.String(),
			Effective:      effective,
			KnownAt:        knownAt,
			Revision:       revision,
			Authority:      rowAuthority("hcmnext.org"),
			Provenance:     provenance,
		}},
	}, nil
}

// PositionRevisionAt implements position.PositionFacts over job_position.
//
// The physical row carries capacity_fte but no head count, so a position with
// any capacity at all is reported as one head. That is a property of the
// current schema and is stated here rather than hidden: a schema that grows a
// head column replaces this line, it does not invalidate the snapshot.
func (s *aggregateSource) PositionRevisionAt(
	ctx context.Context, q position.PositionQuery,
) (position.PositionRevision, bool, error) {
	entityID, err := uuid.Parse(q.Position.Id)
	if err != nil {
		return position.PositionRevision{}, false, nil
	}
	row, err := aggregates.OrganizationStore{}.CurrentJobPosition(ctx, s.db.Conn, s.tenant, entityID, s.businessAt())
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	unit, err := aggregates.OrganizationStore{}.CurrentOrganizationUnit(
		ctx, s.db.Conn, s.tenant, row.OrganizationRef, s.businessAt())
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	capacityFTE, err := storedDecimal(row.CapacityFTE, 4)
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	heads := int64(0)
	if capacityFTE.Sign() > 0 {
		heads = 1
	}
	effective, err := values.NewOpenLocalDateInterval(
		mustLocalDateFromTime(row.EffectiveFrom), integrationCalendar())
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	revision, err := rowRevision("job_position", row.EntityID, row.Digest)
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	provenance, err := rowProvenance("hcmnext.position", row.Digest, row.RecordedAt)
	if err != nil {
		return position.PositionRevision{}, false, err
	}
	return position.PositionRevision{
		Position:    q.Position,
		Revision:    revision,
		Effective:   effective,
		Lifecycle:   position.Lifecycle(row.LifecycleState),
		JobCode:     fixtureTargetJob,
		OrgUnit:     unit.Code,
		LegalEntity: "HarborCare US Inc.",
		Capacity:    position.CapacityPolicy{CapacityFTE: capacityFTE, CapacityHeads: heads},
		Authority:   rowAuthority("hcmnext.position"),
		Provenance:  provenance,
	}, true, nil
}

// CompensationFactsAt implements rewards.CompensationFacts over
// compensation_package and its BASE_PAY component.
func (s *aggregateSource) CompensationFactsAt(
	ctx context.Context, q rewards.CompensationFactsQuery,
) (rewards.CompensationFactSet, error) {
	if q.Worker != s.subject {
		return rewards.CompensationFactSet{Worker: q.Worker, Exists: false}, nil
	}
	at := s.businessAt()
	store := aggregates.CompensationStore{}
	pkg, err := store.CurrentCompensationPackage(ctx, s.db.Conn, s.tenant, s.loaded.PackageID, at)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	component, err := store.CurrentCompensationComponent(ctx, s.db.Conn, s.tenant, s.loaded.ComponentID, at)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	amount, err := storedMoney(component.Amount, component.Currency)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(component.EffectiveFrom.UTC()))
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(component.RecordedAt.UTC()))
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	revision, err := rowRevision("compensation_component", component.EntityID, component.Digest)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	watermark, err := rowRevision("compensation_package", pkg.EntityID, pkg.Digest+component.Digest)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	provenance, err := rowProvenance("hcmnext.rewards", component.Digest, component.RecordedAt)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	return rewards.CompensationFactSet{
		Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: "harborcare.aggregates/2026.1",
		Fact: rewards.CompensationFact{
			Worker:     q.Worker,
			BasePay:    amount,
			PayBandRef: integrationBandKey,
			Currency:   component.Currency,
			PayBasis:   rewards.PayBasisAnnualSalary,
			Frequency:  component.Frequency,
			Effective:  effective,
			KnownAt:    knownAt,
			Revision:   revision,
			Authority:  rowAuthority("hcmnext.rewards"),
			Provenance: provenance,
		},
	}, nil
}

// LookupBand implements rewards.PayBandCatalog over compensation_band.
func (s *aggregateSource) LookupBand(ctx context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	row, err := aggregates.CompensationStore{}.CurrentCompensationBand(
		ctx, s.db.Conn, s.tenant, s.loaded.BandID[integrationBandKey], s.businessAt())
	if err != nil {
		return rewards.BandRecord{}, err
	}
	if row.JobCode != q.JobCode || row.Grade != q.Grade || row.PayZone != q.PayZone || row.Currency != q.Currency {
		return rewards.BandRecord{}, fmt.Errorf("%w: %s/%s/%s", rewards.ErrBandNotFound, q.JobCode, q.Grade, q.PayZone)
	}
	bounds := make([]values.Money, 0, 3)
	for _, text := range []string{row.Minimum, row.Midpoint, row.Maximum} {
		money, err := storedMoney(text, row.Currency)
		if err != nil {
			return rewards.BandRecord{}, err
		}
		bounds = append(bounds, money)
	}
	provenance, err := rowProvenance("hcmnext.rewards.catalog", row.Digest, row.RecordedAt)
	if err != nil {
		return rewards.BandRecord{}, err
	}
	return rewards.BandRecord{
		Band: payband.Band{
			ID:      integrationBandKey,
			Version: row.Digest[:12],
			Scope: payband.Scope{
				JobCode: row.JobCode, Grade: row.Grade, PayZone: row.PayZone,
			},
			Minimum: bounds[0], Midpoint: bounds[1], Maximum: bounds[2],
		},
		CatalogVersion: "harborcare.aggregates/2026.1",
		Blocking:       false,
		Authority:      rowAuthority("hcmnext.rewards.catalog"),
		Provenance:     provenance,
	}, nil
}

// CompensationBudgetAt implements promosnapshot.BudgetFacts over
// workforce_budget.
func (s *aggregateSource) CompensationBudgetAt(
	ctx context.Context, q promosnapshot.BudgetQuery,
) (budget.BudgetAuthorityRef, bool, error) {
	row, err := aggregates.CompensationStore{}.CurrentWorkforceBudget(
		ctx, s.db.Conn, s.tenant, s.loaded.BudgetID, s.businessAt())
	if err != nil {
		return budget.BudgetAuthorityRef{}, false, err
	}
	if row.Scope != q.Scope || row.Period != q.Period {
		return budget.BudgetAuthorityRef{}, false, nil
	}
	quantity, err := storedDecimal(row.AvailableQuantity, 2)
	if err != nil {
		return budget.BudgetAuthorityRef{}, false, err
	}
	return budget.BudgetAuthorityRef{
		BudgetType:        budget.BudgetType(row.BudgetType),
		OwnerSystem:       row.OwnerSystem,
		Scope:             row.Scope,
		Period:            row.Period,
		Currency:          row.Currency,
		Unit:              budget.Unit(row.Unit),
		BaselineVersion:   "harborcare.aggregates/2026.1",
		AvailableQuantity: quantity,
		Evidence: budget.ObservationEvidence{
			ObservationID:   row.EntityID.String(),
			SourceWatermark: values.NewInstant(row.RecordedAt.UTC()),
			RetrievedAt:     values.NewInstant(row.RecordedAt.UTC()),
			Digest:          row.Digest,
		},
	}, true, nil
}

// integrationCalendar is the corpus calendar every effective interval this
// source builds is expressed against.
func integrationCalendar() values.CalendarRef {
	calendar, err := fixtures.Calendar()
	if err != nil {
		panic(err)
	}
	return calendar
}

// storedMoney reads a numeric column's exact decimal text as money at the
// canonical two-place scale. The aggregate columns are numeric(_,4), so the
// text comes back with four fractional digits; quantizing rather than parsing
// at scale 2 keeps the exact stored digits visible to the rounding decision
// instead of refusing the read.
func storedMoney(text, currency string) (values.Money, error) {
	exact, err := values.NewDecimal(text, 4, values.RoundingHalfEven)
	if err != nil {
		return values.Money{}, err
	}
	quantized, err := exact.Quantize(2, values.RoundingHalfEven)
	if err != nil {
		return values.Money{}, err
	}
	return values.NewMoneyFromDecimal(quantized, currency)
}

// storedDecimal reads a numeric column's exact decimal text at a declared
// scale, quantizing from the column's own four places.
func storedDecimal(text string, scale int32) (values.Decimal, error) {
	exact, err := values.NewDecimal(text, 4, values.RoundingHalfEven)
	if err != nil {
		return values.Decimal{}, err
	}
	return exact.Quantize(scale, values.RoundingHalfEven)
}

// mustLocalDateFromTime narrows a stored timestamp to its calendar date.
func mustLocalDateFromTime(at time.Time) values.LocalDate {
	date, err := values.ParseLocalDate(at.UTC().Format(time.DateOnly))
	if err != nil {
		panic(err)
	}
	return date
}
