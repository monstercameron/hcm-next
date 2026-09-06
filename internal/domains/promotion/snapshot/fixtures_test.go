package snapshot_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/budget"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/org"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// The harborcare-demo Promotion fixture, as
// planning/reference-workflows/promote-into-management.md describes it and as
// internal/domains/fixtures publishes it: Omar Reyes (OPS-HRBP2/P2, people-ops,
// US-EAST, 93,000 USD) promoted into OPS-HRBP3/P3 at 98,000 USD effective
// 2026-06-01, against a 50,000 USD compensation pool.
const (
	fixtureWorkerKey    = "omar-reyes"
	fixtureTargetJob    = "OPS-HRBP3"
	fixtureTargetGrade  = "P3"
	fixtureTargetOrg    = "people-ops"
	fixtureTargetZone   = "US-EAST"
	fixturePositionCode = "POS-HRBP-301"
	fixturePositionID   = "55555555-5555-4555-8555-555555555555"
	fixtureCurrentBase  = "93000.00"
	fixtureDesiredBase  = "98000.00"
	fixtureCurrency     = "USD"
	fixtureBudgetScope  = "cost-center:people-ops"
	fixtureBudgetPeriod = "FY2026"
	fixtureEffectiveOn  = "2026-06-01"
	fixtureReferenceVer = "harborcare.promotion.reference/2026.1"
	fixtureConnection   = "primary"
	fixturePolicy       = "harborcare.policy/2026.1"
	fixturePurpose      = "promotion_proposal"
)

// fixtureKnownAtText is the knowledge cut-off every unit test resolves under.
// It is a pinned instant, not a wall clock: a snapshot digest that moved with
// the clock would be untestable, and a build that read the clock would not be
// kernel-pure.
const fixtureKnownAtText = "2026-05-15T12:00:00Z"

func mustInstant(t testing.TB, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(parsed.UTC())
}

func mustKnownAt(t testing.TB, text string) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(mustInstant(t, text))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return known
}

func mustRecordedAt(t testing.TB, text string) values.RecordedAt {
	t.Helper()
	recorded, err := values.NewRecordedAt(mustInstant(t, text))
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	return recorded
}

func mustLocalDate(t testing.TB, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("ParseLocalDate %q: %v", text, err)
	}
	return date
}

func mustMoney(t testing.TB, amount, currency string) values.Money {
	t.Helper()
	money, err := fixtures.Money(amount, currency)
	if err != nil {
		t.Fatalf("Money(%q, %q): %v", amount, currency, err)
	}
	return money
}

func mustRevision(t testing.TB, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return revision
}

func mustDecimal(t testing.TB, text string, scale int32) values.Decimal {
	t.Helper()
	decimal, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal(%q): %v", text, err)
	}
	return decimal
}

func mustCalendar(t testing.TB) values.CalendarRef {
	t.Helper()
	calendar, err := fixtures.Calendar()
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	return calendar
}

func mustWorkerRef(t testing.TB) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef(fixtureWorkerKey)
	if err != nil {
		t.Fatalf("WorkerRef: %v", err)
	}
	return ref
}

func positionRef() values.EntityRef {
	return values.EntityRef{Tenant: fixtures.Tenant, Kind: position.KindPosition, Id: fixturePositionID}
}

func managerRef() values.EntityRef {
	return values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: "44444444-4444-4444-8444-444444444444"}
}

// fixtureAuthority and fixtureProvenance are the evidence coordinates every
// hand-written in-memory port stamps, so a change in one port's evidence is
// visible as a deliberate edit rather than as drift.
func fixtureAuthority(system string) evidence.SourceAuthority {
	return evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: system, PolicyRef: fixturePolicy}
}

func fixtureProvenance(t testing.TB, system, ref string) evidence.Provenance {
	t.Helper()
	return evidence.Provenance{Source: system, EvidenceRef: ref, RecordedAt: mustRecordedAt(t, "2024-04-02T00:00:00Z")}
}

// ---------------------------------------------------------------------------
// In-memory read ports
// ---------------------------------------------------------------------------

// memoryOrgFacts answers one manager relationship for the fixture subject.
type memoryOrgFacts struct {
	fact org.ManagerRelationshipFact
	// exists reports whether the org graph carries the subject at all.
	exists bool
	// vacant makes the graph answer "this worker exists and reports to
	// nobody", which is the shape of an absent manager relationship as
	// opposed to an absent worker.
	vacant  bool
	failure error
}

