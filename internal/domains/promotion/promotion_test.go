package promotion_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

const (
	testPolicyVersion = "authz.people/2026.1"
	testPurpose       = "promotion_preflight"
)

func date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("date(%q): %v", text, err)
	}
	return d
}

func money(t *testing.T, text, currency string) values.Money {
	t.Helper()
	m, err := fixtures.Money(text, currency)
	if err != nil {
		t.Fatalf("money(%q %s): %v", text, currency, err)
	}
	return m
}

func revision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	return r
}

func catalog(t *testing.T) *fixtures.MemoryBandCatalog {
	t.Helper()
	c, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		t.Fatalf("band catalog fixture: %v", err)
	}
	return c
}

// explain performs the governed read the preflight consumes. Going through
// ExplainWorkerState rather than hand-building a baseline is the point: it is
// how the tests exercise the "preflight does not trust caller-supplied facts"
// property end to end.
func explain(t *testing.T, workerKey string, mutate func(people.AuthorizationDecision) people.AuthorizationDecision) people.Explanation {
	t.Helper()
	reader, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("worker facts fixture: %v", err)
	}
	worker, err := fixtures.WorkerRef(workerKey)
	if err != nil {
		t.Fatalf("worker %q: %v", workerKey, err)
	}
	known, err := time.Parse(time.RFC3339, "2026-05-15T00:00:00Z")
	if err != nil {
		t.Fatalf("known timestamp: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(known))
	if err != nil {
		t.Fatalf("known at: %v", err)
	}
	fields := promotion.RequiredWorkerFields()
	decision := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)
	if mutate != nil {
		decision = mutate(decision)
	}
	explanation, err := people.ExplainWorkerState(context.Background(), reader, people.ExplainWorkerStateRequest{
		Tenant:        fixtures.Tenant,
		Worker:        worker,
		AsOf:          people.AsOf{EffectiveOn: date(t, "2026-06-01"), KnownAt: knownAt},
		Fields:        fields,
		Authorization: decision,
	})
	if err != nil {
		t.Fatalf("ExplainWorkerState: %v", err)
	}
	return explanation
}

// snapshot builds a complete, pinned compensation snapshot.
func snapshot(t *testing.T, amount, currency, bonus string, seq uint64) rewards.CompensationSnapshot {
	t.Helper()
	pct, err := fixtures.Percent(bonus)
	if err != nil {
		t.Fatalf("percent %q: %v", bonus, err)
	}
	return rewards.CompensationSnapshot{
		Base:               values.Value(money(t, amount, currency)),
		PayBasis:           rewards.PayBasisAnnualSalary,
		BonusTargetPercent: values.Value(pct),
		EffectiveDate:      date(t, "2026-06-01"),
		Watermark:          revision(t, "rewards.package.omar", seq),
		Complete:           true,
	}
}

// budget builds the workforce-budget observation a promotion cites.
func budget(t *testing.T, available string) *promotion.BudgetAuthorityRef {
	t.Helper()
	return &promotion.BudgetAuthorityRef{
		BudgetType:      promotion.BudgetTypeCompensationPool,
		OwnerSystem:     "adaptive.planning",
		PolicyRef:       "finance.authority/2026.1",
		Scope:           "people-ops:FY26-merit",
		Period:          "FY2026",
		Currency:        "USD",
		Unit:            "MONEY",
		BaselineVersion: "fy26-merit-r7",
		AvailableAmount: values.Value(money(t, available, "USD")),
		ObservationID:   "obs_budget_fy26_merit_r7",
	}
}

// baseRequest is a coherent promotion of Omar Reyes from OPS-HRBP2/P2 to
// OPS-HRBP3/P3 with the legacy 93,000 -> 98,000 USD raise.
func baseRequest(t *testing.T) promotion.PreflightRequest {
	t.Helper()
	subject, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("subject: %v", err)
	}
	return promotion.PreflightRequest{
		Tenant:         fixtures.Tenant,
		Subject:        subject,
		WorkerState:    explain(t, "omar-reyes", nil),
		Target:         promotion.TargetPlacement{JobCode: "OPS-HRBP3", Grade: "P3", OrgUnit: "people-ops", PositionID: "POS-HRBP-301", PayZone: "US-EAST"},
		Current:        snapshot(t, "93000.00", "USD", "0.0500", 11),
		Proposed:       snapshot(t, "98000.00", "USD", "0.0500", 11),
		EffectiveDate:  date(t, "2026-06-01"),
		EvaluationDate: date(t, "2026-05-15"),
		BusinessReason: "promotion_into_senior_hrbp",
		Budget:         budget(t, "50000.00"),
		Policy:         promotion.DefaultPolicy(),
		Annualization:  rewards.DefaultAnnualization(),
	}
}

