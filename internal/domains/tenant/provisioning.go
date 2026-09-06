package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Plane names one independently verifiable dimension of a tenant's
// bootstrap (TENANT-002 GREEN: "identity, admin, keys, placement, products,
// policies, schemas, recovery contacts, audit and health checks all return
// VERIFIED"). A [ProvisioningRun] may only reach [ProvisioningActive] once
// every [Plane] in [AllPlanes] carries a [Verified] record.
type Plane string

// The closed set of planes TENANT-002 GREEN names. There is no eleventh
// plane and no caller may invent one: [ProvisioningRun.RecordVerified] and
// [ProvisioningRun.RecordFailure] both refuse a [Plane] outside this set.
const (
	PlaneIdentity         Plane = "IDENTITY"
	PlaneAdmin            Plane = "ADMIN"
	PlaneKeys             Plane = "KEYS"
	PlanePlacement        Plane = "PLACEMENT"
	PlaneProducts         Plane = "PRODUCTS"
	PlanePolicies         Plane = "POLICIES"
	PlaneSchemas          Plane = "SCHEMAS"
	PlaneRecoveryContacts Plane = "RECOVERY_CONTACTS"
	PlaneAudit            Plane = "AUDIT"
	PlaneHealth           Plane = "HEALTH"
)

// AllPlanes is the closed, ordered set of planes TENANT-002 GREEN requires
// before activation. The order is evidence-presentation order only --
// [ProvisioningRun] accepts verifications in any order and activates the
// instant the set is complete, never once a particular sequence finishes.
var AllPlanes = []Plane{
	PlaneIdentity, PlaneAdmin, PlaneKeys, PlanePlacement, PlaneProducts,
	PlanePolicies, PlaneSchemas, PlaneRecoveryContacts, PlaneAudit, PlaneHealth,
}

// Valid reports whether p is one of the closed set of planes this package
// knows about.
func (p Plane) Valid() bool {
	for _, known := range AllPlanes {
		if p == known {
			return true
		}
	}
	return false
}

var (
	// ErrInvalidVerified reports that a [Verified] record is missing a
	// required field or otherwise cannot be recorded at all. Always wrapped
	// with the offending detail; callers match it with errors.Is.
	ErrInvalidVerified = errors.New("tenant: invalid plane verification")
	// ErrProvisioningRejected reports that a [Verified] record conflicts
	// with a [ProvisioningRun]'s current state and must not be recorded:
	// a different tenant than the run names, or different evidence for a
	// plane that is already verified and not currently degraded. Always
	// wrapped with the offending detail; callers match it with errors.Is.
	ErrProvisioningRejected = errors.New("TENANT_PROVISIONING_REJECTED")
)

// Verified is the evidence one [PlaneVerifier] produced for one [Plane] of
// one tenant's provisioning. It is deliberately not a bare boolean: GREEN
// requires the check to be independently attributable (who verified it,
// distinct from whoever requested the verification) and backed by evidence
// a later audit can follow, not a flag a caller could set unilaterally.
type Verified struct {
	Plane  Plane
	Tenant string
	// VerifierPrincipal identifies who or what performed the verification.
	// It must differ from the requester passed to
	// [ProvisioningRun.RecordVerified]: a plane is never self-attested by
	// the same principal that is asking for the tenant to advance.
	VerifierPrincipal string
	// VerifiedAt is the instant the verification evidence was produced,
	// supplied by the caller rather than read from the wall clock so
	// recorded evidence is a pure function of its inputs.
	VerifiedAt time.Time
	// EvidenceRefs names what was actually checked: a receipt id, a
	// digest, a table name, a resolved configuration object's ref -- opaque
	// to this package, but never empty. A plane cannot be VERIFIED on
	// nothing.
	EvidenceRefs []string
}

