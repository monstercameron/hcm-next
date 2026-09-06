package achrisk

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/contact"
	"github.com/monstercameron/hcm-next/internal/domains/paymethod"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

func achRiskInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

type achRiskContactSource struct {
	endpoint contact.ContactEndpointRevision
}

func (s achRiskContactSource) ResolveIndependentContact(string, string, string) (contact.ContactEndpointRevision, error) {
	return s.endpoint, nil
}

type achRiskDispatcher struct{}

func (achRiskDispatcher) DispatchOutOfBand(paymethod.ChangeConfirmation) error { return nil }

func achRiskDestinationChange(t *testing.T) *paymethod.BankDetailChange {
	t.Helper()
	first, err := paymethod.NewDestination(paymethod.Destination{DestinationID: "destination-1", WorkerRef: "worker-1", Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, GovernedRef: "token:old", DisplayHint: "••••1234", Currency: "USD", CountryCode: "US", Effective: achRiskInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.GovernedRef = "token:new"
	second.DisplayHint = "••••5678"
	second, err = first.NewRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	change, err := paymethod.NewDestinationChange(first, second, "change-1", "requester", "approver", achRiskInterval(t))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := contact.NewContactEndpointRevision(values.EntityRef{Tenant: values.TenantId("tenant-1"), Kind: values.Kind("worker"), Id: "00000000-0000-0000-0000-000000000001"}, "contact-1", contact.EndpointEmail, "payroll-alerts@example.test", "payment_destination_change_confirmation", 1, "hr-contact-profile")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = endpoint.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	started, err := paymethod.StartBankDetailChange(paymethod.BankDetailChangeRequest{
		ID: change.ID, TenantID: "tenant-1", DestinationID: change.DestinationID, WorkerRef: change.WorkerRef,
		BeforeDigest: change.PreviousDigest, AfterDigest: change.ProposedDigest, RequestedBy: change.RequestedBy, Approver: change.Approver,
		RequestedAt: time.Unix(100, 0).UTC(), CoolingOff: time.Hour,
		Constraints:     sod.Constraints{RuleID: "payment-destination-dual-control", RequesterMayNotApprove: true, OneApprovalPerPrincipal: true},
		DecisionContext: sod.DecisionContext{Requester: sod.Actor{Subject: "requester"}, Approvers: []sod.Actor{{Subject: "approver"}}},
	}, achRiskContactSource{endpoint: endpoint}, achRiskDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	return &started
}

func achRiskInput(t *testing.T) EvaluationInput {
	t.Helper()
	return EvaluationInput{
		TenantID: "tenant-1", Participant: Originator, AnnualACHVolume2023: 7000000, EvaluatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Signals:                  Signals{AmountMinor: 20000000, VelocityCount: 11, VelocityWindow: time.Hour, DestinationAge: time.Hour, OperatorRisk: 70, DeviceRisk: 70, TimingRisk: 70, PayrollChange: true},
		PaymentDestinationChange: achRiskDestinationChange(t),
	}
}

func TestTodo_SECARCH_016(t *testing.T) {
	assessment, err := DefaultPolicy().Evaluate(achRiskInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Decision != Reject || assessment.Score < assessment.Threshold || assessment.RuleVersion == "" || !assessment.Deadline.Applicable {
		t.Fatalf("assessment = %+v", assessment)
	}
	if assessment.Deadline.Date != time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("deadline = %+v", assessment.Deadline)
	}
	dispositioned, err := assessment.RecordDisposition(DispositionInput{Investigator: "investigator-1", Outcome: DispositionCleared, RecordedAt: assessment.EvaluatedAt.Add(time.Hour), NotesDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"})
	if err != nil {
		t.Fatal(err)
	}
	if dispositioned.Disposition == nil || dispositioned.Revision != 2 || dispositioned.SupersedesDigest != assessment.CanonicalDigest {
		t.Fatalf("dispositioned = %+v", dispositioned)
	}
}

func TestTodo_SECARCH_016_Golden(t *testing.T) {
	assessment, err := DefaultPolicy().Evaluate(EvaluationInput{TenantID: "tenant-1", Participant: ReceivingDepository, AnnualACHVolume2023: 1, EvaluatedAt: time.Unix(100, 0).UTC(), Signals: Signals{DestinationAge: 48 * time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(assessment.CanonicalDigest, "sha256:") || assessment.Decision != Release || assessment.Threshold != 0 {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestTodo_SECARCH_016_Security(t *testing.T) {
	assessment, err := DefaultPolicy().Evaluate(achRiskInput(t))
	if err != nil {
		t.Fatal(err)
	}
	explanation := assessment.Explain()
	for _, forbidden := range []string{"tenant-1", "investigator-1", "destination-1", "worker-1"} {
		if strings.Contains(explanation, forbidden) {
			t.Fatalf("unsafe explanation contains %q: %s", forbidden, explanation)
		}
	}
	if _, err := ResolveNacha2026Deadline(ParticipantType("UNKNOWN"), 0, false); err == nil {
		t.Fatal("unknown participant was accepted")
	}
}

func TestTodo_SECARCH_016_Integration(t *testing.T) {
	// The integration path uses the real paymethod constructors for the
	// SECARCH-013 destination-change input; the risk package remains pure.
	assessment, err := DefaultPolicy().Evaluate(achRiskInput(t))
	if err != nil || assessment.DestinationChangeDigest == "" || assessment.Deadline.Phase != "PHASE_1" {
		t.Fatalf("assessment=%+v err=%v", assessment, err)
	}
}

func TestTodo_SECARCH_016_Mutation(t *testing.T) {
	assessment, err := DefaultPolicy().Evaluate(achRiskInput(t))
	if err != nil {
		t.Fatal(err)
	}
	before := assessment.CanonicalDigest
	dispositioned, err := assessment.RecordDisposition(DispositionInput{Investigator: "investigator-1", Outcome: DispositionEscalated, RecordedAt: assessment.EvaluatedAt.Add(time.Hour), NotesDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.CanonicalDigest != before || assessment.Disposition != nil || dispositioned.CanonicalDigest == before {
		t.Fatalf("assessment mutated: original=%+v next=%+v", assessment, dispositioned)
	}
}
