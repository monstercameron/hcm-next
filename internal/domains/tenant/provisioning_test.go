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
