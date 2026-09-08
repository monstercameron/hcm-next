package equity_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/equity"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestEquityConformancePreservesVestingLotsAndReconcilesProviderTaxEffects(t *testing.T) {
	plan := testPlan(t)
	grant := testGrant(t, plan)
	asOf := date(t, "2026-06-01")
	result, err := equity.ForfeitUnvestedLots(grant, asOf, "termination-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 4 || result.Position.VestedQuantity.String() != "0.00" || result.Position.ForfeitedQuantity.String() != "1200.00" {
		t.Fatalf("position = %#v", result.Position)
	}
	if result.Position.Lots[0].Vested {
		t.Fatal("future lot marked vested")
	}

	corrected := grant
	corrected.StrikePrice = decimal(t, "13.00")
	successor, err := equity.CorrectGrant(grant, corrected, "board-correction")
	if err != nil || successor.Revision != 2 || successor.ParentDigest != grant.CanonicalDigest {
		t.Fatalf("successor = %#v, err=%v", successor, err)
	}
	if _, err := equity.CorrectGrant(grant, grant, "board-correction"); !errors.Is(err, equity.ErrCorrectionMismatch) {
		t.Fatalf("unchanged correction = %v", err)
	}

	effect, err := equity.NewTaxPayrollEffect(equity.TaxPayrollEffect{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, WorkerRef: grant.WorkerRef, TaxAmount: decimal(t, "100.00"), PayrollAmount: decimal(t, "100.00"), Currency: "USD", EffectiveDate: asOf, SourceRef: "tax-rule-release-1"})
	if err != nil {
		t.Fatal(err)
	}
	obs := equity.ProviderObservation{Provider: "broker", ExternalRef: "statement-1", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Quantity: decimal(t, "1200.00"), TaxAmount: decimal(t, "100.00"), PayrollAmount: decimal(t, "100.00"), Currency: "USD", ObservedAt: instant(t, "2026-06-02T12:00:00Z")}
	rec, err := equity.ReconcileProviderEffect(effect, obs)
	if err != nil || rec.Status != equity.ProviderReconciled || rec.RepairRequired {
		t.Fatalf("reconcile = %#v, err=%v", rec, err)
	}
	obs.TaxAmount = decimal(t, "99.99")
	rec, err = equity.ReconcileProviderEffect(effect, obs)
	if err != nil || rec.Status != equity.ProviderRepairRequired || !rec.RepairRequired {
		t.Fatalf("mismatch = %#v, err=%v", rec, err)
	}
}

func TestTodo_EQUITY_002_Property(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	if _, err := equity.ForfeitUnvestedLots(grant, date(t, "2026-01-01"), ""); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("missing evidence = %v", err)
	}
}

func TestTodo_EQUITY_002_Golden(t *testing.T) {
	e, err := equity.NewVestingLotEvent(equity.VestingLotEvent{GrantDigest: "sha256:grant", GrantRevision: 1, Sequence: 1, Kind: equity.LotForfeited, Quantity: decimal(t, "1.00"), EffectiveDate: date(t, "2026-01-01"), EvidenceRef: "evidence"})
	const expectedDigest = "sha256:534eb3af6170c386b7cc538a0b2ecc2020733e035d9ab75fa8b39063833e81d3"
	if err != nil || e.Digest != expectedDigest || e.CanonicalDigest != expectedDigest {
		t.Fatalf("event = %#v, err=%v", e, err)
	}
}

func TestTodo_EQUITY_002_Race(t *testing.T) {
	store := equity.NewMemoryStore()
	plan := testPlan(t)
	if err := store.SavePlan(t.Context(), "tenant", plan); err != nil {
		t.Fatal(err)
	}
	grant := testGrant(t, plan)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- store.SaveGrant(t.Context(), "tenant", grant) }()
	}
	var successes int
	for i := 0; i < 2; i++ {
		if err := <-done; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent saves succeeded %d times", successes)
	}
}

func TestTodo_EQUITY_002_Fault(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	p, err := equity.CalculateVestingPosition(grant, date(t, "2027-02-01"), []equity.VestingLotEvent{{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: 1, Kind: equity.LotForfeited, Quantity: decimal(t, "300.00"), EffectiveDate: date(t, "2027-02-01"), EvidenceRef: "e"}})
	if !errors.Is(err, equity.ErrForfeitureVested) || p.GrantDigest != "" {
		t.Fatalf("vested forfeiture = %#v, %v", p, err)
	}
}

