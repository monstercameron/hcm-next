package mobility

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func changeFixture(kind MaterialChangeKind) MaterialChange {
	return MaterialChange{Kind: kind, HostJurisdiction: "CA-ON", Reason: "documented change", EvidenceRef: "evidence:change", EffectiveAt: time.Unix(10, 0).UTC()}
}

func TestMobilityConformanceReplansJurisdictionPayrollTaxAndPrivacyOnMaterialChange(t *testing.T) {
	parent := validPlan(t)
	for _, kind := range []MaterialChangeKind{ChangeExtension, ChangeCountry} {
		change := changeFixture(kind)
		if kind == ChangeExtension {
			host := assignment(t, "host:001", AssignmentHost, 5, 20, "0.50")
			change.HostEffective = host.Effective
		}
		got, err := ReplanOnMaterialChange(parent, change)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got.Successor.Revision != parent.Revision+1 || got.Successor.ParentDigest != parent.CanonicalDigest {
			t.Fatalf("%s lineage = %+v", kind, got.Successor)
		}
		if got.Successor.HostAssignment.Revision != parent.HostAssignment.Revision+1 {
			t.Fatalf("%s host revision did not advance", kind)
		}
		if got.ChangeEvidenceRef != change.EvidenceRef || got.ChangeReason != change.Reason || !got.ChangeEffectiveAt.Equal(change.EffectiveAt) {
			t.Fatalf("%s change evidence was not preserved: %+v", kind, got)
		}
		for i, o := range got.Successor.Obligations {
			if o.Status != ObligationUnknown || o.EvidenceRef != parent.Obligations[i].EvidenceRef {
				t.Fatalf("%s did not preserve evidence as dated prior truth: %+v", kind, o)
			}
		}
	}
	if parent.HostAssignment.Jurisdiction != "US-CA" {
		t.Fatal("replan mutated parent")
	}
}

func TestMobilityConformanceTargetsOneHostInMultiHostPlan(t *testing.T) {
	base := validPlan(t)
	first := base.HostAssignment
	second := assignment(t, "host:002", AssignmentHost, 16, 25, "0.50")
	base.HostAssignment = AssignmentRevision{}
	base.HostAssignments = []AssignmentRevision{first, second}
	base.CanonicalDigest = ""
	plan, err := NewMobilityPlan(base)
	if err != nil {
		t.Fatal(err)
	}
	change := changeFixture(ChangeCountry)
	if _, err := ReplanOnMaterialChange(plan, change); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("ambiguous host replan = %v", err)
	}
	change.HostAssignmentID = second.AssignmentID
	result, err := ReplanOnMaterialChange(plan, change)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Successor.HostAssignments) != 2 {
		t.Fatalf("hosts = %+v", result.Successor.HostAssignments)
	}
	gotFirst, gotSecond := result.Successor.HostAssignments[0], result.Successor.HostAssignments[1]
	if gotFirst.CanonicalDigest != first.CanonicalDigest || gotFirst.Revision != first.Revision {
		t.Fatalf("unselected host changed: %+v", gotFirst)
	}
	if gotSecond.AssignmentID != second.AssignmentID || gotSecond.Revision != second.Revision+1 || gotSecond.Jurisdiction != change.HostJurisdiction {
		t.Fatalf("selected host = %+v", gotSecond)
	}
	if plan.HostAssignments[1].Jurisdiction != second.Jurisdiction || plan.HostAssignments[1].Revision != second.Revision {
		t.Fatal("multi-host replan mutated the parent")
	}
	extension := changeFixture(ChangeExtension)
	extension.HostAssignmentID = first.AssignmentID
	extension.HostEffective = mobilityInterval(t, 5, 16)
	extended, err := ReplanOnMaterialChange(plan, extension)
	if err != nil {
		t.Fatal(err)
	}
	if extended.Successor.HostAssignments[0].Revision != first.Revision+1 || extended.Successor.HostAssignments[1].CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("targeted extension changed the wrong host: %+v", extended.Successor.HostAssignments)
	}
}