func TestPreflightPromotionReturnsTypedStatusFindingsAndInputSnapshot(t *testing.T) {
	ctx := context.Background()
	req := baseRequest(t)

	result, err := promotion.PreflightPromotion(ctx, catalog(t), req)
	if err != nil {
		t.Fatalf("PreflightPromotion: %v", err)
	}

	if result.Status != promotion.StatusReady {
		t.Fatalf("status = %s, want READY; findings: %s", result.Status, renderFindings(result.Findings))
	}
	if len(result.Blocking()) != 0 {
		t.Fatalf("a READY preflight carried blocking findings: %s", renderFindings(result.Blocking()))
	}
	// The budget observation advisory is mandatory: a reviewer must never read
	// a silent budget section as an authorization to spend.
	if !result.HasCode(promotion.CodeBudgetObservationOnly) {
		t.Error("a preflight citing a budget must say the figure is an observation, not a reservation")
	}

	// Exact input snapshot, per INTENT-004.
	if result.Input.Baseline.JobCode != "OPS-HRBP2" || result.Input.Baseline.Grade != "P2" {
		t.Errorf("baseline = %s/%s, want OPS-HRBP2/P2", result.Input.Baseline.JobCode, result.Input.Baseline.Grade)
	}
	if !result.Input.Baseline.Watermark.IsSpecified() {
		t.Error("the input snapshot must pin the worker read watermark")
	}
	if result.Input.EvaluationDate != req.EvaluationDate || result.Input.EffectiveDate != req.EffectiveDate {
		t.Error("the input snapshot must echo the dates the verdict was computed against")
	}
	if result.Input.BandQuery == nil || result.Input.BandQuery.Grade != "P3" {
		t.Error("the input snapshot must record the band question that was asked")
	}

	if result.Band.State != rewards.BandResultEvaluated {
		t.Fatalf("band state = %s, want EVALUATED", result.Band.State)
	}
	if result.PolicyVersion != req.Policy.Version || result.RulePackVersion != promotion.RulePackVersion {
		t.Error("the result must pin the policy and rule pack it evaluated under")
	}

	if !result.Effects.IsZero() {
		t.Fatalf("preflight counted effects: %v", result.Effects.NonZero())
	}
	if err := result.Receipt.Validate(); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if result.Receipt.Mode != evidence.ModePreflight {
		t.Errorf("receipt mode = %s, want PREFLIGHT", result.Receipt.Mode)
	}
	if result.Receipt.RequestState != evidence.RequestStatePreflighted {
		t.Errorf("request state = %q, want PREFLIGHTED", result.Receipt.RequestState)
	}
	if result.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Errorf("execution state = %q, want NOT_PLANNED", result.Receipt.ExecutionState)
	}

	repeat, err := promotion.PreflightPromotion(ctx, catalog(t), baseRequest(t))
	if err != nil {
		t.Fatalf("repeat preflight: %v", err)
	}
	if !bytes.Equal(result.Canonical(), repeat.Canonical()) {
		t.Fatal("identical inputs produced different canonical bytes")
	}
}