// Validate reports the first reason v cannot be recorded, given requester --
// the principal asking for this verification to be applied (typically the
// bootstrap orchestrator), which must differ from v.VerifierPrincipal.
func (v Verified) Validate(requester string) error {
	if !v.Plane.Valid() {
		return fmt.Errorf("%w: %q is not a known provisioning plane", ErrInvalidVerified, v.Plane)
	}
	if strings.TrimSpace(v.Tenant) == "" {
		return fmt.Errorf("%w: tenant is required", ErrInvalidVerified)
	}
	if strings.TrimSpace(v.VerifierPrincipal) == "" {
		return fmt.Errorf("%w: verifier principal is required", ErrInvalidVerified)
	}
	if strings.TrimSpace(requester) == "" {
		return fmt.Errorf("%w: requester principal is required", ErrInvalidVerified)
	}
	if strings.EqualFold(v.VerifierPrincipal, requester) {
		return fmt.Errorf("%w: plane %s verifier %q must differ from requester %q", ErrInvalidVerified, v.Plane, v.VerifierPrincipal, requester)
	}
	if v.VerifiedAt.IsZero() {
		return fmt.Errorf("%w: verified_at is required", ErrInvalidVerified)
	}
	if len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("%w: plane %s carries no evidence", ErrInvalidVerified, v.Plane)
	}
	for _, ref := range v.EvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: plane %s carries a blank evidence reference", ErrInvalidVerified, v.Plane)
		}
	}
	return nil
}

// sameEvidence reports whether v and other name the identical verification
// fact: same plane, tenant, verifier principal and evidence references
// (order-sensitive -- a verifier that reorders its own evidence on a
// resumed run is a verifier that changed what it proved). VerifiedAt is
// deliberately excluded: a resumed bootstrap re-running the identical check
// will naturally observe a new wall-clock instant, and that alone must not
// turn an idempotent replay into a conflict.
func (v Verified) sameEvidence(other Verified) bool {
	if v.Plane != other.Plane || v.Tenant != other.Tenant || v.VerifierPrincipal != other.VerifierPrincipal {
		return false
	}
	if len(v.EvidenceRefs) != len(other.EvidenceRefs) {
		return false
	}
	for i := range v.EvidenceRefs {
		if v.EvidenceRefs[i] != other.EvidenceRefs[i] {
			return false
		}
	}
	return true
}

// VerificationRequest is what [ProvisioningRun]'s caller (typically via
// [VerifyPlane]) presents to a [PlaneVerifier].
type VerificationRequest struct {
	// Tenant is the tenant slug being provisioned -- the same identity
	// [BootstrapManifest.Tenant] names.
	Tenant string
	// RequesterPrincipal names who is asking for this plane to be verified.
	// A [PlaneVerifier] implementation is free to ignore it, but
	// [ProvisioningRun.RecordVerified] uses it to enforce that the returned
	// [Verified.VerifierPrincipal] is never the same principal.
	RequesterPrincipal string
	// Now is supplied by the caller so a verifier's [Verified.VerifiedAt]
	// is a pure function of its inputs, never the wall clock.
	Now time.Time
}

// PlaneVerifier is the typed port one [Plane] implements: given a
// [VerificationRequest], independently prove (or refuse to prove) that this
// plane holds for the named tenant. internal/data/tenancy composes real,
// repository-backed implementations of this port for the planes a
// repository can prove today; [FakeVerifier] is the in-memory stand-in this
// package ships for planes whose real implementation depends on an
// undelivered todo, and for pure unit tests of [ProvisioningRun] itself.
type PlaneVerifier interface {
	// Plane names the fixed plane this verifier proves. It never varies
	// across calls to Verify.
	Plane() Plane
	// Verify proves req.Tenant's plane holds right now, or returns an
	// error describing why it does not.
	Verify(ctx context.Context, req VerificationRequest) (Verified, error)
}

