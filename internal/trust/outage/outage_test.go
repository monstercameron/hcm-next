package outage

import (
	"testing"
	"time"
)

func testPolicy() Policy {
	return Policy{
		MaxJWKSStaleness:    10 * time.Minute,
		MaxClockSkew:        30 * time.Second,
		MaxCachedSessionAge: 2 * time.Hour,
	}
}

var baseNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func healthyHealth() Health {
	return Health{IdPReachable: true, JWKSAge: time.Minute, ClockSkew: time.Second}
}

func staleHealth() Health {
	return Health{IdPReachable: true, JWKSAge: time.Hour, ClockSkew: time.Second}
}

func unreachableHealth() Health {
	return Health{IdPReachable: false, JWKSAge: time.Minute, ClockSkew: time.Second}
}

func skewedHealth() Health {
	return Health{IdPReachable: true, JWKSAge: time.Minute, ClockSkew: time.Minute}
}

// TestTodo_TRUST_020 is the primary test: a golden decision table over
// federation health x request class x session age x emergency grant. It is
// the RED->GREEN acceptance test for TRUST-020: a stale-federation new
// session must never be silently permitted, and a privileged operation
// during an outage must never proceed without an approved, evidenced
// emergency grant.
func TestTodo_TRUST_020(t *testing.T) {
	p := testPolicy()
	validEmergency := &EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Hour)}

	cases := []struct {
		name    string
		h       Health
		req     Request
		want    Outcome
		wantRsn string
	}{
		{"healthy_new_session_permits", healthyHealth(), Request{Class: ClassNewSession}, OutcomePermit, ReasonHealthy},
		{"healthy_existing_read_permits", healthyHealth(), Request{Class: ClassExistingRead, SessionAge: time.Minute}, OutcomePermit, ReasonHealthy},
		{"healthy_existing_write_permits", healthyHealth(), Request{Class: ClassExistingWrite, SessionAge: time.Minute}, OutcomePermit, ReasonHealthy},
		{"healthy_privileged_permits_without_emergency", healthyHealth(), Request{Class: ClassPrivileged}, OutcomePermit, ReasonHealthy},

		{"stale_jwks_new_session_denied", staleHealth(), Request{Class: ClassNewSession}, OutcomeDeny, ReasonNewSessionDuringOutage},
		{"unreachable_new_session_denied", unreachableHealth(), Request{Class: ClassNewSession}, OutcomeDeny, ReasonNewSessionDuringOutage},
		{"skewed_new_session_denied", skewedHealth(), Request{Class: ClassNewSession}, OutcomeDeny, ReasonNewSessionDuringOutage},

		{"outage_existing_read_within_window_permits_cached", staleHealth(), Request{Class: ClassExistingRead, SessionAge: time.Hour}, OutcomePermitCached, ReasonCachedWithinWindow},
		{"outage_existing_read_beyond_window_denied", staleHealth(), Request{Class: ClassExistingRead, SessionAge: 3 * time.Hour}, OutcomeDeny, ReasonCachedWindowExceeded},
		{"outage_existing_write_within_window_degrades_readonly", staleHealth(), Request{Class: ClassExistingWrite, SessionAge: time.Hour}, OutcomePermitReadOnly, ReasonCachedWithinWindow},
		{"outage_existing_write_beyond_window_denied", staleHealth(), Request{Class: ClassExistingWrite, SessionAge: 3 * time.Hour}, OutcomeDeny, ReasonCachedWindowExceeded},

		{"outage_privileged_without_emergency_denied", staleHealth(), Request{Class: ClassPrivileged}, OutcomeDeny, ReasonPrivilegedRequiresEmergency},
		{"outage_privileged_with_valid_emergency_permits", staleHealth(), Request{Class: ClassPrivileged, Emergency: validEmergency}, OutcomePermitEmergency, ReasonEmergencyGrantAccepted},
		{"outage_privileged_with_unapproved_emergency_denied", staleHealth(), Request{Class: ClassPrivileged, Emergency: &EmergencyGrant{Approved: false, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Hour)}}, OutcomeDeny, ReasonPrivilegedRequiresEmergency},
		{"outage_privileged_with_expired_emergency_denied", staleHealth(), Request{Class: ClassPrivileged, Emergency: &EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(-time.Minute)}}, OutcomeDeny, ReasonPrivilegedRequiresEmergency},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Evaluate(p, c.h, c.req, baseNow)
			if got.Outcome != c.want {
				t.Fatalf("outcome = %s, want %s (reason %s)", got.Outcome, c.want, got.Reason)
			}
			if got.Reason != c.wantRsn {
				t.Fatalf("reason = %s, want %s", got.Reason, c.wantRsn)
			}
		})
	}
}

