package breakglass_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/breakglass"
)

func TestOpen_RejectsMalformedRequestAndApprovalWithSentinels(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*breakglass.Request)
		want   error
	}{
		{"blank id", func(_ *breakglass.Request) {}, breakglass.ErrInvalidRequest},
		{"blank user", func(r *breakglass.Request) { r.User = "" }, breakglass.ErrInvalidRequest},
		{"padded incident", func(r *breakglass.Request) { r.IncidentRef = " INC-42" }, breakglass.ErrInvalidRequest},
		{"blank justification", func(r *breakglass.Request) { r.Justification = " " }, breakglass.ErrInvalidRequest},
		{"no capabilities", func(r *breakglass.Request) { r.Capabilities = nil }, breakglass.ErrInvalidRequest},
		{"blank capability", func(r *breakglass.Request) { r.Capabilities = []string{" "} }, breakglass.ErrInvalidRequest},
		{"padded capability", func(r *breakglass.Request) { r.Capabilities = []string{" cap"} }, breakglass.ErrInvalidRequest},
		{"duplicate capability", func(r *breakglass.Request) { r.Capabilities = []string{"read", "read"} }, breakglass.ErrInvalidRequest},
		{"zero ttl", func(r *breakglass.Request) { r.TTL = 0 }, breakglass.ErrTTLRequired},
		{"negative ttl", func(r *breakglass.Request) { r.TTL = -time.Second }, breakglass.ErrTTLRequired},
		{"hard ttl breach", func(r *breakglass.Request) { r.TTL = breakglass.HardTTL + time.Nanosecond }, breakglass.ErrTTLExceedsHardLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := breakglassRequest()
			id := "bg-1"
			if tc.name == "blank id" {
				id = " "
			}
			tc.mutate(&req)
			if _, err := breakglass.Open(id, req, breakglassApproval(), breakglassNow); !errors.Is(err, tc.want) {
				t.Fatalf("Open = %v, want %v", err, tc.want)
			}
		})
	}
	badApproval := breakglassApproval()
	badApproval.Approver, badApproval.At = " ", time.Time{}
	if _, err := breakglass.Open("bg-1", breakglassRequest(), badApproval, breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("malformed approval = %v", err)
	}
	self := breakglassApproval()
	self.Approver = "OPERATOR-1"
	if _, err := breakglass.Open("bg-1", breakglassRequest(), self, breakglassNow); !errors.Is(err, breakglass.ErrApproverIsUser) {
		t.Fatalf("self approval = %v", err)
	}
	if _, err := breakglass.Open(" bg-1", breakglassRequest(), breakglassApproval(), breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("padded id = %v", err)
	}
}

func TestGrant_LifecycleErrorsCopiesAndNilReceivers(t *testing.T) {
	var nilGrant *breakglass.Grant
	if nilGrant.IsActive(breakglassNow) || nilGrant.ReviewRequired() {
		t.Fatal("nil grant reported active or review-required")
	}
	if err := nilGrant.Use("cap", "action", breakglassNow); !errors.Is(err, breakglass.ErrGrantContained) {
		t.Fatalf("nil Use = %v", err)
	}
	if err := nilGrant.Contain("actor", breakglassNow); !errors.Is(err, breakglass.ErrGrantContained) {
		t.Fatalf("nil Contain = %v", err)
	}
	if _, ok := nilGrant.Containment(); ok {
		t.Fatal("nil Containment returned a record")
	}
	if err := nilGrant.PostUseReview("reviewer", breakglass.ReviewJustified, "reason", breakglassNow); !errors.Is(err, breakglass.ErrNoUseToReview) {
		t.Fatalf("nil review = %v", err)
	}
	if _, ok := nilGrant.Review(); ok || nilGrant.Evidence() != nil {
		t.Fatal("nil accessors returned state")
	}

	caps := []string{"z-cap", "a-cap"}
	req := breakglassRequest()
	req.Capabilities = caps
	grant, err := breakglass.Open("bg-copy", req, breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatal(err)
	}
	if caps[0] != "z-cap" || grant.Capabilities[0] != "a-cap" {
		t.Fatalf("capability ordering/copy failed: input=%v grant=%v", caps, grant.Capabilities)
	}
	if err := grant.Use("a-cap", "read", breakglassNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := grant.Use("missing", "read", breakglassNow.Add(2*time.Minute)); !errors.Is(err, breakglass.ErrCapabilityNotGranted) {
		t.Fatalf("unknown capability = %v", err)
	}
	if err := grant.Use("a-cap", " ", breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("blank action = %v", err)
	}
	if err := grant.Contain("", breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("blank containment actor = %v", err)
	}
	if err := grant.PostUseReview("reviewer", breakglass.ReviewOutcome("UNKNOWN"), "reason", breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("unknown review outcome = %v", err)
	}
	if err := grant.PostUseReview("reviewer", breakglass.ReviewJustified, " ", breakglassNow); !errors.Is(err, breakglass.ErrInvalidRequest) {
		t.Fatalf("blank review justification = %v", err)
	}
	if err := grant.PostUseReview("reviewer", breakglass.ReviewJustified, "reason", breakglassNow); err != nil {
		t.Fatal(err)
	}
	review, ok := grant.Review()
	if !ok || review.Reviewer != "reviewer" {
		t.Fatalf("review = %+v, ok=%v", review, ok)
	}
	if err := grant.PostUseReview("reviewer-2", breakglass.ReviewViolation, "second", breakglassNow); !errors.Is(err, breakglass.ErrReviewAlreadyRecorded) {
		t.Fatalf("duplicate review = %v", err)
	}
	containment, ok := grant.Containment()
	if !ok {
		if err := grant.Contain("security", breakglassNow); err != nil {
			t.Fatal(err)
		}
		containment, ok = grant.Containment()
	}
	if !ok {
		t.Fatal("containment missing")
	}
	containment.RevokedCapabilities[0] = "tampered"
	again, _ := grant.Containment()
	if again.RevokedCapabilities[0] == "tampered" {
		t.Fatal("Containment exposed internal slice")
	}
	evidence := grant.Evidence()
	if len(evidence) < 3 {
		t.Fatalf("evidence = %+v", evidence)
	}
	if len(evidence[0].RevokedCaps) > 0 {
		evidence[0].RevokedCaps[0] = "tampered"
	}
	if got := grant.Evidence(); len(got[0].RevokedCaps) > 0 && got[0].RevokedCaps[0] == "tampered" {
		t.Fatal("Evidence exposed internal slice")
	}
}

func TestGrant_ContainmentAndExpiryReturnDistinctDenials(t *testing.T) {
	grant, err := breakglass.Open("bg-expiry", breakglassRequest(), breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := grant.Contain("security", breakglassNow); err != nil {
		t.Fatal(err)
	}
	if err := grant.Use("payroll.read", "late", breakglassNow.Add(time.Minute)); !errors.Is(err, breakglass.ErrGrantContained) {
		t.Fatalf("use after containment = %v", err)
	}
	expired, err := breakglass.Open("bg-expired", breakglassRequest(), breakglassApproval(), breakglassNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := expired.Use("payroll.read", "late", breakglassNow.Add(breakglassRequest().TTL+time.Nanosecond)); !errors.Is(err, breakglass.ErrGrantExpired) {
		t.Fatalf("use after expiry = %v", err)
	}
	if _, ok := expired.Containment(); !ok {
		t.Fatal("expiry did not automatically contain grant")
	}
}