func TestPreflightPromotionRaisesTypedBlockingAndAdvisoryFindings(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name       string
		mutate     func(*promotion.PreflightRequest)
		wantStatus promotion.Status
		wantCodes  []string
	}{
		{
			name: "out-of-band pay on a blocking band blocks",
			mutate: func(r *promotion.PreflightRequest) {
				r.Target = promotion.TargetPlacement{JobCode: "CLN-NURSE4", Grade: "N4", PayZone: "US-EAST"}
				r.Proposed = snapshot(t, "130000.00", "USD", "0.0500", 11)
				r.Budget = budget(t, "80000.00")
			},
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeAboveBandMaximum},
		},
		{
			name: "out-of-band pay on an advisory band only advises",
			mutate: func(r *promotion.PreflightRequest) {
				r.Proposed = snapshot(t, "140000.00", "USD", "0.0500", 11)
				r.Budget = budget(t, "80000.00")
			},
			wantStatus: promotion.StatusReady,
			wantCodes:  []string{promotion.CodeAboveBandMaximum, promotion.CodeIncreaseOverThreshold},
		},
		{
			name: "a retroactive effective date beyond policy blocks",
			mutate: func(r *promotion.PreflightRequest) {
				r.EffectiveDate = date(t, "2025-10-01")
			},
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeEffectiveAtTooFarPast},
		},
		{
			name: "an effective date before the hire date blocks",
			mutate: func(r *promotion.PreflightRequest) {
				subject, err := fixtures.WorkerRef("noor-haddad")
				if err != nil {
					t.Fatalf("subject: %v", err)
				}
				r.Subject = subject
				r.WorkerState = explain(t, "noor-haddad", nil)
				r.EffectiveDate = date(t, "2026-05-20")
			},
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeEffectiveBeforeHire},
		},
		{
			name:       "a missing budget authority blocks",
			mutate:     func(r *promotion.PreflightRequest) { r.Budget = nil },
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeBudgetAuthorityMissing},
		},
		{
			name: "a same-grade move is not a promotion",
			mutate: func(r *promotion.PreflightRequest) {
				r.Target.Grade = "P2"
				r.Target.JobCode = "OPS-HRBP2"
			},
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeSameGrade},
		},
		{
			name: "a currency change is not supported",
			mutate: func(r *promotion.PreflightRequest) {
				r.Proposed = snapshot(t, "98000.00", "EUR", "0.0500", 11)
			},
			wantStatus: promotion.StatusNeedsData,
			wantCodes:  []string{promotion.CodeCurrencyChangeNotV0, promotion.CodePayBandNotFound},
		},
		{
			name: "a terminated worker cannot be promoted",
			mutate: func(r *promotion.PreflightRequest) {
				subject, err := fixtures.WorkerRef("lena-park")
				if err != nil {
					t.Fatalf("subject: %v", err)
				}
				r.Subject = subject
				r.WorkerState = explain(t, "lena-park", nil)
				r.Target = promotion.TargetPlacement{JobCode: "ENG-MGR1", Grade: "M1", PayZone: "US-WEST"}
				r.Current = snapshot(t, "171000.00", "USD", "0.0500", 11)
				r.Proposed = snapshot(t, "185000.00", "USD", "0.0500", 11)
				r.Budget = budget(t, "80000.00")
			},
			wantStatus: promotion.StatusBlocked,
			wantCodes:  []string{promotion.CodeWorkerNotActive},
		},
		{
			name: "an observed budget short of the cost advises but does not block",
			mutate: func(r *promotion.PreflightRequest) {
				r.Budget = budget(t, "100.00")
			},
			wantStatus: promotion.StatusReady,
			wantCodes:  []string{promotion.CodeBudgetObservedShort},
		},
		{
			name: "a denied required field denies the whole preflight",
			mutate: func(r *promotion.PreflightRequest) {
				r.WorkerState = explain(t, "omar-reyes", func(d people.AuthorizationDecision) people.AuthorizationDecision {
					return fixtures.DenyFields(d, "compensation_compartment", people.FieldGrade)
				})
			},
			wantStatus: promotion.StatusDenied,
			wantCodes:  []string{promotion.CodeRequiredFieldDenied},
		},
		{
			name: "a non-disclosable subject denies without evaluating the promotion",
			mutate: func(r *promotion.PreflightRequest) {
				r.WorkerState = explain(t, "omar-reyes", func(d people.AuthorizationDecision) people.AuthorizationDecision {
					return fixtures.WithheldSubject(d, "outside_population_scope")
				})
			},
			wantStatus: promotion.StatusDenied,
			wantCodes:  []string{promotion.CodeSubjectNotDisclosable},
		},
		{
			name: "an unresolvable pay band needs data",
			mutate: func(r *promotion.PreflightRequest) {
				r.Target.JobCode = "NOT-A-JOB"
			},
			wantStatus: promotion.StatusNeedsData,
			wantCodes:  []string{promotion.CodePayBandNotFound},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest(t)
			tc.mutate(&req)
			result, err := promotion.PreflightPromotion(ctx, catalog(t), req)
			if err != nil {
				t.Fatalf("PreflightPromotion: %v", err)
			}
			if result.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s; findings:\n%s",
					result.Status, tc.wantStatus, renderFindings(result.Findings))
			}
			for _, code := range tc.wantCodes {
				if !result.HasCode(code) {
					t.Errorf("missing finding %s; findings:\n%s", code, renderFindings(result.Findings))
				}
			}
			if !result.Effects.IsZero() {
				t.Errorf("preflight counted effects: %v", result.Effects.NonZero())
			}
		})
	}
}

