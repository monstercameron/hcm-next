package workforce_test

// PROMOUX-006: "Show the authorized compensation baseline and exact entry
// guardrail before submit."
//
// TestTodo_PROMOUX_006_Integration reaches real PostgreSQL end to end: it
// seeds one worker's real journey_worker row (built by newRow in
// store_test.go, which already carries job code, grade, pay zone, base pay,
// currency and pay basis -- migration 00023's schema stores compensation
// alongside the rest of the created population, even though no domain port
// reads it as such today), reads that row back through workforce.Store.Get
// inside a real tenant-scoped transaction -- the same row-level-security
// path a production read takes -- and feeds the genuinely-stored figures
// into promotion.EvaluateCompensationGuardrail. No fixture and no stub
// reader stands in for the worker's own compensation baseline; only the
// target pay band is a caller-supplied value, because no adapter anywhere
// in this tree yet implements rewards.CompensationFacts or
// rewards.PayBandCatalog against PostgreSQL (confirmed by inspection: every
// existing production composition wires both from
// internal/domains/fixtures). That absence is a boundary of this todo, not
// something this test can manufacture -- see the PROMOUX-006 report.
import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// promoux006Catalog is a minimal rewards.PayBandCatalog answering exactly
// one scope -- the real worker's own job/grade/pay-zone, read back from
// Postgres below -- since no production pay-band adapter exists yet.
type promoux006Catalog struct{ band payband.Band }

func (c promoux006Catalog) LookupBand(_ context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if q.JobCode != c.band.Scope.JobCode || q.Grade != c.band.Scope.Grade || q.PayZone != c.band.Scope.PayZone || q.Currency != c.band.Currency() {
		return rewards.BandRecord{}, rewards.ErrBandNotFound
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(fixedInstant))
	if err != nil {
		return rewards.BandRecord{}, err
	}
	return rewards.BandRecord{
		Band: c.band, CatalogVersion: "promoux006-integration-catalog/1",
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "promoux006-test", PolicyRef: "promoux006-test/v1"},
		Provenance: evidence.Provenance{Source: "promoux006-test", EvidenceRef: "evidence-promoux006-integration", RecordedAt: recorded},
	}, nil
}

func promoux006Money(t *testing.T, amount, currency string) values.Money {
	t.Helper()
	m, err := values.NewMoney(amount, currency, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("money(%q %s): %v", amount, currency, err)
	}
	return m
}

func TestTodo_PROMOUX_006_Integration(t *testing.T) {
	db, tenant, row := seedWorker(t, "promoux006-guardrail")
	conn := appConn(t, db)

	// Read the worker's own stored compensation and role facts back through
	// the real store, inside a real tenant-scoped transaction.
	var got workforce.WorkerRow
	var found bool
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		got, found, err = workforce.Store{}.Get(context.Background(), tx, tenant, row.WorkerID.String())
		return err
	})
	if !found {
		t.Fatal("the just-created worker was not found through a real tenant-scoped read")
	}
	if got.PayBasis != "ANNUAL_SALARY" {
		t.Fatalf("fixture pay basis is now %q; this test's rewards.PayBasisAnnualSalary mapping needs updating", got.PayBasis)
	}
	if got.BasePay != "90000.00" {
		t.Fatalf("fixture base pay is now %q; this test's exact expectations below need updating", got.BasePay)
	}

	currentPay := promoux006Money(t, got.BasePay, got.Currency)
	revision, err := values.NewSequenceRevision(got.RevisionStream, got.RevisionSequence)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	effective, err := values.ParseLocalDate(got.EffectiveFrom)
	if err != nil {
		t.Fatalf("effective date: %v", err)
	}

	band := payband.Band{
		ID: "promoux006-integration-band", Version: "1",
		Scope:    payband.Scope{JobCode: got.JobCode, Grade: got.Grade, PayZone: got.PayZone},
		Minimum:  promoux006Money(t, "80000.00", got.Currency),
		Midpoint: promoux006Money(t, "95000.00", got.Currency),
		Maximum:  promoux006Money(t, "110000.00", got.Currency),
	}

	req := promotion.CompensationGuardrailRequest{
		Current: rewards.CompensationSnapshot{
			Base: values.Value(currentPay), PayBasis: rewards.PayBasisAnnualSalary,
			EffectiveDate: effective, Watermark: revision, Complete: true,
		},
		Target: rewards.BandQuery{
			Tenant: "promoux006-guardrail-tenant", JobCode: got.JobCode, Grade: got.Grade, PayZone: got.PayZone,
			Currency: got.Currency, AsOf: effective,
		},
		Catalog:       promoux006Catalog{band: band},
		Annualization: rewards.DefaultAnnualization(),
	}
	guardrail, err := promotion.EvaluateCompensationGuardrail(context.Background(), req)
	if err != nil {
		t.Fatalf("EvaluateCompensationGuardrail: %v", err)
	}
	if !guardrail.Available() {
		t.Fatalf("guardrail = %s/%s, want available for a real, well-formed worker row", guardrail.Status, guardrail.Reason)
	}
	if cmp, err := guardrail.CurrentAnnualized.Cmp(currentPay); err != nil || cmp != 0 {
		t.Errorf("CurrentAnnualized = %s, want the worker's real stored base pay %s", guardrail.CurrentAnnualized, currentPay)
	}
	// 90000.00 stored, real band 80000.00..95000.00..110000.00: permitted
	// increase to the real maximum is (110000.00-90000.00)/90000.00 =
	// 0.222222... exactly, at the guardrail's declared 6-digit scale.
	wantPercent, err := values.NewPercentage("0.222222", 6, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	if guardrail.PermittedIncreasePercent.Fraction().Cmp(wantPercent.Fraction()) != 0 {
		t.Errorf("PermittedIncreasePercent = %s, want %s", guardrail.PermittedIncreasePercent, wantPercent)
	}
	if guardrail.BandPosition != payband.PlacementInBand {
		t.Errorf("BandPosition = %s, want IN_BAND (90000.00 sits inside 80000.00..110000.00)", guardrail.BandPosition)
	}

	// The authorization boundary holds against this same real worker: a
	// redacted read of the identical real row fails closed, never to a
	// permissive blank.
	req.Current.Base = values.Redacted[values.Money]("scope.compensation.denied")
	unauthorized, err := promotion.EvaluateCompensationGuardrail(context.Background(), req)
	if err != nil {
		t.Fatalf("EvaluateCompensationGuardrail (redacted): %v", err)
	}
	if unauthorized.Available() || unauthorized.Reason != promotion.GuardrailReasonNotAuthorized {
		t.Fatalf("redacted read of a real worker's compensation = %s/%s, want unavailable/NOT_AUTHORIZED", unauthorized.Status, unauthorized.Reason)
	}
}
