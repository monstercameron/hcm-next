package breakglass_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/breakglass"
)

var breakglassNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func breakglassRequest() breakglass.Request {
	return breakglass.Request{
		User: "operator-1", IncidentRef: "INC-42", Justification: "restore a blocked payroll run",
		Capabilities: []string{"payroll.read", "payroll.replay"}, TTL: 30 * time.Minute,
	}
}

func breakglassApproval() breakglass.Approval {
	return breakglass.Approval{Approver: "approver-1", At: breakglassNow}
}

// TestTodo_TRUST_022 is the primary contract: dual control and a finite TTL
// open a narrow grant, use is evidenced, expiry automatically revokes and
// lists every capability, and a distinct reviewer must record the outcome.
func TestTodo_TRUST_022(t *testing.T) {
	grant, err := breakglass.Open("bg-1", breakglassRequest(), breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !grant.IsActive(breakglassNow.Add(time.Minute)) {
		t.Fatal("fresh grant is not active")
	}
	if err := grant.Use("payroll.read", "inspect blocked run", breakglassNow.Add(time.Minute)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if !grant.ReviewRequired() {
		t.Fatal("used grant does not require review")
	}

	if grant.IsActive(breakglassNow.Add(31 * time.Minute)) {
		t.Fatal("expired grant remained active")
	}
	containment, ok := grant.Containment()
	if !ok || containment.Actor != "system" || len(containment.RevokedCapabilities) != 2 || containment.RevokedCapabilities[0] != "payroll.read" || containment.RevokedCapabilities[1] != "payroll.replay" {
		t.Fatalf("containment = %+v, ok=%v, want automatic complete revocation", containment, ok)
	}
	if err := grant.Use("payroll.replay", "late replay", breakglassNow.Add(31*time.Minute)); !errors.Is(err, breakglass.ErrGrantExpired) {
		t.Fatalf("late use = %v, want ErrGrantExpired", err)
	}
	if err := grant.PostUseReview("reviewer-1", breakglass.ReviewJustified, "scope and action matched the incident", breakglassNow.Add(time.Hour)); err != nil {
		t.Fatalf("PostUseReview: %v", err)
	}
	review, ok := grant.Review()
	if !ok || review.Reviewer != "reviewer-1" || review.Outcome != breakglass.ReviewJustified {
		t.Fatalf("review = %+v, ok=%v", review, ok)
	}
	kinds := map[breakglass.EvidenceKind]bool{}
	for _, evidence := range grant.Evidence() {
		kinds[evidence.Kind] = true
	}
	for _, want := range []breakglass.EvidenceKind{breakglass.EvidenceOpened, breakglass.EvidenceUsed, breakglass.EvidenceContained, breakglass.EvidenceReviewed} {
		if !kinds[want] {
			t.Fatalf("evidence missing %s: %+v", want, grant.Evidence())
		}
	}
}

// FuzzTodo_TRUST_022 verifies malformed request values never panic and never
// produce a standing grant.
func FuzzTodo_TRUST_022(f *testing.F) {
	f.Add("operator-1", "INC-1", "justification", int64(30*time.Minute))
	f.Add("", "", "", int64(0))
	f.Fuzz(func(t *testing.T, user, incident, justification string, ttlNanos int64) {
		req := breakglass.Request{User: user, IncidentRef: incident, Justification: justification, Capabilities: []string{"cap.read"}, TTL: time.Duration(ttlNanos)}
		grant, err := breakglass.Open("fuzz", req, breakglass.Approval{Approver: "approver", At: breakglassNow}, breakglassNow)
		if err == nil {
			if grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(grant.OpenedAt) || grant.ExpiresAt.Sub(grant.OpenedAt) > breakglass.HardTTL {
				t.Fatalf("grant has invalid bounded expiry: %+v", grant)
			}
		}
	})
}

// TestTodo_TRUST_022_Security covers declared incident, dual control,
// capability narrowing, bounded TTL, and mandatory reviewer separation.
func TestTodo_TRUST_022_Security(t *testing.T) {
	missingIncident := breakglassRequest()
	missingIncident.IncidentRef = ""
	if _, err := breakglass.Open("x", missingIncident, breakglassApproval(), breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("missing incident = %v, want ErrInvalidRequest", err)
	}
	self := breakglassApproval()
	self.Approver = "OPERATOR-1"
	if _, err := breakglass.Open("x", breakglassRequest(), self, breakglassNow); !errors.Is(err, breakglass.ErrApproverIsUser) {
		t.Fatalf("self approval = %v, want ErrApproverIsUser", err)
	}
	standing := breakglassRequest()
	standing.TTL = 0
	if _, err := breakglass.Open("x", standing, breakglassApproval(), breakglassNow); !errors.Is(err, breakglass.ErrTTLRequired) {
		t.Fatalf("zero ttl = %v, want ErrTTLRequired", err)
	}
	tooLong := breakglassRequest()
	tooLong.TTL = breakglass.HardTTL + time.Nanosecond
	if _, err := breakglass.Open("x", tooLong, breakglassApproval(), breakglassNow); !errors.Is(err, breakglass.ErrTTLExceedsHardLimit) {
		t.Fatalf("long ttl = %v, want ErrTTLExceedsHardLimit", err)
	}
	wildcard := breakglassRequest()
	wildcard.Capabilities = []string{"*"}
	if _, err := breakglass.Open("x", wildcard, breakglassApproval(), breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("wildcard capability = %v, want ErrInvalidRequest", err)
	}

	grant, err := breakglass.Open("x", breakglassRequest(), breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := grant.PostUseReview("reviewer-1", breakglass.ReviewJustified, "no use", breakglassNow); !errors.Is(err, breakglass.ErrNoUseToReview) {
		t.Fatalf("review before use = %v, want ErrNoUseToReview", err)
	}
	if err := grant.Use("payroll.read", "read", breakglassNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := grant.PostUseReview("operator-1", breakglass.ReviewJustified, "self review", breakglassNow.Add(2*time.Minute)); !errors.Is(err, breakglass.ErrReviewerIsUser) {
		t.Fatalf("self review = %v, want ErrReviewerIsUser", err)
	}
	if err := grant.PostUseReview("reviewer-1", breakglass.ReviewJustified, "reviewed", breakglassNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := grant.PostUseReview("reviewer-2", breakglass.ReviewViolation, "duplicate", breakglassNow.Add(3*time.Minute)); !errors.Is(err, breakglass.ErrReviewAlreadyRecorded) {
		t.Fatalf("duplicate review = %v, want ErrReviewAlreadyRecorded", err)
	}
}

// TestTodo_TRUST_022_Mutation checks the exact TTL boundary and idempotent
// expiry containment.
func TestTodo_TRUST_022_Mutation(t *testing.T) {
	atLimit := breakglassRequest()
	atLimit.TTL = breakglass.HardTTL
	grant, err := breakglass.Open("at-limit", atLimit, breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatalf("ttl at limit = %v, want success", err)
	}
	if err := grant.Use("payroll.read", "read", breakglassNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := grant.Contain("security", breakglassNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := grant.Contain("security-again", breakglassNow.Add(3*time.Minute)); err != nil {
		t.Fatalf("idempotent Contain = %v", err)
	}
	containment, _ := grant.Containment()
	if containment.Actor != "security" {
		t.Fatalf("containment overwritten: %+v", containment)
	}
	if err := grant.Use("missing", "action", breakglassNow.Add(4*time.Minute)); !errors.Is(err, breakglass.ErrGrantContained) {
		t.Fatalf("use after explicit containment = %v, want ErrGrantContained", err)
	}
}