// TestTodo_TRUST_020_Security asserts the privileged path cannot be talked
// into permitting without a fully-formed emergency grant: partial credit
// (approval without step-up, step-up without a ticket reference, or a
// grant that has run past its own expiry) is never enough.
func TestTodo_TRUST_020_Security(t *testing.T) {
	p := testPolicy()
	h := staleHealth()

	grants := []*EmergencyGrant{
		{Approved: true, StepUpSatisfied: false, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Hour)},
		{Approved: false, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Hour)},
		{Approved: true, StepUpSatisfied: true, TicketRef: "", ExpiresAt: baseNow.Add(time.Hour)},
		{Approved: true, StepUpSatisfied: true, TicketRef: "   ", ExpiresAt: baseNow.Add(time.Hour)},
		{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow},
		nil,
	}
	for i, g := range grants {
		got := Evaluate(p, h, Request{Class: ClassPrivileged, Emergency: g}, baseNow)
		if got.Outcome != OutcomeDeny {
			t.Fatalf("case %d: outcome = %s, want DENY for malformed emergency grant %+v", i, got.Outcome, g)
		}
	}

	// A fully valid emergency grant must not leak into any other class: it
	// is scoped to the privileged path only.
	valid := &EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Hour)}
	for _, class := range []Class{ClassNewSession, ClassExistingRead, ClassExistingWrite} {
		got := Evaluate(p, h, Request{Class: class, Emergency: valid, SessionAge: 3 * time.Hour}, baseNow)
		if got.Outcome == OutcomePermitEmergency {
			t.Fatalf("class %s must not honor an emergency grant", class)
		}
	}
}

// TestTodo_TRUST_020_Mutation flips the boundary conditions of the policy to
// confirm the comparisons are the strict ones intended, not off-by-one.
func TestTodo_TRUST_020_Mutation(t *testing.T) {
	p := testPolicy()

	// JWKS age exactly at the bound is still healthy; one tick over is not.
	atBound := Health{IdPReachable: true, JWKSAge: p.MaxJWKSStaleness, ClockSkew: 0}
	overBound := Health{IdPReachable: true, JWKSAge: p.MaxJWKSStaleness + time.Nanosecond, ClockSkew: 0}
	if !healthyAt(p, atBound) {
		t.Fatalf("jwks age exactly at bound must be healthy")
	}
	if healthyAt(p, overBound) {
		t.Fatalf("jwks age one tick over bound must be unhealthy")
	}

	// Clock skew is compared by absolute value: a negative skew of the same
	// magnitude as a positive one must be treated identically.
	pos := Health{IdPReachable: true, JWKSAge: 0, ClockSkew: p.MaxClockSkew}
	neg := Health{IdPReachable: true, JWKSAge: 0, ClockSkew: -p.MaxClockSkew}
	if healthyAt(p, pos) != healthyAt(p, neg) {
		t.Fatalf("positive and negative skew of equal magnitude must agree")
	}

	// Session age exactly at the cached window is still permitted; one tick
	// over is denied.
	atWindow := Evaluate(p, staleHealth(), Request{Class: ClassExistingRead, SessionAge: p.MaxCachedSessionAge}, baseNow)
	if atWindow.Outcome != OutcomePermitCached {
		t.Fatalf("session age exactly at window must permit cached, got %s", atWindow.Outcome)
	}
	overWindow := Evaluate(p, staleHealth(), Request{Class: ClassExistingRead, SessionAge: p.MaxCachedSessionAge + time.Nanosecond}, baseNow)
	if overWindow.Outcome != OutcomeDeny {
		t.Fatalf("session age one tick over window must deny, got %s", overWindow.Outcome)
	}

	// An emergency grant that expires at exactly now is no longer valid:
	// expiry is exclusive of the boundary instant.
	boundaryGrant := &EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow}
	atExpiry := Evaluate(p, staleHealth(), Request{Class: ClassPrivileged, Emergency: boundaryGrant}, baseNow)
	if atExpiry.Outcome != OutcomeDeny {
		t.Fatalf("emergency grant expiring exactly now must deny, got %s", atExpiry.Outcome)
	}
}

