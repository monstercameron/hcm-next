package tenant_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/tenant"
)

const requester = "system:tenant-bootstrap-orchestrator"

// planeVerified returns a well-formed Verified record for plane, verified by
// a principal distinct from requester.
func planeVerified(plane tenant.Plane, tenantSlug string, at time.Time, refs ...string) tenant.Verified {
	if len(refs) == 0 {
		refs = []string{"evidence:" + string(plane)}
	}
	return tenant.Verified{
		Plane:             plane,
		Tenant:            tenantSlug,
		VerifierPrincipal: "system:" + string(plane) + "-verifier",
		VerifiedAt:        at,
		EvidenceRefs:      refs,
	}
}

// TestProvisioningRunReachesActiveOnlyWhenEveryPlaneVerified is the primary
// evidence for TENANT-002's remaining GREEN clause: a ProvisioningRun stays
// PENDING while any plane in tenant.AllPlanes is unverified, and moves to
// ACTIVE -- in the very call that supplies the last one -- only once every
// plane holds a Verified record.
func TestProvisioningRunReachesActiveOnlyWhenEveryPlaneVerified(t *testing.T) {
	const slug = "pilot-partner"
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	run, err := tenant.NewProvisioningRun(slug)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	if run.Status() != tenant.ProvisioningPending {
		t.Fatalf("a fresh run reports status %s, want PENDING", run.Status())
	}

	for i, plane := range tenant.AllPlanes {
		outcome, err := run.RecordVerified(requester, planeVerified(plane, slug, at))
		if err != nil {
			t.Fatalf("record plane %s: %v", plane, err)
		}
		if outcome.Decision != tenant.ProvisioningApply {
			t.Fatalf("plane %s decision %s, want APPLY", plane, outcome.Decision)
		}
		if outcome.Event.Digest() == "" {
			t.Fatalf("plane %s APPLY carries no event digest", plane)
		}

		isLast := i == len(tenant.AllPlanes)-1
		if outcome.Activated != isLast {
			t.Fatalf("plane %s (index %d of %d) Activated=%v, want %v", plane, i, len(tenant.AllPlanes), outcome.Activated, isLast)
		}
		if isLast {
			if outcome.ActivationEvent.Digest() == "" {
				t.Fatal("the completing plane's outcome carries no activation event digest")
			}
			if run.Status() != tenant.ProvisioningActive {
				t.Fatalf("status after every plane verified is %s, want ACTIVE", run.Status())
			}
		} else if run.Status() != tenant.ProvisioningPending {
			t.Fatalf("status after %d of %d planes verified is %s, want still PENDING", i+1, len(tenant.AllPlanes), run.Status())
		}
	}

	if got := run.VerifiedPlanes(); len(got) != len(tenant.AllPlanes) {
		t.Fatalf("VerifiedPlanes returned %d planes, want %d", len(got), len(tenant.AllPlanes))
	}
}

// TestProvisioningRunReplayIsIdempotent proves resumability: presenting the
// exact same evidence for a plane already verified -- including after the
// run has already reached ACTIVE -- is a NOOP that appends no new event and
// changes nothing observable.
func TestProvisioningRunReplayIsIdempotent(t *testing.T) {
	const slug = "pilot-partner"
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	run, err := tenant.NewProvisioningRun(slug)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	for _, plane := range tenant.AllPlanes {
		if _, err := run.RecordVerified(requester, planeVerified(plane, slug, at)); err != nil {
			t.Fatalf("record plane %s: %v", plane, err)
		}
	}
	if run.Status() != tenant.ProvisioningActive {
		t.Fatalf("status %s, want ACTIVE before replay", run.Status())
	}
	eventsBefore := run.Events()

	// Replay the identical evidence for one plane, at a different wall-clock
	// instant -- VerifiedAt is deliberately excluded from the idempotency
	// check (see Verified.sameEvidence's doc comment), so this must still
	// be a NOOP.
	replay := planeVerified(tenant.PlaneIdentity, slug, at.Add(time.Hour))
	outcome, err := run.RecordVerified(requester, replay)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if outcome.Decision != tenant.ProvisioningNoop {
		t.Fatalf("replay decision %s, want NOOP", outcome.Decision)
	}
	if outcome.Event.Digest() != "" {
		t.Fatal("a NOOP replay appended an event")
	}
	if run.Status() != tenant.ProvisioningActive {
		t.Fatalf("status after replay is %s, want still ACTIVE", run.Status())
	}
	if got := run.Events(); len(got) != len(eventsBefore) {
		t.Fatalf("%d events after a NOOP replay, want %d (unchanged)", len(got), len(eventsBefore))
	}
	stillVerified, ok := run.Verified(tenant.PlaneIdentity)
	if !ok || !stillVerified.VerifiedAt.Equal(at) {
		t.Fatalf("plane identity's VerifiedAt moved to %v after a NOOP replay, want unchanged at %v", stillVerified.VerifiedAt, at)
	}
}