func TestPreflightPromotionPortsTheLegacyCompensationScenarios(t *testing.T) {
	ctx := context.Background()
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		t.Fatalf("legacy scenarios: %v", err)
	}
	if len(set.Scenarios) == 0 {
		t.Fatal("the ported legacy corpus is empty")
	}

	for _, sc := range set.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			subject, err := fixtures.WorkerRef(set.Worker)
			if err != nil {
				t.Fatalf("subject: %v", err)
			}
			req := promotion.PreflightRequest{
				Tenant:      fixtures.Tenant,
				Subject:     subject,
				WorkerState: explain(t, set.Worker, nil),
				Target: promotion.TargetPlacement{
					JobCode: set.Target.JobCode,
					Grade:   set.Target.Grade,
					PayZone: set.Target.PayZone,
				},
				Current:        snapshot(t, sc.CurrentAmount, sc.CurrentCurrency, sc.BonusTarget, 11),
				Proposed:       snapshot(t, sc.ProposedAmount, sc.ProposedCurrency, sc.BonusTarget, 11),
				EffectiveDate:  date(t, sc.EffectiveAt),
				EvaluationDate: date(t, sc.EvaluationAt),
				BusinessReason: sc.BusinessReason,
				Budget:         budget(t, set.BudgetAvailable),
				Policy:         promotion.DefaultPolicy(),
				Annualization:  rewards.DefaultAnnualization(),
			}

			result, err := promotion.PreflightPromotion(ctx, catalog(t), req)
			if err != nil {
				t.Fatalf("PreflightPromotion: %v", err)
			}
			if result.Status.String() != sc.ExpectStatus {
				t.Errorf("status = %s, want %s (legacy case %s); findings:\n%s",
					result.Status, sc.ExpectStatus, sc.LegacyCase, renderFindings(result.Findings))
			}
			for _, code := range sc.ExpectCodes {
				if !result.HasCode(code) {
					t.Errorf("missing legacy finding %s; findings:\n%s", code, renderFindings(result.Findings))
				}
			}
			for _, code := range sc.ForbidCodes {
				if result.HasCode(code) {
					t.Errorf("unexpected finding %s; findings:\n%s", code, renderFindings(result.Findings))
				}
			}
		})
	}
}

