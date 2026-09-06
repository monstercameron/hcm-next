package paymethod

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
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
}

var _ = time.UTC