// VerifyPlane runs verifier against run's own tenant and records the
// outcome, as requester. It is the composition most callers want: a
// verifier's own Verify error is returned as-is (never recorded), while a
// successful [Verified] is handed to [ProvisioningRun.RecordVerified].
func VerifyPlane(ctx context.Context, run *ProvisioningRun, verifier PlaneVerifier, requester string, now time.Time) (RecordVerifiedOutcome, error) {
	if run == nil {
		return RecordVerifiedOutcome{}, fmt.Errorf("%w: nil provisioning run", ErrInvalidVerified)
	}
	if verifier == nil {
		return RecordVerifiedOutcome{}, fmt.Errorf("%w: nil plane verifier", ErrInvalidVerified)
	}
	v, err := verifier.Verify(ctx, VerificationRequest{
		Tenant:             run.Tenant(),
		RequesterPrincipal: requester,
		Now:                now,
	})
	if err != nil {
		return RecordVerifiedOutcome{}, fmt.Errorf("tenant: plane %s verification failed: %w", verifier.Plane(), err)
	}
	return run.RecordVerified(requester, v)
}

// ProvisioningStatus is one state of a [ProvisioningRun].
type ProvisioningStatus string

const (
	// ProvisioningPending means at least one plane has not yet reported
	// VERIFIED. A never-started run begins here.
	ProvisioningPending ProvisioningStatus = "PENDING"
	// ProvisioningActive means every plane in [AllPlanes] currently holds
	// a [Verified] record.
	ProvisioningActive ProvisioningStatus = "ACTIVE"
	// ProvisioningDegraded means a plane that previously reported VERIFIED
	// (whether or not the run had already reached ACTIVE) has since failed
	// via [ProvisioningRun.RecordFailure]. DegradedPlane names which one.
	ProvisioningDegraded ProvisioningStatus = "DEGRADED"
)

// ProvisioningEventKind names one kind of digested, append-only fact a
// [ProvisioningRun] records in its own [ProvisioningRun.Events] trail.
type ProvisioningEventKind string

const (
	// EventPlaneVerified is recorded every time RecordVerified actually
	// applies new evidence (never on a NOOP replay or a REJECTED attempt).
	EventPlaneVerified ProvisioningEventKind = "PLANE_VERIFIED"
	// EventTenantActivated is recorded exactly once, the instant the last
	// of [AllPlanes] is recorded VERIFIED.
	EventTenantActivated ProvisioningEventKind = "TENANT_ACTIVATED"
	// EventTenantDegraded is recorded every time RecordFailure applies.
	EventTenantDegraded ProvisioningEventKind = "TENANT_DEGRADED"
)

// ProvisioningEvent is one digested, append-only fact in a
// [ProvisioningRun]'s evidence trail. Two events with the same meaning
// digest identically (see [ProvisioningEvent.Digest]); changing any field
// changes the digest, so the trail [ProvisioningRun.Events] returns is
// tamper-evident evidence, not just a log line.
type ProvisioningEvent struct {
	Kind   ProvisioningEventKind
	Tenant string
	// Plane names the plane a PLANE_VERIFIED or TENANT_DEGRADED event
	// concerns. Empty for TENANT_ACTIVATED, which concerns the whole run.
	Plane Plane
	// VerifierPrincipal is set on a PLANE_VERIFIED event: who verified it.
	VerifierPrincipal string
	// Reason is set on a TENANT_DEGRADED event: why the plane failed.
	Reason string
	At     time.Time

	digest string
}

// canonicalProvisioningEvent is the exact shape [ProvisioningEvent.Digest]
// hashes -- a plain, deterministic JSON encoding with a field order fixed by
// its own tags rather than by Go's struct field order.
type canonicalProvisioningEvent struct {
	Kind              string `json:"kind"`
	Tenant            string `json:"tenant"`
	Plane             string `json:"plane"`
	VerifierPrincipal string `json:"verifier_principal"`
	Reason            string `json:"reason"`
	AtUnixNS          int64  `json:"at_unix_nano"`
}

func (e ProvisioningEvent) canonical() ([]byte, error) {
	return json.Marshal(canonicalProvisioningEvent{
		Kind:              string(e.Kind),
		Tenant:            e.Tenant,
		Plane:             string(e.Plane),
		VerifierPrincipal: e.VerifierPrincipal,
		Reason:            e.Reason,
		AtUnixNS:          e.At.UTC().UnixNano(),
	})
}