func TestSimulatePromotionProjectsWorkerStateAndCompensationWithAZeroEffectReceipt(t *testing.T) {
	ctx := context.Background()
	req := baseRequest(t)

	result, err := promotion.SimulatePromotion(ctx, catalog(t), req)
	if err != nil {
		t.Fatalf("SimulatePromotion: %v", err)
	}

	if !result.Executable {
		t.Fatalf("a READY preflight must yield an executable simulation; status %s", result.Preflight.Status)
	}

	changed := map[string]promotion.PlacementChange{}
	for _, c := range result.Projected.Changes {
		changed[c.Field] = c
	}
	job := changed[people.FieldJobCode.String()]
	if job.Before != "OPS-HRBP2" || job.After != "OPS-HRBP3" || !job.Changed {
		t.Errorf("job change = %+v, want OPS-HRBP2 -> OPS-HRBP3", job)
	}
	grade := changed[people.FieldGrade.String()]
	if grade.Before != "P2" || grade.After != "P3" || !grade.Changed {
		t.Errorf("grade change = %+v, want P2 -> P3", grade)
	}
	if zone := changed[people.FieldPayZone.String()]; zone.Changed {
		t.Errorf("pay zone must be unchanged, got %+v", zone)
	}
	if result.Projected.EffectiveDate != req.EffectiveDate {
		t.Error("the projection must carry the effective date it was projected for")
	}

	if result.CompensationState != promotion.CompensationEvaluated {
		t.Fatalf("compensation state = %s, want EVALUATED (%s)",
			result.CompensationState, result.CompensationReason)
	}
	if got := result.Compensation.Delta.AnnualizedTotalCash.Amount().String(); got != "5250.00" {
		t.Errorf("annualized total cash delta = %s, want 5250.00", got)
	}
	if got := result.Compensation.Delta.IncreasePercent.String(); got != "5.3763" {
		t.Errorf("increase percent = %s, want 5.3763", got)
	}

	if !result.Effects.IsZero() || !result.Compensation.Effects.IsZero() ||
		!result.Preflight.Effects.IsZero() {
		t.Fatal("a simulation must count zero effects at every level")
	}
	if err := result.Receipt.Validate(); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if result.Receipt.Mode != evidence.ModeSimulate {
		t.Errorf("receipt mode = %s, want SIMULATE", result.Receipt.Mode)
	}
	if result.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Errorf("execution state = %q, want NOT_PLANNED", result.Receipt.ExecutionState)
	}

	repeat, err := promotion.SimulatePromotion(ctx, catalog(t), baseRequest(t))
	if err != nil {
		t.Fatalf("repeat simulation: %v", err)
	}
	if !bytes.Equal(result.Canonical(), repeat.Canonical()) {
		t.Fatal("identical inputs produced different canonical bytes")
	}
	if result.ResultDigest != repeat.ResultDigest {
		t.Fatalf("result digest drifted: %s vs %s", result.ResultDigest, repeat.ResultDigest)
	}
}

func TestSimulatePromotionRefusesToPresentABlockedPromotionAsExecutable(t *testing.T) {
	ctx := context.Background()

	t.Run("a blocked promotion still simulates but is not executable", func(t *testing.T) {
		req := baseRequest(t)
		req.EffectiveDate = date(t, "2025-10-01")
		result, err := promotion.SimulatePromotion(ctx, catalog(t), req)
		if err != nil {
			t.Fatalf("SimulatePromotion: %v", err)
		}
		if result.Executable {
			t.Fatal("a BLOCKED preflight must not yield an executable simulation")
		}
		if result.Preflight.Status != promotion.StatusBlocked {
			t.Fatalf("preflight status = %s, want BLOCKED", result.Preflight.Status)
		}
	})

	t.Run("a currency mismatch leaves the compensation section unevaluated with a reason", func(t *testing.T) {
		req := baseRequest(t)
		req.Proposed = snapshot(t, "98000.00", "EUR", "0.0500", 11)
		result, err := promotion.SimulatePromotion(ctx, catalog(t), req)
		if err != nil {
			t.Fatalf("SimulatePromotion: %v", err)
		}
		if result.CompensationState != promotion.CompensationNotEvaluated {
			t.Fatal("a currency mismatch must not produce compensation numbers")
		}
		if result.CompensationReason == "" {
			t.Error("an unevaluated compensation section must say why")
		}
		if result.Executable {
			t.Fatal("a promotion with a currency mismatch must not be executable")
		}
	})

	t.Run("a denied subject yields no simulation at all", func(t *testing.T) {
		req := baseRequest(t)
		req.WorkerState = explain(t, "omar-reyes", func(d people.AuthorizationDecision) people.AuthorizationDecision {
			return fixtures.WithheldSubject(d, "outside_population_scope")
		})
		_, err := promotion.SimulatePromotion(ctx, catalog(t), req)
		if !errors.Is(err, promotion.ErrPreflightDenied) {
			t.Fatalf("error = %v, want ErrPreflightDenied", err)
		}
	})

	t.Run("a catalog fault is an error, not a promotion without a band", func(t *testing.T) {
		broken := catalog(t)
		broken.Fail = errors.New("connection reset")
		_, err := promotion.SimulatePromotion(ctx, broken, baseRequest(t))
		if !errors.Is(err, promotion.ErrCatalogFailed) {
			t.Fatalf("error = %v, want ErrCatalogFailed", err)
		}
	})

	t.Run("a preflight for a subject the worker state does not describe is refused", func(t *testing.T) {
		req := baseRequest(t)
		other, err := fixtures.WorkerRef("jane-doe")
		if err != nil {
			t.Fatalf("other worker: %v", err)
		}
		req.Subject = other
		if _, err := promotion.PreflightPromotion(ctx, catalog(t), req); !errors.Is(err, promotion.ErrRequestInvalid) {
			t.Fatalf("error = %v, want ErrRequestInvalid", err)
		}
	})
}

