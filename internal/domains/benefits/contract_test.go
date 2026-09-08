package benefits

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func benefitRef(tenant, kind string, id uuid.UUID) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind(kind), Id: id.String()}
}

func benefitInterval(t *testing.T, year int) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(year, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(year+1, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func benefitRevision(t *testing.T, sequence uint64) PlanRevision {
	t.Helper()
	tenant := "tenant-benefits"
	planID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	revisionID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	token, err := values.NewSequenceRevision("benefits.plan", sequence)
	if err != nil {
		t.Fatal(err)
	}
	planRef := benefitRef(tenant, "benefit_plan", planID)
	revision := PlanRevision{
		RevisionID: benefitRef(tenant, "benefit_plan_revision", revisionID), PlanID: planRef, Revision: token,
		PlanYear: PlanYear{PlanID: planRef, Year: 2026, Effective: benefitInterval(t, 2026)},
		Name:     "Medical PPO", Jurisdiction: "US-NY", Currency: "USD",
		CoverageTiers: []string{"employee", "family"}, Options: []string{"hsa", "telehealth"},
		Carrier:              benefitRef(tenant, "carrier", uuid.MustParse("30000000-0000-4000-8000-000000000001")),
		Provider:             benefitRef(tenant, "provider", uuid.MustParse("30000000-0000-4000-8000-000000000002")),
		Sponsor:              benefitRef(tenant, "sponsor", uuid.MustParse("30000000-0000-4000-8000-000000000003")),
		RateScheduleRef:      benefitRef(tenant, "rate_schedule", uuid.MustParse("40000000-0000-4000-8000-000000000001")),
		EligibilityRulesRef:  benefitRef(tenant, "eligibility_rules", uuid.MustParse("40000000-0000-4000-8000-000000000002")),
		EnrollmentRulesRef:   benefitRef(tenant, "enrollment_rules", uuid.MustParse("40000000-0000-4000-8000-000000000003")),
		ContributionRulesRef: benefitRef(tenant, "contribution_rules", uuid.MustParse("40000000-0000-4000-8000-000000000004")),
		Authority:            benefitRef(tenant, "authority", uuid.MustParse("50000000-0000-4000-8000-000000000001")),
		Effective:            benefitInterval(t, 2026),
	}
	return revision
}

func TestTodo_BEN_001(t *testing.T) {
	tenant := "tenant-benefits"
	planID := benefitRef(tenant, "benefit_plan", uuid.MustParse("10000000-0000-4000-8000-000000000001"))
	plan := Plan{PlanID: planID, Name: "Medical PPO", Jurisdiction: "US-NY", Currency: "USD", PlanYears: []PlanYear{
		{PlanID: planID, Year: 2026, Effective: benefitInterval(t, 2026)},
		{PlanID: planID, Year: 2027, Effective: func() values.EffectiveInterval {
			start, _ := values.NewLocalDate(2026, time.July, 1)
			end, _ := values.NewLocalDate(2027, time.July, 1)
			interval, _ := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
			return interval
		}()},
	}}
	if _, err := NewPlan(plan); !errors.Is(err, ErrOverlappingPlanYears) {
		t.Fatalf("overlapping plan years error = %v, want ErrOverlappingPlanYears", err)
	}
	revision, err := NewPlanRevision(benefitRevision(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := revision.Digest()
	if err != nil || revision.CanonicalDigest == "" || digest != revision.CanonicalDigest {
		t.Fatalf("constructor did not mint a digest: %q", revision.CanonicalDigest)
	}
}

func TestTodo_BEN_001_Property(t *testing.T) {
	input := benefitRevision(t, 1)
	first, err := NewPlanRevision(input)
	if err != nil {
		t.Fatal(err)
	}
	input.CoverageTiers[0] = "mutated locally"
	if first.CoverageTiers[0] == "mutated locally" {
		t.Fatal("constructor shared the caller's coverage slice")
	}
	next := first
	next.RevisionID = benefitRef(first.PlanID.Tenant.String(), "benefit_plan_revision", uuid.MustParse("20000000-0000-4000-8000-000000000002"))
	next.Revision, _ = values.NewSequenceRevision("benefits.plan", 2)
	next.Supersedes = first.RevisionID
	next.CanonicalDigest = ""
	next, err = NewPlanRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	if next.Supersedes != first.RevisionID || next.CanonicalDigest == first.CanonicalDigest {
		t.Fatal("successor did not retain lineage and mint a distinct digest")
	}
}

func TestTodo_BEN_001_Conformance(t *testing.T) {
	revision, err := NewPlanRevision(benefitRevision(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := revision.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.PlanYear != 2026 || explanation.CoverageTiers != 2 || explanation.Options != 2 || !explanation.HasRateSchedule || explanation.Digest != revision.CanonicalDigest {
		t.Fatalf("explanation = %+v", explanation)
	}
}

func TestTodo_BEN_001_Mutation(t *testing.T) {
	store := NewMemoryStore()
	first, err := NewPlanRevision(benefitRevision(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), first.PlanID.Tenant.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.Save(context.Background(), first.PlanID.Tenant.String(), first, "")); got != StoreDuplicateCode {
		t.Fatalf("duplicate code = %q", got)
	}
	if got := CodeOf(store.Save(context.Background(), first.PlanID.Tenant.String(), first, "wrong")); got != StoreDuplicateCode {
		t.Fatalf("duplicate precedence code = %q", got)
	}
	if !errors.Is(store.Save(context.Background(), first.PlanID.Tenant.String(), first, ""), ErrStoreDuplicate) {
		t.Fatal("duplicate did not unwrap to ErrStoreDuplicate")
	}
}

func TestMemoryStoreRevisionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first, err := NewPlanRevision(benefitRevision(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "tenant-benefits", first, ""); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(ctx, "tenant-benefits", first.PlanID.Id, first.Revision.String())
	if err != nil || loaded.CanonicalDigest != first.CanonicalDigest {
		t.Fatalf("load = %+v, %v", loaded, err)
	}
	loaded.CoverageTiers[0] = "caller mutation"
	reloaded, err := store.Current(ctx, "tenant-benefits", first.PlanID.Id)
	if err != nil || reloaded.CoverageTiers[0] != "employee" {
		t.Fatalf("store leaked mutable slices: %+v, %v", reloaded.CoverageTiers, err)
	}

	token, _ := values.NewSequenceRevision("benefits.plan", 2)
	nextID := benefitRef("tenant-benefits", "benefit_plan_revision", uuid.MustParse("20000000-0000-4000-8000-000000000002"))
	next, err := first.Successor(nextID, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "tenant-benefits", next, first.Revision.String()); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx, "tenant-benefits", first.PlanID.Id)
	if err != nil || len(list) != 2 || list[0].Revision.String() != first.Revision.String() || list[1].Revision.String() != next.Revision.String() {
		t.Fatalf("ordered history = %+v, %v", list, err)
	}
	current, err := store.Current(ctx, "tenant-benefits", first.PlanID.Id)
	if err != nil || current.Revision.String() != next.Revision.String() {
		t.Fatalf("current = %+v, %v", current, err)
	}

	third := next
	third.RevisionID = benefitRef("tenant-benefits", "benefit_plan_revision", uuid.MustParse("20000000-0000-4000-8000-000000000003"))
	third.Revision, _ = values.NewSequenceRevision("benefits.plan", 3)
	third.Supersedes = next.RevisionID
	third.CanonicalDigest = ""
	if err := store.Save(ctx, "tenant-benefits", third, first.Revision.String()); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale save error = %v", err)
	} else {
		var typed *StoreError
		if !errors.As(err, &typed) || typed.Expected != first.Revision.String() || typed.Actual != next.Revision.String() {
			t.Fatalf("stale context = %#v", typed)
		}
	}
}

func TestMemoryStoreRejectsInvalidScopeAndMissingRecords(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	revision := benefitRevision(t, 1)
	for name, err := range map[string]error{
		"nil store":       (*MemoryStore)(nil).Save(ctx, "tenant-benefits", revision, ""),
		"empty tenant":    store.Save(ctx, "", revision, ""),
		"tenant mismatch": store.Save(ctx, "other-benefits", revision, ""),
	} {
		if !errors.Is(err, ErrStoreInvalid) {
			t.Errorf("%s error = %v", name, err)
		}
	}
	for name, call := range map[string]func() error{
		"load missing":    func() error { _, err := store.Load(ctx, "tenant-benefits", revision.PlanID.Id, "missing"); return err },
		"current missing": func() error { _, err := store.Current(ctx, "tenant-benefits", revision.PlanID.Id); return err },
		"list missing":    func() error { _, err := store.List(ctx, "tenant-benefits", revision.PlanID.Id); return err },
	} {
		if err := call(); !errors.Is(err, ErrStoreNotFound) {
			t.Errorf("%s error = %v", name, err)
		}
	}
}

func TestPlanRevisionPublicContracts(t *testing.T) {
	revision, err := NewPlanRevision(benefitRevision(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanRevision(revision); err != nil {
		t.Fatal(err)
	}
	if len(revision.Canonical()) == 0 {
		t.Fatal("valid revision has no canonical bytes")
	}
	explanation, err := ExplainPlanRevision(revision)
	if err != nil || explanation.Revision != revision.Revision.String() || !explanation.HasEnrollment || !explanation.HasEligibility || !explanation.HasContribution {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}

	bad := revision
	bad.CanonicalDigest = "sha256:forged"
	if bad.Canonical() != nil {
		t.Fatal("invalid revision emitted canonical bytes")
	}
	if _, err := bad.Digest(); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("forged digest error = %v", err)
	}
	if _, err := bad.Explain(); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("invalid explanation error = %v", err)
	}
}