func newMemoryOrgFacts(t testing.TB) *memoryOrgFacts {
	t.Helper()
	effective, err := values.NewOpenInstantInterval(mustInstant(t, "2024-04-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return &memoryOrgFacts{
		exists: true,
		fact: org.ManagerRelationshipFact{
			RelationshipID: "rel_mgr_1002",
			Type:           org.RelationshipDirectManager,
			Worker:         mustWorkerRef(t),
			Manager:        managerRef(),
			AssignmentID:   "asg_1002",
			Effective:      effective,
			KnownAt:        mustKnownAt(t, "2024-04-01T00:00:00Z"),
			Revision:       mustRevision(t, "org.relationship.rel_mgr_1002", 3),
			Authority:      fixtureAuthority("hcmnext.org"),
			Provenance:     fixtureProvenance(t, "hcmnext.org", "evd_rel_mgr_1002_r3"),
		},
	}
}

func (m *memoryOrgFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if m.failure != nil {
		return org.WorkerFactSet{}, m.failure
	}
	// The whole graph answers at one watermark and one policy version, for
	// every worker it is asked about. The resolver refuses a chain whose hops
	// disagree on either, so a fake that stamped them per worker would report
	// DISAGREEING for a graph that is in fact consistent.
	watermark := mustRevisionUnchecked("org.graph."+string(q.Tenant), 9)
	if !m.exists || q.Worker != m.fact.Worker {
		return org.WorkerFactSet{Worker: q.Worker, Exists: false, Watermark: watermark, PolicyVersion: fixturePolicy}, nil
	}
	relationships := []org.ManagerRelationshipFact{m.fact}
	if m.vacant {
		relationships = nil
	}
	return org.WorkerFactSet{
		Worker:        q.Worker,
		Exists:        true,
		Relationships: relationships,
		Watermark:     watermark,
		PolicyVersion: fixturePolicy,
	}, nil
}

// mustRevisionUnchecked builds a revision outside a *testing.T scope, for the
// port methods that have no test handle. A malformed stream here is a
// test-setup bug, so it panics rather than returning an error the port would
// have to invent a meaning for.
func mustRevisionUnchecked(stream string, sequence uint64) values.RevisionToken {
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		panic(err)
	}
	return revision
}

// memoryPositionFacts answers one position revision for the fixture target.
type memoryPositionFacts struct {
	revision position.PositionRevision
	exists   bool
	failure  error
}

func newMemoryPositionFacts(t testing.TB) *memoryPositionFacts {
	t.Helper()
	effective, err := values.NewOpenLocalDateInterval(mustLocalDate(t, "2026-01-01"), mustCalendar(t))
	if err != nil {
		t.Fatalf("NewOpenLocalDateInterval: %v", err)
	}
	return &memoryPositionFacts{
		exists: true,
		revision: position.PositionRevision{
			Position:    positionRef(),
			Revision:    mustRevision(t, "position."+fixturePositionCode, 5),
			Effective:   effective,
			Lifecycle:   position.LifecycleOpen,
			JobCode:     fixtureTargetJob,
			OrgUnit:     fixtureTargetOrg,
			LegalEntity: "HarborCare US Inc.",
			Capacity: position.CapacityPolicy{
				CapacityFTE:   mustDecimal(t, "1.0000", 4),
				CapacityHeads: 1,
			},
			Authority:  fixtureAuthority("hcmnext.position"),
			Provenance: fixtureProvenance(t, "hcmnext.position", "evd_position_hrbp301_r5"),
		},
	}
}

func (m *memoryPositionFacts) PositionRevisionAt(
	_ context.Context, q position.PositionQuery,
) (position.PositionRevision, bool, error) {
	if m.failure != nil {
		return position.PositionRevision{}, false, m.failure
	}
	if !m.exists || q.Position != m.revision.Position {
		return position.PositionRevision{}, false, nil
	}
	return m.revision, true, nil
}

// memoryCompensationFacts answers one compensation fact for the subject.
type memoryCompensationFacts struct {
	set     rewards.CompensationFactSet
	exists  bool
	failure error
}