// TestTodo_TRUST_020_Fault exercises malformed and degenerate inputs: an
// invalid policy, a negative session age, and reconciliation after recovery.
func TestTodo_TRUST_020_Fault(t *testing.T) {
	// A zero-value policy has no bounds and must fail closed rather than be
	// read as "always healthy".
	zero := Policy{}
	got := Evaluate(zero, healthyHealth(), Request{Class: ClassNewSession}, baseNow)
	if got.Outcome != OutcomeDeny || got.Reason != ReasonInvalidPolicy {
		t.Fatalf("zero policy: outcome = %s/%s, want DENY/invalid_policy", got.Outcome, got.Reason)
	}

	// A negative session age is malformed input, not "very fresh".
	p := testPolicy()
	got = Evaluate(p, staleHealth(), Request{Class: ClassExistingRead, SessionAge: -time.Second}, baseNow)
	if got.Outcome != OutcomeDeny || got.Reason != ReasonInvalidRequest {
		t.Fatalf("negative session age: outcome = %s/%s, want DENY/invalid_request", got.Outcome, got.Reason)
	}

	// An unrecognized class fails closed.
	got = Evaluate(p, healthyHealth(), Request{Class: Class("BOGUS")}, baseNow)
	if got.Outcome != OutcomeDeny || got.Reason != ReasonInvalidRequest {
		t.Fatalf("bogus class: outcome = %s/%s, want DENY/invalid_request", got.Outcome, got.Reason)
	}

	// Reconciliation after recovery: every cached, read-only fallback and
	// emergency use recorded during the outage must be flagged for
	// revalidation; nothing is silently upgraded to full trust.
	uses := []CachedUse{
		{SessionRef: "s1", Outcome: OutcomePermitCached, DecidedAt: baseNow},
		{SessionRef: "s2", Outcome: OutcomePermitReadOnly, DecidedAt: baseNow},
		{SessionRef: "s3", Outcome: OutcomePermitEmergency, DecidedAt: baseNow},
		{SessionRef: "s4", Outcome: OutcomePermit, DecidedAt: baseNow},
	}
	results := Reconcile(uses)
	if len(results) != 4 {
		t.Fatalf("expected 4 reconciliation results, got %d", len(results))
	}
	want := map[string]ReconciliationAction{
		"s1": ReconcileRevalidateRequired,
		"s2": ReconcileRevalidateRequired,
		"s3": ReconcileRevalidateRequired,
		"s4": ReconcileNoActionNeeded,
	}
	for _, r := range results {
		if r.Action != want[r.SessionRef] {
			t.Fatalf("session %s: action = %s, want %s", r.SessionRef, r.Action, want[r.SessionRef])
		}
	}
}

// FuzzTodo_TRUST_020 checks invariants that must hold for every combination
// of policy, health, and request: a healthy signal always permits, an
// unhealthy signal never permits a new session, and a privileged operation
// during an outage never permits without a fully valid emergency grant.
func FuzzTodo_TRUST_020(f *testing.F) {
	f.Add(int64(10*time.Minute), int64(30*time.Second), int64(2*time.Hour), true, int64(time.Minute), int64(time.Second), 0, int64(time.Minute), true, true, "INC-1", int64(time.Hour))
	f.Fuzz(func(t *testing.T, maxJWKS, maxSkew, maxCached int64, reachable bool, jwksAge, skew int64, classSel int, sessionAge int64, approved, stepUp bool, ticket string, grantExpiresOffset int64) {
		p := Policy{
			MaxJWKSStaleness:    time.Duration(maxJWKS),
			MaxClockSkew:        time.Duration(maxSkew),
			MaxCachedSessionAge: time.Duration(maxCached),
		}
		h := Health{IdPReachable: reachable, JWKSAge: time.Duration(jwksAge), ClockSkew: time.Duration(skew)}
		classes := []Class{ClassNewSession, ClassExistingRead, ClassExistingWrite, ClassPrivileged}
		class := classes[((classSel%len(classes))+len(classes))%len(classes)]
		var age time.Duration
		if class != ClassNewSession {
			// Keep the fuzzed session age within a sane, non-negative range
			// so this fuzz target is exercising the policy logic rather
			// than always tripping the malformed-input fault path.
			if sessionAge < 0 {
				sessionAge = -sessionAge
			}
			age = time.Duration(sessionAge % int64(24*time.Hour))
		}
		grant := &EmergencyGrant{Approved: approved, StepUpSatisfied: stepUp, TicketRef: ticket, ExpiresAt: baseNow.Add(time.Duration(grantExpiresOffset))}
		req := Request{Class: class, SessionAge: age, Emergency: grant}

		d := Evaluate(p, h, req, baseNow)

		if err := p.validate(); err == nil {
			healthy := healthyAt(p, h)
			if healthy && d.Outcome != OutcomePermit {
				t.Fatalf("healthy signal must permit, got %s (policy=%+v health=%+v)", d.Outcome, p, h)
			}
			if !healthy && class == ClassNewSession && d.Outcome != OutcomeDeny {
				t.Fatalf("unhealthy signal must deny a new session, got %s", d.Outcome)
			}
			if !healthy && class == ClassPrivileged && d.Outcome == OutcomePermitEmergency && !grant.validAt(baseNow) {
				t.Fatalf("privileged operation permitted via emergency without a valid grant")
			}
			if !healthy && class == ClassPrivileged && !grant.validAt(baseNow) && d.Outcome != OutcomeDeny {
				t.Fatalf("privileged operation without a valid emergency grant must deny, got %s", d.Outcome)
			}
		}
	})
}