// TestProvisioningRunRejectsChangedEvidenceForAVerifiedPlane proves that a
// plane already VERIFIED cannot have its evidence silently swapped: only a
// RecordFailure followed by fresh evidence (the repair path) may change
// what a verified plane means.
func TestProvisioningRunRejectsChangedEvidenceForAVerifiedPlane(t *testing.T) {
	const slug = "pilot-partner"
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	run, err := tenant.NewProvisioningRun(slug)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	if _, err := run.RecordVerified(requester, planeVerified(tenant.PlaneKeys, slug, at)); err != nil {
		t.Fatalf("first verification: %v", err)
	}

	changed := planeVerified(tenant.PlaneKeys, slug, at, "a-different-evidence-ref")
	_, err = run.RecordVerified(requester, changed)
	if err == nil {
		t.Fatal("different evidence for an already-verified plane was accepted")
	}
	if !errors.Is(err, tenant.ErrProvisioningRejected) {
		t.Fatalf("error %v, want ErrProvisioningRejected", err)
	}
}

// TestProvisioningRunDegradesOnLaterPlaneFailure is the GREEN clause for a
// plane regressing after activation: RecordFailure names the exact plane,
// moves status to DEGRADED, and the plane no longer reads as VERIFIED until
// it is repaired by a fresh RecordVerified call, which clears the
// degradation.
func TestProvisioningRunDegradesOnLaterPlaneFailure(t *testing.T) {
	const slug = "pilot-partner"
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	run, err := tenant.NewProvisioningRun(slug)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	for _, plane := range tenant.AllPlanes {
		if _, err := run.RecordVerified(requester, planeVerified(plane, slug, at)); err != nil {
			t.Fatalf("record plane %s: %v", plane, err)
		}
	}
	if run.Status() != tenant.ProvisioningActive {
		t.Fatalf("status %s, want ACTIVE before the failure", run.Status())
	}

	failAt := at.Add(24 * time.Hour)
	ev, err := run.RecordFailure(tenant.PlaneHealth, "governed read timed out", failAt)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if ev.Digest() == "" {
		t.Fatal("the degradation event carries no digest")
	}
	if run.Status() != tenant.ProvisioningDegraded {
		t.Fatalf("status after a plane failure is %s, want DEGRADED", run.Status())
	}
	plane, reason, ok := run.DegradedPlane()
	if !ok || plane != tenant.PlaneHealth || reason == "" {
		t.Fatalf("DegradedPlane returned (%s, %q, %v), want (HEALTH, non-empty, true)", plane, reason, ok)
	}
	if _, ok := run.Verified(tenant.PlaneHealth); ok {
		t.Fatal("the failed plane still reads as Verified")
	}

	// Repair: a fresh verification for exactly the degraded plane clears
	// the degradation and returns the run to ACTIVE (every other plane's
	// evidence is untouched).
	repairAt := failAt.Add(time.Hour)
	outcome, err := run.RecordVerified(requester, planeVerified(tenant.PlaneHealth, slug, repairAt))
	if err != nil {
		t.Fatalf("repair verification: %v", err)
	}
	if outcome.Decision != tenant.ProvisioningApply {
		t.Fatalf("repair decision %s, want APPLY", outcome.Decision)
	}
	if run.Status() != tenant.ProvisioningActive {
		t.Fatalf("status after repair is %s, want ACTIVE again", run.Status())
	}
	if _, _, ok := run.DegradedPlane(); ok {
		t.Fatal("DegradedPlane still reports a degradation after repair")
	}
}