func newMemoryCompensationFacts(t testing.TB) *memoryCompensationFacts {
	t.Helper()
	effective, err := values.NewOpenInstantInterval(mustInstant(t, "2024-04-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	worker := mustWorkerRef(t)
	return &memoryCompensationFacts{
		exists: true,
		set: rewards.CompensationFactSet{
			Worker:        worker,
			Exists:        true,
			Watermark:     mustRevision(t, "rewards.package."+fixtureWorkerKey, 11),
			PolicyVersion: fixturePolicy,
			Fact: rewards.CompensationFact{
				Worker:     worker,
				BasePay:    mustMoney(t, fixtureCurrentBase, fixtureCurrency),
				PayBandRef: "BAND-OPS-P2-USEAST",
				Currency:   fixtureCurrency,
				PayBasis:   rewards.PayBasisAnnualSalary,
				Frequency:  "MONTHLY",
				Effective:  effective,
				KnownAt:    mustKnownAt(t, "2024-04-01T00:00:00Z"),
				Revision:   mustRevision(t, "rewards.package."+fixtureWorkerKey, 11),
				Authority:  fixtureAuthority("hcmnext.rewards"),
				Provenance: fixtureProvenance(t, "hcmnext.rewards", "evd_comp_1002_r11"),
			},
		},
	}
}

func (m *memoryCompensationFacts) CompensationFactsAt(
	_ context.Context, q rewards.CompensationFactsQuery,
) (rewards.CompensationFactSet, error) {
	if m.failure != nil {
		return rewards.CompensationFactSet{}, m.failure
	}
	if !m.exists || q.Worker != m.set.Worker {
		return rewards.CompensationFactSet{Worker: q.Worker, Exists: false}, nil
	}
	return m.set, nil
}

// memoryBudgetFacts answers one compensation-pool observation.
type memoryBudgetFacts struct {
	ref     budget.BudgetAuthorityRef
	exists  bool
	failure error
}

func newMemoryBudgetFacts(t testing.TB) *memoryBudgetFacts {
	t.Helper()
	return &memoryBudgetFacts{
		exists: true,
		ref: budget.BudgetAuthorityRef{
			BudgetType:        budget.CompensationPool,
			OwnerSystem:       "finance.incumbent.erp",
			Scope:             fixtureBudgetScope,
			Period:            fixtureBudgetPeriod,
			Currency:          fixtureCurrency,
			Unit:              budget.UnitMoney,
			BaselineVersion:   "finance.budget.baseline/2026.09",
			AvailableQuantity: mustDecimal(t, "50000.00", 2),
			Evidence: budget.ObservationEvidence{
				ObservationID:   "obs_budget_people_ops",
				SourceWatermark: mustInstant(t, "2026-05-10T00:00:00Z"),
				RetrievedAt:     mustInstant(t, "2026-05-10T00:05:00Z"),
				Digest:          "sha256:budget-observation",
			},
		},
	}
}

func (m *memoryBudgetFacts) CompensationBudgetAt(
	_ context.Context, q promosnapshot.BudgetQuery,
) (budget.BudgetAuthorityRef, bool, error) {
	if m.failure != nil {
		return budget.BudgetAuthorityRef{}, false, m.failure
	}
	if !m.exists || q.Scope != m.ref.Scope || q.Period != m.ref.Period {
		return budget.BudgetAuthorityRef{}, false, nil
	}
	return m.ref, true, nil
}

// ---------------------------------------------------------------------------
// Composition
// ---------------------------------------------------------------------------

// harness is the mutable set of ports one test composes, so a case can swap
// exactly the one reader or decision it is about.
type harness struct {
	Worker       *fixtures.MemoryWorkerFacts
	Org          *memoryOrgFacts
	Position     *memoryPositionFacts
	Compensation *memoryCompensationFacts
	Bands        *fixtures.MemoryBandCatalog
	Budget       *memoryBudgetFacts
}

func newHarness(t testing.TB) *harness {
	t.Helper()
	worker, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("NewMemoryWorkerFacts: %v", err)
	}
	bands, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		t.Fatalf("NewMemoryBandCatalog: %v", err)
	}
	return &harness{
		Worker:       worker,
		Org:          newMemoryOrgFacts(t),
		Position:     newMemoryPositionFacts(t),
		Compensation: newMemoryCompensationFacts(t),
		Bands:        bands,
		Budget:       newMemoryBudgetFacts(t),
	}
}

