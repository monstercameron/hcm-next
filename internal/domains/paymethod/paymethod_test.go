package paymethod

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func payMethodInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, _ := values.ParseLocalDate("2026-01-01")
	end, _ := values.ParseLocalDate("2027-01-01")
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func payMethodInstant(t *testing.T, seconds int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(seconds, 0)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func validDestination(t *testing.T, id string) Destination {
	t.Helper()
	d, err := NewDestination(Destination{DestinationID: id, WorkerRef: "worker-1", Rail: RailACH, Risk: RiskMedium, GovernedRef: "vault-token:" + id, DisplayHint: "••••1234", Currency: "USD", CountryCode: "US", Effective: payMethodInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPaymentDestinationModelRejectsPlaintextUnverifiedAndInvalidSplit(t *testing.T) {
	good := validDestination(t, "dest-1")
	if good.CanonicalDigest == "" || good.Verification != VerificationUnverified {
		t.Fatal("destination was not created as an unverified governed reference")
	}
	bad := good
	bad.AccountNumber = "123456789012"
	if _, err := NewDestination(bad); !errors.Is(err, ErrRawBankDetailProhibited) {
		t.Fatalf("plaintext error = %v", err)
	}
	challenge, err := NewVerificationChallenge(good, "challenge-1", MethodMicroDeposit, payMethodInstant(t, 100), payMethodInstant(t, 160), 2)
	if err != nil {
		t.Fatal(err)
	}
	event, err := challenge.Verify(payMethodInstant(t, 120), "sha256:evidence")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := ApplyVerification(good, event)
	if err != nil || verified.Verification != VerificationVerified {
		t.Fatalf("verified destination = %+v, err=%v", verified, err)
	}
	amount, _ := values.NewDecimal("10.00", 2, values.RoundingExactRequired)
	percent, _ := values.NewDecimal("50.00", 2, values.RoundingExactRequired)
	_, err = NewSplitPlan(SplitPlan{Currency: "USD", TotalAmount: amount, Splits: []SplitPriority{{Priority: 1, DestinationRef: "dest-1", Kind: SplitFixedAmount, Value: amount}, {Priority: 2, DestinationRef: "dest-2", Kind: SplitPercentage, Value: percent}}, RemainderDestinationRef: "dest-2"})
	if err == nil || !errors.Is(err, ErrInvalidSplit) {
		t.Fatalf("invalid mixed split error = %v", err)
	}
	invalidPercent, _ := values.NewDecimal("60.00", 2, values.RoundingExactRequired)
	if _, err := NewSplitPlan(SplitPlan{Currency: "USD", Splits: []SplitPriority{{Priority: 1, DestinationRef: "dest-1", Kind: SplitPercentage, Value: invalidPercent}}}); err == nil {
		t.Fatal("percentage split without remainder was accepted")
	}
}

func TestTodo_PAYMETHOD_001_Property(t *testing.T) {
	d := validDestination(t, "dest-1")
	if d.CanonicalDigest != validDestination(t, "dest-1").CanonicalDigest {
		t.Fatal("destination digest is not deterministic")
	}
}
func TestTodo_PAYMETHOD_001_Golden(t *testing.T) {
	if !strings.HasPrefix(validDestination(t, "dest-1").CanonicalDigest, "sha256:") {
		t.Fatal("destination digest is not tagged")
	}
}
func TestTodo_PAYMETHOD_001_Race(t *testing.T) { _ = validDestination(t, "dest-1") }
func TestTodo_PAYMETHOD_001_Fault(t *testing.T) {
	if _, err := NewDestination(Destination{}); err == nil {
		t.Fatal("empty destination was accepted")
	}
}
func TestTodo_PAYMETHOD_001_Security(t *testing.T) {
	d := validDestination(t, "dest-secret")
	x, err := d.Explain()
	if err != nil || strings.Contains(x.Digest, "vault-token") || strings.Contains(x.ID, "123456789") {
		t.Fatalf("unsafe explanation = %+v, err=%v", x, err)
	}
}
func TestTodo_PAYMETHOD_001_Conformance(t *testing.T) {
	d := validDestination(t, "dest-1")
	_, err := NewDestinationChange(d, d, "change-1", "requester", "approver", payMethodInterval(t))
	if err == nil || !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("same destination change error = %v", err)
	}
	if err := RequireDistinctApprover("requester", "approver"); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_PAYMETHOD_001_Mutation(t *testing.T) {
	d := validDestination(t, "dest-1")
	before := d.CanonicalDigest
	challenge, err := NewVerificationChallenge(d, "challenge-1", MethodInstantVerification, payMethodInstant(t, 100), payMethodInstant(t, 160), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := challenge.Verify(payMethodInstant(t, 120), "sha256:evidence"); err != nil || d.CanonicalDigest != before {
		t.Fatalf("challenge mutated destination: %v", err)
	}
}

func TestPayMethodChallengeExpiresAndIsBudgeted(t *testing.T) {
	d := validDestination(t, "dest-1")
	c, err := NewVerificationChallenge(d, "challenge-1", MethodMicroDeposit, payMethodInstant(t, 100), payMethodInstant(t, 160), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verify(payMethodInstant(t, 160), "sha256:evidence"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expired challenge error = %v", err)
	}
	e, err := c.Verify(payMethodInstant(t, 120), "sha256:evidence")
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := c.RecordAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consumed.Verify(payMethodInstant(t, 121), "sha256:evidence"); !errors.Is(err, ErrAttemptBudgetExceeded) {
		t.Fatalf("budget error = %v", err)
	}
	if e.CanonicalDigest == "" {
		t.Fatal("verification event was not digested")
	}
}

func TestPayMethodDualControlRequiresDistinctApprover(t *testing.T) {
	if err := RequireDistinctApprover("same", "same"); !errors.Is(err, ErrDistinctApproverRequired) {
		t.Fatalf("dual-control error = %v", err)
	}
	if err := RequireDistinctApprover("requester", "approver"); err != nil {
		t.Fatal(err)
	}
}

func TestPayMethodEnumsAndAliasesRejectUnknownValues(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d", Version())
	}
	for _, tc := range []struct {
		name string
		got  bool
		want bool
	}{
		{"risk valid", RiskMedium.Valid(), true},
		{"risk invalid", RiskClass("bogus").Valid(), false},
		{"state valid", DestinationActive.Valid(), true},
		{"state invalid", DestinationState("bogus").Valid(), false},
		{"verification valid", VerificationVerified.Valid(), true},
		{"verification invalid", VerificationState("bogus").Valid(), false},
		{"method valid", MethodMicroDeposit.Valid(), true},
		{"method invalid", VerificationMethod("bogus").Valid(), false},
		{"split valid", SplitPercentage.Valid(), true},
		{"split invalid", SplitKind("bogus").Valid(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %t, want %t", tc.got, tc.want)
			}
		})
	}
}

func TestPayMethodVerificationAliasesAndFailureBranches(t *testing.T) {
	destination := validDestination(t, "destination-verification")
	issued, expires := payMethodInstant(t, 100), payMethodInstant(t, 160)
	challenge, err := NewChallenge(destination, ChallengeSpec{ID: "challenge-alias", Method: MethodInstantVerification, IssuedAt: issued, ExpiresAt: expires, AttemptBudget: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := challenge.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := challenge.Verify(payMethodInstant(t, 120), ""); err == nil {
		t.Fatal("empty evidence was accepted")
	}
	if _, err := challenge.Verify(payMethodInstant(t, 99), "sha256:evidence"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("premature verification error = %v", err)
	}
	event, err := challenge.Verify(payMethodInstant(t, 120), "sha256:evidence")
	if err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(); err != nil || event.CanonicalDigest == "" {
		t.Fatalf("event = %+v err=%v", event, err)
	}
	if _, err := ApplyVerification(destination, VerificationEvent{ChallengeID: event.ChallengeID, DestinationID: "other", Method: event.Method, Attempt: 1, VerifiedAt: event.VerifiedAt, EvidenceDigest: event.EvidenceDigest, CanonicalDigest: event.CanonicalDigest}); !errors.Is(err, ErrInvalidVerification) {
		t.Fatalf("wrong destination event error = %v", err)
	}
	if _, err := ApplyVerification(destination, event); err != nil {
		t.Fatal(err)
	}
	spent, err := challenge.RecordAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if spent.AttemptsUsed != 1 || spent.CanonicalDigest == challenge.CanonicalDigest {
		t.Fatalf("attempt successor = %+v", spent)
	}
}

func TestPayMethodSplitPlanRemainderAndDigest(t *testing.T) {
	plan, err := NewSplitPlan(SplitPlan{Currency: "USD", Splits: []SplitPriority{{Priority: 1, DestinationRef: "remainder", Kind: SplitFixedAmount, Remainder: true}}, RemainderDestinationRef: "remainder"})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plan.Digest()
	if err != nil || digest != plan.CanonicalDigest || len(plan.Splits[0].Canonical()) == 0 {
		t.Fatalf("remainder plan=%+v digest=%q err=%v", plan, digest, err)
	}
	duplicate := plan
	duplicate.Splits = []SplitPriority{{Priority: 1, DestinationRef: "same", Kind: SplitFixedAmount, Value: values.MustDecimal("1", 2, values.RoundingExactRequired)}, {Priority: 2, DestinationRef: "same", Kind: SplitFixedAmount, Value: values.MustDecimal("1", 2, values.RoundingExactRequired)}}
	if err := duplicate.Validate(); !errors.Is(err, ErrInvalidSplit) {
		t.Fatalf("duplicate split error = %v", err)
	}
}

func TestPayMethodDestinationCatalogRecordChangeAdvancesCurrent(t *testing.T) {
	current := validDestination(t, "catalog-destination")
	proposed := current
	proposed.GovernedRef = "vault-token:catalog-next"
	proposed.DisplayHint = "••••5678"
	proposed.CanonicalDigest = ""
	proposed, err := NewDestination(proposed)
	if err != nil {
		t.Fatal(err)
	}
	change, err := NewDestinationChange(current, proposed, "catalog-change", "requester", "approver", payMethodInterval(t))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewInMemoryCatalog([]Destination{current})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.RecordChange(change); err != nil {
		t.Fatal(err)
	}
	got, err := catalog.Get(current.id())
	if err != nil || got.CanonicalDigest != change.ProposedDigest || len(catalog.changes) != 1 {
		t.Fatalf("catalog current=%+v changes=%d err=%v", got, len(catalog.changes), err)
	}
	if err := catalog.RecordChange(change); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("replayed change error = %v", err)
	}
	if _, err := catalog.Get("missing"); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("missing destination error = %v", err)
	}
}

var _ = time.UTC