// TestVerifiedValidateRequiresDistinctVerifier proves GREEN's "a verifier
// principal distinct from the requester": a plane cannot be self-attested
// by the same principal that asked for it.
func TestVerifiedValidateRequiresDistinctVerifier(t *testing.T) {
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	v := tenant.Verified{
		Plane:             tenant.PlaneAudit,
		Tenant:            "pilot-partner",
		VerifierPrincipal: requester,
		VerifiedAt:        at,
		EvidenceRefs:      []string{"evidence:audit"},
	}
	if err := v.Validate(requester); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("error %v, want ErrInvalidVerified for a self-attested plane", err)
	}
}

// TestVerifiedValidateRequiresEvidence proves GREEN's "backed by evidence
// refs": a plane cannot be VERIFIED with no evidence at all.
func TestVerifiedValidateRequiresEvidence(t *testing.T) {
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	v := tenant.Verified{
		Plane:             tenant.PlaneAudit,
		Tenant:            "pilot-partner",
		VerifierPrincipal: "system:audit-verifier",
		VerifiedAt:        at,
	}
	if err := v.Validate(requester); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("error %v, want ErrInvalidVerified for no evidence", err)
	}
}

// TestProvisioningRunRejectsAnotherTenantsEvidence proves a run for one
// tenant refuses evidence naming a different tenant outright -- the
// provisioning-model half of "a second tenant's bootstrap never sees the
// first's receipts".
func TestProvisioningRunRejectsAnotherTenantsEvidence(t *testing.T) {
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	run, err := tenant.NewProvisioningRun("pilot-partner")
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	_, err = run.RecordVerified(requester, planeVerified(tenant.PlaneIdentity, "someone-elses-tenant", at))
	if err == nil {
		t.Fatal("evidence naming a different tenant was accepted")
	}
	if !errors.Is(err, tenant.ErrProvisioningRejected) {
		t.Fatalf("error %v, want ErrProvisioningRejected", err)
	}
}

// TestFakeVerifierRecordsRequestsAndCanFail proves the in-memory
// PlaneVerifier fixture other lanes and this package's own tests rely on:
// it reports VERIFIED by default, records every request, and can be made to
// fail on demand.
func TestFakeVerifierRecordsRequestsAndCanFail(t *testing.T) {
	fake := tenant.NewFakeVerifier(tenant.PlaneProducts, "system:products-verifier", "catalog:v3")
	if fake.Plane() != tenant.PlaneProducts {
		t.Fatalf("Plane() = %s, want PRODUCTS", fake.Plane())
	}

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	v, err := fake.Verify(context.Background(), tenant.VerificationRequest{Tenant: "pilot-partner", RequesterPrincipal: requester, Now: now})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if v.VerifierPrincipal != "system:products-verifier" || len(v.EvidenceRefs) != 1 || v.EvidenceRefs[0] != "catalog:v3" {
		t.Fatalf("unexpected verified record: %+v", v)
	}
	if calls := fake.Calls(); len(calls) != 1 || calls[0].Tenant != "pilot-partner" {
		t.Fatalf("Calls() = %+v, want one call for pilot-partner", calls)
	}

	failure := errors.New("catalog unreachable")
	fake.FailNext(failure)
	if _, err := fake.Verify(context.Background(), tenant.VerificationRequest{Tenant: "pilot-partner", Now: now}); !errors.Is(err, failure) {
		t.Fatalf("error %v, want %v", err, failure)
	}
	if calls := fake.Calls(); len(calls) != 2 {
		t.Fatalf("%d calls recorded, want 2 (including the failing one)", len(calls))
	}
}

// TestVerifyPlaneComposesVerifyAndRecord proves the VerifyPlane helper wires
// a PlaneVerifier's own Verify straight into RecordVerified, and that a
// verifier's own error is returned as-is rather than recorded.
func TestVerifyPlaneComposesVerifyAndRecord(t *testing.T) {
	run, err := tenant.NewProvisioningRun("pilot-partner")
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fake := tenant.NewFakeVerifier(tenant.PlaneAdmin, "system:admin-verifier", "admin:seeded")

	outcome, err := tenant.VerifyPlane(context.Background(), run, fake, requester, now)
	if err != nil {
		t.Fatalf("VerifyPlane: %v", err)
	}
	if outcome.Decision != tenant.ProvisioningApply {
		t.Fatalf("decision %s, want APPLY", outcome.Decision)
	}
	if _, ok := run.Verified(tenant.PlaneAdmin); !ok {
		t.Fatal("VerifyPlane did not record the plane as verified")
	}

	failure := errors.New("admin directory unreachable")
	fake.FailNext(failure)
	if _, err := tenant.VerifyPlane(context.Background(), run, fake, requester, now); !errors.Is(err, failure) {
		t.Fatalf("error %v, want %v surfaced unwrapped through VerifyPlane", err, failure)
	}
}