// digestEvent computes e's content digest without requiring e already carry
// one -- used both by Digest (which trusts an already-computed digest) and
// by the constructors below (which must compute one before it exists).
func digestEvent(e ProvisioningEvent) (string, error) {
	b, err := e.canonical()
	if err != nil {
		return "", fmt.Errorf("tenant: encode provisioning event: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Digest returns e's content digest: the lowercase hex sha256 of its
// canonical encoding, computed when e was appended to a [ProvisioningRun].
func (e ProvisioningEvent) Digest() string { return e.digest }

// RecordVerifiedOutcome is the result of [ProvisioningRun.RecordVerified].
// It mirrors [BootstrapOutcome]'s three-valued shape deliberately: both
// answer the same question -- "does this caller-presented fact change
// durable state, restate it unchanged, or conflict with it" -- for their
// respective ledgers.
type RecordVerifiedOutcome struct {
	Decision ProvisioningDecision
	// Event is the digested [ProvisioningEvent] this call appended. Zero
	// for NOOP and REJECTED: neither appends anything.
	Event ProvisioningEvent
	// Activated is true when this call's APPLY was the one that completed
	// [AllPlanes] and moved the run to [ProvisioningActive]. ActivationEvent
	// then carries the TENANT_ACTIVATED event appended in the same call.
	Activated       bool
	ActivationEvent ProvisioningEvent
	// Reason explains a REJECTED decision. Empty otherwise.
	Reason string
}

// ProvisioningDecision is the three-valued outcome of presenting one
// [Verified] record to a [ProvisioningRun].
type ProvisioningDecision string

const (
	// ProvisioningApply reports that the presented evidence was recorded:
	// either the plane had never been verified, or it was previously named
	// by a [ProvisioningRun.RecordFailure] and this evidence repairs it.
	ProvisioningApply ProvisioningDecision = "APPLY"
	// ProvisioningNoop reports that the presented evidence exactly restates
	// a plane already verified (see [Verified.sameEvidence]): nothing was
	// written. This is what makes a resumed bootstrap idempotent.
	ProvisioningNoop ProvisioningDecision = "NOOP"
	// ProvisioningRejectedDecision reports that the presented evidence
	// conflicts with the run's current state and was refused.
	ProvisioningRejectedDecision ProvisioningDecision = "REJECTED"
)

// ProvisioningRun tracks per-plane verification state for one tenant's
// bootstrap toward [ProvisioningActive]. The zero value is not usable;
// construct one with [NewProvisioningRun].
//
// A ProvisioningRun is not safe for concurrent use without external
// synchronization -- exactly like [BootstrapManifest]'s own ledger, the
// concurrency story for two callers racing to verify the same plane is a
// durable-storage concern (internal/data/tenancy composes this type with
// its own receipt table for that), not this in-memory type's.
type ProvisioningRun struct {
	tenant string

	verified map[Plane]Verified
	status   ProvisioningStatus

	degradedPlane  Plane
	degradedReason string

	events []ProvisioningEvent
}

// NewProvisioningRun starts a new run for tenant, in [ProvisioningPending]
// with no plane yet verified.
func NewProvisioningRun(tenant string) (*ProvisioningRun, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalidVerified)
	}
	return &ProvisioningRun{
		tenant:   tenant,
		verified: make(map[Plane]Verified, len(AllPlanes)),
		status:   ProvisioningPending,
	}, nil
}

// Tenant returns the tenant slug this run provisions.
func (r *ProvisioningRun) Tenant() string { return r.tenant }

// Status returns the run's current status.
func (r *ProvisioningRun) Status() ProvisioningStatus { return r.status }

// DegradedPlane names the plane that moved the run to [ProvisioningDegraded],
// and reports false when the run is not currently degraded.
func (r *ProvisioningRun) DegradedPlane() (Plane, string, bool) {
	if r.status != ProvisioningDegraded {
		return "", "", false
	}
	return r.degradedPlane, r.degradedReason, true
}

// Verified returns the recorded evidence for plane, if any is currently on
// file. A plane named by a later [ProvisioningRun.RecordFailure] is removed
// from this set -- Verified reports false for it until it is repaired.
func (r *ProvisioningRun) Verified(plane Plane) (Verified, bool) {
	v, ok := r.verified[plane]
	return v, ok
}

// VerifiedPlanes returns every plane currently holding a [Verified] record,
// in [AllPlanes] order.
func (r *ProvisioningRun) VerifiedPlanes() []Plane {
	out := make([]Plane, 0, len(r.verified))
	for _, p := range AllPlanes {
		if _, ok := r.verified[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Events returns the run's full digested evidence trail, oldest first. The
// returned slice is a copy; mutating it never affects the run.
func (r *ProvisioningRun) Events() []ProvisioningEvent {
	return append([]ProvisioningEvent(nil), r.events...)
}

func (r *ProvisioningRun) allPlanesVerified() bool {
	for _, p := range AllPlanes {
		if _, ok := r.verified[p]; !ok {
			return false
		}
	}
	return true
}

func (r *ProvisioningRun) appendEvent(ev ProvisioningEvent) (ProvisioningEvent, error) {
	digest, err := digestEvent(ev)
	if err != nil {
		return ProvisioningEvent{}, err
	}
	ev.digest = digest
	r.events = append(r.events, ev)
	return ev, nil
}

// RecordVerified presents v -- one plane's verification evidence -- to the
// run, as requester (see [Verified.Validate] for why requester must differ
// from v.VerifierPrincipal).
//
// TENANT-002 GREEN: recording the last of [AllPlanes] moves the run
// straight to [ProvisioningActive] in the same call (RecordVerifiedOutcome.
// Activated reports this); recording identical evidence for an
// already-verified plane is a NOOP that changes nothing (resumable
// bootstrap); recording different evidence for a plane already verified --
// and not currently named by a degradation -- is REJECTED, since a
// verified plane's evidence does not silently change underfoot; recording
// evidence for a tenant other than r.Tenant is REJECTED outright.
func (r *ProvisioningRun) RecordVerified(requester string, v Verified) (RecordVerifiedOutcome, error) {
	if err := v.Validate(requester); err != nil {
		return RecordVerifiedOutcome{Decision: ProvisioningRejectedDecision, Reason: err.Error()}, err
	}
	if v.Tenant != r.tenant {
		err := fmt.Errorf("%w: plane %s verification names tenant %q; this run is for %q",
			ErrProvisioningRejected, v.Plane, v.Tenant, r.tenant)
		return RecordVerifiedOutcome{Decision: ProvisioningRejectedDecision, Reason: err.Error()}, err
	}

	wasDegradedForThisPlane := r.status == ProvisioningDegraded && r.degradedPlane == v.Plane

	if existing, ok := r.verified[v.Plane]; ok {
		if existing.sameEvidence(v) {
			return RecordVerifiedOutcome{Decision: ProvisioningNoop}, nil
		}
		if !wasDegradedForThisPlane {
			err := fmt.Errorf("%w: plane %s is already verified with different evidence; record a failure before re-verifying it",
				ErrProvisioningRejected, v.Plane)
			return RecordVerifiedOutcome{Decision: ProvisioningRejectedDecision, Reason: err.Error()}, err
		}
	}

	r.verified[v.Plane] = v
	ev, err := r.appendEvent(ProvisioningEvent{
		Kind:              EventPlaneVerified,
		Tenant:            r.tenant,
		Plane:             v.Plane,
		VerifierPrincipal: v.VerifierPrincipal,
		At:                v.VerifiedAt,
	})
	if err != nil {
		return RecordVerifiedOutcome{}, err
	}

	if wasDegradedForThisPlane {
		r.status = ProvisioningPending
		r.degradedPlane = ""
		r.degradedReason = ""
	}

	outcome := RecordVerifiedOutcome{Decision: ProvisioningApply, Event: ev}
	if r.status != ProvisioningActive && r.allPlanesVerified() {
		r.status = ProvisioningActive
		actEvent, err := r.appendEvent(ProvisioningEvent{
			Kind:   EventTenantActivated,
			Tenant: r.tenant,
			At:     v.VerifiedAt,
		})
		if err != nil {
			return RecordVerifiedOutcome{}, err
		}
		outcome.Activated = true
		outcome.ActivationEvent = actEvent
	}
	return outcome, nil
}

// RecordFailure reports that plane no longer holds for the run's tenant --
// GREEN's "any plane's later failure moving the tenant to DEGRADED with the
// plane named". It is valid regardless of the run's current status
// (including before the run ever reached ACTIVE): a plane failing while
// others are still pending is exactly as much a definite regression as one
// failing after full activation, and DEGRADED communicates that plainly
// rather than leaving the run looking merely "still pending".
//
// The failed plane's [Verified] record, if any, is removed: [Verified]
// reports false for it until a fresh [ProvisioningRun.RecordVerified] call
// repairs it (GREEN's REFACTOR clause: "per-plane steps expose independent
// idempotency keys and repair routes").
func (r *ProvisioningRun) RecordFailure(plane Plane, reason string, at time.Time) (ProvisioningEvent, error) {
	if !plane.Valid() {
		return ProvisioningEvent{}, fmt.Errorf("%w: %q is not a known provisioning plane", ErrInvalidVerified, plane)
	}
	if strings.TrimSpace(reason) == "" {
		return ProvisioningEvent{}, fmt.Errorf("%w: a plane failure requires a reason", ErrInvalidVerified)
	}
	if at.IsZero() {
		return ProvisioningEvent{}, fmt.Errorf("%w: a plane failure requires a time", ErrInvalidVerified)
	}

	delete(r.verified, plane)
	r.status = ProvisioningDegraded
	r.degradedPlane = plane
	r.degradedReason = reason

	return r.appendEvent(ProvisioningEvent{
		Kind:   EventTenantDegraded,
		Tenant: r.tenant,
		Plane:  plane,
		Reason: reason,
		At:     at,
	})
}

// FakeVerifier is an in-memory [PlaneVerifier]: it always returns a canned,
// well-formed [Verified] record for its fixed plane (unless configured to
// fail via [FakeVerifier.FailNext]) and records every request it received.
// internal/data/tenancy's pgtest-backed composition uses it to stand in for
// the planes that have no repository-backed proof yet (their own todos
// remain undelivered); pure unit tests in this package use it to drive
// [ProvisioningRun] without any real verifier at all.
type FakeVerifier struct {
	plane        Plane
	verifierName string
	evidenceRefs []string

	mu       sync.Mutex
	calls    []VerificationRequest
	failWith error
}

var _ PlaneVerifier = (*FakeVerifier)(nil)

// NewFakeVerifier returns a fake for plane that, absent a configured
// failure, reports VERIFIED as verifierPrincipal with evidenceRefs.
func NewFakeVerifier(plane Plane, verifierPrincipal string, evidenceRefs ...string) *FakeVerifier {
	return &FakeVerifier{
		plane:        plane,
		verifierName: verifierPrincipal,
		evidenceRefs: append([]string(nil), evidenceRefs...),
	}
}

// Plane implements [PlaneVerifier].
func (f *FakeVerifier) Plane() Plane { return f.plane }

// FailNext makes every subsequent call to Verify return err instead of a
// canned success, until cleared by calling FailNext(nil).
func (f *FakeVerifier) FailNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWith = err
}

// Calls returns every [VerificationRequest] this fake has received, oldest
// first. The returned slice is a copy.
func (f *FakeVerifier) Calls() []VerificationRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]VerificationRequest(nil), f.calls...)
}

// Verify implements [PlaneVerifier].
func (f *FakeVerifier) Verify(_ context.Context, req VerificationRequest) (Verified, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	failWith := f.failWith
	f.mu.Unlock()

	if failWith != nil {
		return Verified{}, failWith
	}
	if strings.TrimSpace(req.Tenant) == "" {
		return Verified{}, fmt.Errorf("%w: verification request carries no tenant", ErrInvalidVerified)
	}
	return Verified{
		Plane:             f.plane,
		Tenant:            req.Tenant,
		VerifierPrincipal: f.verifierName,
		VerifiedAt:        req.Now,
		EvidenceRefs:      append([]string(nil), f.evidenceRefs...),
	}, nil
}