func TestTodo_EQUITY_002_Security(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	p, err := equity.CalculateVestingPosition(grant, date(t, "2026-06-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.GrantDigest != grant.CanonicalDigest {
		t.Fatal("position is not bound to grant")
	}
}

func TestTodo_EQUITY_002_Conformance(t *testing.T) {
	if equity.Version() != 1 {
		t.Fatal("schema drift")
	}
}

func TestTodo_EQUITY_002_Mutation(t *testing.T) {
	d, _ := values.NewDecimal("1.00", 2, values.RoundingExactRequired)
	if _, err := equity.NewTaxPayrollEffect(equity.TaxPayrollEffect{GrantDigest: "g", GrantRevision: 1, WorkerRef: "w", TaxAmount: d, PayrollAmount: values.Decimal{}, Currency: "USD", EffectiveDate: date(t, "2026-01-01"), SourceRef: "s"}); !errors.Is(err, equity.ErrInvalidEffect) {
		t.Fatalf("unset payroll amount accepted: %v", err)
	}
}

func TestTodo_EQUITY_002_ValidationMatrix(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	badEvent := equity.VestingLotEvent{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: 0, Kind: "NOPE"}
	if _, err := equity.NewVestingLotEvent(badEvent); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("bad event = %v", err)
	}
	if _, err := equity.CalculateVestingPosition(grant, date(t, "2026-06-01"), []equity.VestingLotEvent{{GrantDigest: "other", GrantRevision: 1, Sequence: 1, Kind: equity.LotForfeited, Quantity: decimal(t, "1.00"), EffectiveDate: date(t, "2026-06-01"), EvidenceRef: "e"}}); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("unbound event = %v", err)
	}
	if _, err := equity.CalculateVestingPosition(grant, date(t, "2026-06-01"), []equity.VestingLotEvent{{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: 99, Kind: equity.LotForfeited, Quantity: decimal(t, "1.00"), EffectiveDate: date(t, "2026-06-01"), EvidenceRef: "e"}}); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("unknown lot = %v", err)
	}
	if _, err := equity.CalculateVestingPosition(grant, date(t, "2026-06-01"), []equity.VestingLotEvent{{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: 1, Kind: equity.LotForfeited, Quantity: decimal(t, "9999.00"), EffectiveDate: date(t, "2026-06-01"), EvidenceRef: "e"}}); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("oversized lot = %v", err)
	}
	if _, err := equity.CorrectGrant(grant, grant, ""); !errors.Is(err, equity.ErrCorrectionMismatch) {
		t.Fatalf("missing correction evidence = %v", err)
	}
	if got := equity.UnknownProviderEffect("timeout"); got.Status != equity.ProviderUnknown {
		t.Fatalf("unknown = %#v", got)
	}
	if _, err := equity.ReconcileProviderEffect(equity.TaxPayrollEffect{}, equity.ProviderObservation{}); !errors.Is(err, equity.ErrInvalidEffect) {
		t.Fatalf("bad expected effect = %v", err)
	}
	if _, err := equity.NewProviderObservation(equity.ProviderObservation{}); !errors.Is(err, equity.ErrInvalidObservation) {
		t.Fatalf("bad observation = %v", err)
	}
}