func TestPlaneAndVerified_ValidateRejectsBoundaryInputs(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := tenant.Verified{
		Plane:             tenant.PlaneAudit,
		Tenant:            "pilot-partner",
		VerifierPrincipal: "system:audit-verifier",
		VerifiedAt:        at,
		EvidenceRefs:      []string{"evidence:audit"},
	}
	if !tenant.PlaneAudit.Valid() || tenant.Plane("UNKNOWN").Valid() {
		t.Fatal("Plane.Valid did not enforce the closed set")
	}
	tests := []struct {
		name      string
		mutate    func(*tenant.Verified)
		requester string
	}{
		{"unknown plane", func(v *tenant.Verified) { v.Plane = "UNKNOWN" }, requester},
		{"missing tenant", func(v *tenant.Verified) { v.Tenant = "" }, requester},
		{"missing verifier", func(v *tenant.Verified) { v.VerifierPrincipal = "" }, requester},
		{"missing requester", func(*tenant.Verified) {}, ""},
		{"self attestation case insensitive", func(v *tenant.Verified) { v.VerifierPrincipal = "SYSTEM:REQUESTER" }, "system:requester"},
		{"missing time", func(v *tenant.Verified) { v.VerifiedAt = time.Time{} }, requester},
		{"missing evidence", func(v *tenant.Verified) { v.EvidenceRefs = nil }, requester},
		{"blank evidence", func(v *tenant.Verified) { v.EvidenceRefs = []string{"  "} }, requester},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := base
			tt.mutate(&v)
			if err := v.Validate(tt.requester); !errors.Is(err, tenant.ErrInvalidVerified) {
				t.Fatalf("Validate error = %v, want ErrInvalidVerified", err)
			}
		})
	}
}

func TestProvisioningRun_ConstructorsAccessorsAndCopies(t *testing.T) {
	if _, err := tenant.NewProvisioningRun(" "); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("blank tenant error = %v, want ErrInvalidVerified", err)
	}
	run, err := tenant.NewProvisioningRun("accessors")
	if err != nil {
		t.Fatal(err)
	}
	if run.Tenant() != "accessors" || run.Status() != tenant.ProvisioningPending {
		t.Fatalf("run identity/status = %q/%q", run.Tenant(), run.Status())
	}
	if plane, reason, ok := run.DegradedPlane(); ok || plane != "" || reason != "" {
		t.Fatalf("fresh DegradedPlane = %q/%q/%v", plane, reason, ok)
	}
	if verified, ok := run.Verified(tenant.PlaneAudit); ok || verified.Plane != "" || verified.Tenant != "" || len(verified.EvidenceRefs) != 0 {
		t.Fatalf("unverified plane = %#v/%v", verified, ok)
	}
	if got := run.VerifiedPlanes(); len(got) != 0 {
		t.Fatalf("fresh VerifiedPlanes = %v", got)
	}
	if got := run.Events(); got != nil {
		t.Fatalf("fresh Events = %#v, want nil", got)
	}

	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if _, err := run.RecordVerified(requester, planeVerified(tenant.PlaneAudit, "accessors", at)); err != nil {
		t.Fatal(err)
	}
	ordered := run.VerifiedPlanes()
	if len(ordered) != 1 || ordered[0] != tenant.PlaneAudit {
		t.Fatalf("VerifiedPlanes = %v, want AUDIT", ordered)
	}
	events := run.Events()
	if len(events) != 1 {
		t.Fatalf("Events = %d, want one event", len(events))
	}
	events[0].Kind = "tampered"
	if run.Events()[0].Kind == "tampered" {
		t.Fatal("Events returned an aliased event")
	}
}

func TestVerifyPlane_RejectsNilRunAndVerifier(t *testing.T) {
	if _, err := tenant.VerifyPlane(context.Background(), nil, tenant.NewFakeVerifier(tenant.PlaneAudit, "verifier", "evidence"), requester, lifecycleAt); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("nil run error = %v, want ErrInvalidVerified", err)
	}
	run, err := tenant.NewProvisioningRun("nil-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.VerifyPlane(context.Background(), run, nil, requester, lifecycleAt); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("nil verifier error = %v, want ErrInvalidVerified", err)
	}
	if run.Status() != tenant.ProvisioningPending || len(run.Events()) != 0 {
		t.Fatalf("nil input changed run: status=%s events=%d", run.Status(), len(run.Events()))
	}
}