func TestTodo_MOBILITY_002_Property(t *testing.T) {
	countries := []string{"CA-ON", "GB-LND", "DE-BE", "JP-13"}
	property := func(extension bool, raw uint8) bool {
		p := validPlan(t)
		kind := ChangeCountry
		if extension {
			kind = ChangeExtension
		}
		c := changeFixture(kind)
		if extension {
			c.HostEffective = mobilityInterval(t, 5, 16+int(raw)%12)
		} else {
			c.HostJurisdiction = countries[int(raw)%len(countries)]
		}
		r, err := ReplanOnMaterialChange(p, c)
		if err != nil || r.Successor.Revision != p.Revision+1 || r.Successor.CanonicalDigest == p.CanonicalDigest {
			return false
		}
		if r.Successor.ParentDigest != p.CanonicalDigest || r.Change != kind || r.ChangeReason != c.Reason || r.ChangeEvidenceRef != c.EvidenceRef {
			return false
		}
		got, err := r.Successor.Digest()
		return err == nil && got == r.Successor.CanonicalDigest
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 64}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_MOBILITY_002_Golden(t *testing.T) {
	p := validPlan(t)
	a, err := ReplanOnMaterialChange(p, changeFixture(ChangeCountry))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReplanOnMaterialChange(p, changeFixture(ChangeCountry))
	if err != nil {
		t.Fatal(err)
	}
	if a.Successor.CanonicalDigest != b.Successor.CanonicalDigest {
		t.Fatal("identical replans differ")
	}
	const want = "sha256:abb74fc7e970b42103d0d939f2bd9adaf4e1904398118247abac8f2556412356"
	if a.Successor.CanonicalDigest != want {
		t.Fatalf("successor digest = %q, want %q", a.Successor.CanonicalDigest, want)
	}
}

func TestTodo_MOBILITY_002_Race(t *testing.T) {
	p := validPlan(t)
	c := changeFixture(ChangeCountry)
	var wg sync.WaitGroup
	type result struct {
		successor MobilityPlan
		vendor    VendorReconciliation
		returned  ReturnResult
		err       error
	}
	results := make(chan result, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, err := ReplanOnMaterialChange(p, c)
			vendor, vendorErr := ReconcileVendor(p.Obligations[0], VendorObservation{ObligationKind: ObligationPayroll, ObligationRef: p.Obligations[0].Ref, VendorRef: "vendor", ObservationRef: "obs", Status: ObligationReady, Accepted: true, ObservedAt: time.Unix(2, 0)})
			returned, returnErr := CloseHostEffects([]HostEffect{{Kind: EffectPayroll, Ref: "pay", Active: true}}, []string{"pay"})
			if err == nil {
				err = vendorErr
			}
			if err == nil {
				err = returnErr
			}
			var successor MobilityPlan
			if err == nil {
				successor = next.Successor
			}
			results <- result{successor: successor, vendor: vendor, returned: returned, err: err}
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got.err != nil || got.successor.Revision != 2 || got.successor.ParentDigest != p.CanonicalDigest || got.vendor.Status != VendorReview || len(got.returned.Closed) != 1 || got.returned.Effects[0].Active {
			t.Fatalf("shared race result=%+v", got)
		}
	}
}

func TestTodo_MOBILITY_002_Fault(t *testing.T) {
	p := validPlan(t)
	if _, err := ReplanOnMaterialChange(p, MaterialChange{Kind: ChangeCountry, Reason: "x", EvidenceRef: "e"}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("undated change = %v", err)
	}
	if _, err := ReplanOnMaterialChange(p, changeFixture(ChangeReturn)); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("return accepted as a replan = %v", err)
	}
	noChange := changeFixture(ChangeCountry)
	noChange.HostJurisdiction = p.HostAssignment.Jurisdiction
	if _, err := ReplanOnMaterialChange(p, noChange); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("no-op country change accepted = %v", err)
	}
	shortening := changeFixture(ChangeExtension)
	shortening.HostEffective = mobilityInterval(t, 5, 14)
	if _, err := ReplanOnMaterialChange(p, shortening); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("shortening accepted as extension = %v", err)
	}
	if _, err := ReconcileVendor(p.Obligations[0], VendorObservation{ObligationKind: p.Obligations[0].Kind, VendorRef: "v", ObservationRef: "o"}); !errors.Is(err, ErrStaleVendor) {
		t.Fatalf("incomplete vendor = %v", err)
	}
	if _, err := AppendRetroCorrection(nil, RetroCorrection{CorrectionID: "c", MobilityID: p.MobilityID, EffectiveAt: time.Unix(1, 0), PriorDigest: "old", CorrectedDigest: "new", Reason: "r", EvidenceRef: "e"}, p.CanonicalDigest); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("wrong prior = %v", err)
	}
}