func TestEquityVestingReplayIsExactAndConservative(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	asOf := date(t, "2026-06-01")
	base := equity.VestingLotEvent{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: 1, Kind: equity.LotForfeited, Quantity: decimal(t, "300.00"), EffectiveDate: asOf, EvidenceRef: "termination"}
	event, err := equity.NewVestingLotEvent(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := equity.CalculateVestingPosition(grant, asOf, []equity.VestingLotEvent{event, event}); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("duplicate replay accepted: %v", err)
	}
	partial := event
	partial.Quantity = decimal(t, "1.00")
	partial.CanonicalDigest, partial.Digest = "", ""
	if _, err := equity.CalculateVestingPosition(grant, asOf, []equity.VestingLotEvent{partial}); !errors.Is(err, equity.ErrInvalidLotEvent) {
		t.Fatalf("partial whole-lot event accepted: %v", err)
	}
	future := event
	future.EffectiveDate = date(t, "2028-01-01")
	future.CanonicalDigest, future.Digest = "", ""
	position, err := equity.CalculateVestingPosition(grant, asOf, []equity.VestingLotEvent{future})
	if err != nil {
		t.Fatal(err)
	}
	if position.ForfeitedQuantity.String() != "0.00" || position.UnvestedQuantity.String() != "1200.00" {
		t.Fatalf("future event affected position: %#v", position)
	}
	vested := event
	vested.Kind = equity.LotVested
	vested.CanonicalDigest, vested.Digest = "", ""
	position, err = equity.CalculateVestingPosition(grant, asOf, []equity.VestingLotEvent{vested})
	if err != nil || position.VestedQuantity.String() != "300.00" || position.UnvestedQuantity.String() != "900.00" {
		t.Fatalf("explicit vesting = %#v, %v", position, err)
	}
	historicalForfeiture := event
	historicalForfeiture.EffectiveDate = date(t, "2026-12-31")
	historicalForfeiture.CanonicalDigest, historicalForfeiture.Digest = "", ""
	position, err = equity.CalculateVestingPosition(grant, date(t, "2027-02-01"), []equity.VestingLotEvent{historicalForfeiture})
	if err != nil || position.ForfeitedQuantity.String() != "300.00" || position.VestedQuantity.String() != "0.00" || position.UnvestedQuantity.String() != "900.00" {
		t.Fatalf("historical forfeiture after vest date = %#v, %v", position, err)
	}
	position, err = equity.CalculateVestingPosition(grant, date(t, "2026-12-30"), []equity.VestingLotEvent{historicalForfeiture})
	if err != nil || position.ForfeitedQuantity.String() != "0.00" || position.UnvestedQuantity.String() != "1200.00" {
		t.Fatalf("future historical event applied too early = %#v, %v", position, err)
	}
}

func TestEquityConformanceRejectsInvalidAmountsAndBindings(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	asOf := date(t, "2026-06-01")
	negative := decimal(t, "-1.00")
	zero := decimal(t, "0.00")
	for name, effect := range map[string]equity.TaxPayrollEffect{
		"negative tax":     {GrantDigest: grant.CanonicalDigest, GrantRevision: 1, WorkerRef: grant.WorkerRef, TaxAmount: negative, PayrollAmount: zero, Currency: "USD", EffectiveDate: asOf, SourceRef: "source"},
		"negative payroll": {GrantDigest: grant.CanonicalDigest, GrantRevision: 1, WorkerRef: grant.WorkerRef, TaxAmount: zero, PayrollAmount: negative, Currency: "USD", EffectiveDate: asOf, SourceRef: "source"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := equity.NewTaxPayrollEffect(effect); !errors.Is(err, equity.ErrInvalidEffect) {
				t.Fatalf("invalid effect accepted: %v", err)
			}
		})
	}
	badObs := equity.ProviderObservation{Provider: "broker", ExternalRef: "statement", GrantDigest: grant.CanonicalDigest, GrantRevision: 1, Quantity: decimal(t, "1.00"), TaxAmount: zero, PayrollAmount: zero, ObservedAt: instant(t, "2026-06-02T12:00:00Z")}
	if _, err := equity.NewProviderObservation(badObs); !errors.Is(err, equity.ErrInvalidObservation) {
		t.Fatalf("currency-free observation accepted: %v", err)
	}
	badObs.Currency = "USD"
	badObs.Quantity = negative
	if _, err := equity.NewProviderObservation(badObs); !errors.Is(err, equity.ErrInvalidObservation) {
		t.Fatalf("negative observation accepted: %v", err)
	}
	badObs.Quantity = decimal(t, "1.00")
	observation, err := equity.NewProviderObservation(badObs)
	if err != nil || observation.ObservationDigest == "" {
		t.Fatalf("observation evidence = %#v, %v", observation, err)
	}
	observation.ExternalRef = "tampered"
	if err := observation.Validate(); !errors.Is(err, equity.ErrInvalidObservation) {
		t.Fatalf("tampered observation digest accepted: %v", err)
	}
}

func TestEquityCorrectionUsesVerifiedLineage(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	forged := grant
	forged.CanonicalDigest = "sha256:forged"
	replacement := grant
	replacement.StrikePrice = decimal(t, "13.00")
	replacement.WorkerRef = "different-worker"
	if _, err := equity.CorrectGrant(forged, replacement, "correction"); err == nil {
		t.Fatal("grant carrying forged digest accepted")
	}
	successor, err := equity.CorrectGrant(grant, replacement, "correction")
	if err != nil {
		t.Fatal(err)
	}
	if successor.ParentDigest != grant.CanonicalDigest || successor.SupersedesRevision != grant.Revision || successor.GrantID != grant.GrantID {
		t.Fatalf("lineage = %s", fmt.Sprintf("%#v", successor))
	}
	if successor.WorkerRef != grant.WorkerRef {
		t.Fatalf("correction transferred worker identity: %q", successor.WorkerRef)
	}
}