func TestProvisioningRun_RecordFailureBoundaryAndRepairBeforeActivation(t *testing.T) {
	run, err := tenant.NewProvisioningRun("repair")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		plane  tenant.Plane
		reason string
		at     time.Time
	}{
		{"unknown plane", "UNKNOWN", "bad", at},
		{"missing reason", tenant.PlaneAudit, " ", at},
		{"missing time", tenant.PlaneAudit, "bad", time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := run.RecordFailure(tt.plane, tt.reason, tt.at); !errors.Is(err, tenant.ErrInvalidVerified) {
				t.Fatalf("RecordFailure error = %v, want ErrInvalidVerified", err)
			}
			if run.Status() != tenant.ProvisioningPending || len(run.Events()) != 0 {
				t.Fatalf("invalid failure changed run: status=%s events=%d", run.Status(), len(run.Events()))
			}
		})
	}
	if _, err := run.RecordFailure(tenant.PlaneAudit, "audit unavailable", at); err != nil {
		t.Fatal(err)
	}
	if run.Status() != tenant.ProvisioningDegraded {
		t.Fatalf("status after failure = %s, want DEGRADED", run.Status())
	}
	if plane, reason, ok := run.DegradedPlane(); !ok || plane != tenant.PlaneAudit || reason != "audit unavailable" {
		t.Fatalf("degraded evidence = %s/%q/%v", plane, reason, ok)
	}
	outcome, err := run.RecordVerified(requester, planeVerified(tenant.PlaneAudit, "repair", at.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Decision != tenant.ProvisioningApply || run.Status() != tenant.ProvisioningPending {
		t.Fatalf("repair outcome/status = %#v/%s, want APPLY/PENDING", outcome, run.Status())
	}
	if _, _, ok := run.DegradedPlane(); ok {
		t.Fatal("repair left a degraded plane")
	}
}

func TestFakeVerifier_RecordsCopiesAndClearsFailure(t *testing.T) {
	refs := []string{"evidence:original"}
	fake := tenant.NewFakeVerifier(tenant.PlaneProducts, "system:products", refs...)
	refs[0] = "evidence:caller-mutated"
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if _, err := fake.Verify(context.Background(), tenant.VerificationRequest{RequesterPrincipal: requester, Now: now}); !errors.Is(err, tenant.ErrInvalidVerified) {
		t.Fatalf("blank request tenant error = %v, want ErrInvalidVerified", err)
	}
	failure := errors.New("temporary outage")
	fake.FailNext(failure)
	if _, err := fake.Verify(context.Background(), tenant.VerificationRequest{Tenant: "pilot", RequesterPrincipal: requester, Now: now}); !errors.Is(err, failure) {
		t.Fatalf("configured failure = %v, want %v", err, failure)
	}
	fake.FailNext(nil)
	verified, err := fake.Verify(context.Background(), tenant.VerificationRequest{Tenant: "pilot", RequesterPrincipal: requester, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Plane != tenant.PlaneProducts || verified.VerifierPrincipal != "system:products" || !verified.VerifiedAt.Equal(now) || len(verified.EvidenceRefs) != 1 || verified.EvidenceRefs[0] != "evidence:original" {
		t.Fatalf("verified record = %#v", verified)
	}
	verified.EvidenceRefs[0] = "tampered"
	verifiedAgain, err := fake.Verify(context.Background(), tenant.VerificationRequest{Tenant: "pilot", RequesterPrincipal: requester, Now: now})
	if err != nil || verifiedAgain.EvidenceRefs[0] != "evidence:original" {
		t.Fatalf("fake evidence was aliased: record=%#v err=%v", verifiedAgain, err)
	}
	calls := fake.Calls()
	if len(calls) != 4 || calls[1].Tenant != "pilot" || !calls[2].Now.Equal(now) {
		t.Fatalf("Calls = %#v, want blank, failed, and two successful requests", calls)
	}
	calls[0].Tenant = "tampered"
	if fake.Calls()[0].Tenant == "tampered" {
		t.Fatal("Calls returned an aliased request slice")
	}
}