func TestSimulatePromotionGolden(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*promotion.PreflightRequest)
	}{
		{"ready-promotion", func(*promotion.PreflightRequest) {}},
		{"blocked-retroactive", func(r *promotion.PreflightRequest) { r.EffectiveDate = date(t, "2025-10-01") }},
		{"advisory-band-exception", func(r *promotion.PreflightRequest) {
			r.Proposed = snapshot(t, "140000.00", "USD", "0.0500", 11)
			r.Budget = budget(t, "80000.00")
		}},
		{"needs-data-missing-band", func(r *promotion.PreflightRequest) { r.Target.JobCode = "NOT-A-JOB" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest(t)
			tc.mutate(&req)
			result, err := promotion.SimulatePromotion(ctx, catalog(t), req)
			if err != nil {
				t.Fatalf("SimulatePromotion: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", "golden", tc.name+".txt"), renderSimulation(result))
		})
	}
}

// renderFindings prints a finding set for a failure message.
func renderFindings(findings []promotion.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s [%s] %s: %s\n", f.Code, f.Severity, f.Field, f.Message)
	}
	if b.Len() == 0 {
		return "  (none)\n"
	}
	return b.String()
}

// renderSimulation prints the simulation in a stable, reviewable form.
func renderSimulation(r promotion.SimulationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "intent: %s/%s\n", r.IntentType, r.IntentVersion)
	fmt.Fprintf(&b, "status: %s\n", r.Preflight.Status)
	fmt.Fprintf(&b, "executable: %v\n", r.Executable)
	b.WriteString("findings:\n")
	b.WriteString(renderFindings(r.Preflight.Findings))
	for _, c := range r.Projected.Changes {
		fmt.Fprintf(&b, "change %s: %q -> %q changed=%v\n", c.Field, c.Before, c.After, c.Changed)
	}
	fmt.Fprintf(&b, "compensation_state: %s\n", r.CompensationState)
	if r.CompensationReason != "" {
		fmt.Fprintf(&b, "compensation_reason: %s\n", r.CompensationReason)
	}
	if r.CompensationState == promotion.CompensationEvaluated {
		fmt.Fprintf(&b, "annualized_base_delta: %s\n", r.Compensation.Delta.AnnualizedBase)
		fmt.Fprintf(&b, "annualized_total_cash_delta: %s\n", r.Compensation.Delta.AnnualizedTotalCash)
		fmt.Fprintf(&b, "increase_percent: %s\n", r.Compensation.Delta.IncreasePercent)
	}
	fmt.Fprintf(&b, "band_state: %s\n", r.Preflight.Band.State)
	if r.Preflight.Band.State == rewards.BandResultEvaluated {
		p := r.Preflight.Band.Evaluation.Position
		fmt.Fprintf(&b, "band: %s@%s placement=%s compa=%s penetration=%s quartile=%d outcome=%s\n",
			p.BandID, p.BandVersion, p.Placement, p.CompaRatio, p.RangePenetration,
			p.Quartile, r.Preflight.Band.Evaluation.Outcome)
	}
	for _, c := range r.Receipt.Controls {
		fmt.Fprintf(&b, "control %s@%s\n", c.Name, c.Version)
	}
	fmt.Fprintf(&b, "effects_zero: %v\n", r.Effects.IsZero())
	fmt.Fprintf(&b, "receipt_mode: %s\n", r.Receipt.Mode)
	fmt.Fprintf(&b, "receipt_execution_state: %s\n", r.Receipt.ExecutionState)
	fmt.Fprintf(&b, "inputs_digest: %s\n", r.InputsDigest)
	fmt.Fprintf(&b, "result_digest: %s\n", r.ResultDigest)
	return b.String()
}

// compareGolden compares got against the golden file at path, or rewrites it
// under -update.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