func TestEquityTaxPayrollEffectRejectsPostConstructionTampering(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	effect, err := equity.NewTaxPayrollEffect(equity.TaxPayrollEffect{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, WorkerRef: grant.WorkerRef, TaxAmount: decimal(t, "10.00"), PayrollAmount: decimal(t, "20.00"), Currency: "USD", EffectiveDate: date(t, "2026-06-01"), SourceRef: "assessed-effect"})
	if err != nil {
		t.Fatal(err)
	}
	effect.TaxAmount = decimal(t, "0.00")
	observation := equity.ProviderObservation{Provider: "broker", ExternalRef: "statement", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Quantity: decimal(t, "1.00"), TaxAmount: decimal(t, "0.00"), PayrollAmount: decimal(t, "20.00"), Currency: "USD", ObservedAt: instant(t, "2026-06-02T12:00:00Z")}
	if _, err := equity.ReconcileProviderEffect(effect, observation); !errors.Is(err, equity.ErrInvalidEffect) {
		t.Fatalf("tampered authoritative effect reconciled: %v", err)
	}
}

func TestEquityStorePreservesRevisionAndAcceptanceEvidence(t *testing.T) {
	ctx := t.Context()
	store := equity.NewMemoryStore()
	plan := testPlan(t)
	if err := store.SavePlan(ctx, "tenant", plan); err != nil {
		t.Fatal(err)
	}
	nextPlan := plan
	nextPlan.Revision = 0
	nextPlan.Name = "Amended LTIP"
	nextPlan.CanonicalDigest, nextPlan.Digest = "", ""
	nextPlan, err := plan.Successor(nextPlan)
	if err != nil || store.SavePlan(ctx, "tenant", nextPlan) != nil {
		t.Fatalf("successor plan = %#v, %v", nextPlan, err)
	}
	plans, err := store.ListPlans(ctx, "tenant", plan.PlanID)
	if err != nil || len(plans) != 2 || plans[0].Revision != 1 || plans[1].Revision != 2 {
		t.Fatalf("plans = %#v, %v", plans, err)
	}
	loadedPlan, err := store.LoadPlan(ctx, "tenant", plan.PlanID, 2)
	if err != nil || loadedPlan.CanonicalDigest != nextPlan.CanonicalDigest {
		t.Fatalf("loaded plan = %#v, %v", loadedPlan, err)
	}
	grant := testGrant(t, plan)
	if err := store.SaveGrant(ctx, "tenant", grant); err != nil {
		t.Fatal(err)
	}
	event, err := equity.NewAcceptanceEvent(equity.AcceptanceEvent{EventID: "accept-1", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, AcceptedBy: grant.WorkerRef, AcceptedAt: instant(t, "2026-02-01T12:00:00Z"), EvidenceRef: "signed"})
	if err != nil || store.RecordAcceptance(ctx, "tenant", event) != nil {
		t.Fatalf("acceptance = %#v, %v", event, err)
	}
	accepted, err := grant.Accept(event)
	if err != nil || store.SaveGrant(ctx, "tenant", accepted) != nil {
		t.Fatalf("accepted grant = %#v, %v", accepted, err)
	}
	grants, err := store.ListGrants(ctx, "tenant", grant.GrantID)
	if err != nil || len(grants) != 2 || grants[1].State != equity.GrantAccepted {
		t.Fatalf("grants = %#v, %v", grants, err)
	}
	loadedGrant, err := store.LoadGrant(ctx, "tenant", grant.GrantID, 2)
	if err != nil || loadedGrant.ParentDigest != grant.CanonicalDigest {
		t.Fatalf("loaded grant = %#v, %v", loadedGrant, err)
	}
	events, err := store.ListAcceptanceEvents(ctx, "tenant", grant.CanonicalDigest)
	if err != nil || len(events) != 1 || events[0].CanonicalDigest != event.CanonicalDigest {
		t.Fatalf("events = %#v, %v", events, err)
	}
	if err := store.RecordAcceptance(ctx, "tenant", event); !errors.Is(err, equity.ErrStoreDuplicate) {
		t.Fatalf("duplicate acceptance = %v", err)
	}
	active, err := accepted.Activate("active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := active.Settle("settled"); err != nil {
		t.Fatal(err)
	}
	if _, err := active.Forfeit("termination"); err != nil {
		t.Fatal(err)
	}
}