func TestTodo_MOBILITY_002_Security(t *testing.T) {
	p := validPlan(t)
	r, err := ReplanOnMaterialChange(p, changeFixture(ChangeCountry))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Successor.CanonicalDigest, p.WorkerRef) {
		t.Fatal("digest exposed worker reference")
	}
	if _, err := ReconcileVendor(p.Obligations[0], VendorObservation{ObligationKind: ObligationTax, ObligationRef: p.Obligations[0].Ref, VendorRef: "vendor", ObservationRef: "obs", Status: ObligationReady, Accepted: true, ObservedAt: time.Unix(2, 0)}); !errors.Is(err, ErrStaleVendor) {
		t.Fatalf("cross-obligation vendor acknowledgement accepted = %v", err)
	}
	other := p.Obligations[0]
	other.Ref = "payroll-review:other-plan"
	if _, err := ReconcileVendor(other, VendorObservation{ObligationKind: other.Kind, ObligationRef: p.Obligations[0].Ref, VendorRef: "vendor", ObservationRef: "obs", Status: ObligationReady, Accepted: true, ObservedAt: time.Unix(2, 0)}); !errors.Is(err, ErrStaleVendor) {
		t.Fatalf("same-kind cross-obligation vendor acknowledgement accepted = %v", err)
	}
}

func TestTodo_MOBILITY_002_Conformance(t *testing.T) {
	p := validPlan(t)
	o := p.Obligations[0]
	r, err := ReconcileVendor(o, VendorObservation{ObligationKind: o.Kind, ObligationRef: o.Ref, VendorRef: "vendor:1", ObservationRef: "obs:1", Status: ObligationReady, Accepted: true, ObservedAt: time.Unix(2, 0)})
	if err != nil || r.Status != VendorReview {
		t.Fatalf("vendor acceptance = %+v, %v", r, err)
	}
	if r.Obligation != o {
		t.Fatal("vendor acknowledgement changed the authoritative obligation")
	}
	input := []HostEffect{{Kind: EffectPayroll, Ref: "pay:primary", Active: true}, {Kind: EffectPayroll, Ref: "pay:shadow", Active: true}, {Kind: EffectTax, Ref: "tax", Active: true}}
	effects, err := CloseHostEffects(input, []string{"pay:primary"})
	if err != nil || effects.Effects[0].Active || !effects.Effects[1].Active || !effects.Effects[2].Active || len(effects.Closed) != 1 || effects.Closed[0] != "pay:primary" {
		t.Fatalf("return = %+v, %v", effects, err)
	}
	for i := range input {
		if !input[i].Active {
			t.Fatalf("return mutated caller input: %+v", input)
		}
	}
	history, err := AppendRetroCorrection(nil, RetroCorrection{CorrectionID: "c", MobilityID: p.MobilityID, EffectiveAt: time.Unix(1, 0), PriorDigest: "old", CorrectedDigest: "new", Reason: "r", EvidenceRef: "e"}, "old")
	if err != nil || len(history) != 1 {
		t.Fatalf("correction = %+v, %v", history, err)
	}
}

