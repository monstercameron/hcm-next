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