func (h *harness) readers() promosnapshot.Readers {
	return promosnapshot.Readers{
		Worker:       h.Worker,
		Org:          h.Org,
		Position:     h.Position,
		Compensation: h.Compensation,
		Bands:        h.Bands,
		Budget:       h.Budget,
	}
}

// allowAllAuthorization is the permissive decision set: every field allowed,
// every hop disclosed, the pool readable. Restrictive variants are built by
// the security test from this one, so what a denial changes is visible as a
// diff rather than as two unrelated fixtures.
func allowAllAuthorization(t testing.TB) promosnapshot.Authorization {
	t.Helper()
	return promosnapshot.Authorization{
		Worker:     fixtures.AllowAll(fixturePolicy, fixturePurpose, promosnapshot.WorkerFactFields()),
		ManagerHop: func(org.ManagerRelationshipFact) people.AuthorizationDecision { return allowManagerHop(t) },
		Compensation: rewards.CompensationAuthorization{
			PolicyVersion:      fixturePolicy,
			Purpose:            fixturePurpose,
			SubjectDisclosable: true,
			Scopes:             []string{rewards.CompensationReadScope},
		},
		PayBand: rewards.PayBandPositionAuthorization{
			PolicyVersion:      fixturePolicy,
			Purpose:            fixturePurpose,
			SubjectDisclosable: true,
			Scopes:             []string{rewards.PayBandPositionReadScope},
		},
		BudgetDisclosable: true,
	}
}

// allowManagerHop is the per-hop decision the org resolver applies.
func allowManagerHop(t testing.TB) people.AuthorizationDecision {
	t.Helper()
	return people.AuthorizationDecision{
		PolicyVersion:      fixturePolicy,
		Purpose:            fixturePurpose,
		SubjectDisclosable: true,
		Fields: map[people.FieldID]people.FieldRuling{
			people.FieldManagerRelation: {Effect: people.EffectAllow},
		},
	}
}

// fixtureRequest is the harborcare-demo Promotion request every test builds
// from.
func fixtureRequest(t testing.TB) promosnapshot.Request {
	t.Helper()
	return promosnapshot.Request{
		Tenant:         fixtures.Tenant,
		Subject:        mustWorkerRef(t),
		TargetPosition: positionRef(),
		Target: promosnapshot.Target{
			JobCode: fixtureTargetJob,
			Grade:   fixtureTargetGrade,
			OrgUnit: fixtureTargetOrg,
			PayZone: fixtureTargetZone,
		},
		DesiredBasePay:    mustMoney(t, fixtureDesiredBase, fixtureCurrency),
		DesiredPayBasis:   rewards.PayBasisAnnualSalary,
		EffectiveOn:       mustLocalDate(t, fixtureEffectiveOn),
		KnownAt:           mustKnownAt(t, fixtureKnownAtText),
		Calendar:          mustCalendar(t),
		ManagerChainDepth: 3,
		Annualization:     fixtureAnnualization(t),
		BudgetScope:       fixtureBudgetScope,
		BudgetPeriod:      fixtureBudgetPeriod,
		Authorization:     allowAllAuthorization(t),
		ReferenceVersion:  fixtureReferenceVer,
		SourceConnection:  fixtureConnection,
	}
}

// fixtureAnnualization is the declared COMP-002 rule set. Every factor is
// stated: an annualization that reached for a conventional constant would move
// the digest the day somebody changed the convention.
func fixtureAnnualization(t testing.TB) rewards.CompensationAnnualizationRule {
	t.Helper()
	return rewards.CompensationAnnualizationRule{
		Version:       "rewards.annualization/2026.1",
		HoursPerWeek:  mustDecimal(t, "40.0000", 4),
		DaysPerWeek:   mustDecimal(t, "5.0000", 4),
		WeeksPerYear:  mustDecimal(t, "52.0000", 4),
		MonthsPerYear: mustDecimal(t, "12.0000", 4),
		Currency:      fixtureCurrency,
		MoneyScale:    2,
		MoneyRounding: values.RoundingHalfEven,
	}
}

// build runs one Build over the harness, failing the test on an unexpected
// refusal.
func (h *harness) build(t testing.TB, req promosnapshot.Request) promosnapshot.PromotionInputSnapshot {
	t.Helper()
	snap, err := promosnapshot.Build(context.Background(), h.readers(), req)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return snap
}