func TestTodo_MOBILITY_002_Mutation(t *testing.T) {
	p := validPlan(t)
	change := changeFixture(ChangeExtension)
	change.HostEffective = mobilityInterval(t, 5, 25)
	r, err := ReplanOnMaterialChange(p, change)
	if err != nil {
		t.Fatal(err)
	}
	if r.Successor.HostAssignment.SourceAuthority != p.HostAssignment.SourceAuthority || r.Successor.HostAssignment.Jurisdiction != p.HostAssignment.Jurisdiction || string(r.Successor.HostAssignment.CostAllocationSplit.Canonical()) != string(p.HostAssignment.CostAllocationSplit.Canonical()) {
		t.Fatalf("extension changed facts outside its scope: %+v", r.Successor.HostAssignment)
	}
	if _, err := CloseHostEffects([]HostEffect{{Kind: EffectPayroll, Ref: "pay", Active: false}}, []string{"pay"}); !errors.Is(err, ErrReturnAlreadyClosed) {
		t.Fatalf("closed effect reopened: %v", err)
	}
	if _, err := CloseHostEffects([]HostEffect{{Kind: EffectPayroll, Ref: "pay", Active: true}}, []string{"tax"}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("missing return effect silently closed: %v", err)
	}
	if _, err := AppendRetroCorrection(nil, RetroCorrection{CorrectionID: "c", MobilityID: "m", EffectiveAt: time.Unix(1, 0), PriorDigest: "x", CorrectedDigest: "x", Reason: "r", EvidenceRef: "e"}, "x"); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("zero correction accepted: %v", err)
	}
	history := []RetroCorrection{{CorrectionID: "prior", MobilityID: "other", EffectiveAt: time.Unix(1, 0), PriorDigest: "a", CorrectedDigest: "b", Reason: "r", EvidenceRef: "e"}}
	if _, err := AppendRetroCorrection(history, RetroCorrection{CorrectionID: "next", MobilityID: "m", EffectiveAt: time.Unix(2, 0), PriorDigest: "b", CorrectedDigest: "c", Reason: "r", EvidenceRef: "e"}, "b"); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("cross-plan correction accepted: %v", err)
	}
	validHistory := []RetroCorrection{{CorrectionID: "prior", MobilityID: "m", EffectiveAt: time.Unix(1, 0), PriorDigest: "a", CorrectedDigest: "b", Reason: "r", EvidenceRef: "e"}}
	appended, err := AppendRetroCorrection(validHistory, RetroCorrection{CorrectionID: "next", MobilityID: "m", EffectiveAt: time.Unix(2, 0), PriorDigest: "b", CorrectedDigest: "c", Reason: "r", EvidenceRef: "e"}, "b")
	if err != nil {
		t.Fatal(err)
	}
	appended[0].Reason = "mutated"
	if validHistory[0].Reason != "r" {
		t.Fatal("retro append mutated caller history")
	}
}

func TestTodo_MOBILITY_002_StoreFence(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("11111111-1111-1111-1111-111111111111")
	other := values.TenantId("22222222-2222-2222-2222-222222222222")
	store := NewInMemoryPlanStore()
	p := validPlan(t)
	if err := store.PutForTenant(ctx, tenant, p); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetForTenant(ctx, other, p.MobilityID, p.Revision); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("cross-tenant plan read = %v", err)
	}
	next, err := ReplanOnMaterialChange(p, changeFixture(ChangeCountry))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutForTenant(ctx, tenant, next.Successor); err != nil {
		t.Fatal(err)
	}
	if err := store.PutForTenant(ctx, tenant, next.Successor); !errors.Is(err, ErrPlanDuplicate) {
		t.Fatalf("duplicate successor = %v", err)
	}
	stale := next.Successor
	stale.Revision = 3
	stale.ParentRevision = 1
	stale.ParentDigest = p.CanonicalDigest
	stale.CanonicalDigest = ""
	stale, err = NewMobilityPlan(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutForTenant(ctx, tenant, stale); !errors.Is(err, ErrPlanVersionConflict) {
		t.Fatalf("stale successor = %v", err)
	}
	got, err := store.GetForTenant(ctx, tenant, p.MobilityID, 2)
	if err != nil || got.ParentDigest != p.CanonicalDigest {
		t.Fatalf("stored successor = %+v, %v", got, err)
	}
	m := validImmigration(t)[0]
	if err := store.AppendImmigrationMilestoneForTenant(ctx, tenant, m, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendImmigrationMilestoneForTenant(ctx, tenant, m, 1); !errors.Is(err, ErrMilestoneDuplicate) {
		t.Fatalf("duplicate milestone = %v", err)
	}
	if err := store.AppendImmigrationMilestoneForTenant(ctx, tenant, m, 3); !errors.Is(err, ErrMilestoneVersionConflict) {
		t.Fatalf("skipped milestone sequence = %v", err)
	}
	items, err := store.ListImmigrationMilestonesForTenant(ctx, tenant, m.ProcessRef)
	if err != nil || len(items) != 1 || items[0].MilestoneID != m.MilestoneID {
		t.Fatalf("milestones = %+v, %v", items, err)
	}
	if _, err := store.ListImmigrationMilestonesForTenant(ctx, other, m.ProcessRef); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("cross-tenant milestone read = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.GetForTenant(cancelled, tenant, p.MobilityID, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read = %v", err)
	}
}
