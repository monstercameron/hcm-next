package outage_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/outage"
	"github.com/monstercameron/hcm-next/internal/operations/incidentstate"
	trustoutage "github.com/monstercameron/hcm-next/internal/trust/outage"
)

var continuityNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func continuityProfile() outage.Profile {
	return outage.Profile{
		TenantID: "tenant-a", IncidentID: "inc-idp-1",
		Policy: trustoutage.Policy{
			MaxJWKSStaleness: 10 * time.Minute, MaxClockSkew: 30 * time.Second,
			MaxCachedSessionAge: 2 * time.Hour,
		},
		ManualEnabled: true, MaxManualAge: 15 * time.Minute,
	}
}

func degradedHealth() trustoutage.Health {
	return trustoutage.Health{IdPReachable: false, JWKSAge: time.Minute, ClockSkew: time.Second}
}

func healthyHealth() trustoutage.Health {
	return trustoutage.Health{IdPReachable: true, JWKSAge: time.Minute, ClockSkew: time.Second}
}

func request(class trustoutage.Class, age time.Duration) outage.Request {
	return outage.Request{SessionRef: "session-1", Class: class, SessionAge: age, Revocation: outage.RevocationConfirmed}
}

func incidentFixture(t *testing.T) incidentstate.Incident {
	t.Helper()
	in, err := incidentstate.New("inc-idp-1", "tenant-a", continuityNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	in, err = incidentstate.Transition(in, incidentstate.Command{To: incidentstate.Triaged, Actor: "on-call", Reason: "federation signal correlated", EvidenceRef: "signal-1"}, continuityNow.Add(-50*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	in, err = incidentstate.Transition(in, incidentstate.Command{To: incidentstate.Declared, Actor: "commander", Reason: "identity provider outage declared", EvidenceRef: "declare-1"}, continuityNow.Add(-45*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func manualApproval(expires time.Time) *outage.ManualApproval {
	return &outage.ManualApproval{
		Approved: true, StepUpSatisfied: true, Actor: "operator", Approver: "security",
		Reason: "bounded manual continuity review", IncidentID: "inc-idp-1", ExpiresAt: expires,
	}
}

func emergencyGrant(expires time.Time) *trustoutage.EmergencyGrant {
	return &trustoutage.EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "inc-idp-1", ExpiresAt: expires}
}

// TestTodo_AUTHN_007 is the primary acceptance table: the outage is explicit,
// ordinary login is denied, existing access is bounded, manual access is
// audited, and recovery never silently promotes degraded authority.
func TestTodo_AUTHN_007(t *testing.T) {
	p := continuityProfile()
	cases := []struct {
		name          string
		phase         outage.Phase
		health        trustoutage.Health
		req           outage.Request
		wantMode      outage.Mode
		wantAllow     bool
		wantReadOnly  bool
		wantReconcile bool
	}{
		{"healthy_normal_session", outage.PhaseNormal, healthyHealth(), request(trustoutage.ClassExistingRead, time.Minute), outage.ModeNormal, true, false, false},
		{"declared_new_session_denied", outage.PhaseDegradedIdentity, degradedHealth(), outage.Request{Class: trustoutage.ClassNewSession, Revocation: outage.RevocationConfirmed}, outage.ModeDenied, false, false, false},
		{"declared_new_session_waits_for_reconciliation", outage.PhaseDeclared, healthyHealth(), outage.Request{Class: trustoutage.ClassNewSession, Revocation: outage.RevocationConfirmed}, outage.ModeDenied, false, false, false},
		{"declared_existing_read_cached", outage.PhaseDegradedIdentity, degradedHealth(), request(trustoutage.ClassExistingRead, time.Hour), outage.ModeCached, true, false, true},
		{"declared_existing_write_read_only", outage.PhaseDegradedIdentity, degradedHealth(), request(trustoutage.ClassExistingWrite, time.Hour), outage.ModeReadOnly, true, true, true},
		{"declared_privileged_requires_emergency", outage.PhaseDegradedIdentity, degradedHealth(), request(trustoutage.ClassPrivileged, time.Minute), outage.ModeDenied, false, false, false},
		{"declared_privileged_emergency", outage.PhaseDegradedIdentity, degradedHealth(), func() outage.Request {
			r := request(trustoutage.ClassPrivileged, time.Minute)
			r.Emergency = emergencyGrant(continuityNow.Add(time.Hour))
			return r
		}(), outage.ModeEmergency, true, false, true},
		{"declared_manual_read", outage.PhaseDeclared, degradedHealth(), func() outage.Request {
			r := request(trustoutage.ClassExistingRead, 4*time.Hour)
			r.Manual = manualApproval(continuityNow.Add(10 * time.Minute))
			return r
		}(), outage.ModeManual, true, false, true},
		{"declared_manual_write_is_read_only", outage.PhaseDeclared, degradedHealth(), func() outage.Request {
			r := request(trustoutage.ClassExistingWrite, 4*time.Hour)
			r.Manual = manualApproval(continuityNow.Add(10 * time.Minute))
			return r
		}(), outage.ModeManual, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := outage.Decide(p, tc.phase, tc.health, tc.req, continuityNow)
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode != tc.wantMode || got.Allowed != tc.wantAllow || got.ReadOnly != tc.wantReadOnly || got.RequiresReconcile != tc.wantReconcile {
				t.Fatalf("decision=%+v, want mode=%s allow=%t readonly=%t reconcile=%t", got, tc.wantMode, tc.wantAllow, tc.wantReadOnly, tc.wantReconcile)
			}
			if !got.Allowed && got.Evidence.ID == "" {
				t.Fatal("denied outage decision must carry incident evidence")
			}
			if got.RequiresReconcile && got.Evidence.ID == "" {
				t.Fatal("degraded decision must carry reconciliation evidence")
			}
		})
	}

	if got, err := outage.Decide(p, outage.PhaseDegradedIdentity, degradedHealth(), request(trustoutage.ClassExistingRead, time.Hour), continuityNow); err != nil {
		t.Fatal(err)
	} else if err := outage.IncidentEvidence(p, incidentFixture(t), got.Evidence); err != nil {
		t.Fatalf("incident evidence binding: %v", err)
	}
	if outage.Version() != 1 || !strings.Contains(outage.Explain(outage.Decision{Phase: outage.PhaseDeclared, Mode: outage.ModeDenied}), "phase=DECLARED") {
		t.Fatal("version/explanation contract is not stable")
	}
}

func TestTodo_AUTHN_007_Integration(t *testing.T) {
	incident := incidentFixture(t)
	p := continuityProfile()
	if incident.State != incidentstate.Declared {
		t.Fatalf("incident state=%s, want DECLARED", incident.State)
	}
	decision, err := outage.Decide(p, outage.PhaseDegradedIdentity, degradedHealth(), request(trustoutage.ClassExistingWrite, time.Hour), continuityNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := outage.IncidentEvidence(p, incident, decision.Evidence); err != nil {
		t.Fatal(err)
	}
	uses := []outage.RecordedUse{{SessionRef: "session-1", Mode: decision.Mode, Outcome: decision.Outcome, Revocation: outage.RevocationConfirmed, EvidenceID: decision.Evidence.ID, DecidedAt: continuityNow}}
	results, err := outage.Reconcile(p, healthyHealth(), uses, continuityNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != outage.ReconcileRevalidateRequired {
		t.Fatalf("reconciliation=%+v", results)
	}
}

func TestTodo_AUTHN_007_Fault(t *testing.T) {
	p := continuityProfile()
	event, err := outage.Advance(outage.PhaseNormal, outage.PhaseSuspected, "monitor", "IdP probe failed", "probe-1", continuityNow)
	if err != nil || event.To != outage.PhaseSuspected {
		t.Fatalf("valid phase transition=%+v err=%v", event, err)
	}
	if _, err := outage.Advance(outage.PhaseNormal, outage.PhaseDegradedIdentity, "monitor", "skip declaration", "probe-2", continuityNow); !errors.Is(err, outage.ErrPhaseTransition) {
		t.Fatalf("phase skip error=%v", err)
	}
	if _, err := outage.Decide(outage.Profile{}, outage.PhaseDeclared, degradedHealth(), request(trustoutage.ClassExistingRead, time.Minute), continuityNow); !errors.Is(err, outage.ErrInvalidProfile) {
		t.Fatalf("invalid profile error=%v", err)
	}
	bad := request(trustoutage.ClassExistingRead, time.Minute)
	bad.Revocation = outage.RevocationStatus("MISSING")
	if _, err := outage.Decide(p, outage.PhaseDeclared, degradedHealth(), bad, continuityNow); !errors.Is(err, outage.ErrInvalidRequest) {
		t.Fatalf("invalid request error=%v", err)
	}
	if _, err := outage.Decide(p, outage.PhaseNormal, degradedHealth(), request(trustoutage.ClassExistingRead, time.Minute), continuityNow); err != nil {
		t.Fatalf("undeclared outage should be a fail-closed decision, got %v", err)
	}
	if _, err := outage.Reconcile(p, degradedHealth(), nil, continuityNow); !errors.Is(err, outage.ErrRecoveryNotReady) {
		t.Fatalf("unhealthy recovery error=%v", err)
	}
	if err := outage.IncidentEvidence(p, incidentFixture(t), outage.Evidence{ID: "e", IncidentID: "other", TenantID: p.TenantID}); !errors.Is(err, outage.ErrInvalidRequest) {
		t.Fatalf("cross-incident evidence error=%v", err)
	}
}

func TestTodo_AUTHN_007_Security(t *testing.T) {
	p := continuityProfile()
	newSession := outage.Request{Class: trustoutage.ClassNewSession, Revocation: outage.RevocationConfirmed, Manual: manualApproval(continuityNow.Add(time.Minute))}
	if got, err := outage.Decide(p, outage.PhaseDeclared, degradedHealth(), newSession, continuityNow); err != nil || got.Allowed {
		t.Fatalf("manual approval must not create a new session: decision=%+v err=%v", got, err)
	}
	for _, approval := range []*outage.ManualApproval{
		{Approved: false, StepUpSatisfied: true, Actor: "operator", Approver: "security", Reason: "x", IncidentID: p.IncidentID, ExpiresAt: continuityNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: false, Actor: "operator", Approver: "security", Reason: "x", IncidentID: p.IncidentID, ExpiresAt: continuityNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: true, Actor: "operator", Approver: "operator", Reason: "x", IncidentID: p.IncidentID, ExpiresAt: continuityNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: true, Actor: "operator", Approver: "security", Reason: "x", IncidentID: "other", ExpiresAt: continuityNow.Add(time.Minute)},
	} {
		r := request(trustoutage.ClassExistingRead, 4*time.Hour)
		r.Manual = approval
		got, err := outage.Decide(p, outage.PhaseDeclared, degradedHealth(), r, continuityNow)
		if err != nil || got.Allowed {
			t.Fatalf("incomplete manual approval must deny: decision=%+v err=%v", got, err)
		}
	}
	for _, status := range []outage.RevocationStatus{outage.RevocationUnknown, outage.RevocationRevoked} {
		r := request(trustoutage.ClassExistingRead, time.Minute)
		r.Revocation = status
		got, err := outage.Decide(p, outage.PhaseDegradedIdentity, degradedHealth(), r, continuityNow)
		if err != nil || got.Allowed {
			t.Fatalf("revocation status %s must fail closed: decision=%+v err=%v", status, got, err)
		}
	}
}

func TestTodo_AUTHN_007_Mutation(t *testing.T) {
	p := continuityProfile()
	r := request(trustoutage.ClassExistingRead, 4*time.Hour)
	r.Manual = manualApproval(continuityNow.Add(p.MaxManualAge))
	if got, err := outage.Decide(p, outage.PhaseDeclared, degradedHealth(), r, continuityNow); err != nil || got.Allowed {
		t.Fatalf("manual expiry boundary must deny: decision=%+v err=%v", got, err)
	}
	r.Manual = manualApproval(continuityNow.Add(p.MaxManualAge - time.Nanosecond))
	if got, err := outage.Decide(p, outage.PhaseDeclared, degradedHealth(), r, continuityNow); err != nil || !got.Allowed {
		t.Fatalf("manual age just inside bound must permit: decision=%+v err=%v", got, err)
	}
	uses := []outage.RecordedUse{
		{SessionRef: "z", Mode: outage.ModeCached, Revocation: outage.RevocationConfirmed, DecidedAt: continuityNow},
		{SessionRef: "a", Mode: outage.ModeReadOnly, Revocation: outage.RevocationRevoked, DecidedAt: continuityNow},
		{SessionRef: "m", Mode: outage.ModeManual, Revocation: outage.RevocationConfirmed, DecidedAt: continuityNow},
		{SessionRef: "u", Mode: outage.ModeCached, Revocation: outage.RevocationUnknown, DecidedAt: continuityNow},
	}
	got, err := outage.Reconcile(p, healthyHealth(), uses, continuityNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]outage.ReconciliationAction{"a": outage.ReconcileRevokeRequired, "m": outage.ReconcileReviewRequired, "u": outage.ReconcileBlocked, "z": outage.ReconcileRevalidateRequired}
	for _, result := range got {
		if result.Action != want[result.SessionRef] {
			t.Fatalf("session %s action=%s, want %s", result.SessionRef, result.Action, want[result.SessionRef])
		}
	}
	if got[0].SessionRef != "a" {
		t.Fatalf("reconciliation must be deterministic and sorted: %+v", got)
	}
}
func FuzzTodo_AUTHN_007(f *testing.F) {
	f.Add(int64(time.Minute), true, int64(time.Hour), int64(0))
	f.Fuzz(func(t *testing.T, age int64, reachable bool, sessionAge int64, phaseSelector int64) {
		p := continuityProfile()
		if age < 0 {
			age = -age
		}
		if sessionAge < 0 {
			sessionAge = -sessionAge
		}
		health := trustoutage.Health{IdPReachable: reachable, JWKSAge: time.Duration(age % int64(time.Hour)), ClockSkew: 0}
		phases := []outage.Phase{outage.PhaseNormal, outage.PhaseDeclared, outage.PhaseDegradedIdentity, outage.PhaseRecovering, outage.PhaseReconciling}
		phase := phases[((phaseSelector%int64(len(phases)))+int64(len(phases)))%int64(len(phases))]
		got, err := outage.Decide(p, phase, health, outage.Request{Class: trustoutage.ClassNewSession, SessionAge: time.Duration(sessionAge % int64(time.Hour)), Revocation: outage.RevocationConfirmed}, continuityNow)
		if err != nil {
			t.Fatal(err)
		}
		if !reachable && got.Allowed {
			t.Fatalf("unreachable IdP must never permit a new ordinary session: %+v", got)
		}
		if (phase == outage.PhaseRecovering || phase == outage.PhaseReconciling) && got.Allowed {
			t.Fatalf("recovery phase must not permit a new session: %+v", got)
		}
	})
}

func TestAdvanceRejectsInvalidTransitionsAndEvidence(t *testing.T) {
	cases := []struct {
		name string
		from outage.Phase
		to   outage.Phase
	}{
		{"unknown_from", outage.Phase("UNKNOWN"), outage.PhaseNormal},
		{"unknown_to", outage.PhaseNormal, outage.Phase("UNKNOWN")},
		{"skipped_phase", outage.PhaseNormal, outage.PhaseDegradedIdentity},
		{"missing_actor", outage.PhaseNormal, outage.PhaseSuspected},
		{"missing_reason", outage.PhaseNormal, outage.PhaseSuspected},
		{"missing_evidence", outage.PhaseNormal, outage.PhaseSuspected},
		{"missing_time", outage.PhaseNormal, outage.PhaseSuspected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actor, reason, ref := "actor", "reason", "evidence"
			at := continuityNow
			// Keep the table focused on one malformed field at a time.
			switch tc.name {
			case "missing_actor":
				actor = ""
			case "missing_reason":
				reason = ""
			case "missing_evidence":
				ref = ""
			case "missing_time":
				at = time.Time{}
			}
			if _, err := outage.Advance(tc.from, tc.to, actor, reason, ref, at); !errors.Is(err, outage.ErrPhaseTransition) {
				t.Fatalf("Advance error = %v, want ErrPhaseTransition", err)
			}
		})
	}
	got, err := outage.Advance(outage.PhaseSuspected, outage.PhaseNormal, "monitor", "probe recovered", "probe-2", continuityNow)
	if err != nil || got.At != continuityNow || got.From != outage.PhaseSuspected || got.To != outage.PhaseNormal {
		t.Fatalf("valid reverse transition = %+v err=%v", got, err)
	}
}

func TestDecideFailClosedAndRecoveryModes(t *testing.T) {
	p := continuityProfile()
	badRequests := []outage.Request{
		{Class: trustoutage.ClassExistingRead, SessionAge: -time.Second, Revocation: outage.RevocationConfirmed},
		{Class: trustoutage.ClassExistingRead, SessionAge: time.Minute, Revocation: outage.RevocationStatus("bad")},
		{Class: trustoutage.Class("bad"), Revocation: outage.RevocationConfirmed},
	}
	for i, req := range badRequests {
		if _, err := outage.Decide(p, outage.PhaseNormal, healthyHealth(), req, continuityNow); !errors.Is(err, outage.ErrInvalidRequest) {
			t.Fatalf("bad request %d error = %v, want ErrInvalidRequest", i, err)
		}
	}
	if _, err := outage.Decide(p, outage.Phase("UNKNOWN"), healthyHealth(), request(trustoutage.ClassExistingRead, time.Minute), continuityNow); !errors.Is(err, outage.ErrInvalidRequest) {
		t.Fatalf("unknown phase error = %v, want ErrInvalidRequest", err)
	}
	if _, err := outage.Decide(p, outage.PhaseNormal, healthyHealth(), request(trustoutage.ClassExistingRead, time.Minute), time.Time{}); !errors.Is(err, outage.ErrInvalidRequest) {
		t.Fatalf("zero decision time error = %v, want ErrInvalidRequest", err)
	}
	if got, err := outage.Decide(p, outage.PhaseSuspected, healthyHealth(), request(trustoutage.ClassExistingRead, time.Minute), continuityNow); err != nil || got.Allowed || got.Reason != "identity_outage_suspected" {
		t.Fatalf("suspected phase decision=%+v err=%v", got, err)
	}
	for _, phase := range []outage.Phase{outage.PhaseRecovering, outage.PhaseReconciling} {
		got, err := outage.Decide(p, phase, healthyHealth(), request(trustoutage.ClassExistingRead, time.Minute), continuityNow)
		if err != nil || !got.Allowed || got.Mode != outage.ModeReconciliation || !got.RequiresReconcile {
			t.Fatalf("%s decision=%+v err=%v, want reconciliation", phase, got, err)
		}
	}
}

func TestReconcileNoActionAndInputErrors(t *testing.T) {
	p := continuityProfile()
	uses := []outage.RecordedUse{{SessionRef: "normal", Mode: outage.ModeNormal, Revocation: outage.RevocationConfirmed, DecidedAt: continuityNow, EvidenceID: "e-normal"}}
	got, err := outage.Reconcile(p, healthyHealth(), uses, continuityNow)
	if err != nil || len(got) != 1 || got[0].Action != outage.ReconcileNoActionNeeded || got[0].EvidenceID != "e-normal" {
		t.Fatalf("no-action reconciliation=%+v err=%v", got, err)
	}
	if _, err := outage.Reconcile(p, healthyHealth(), []outage.RecordedUse{{Mode: outage.ModeCached}}, continuityNow); !errors.Is(err, outage.ErrInvalidRequest) {
		t.Fatalf("missing session reference = %v, want ErrInvalidRequest", err)
	}
	if _, err := outage.Reconcile(p, healthyHealth(), nil, time.Time{}); !errors.Is(err, outage.ErrRecoveryNotReady) {
		t.Fatalf("zero recovery time = %v, want ErrRecoveryNotReady", err)
	}
	bad := p
	bad.ManualEnabled, bad.MaxManualAge = true, 0
	if _, err := outage.Reconcile(bad, healthyHealth(), nil, continuityNow); !errors.Is(err, outage.ErrInvalidProfile) {
		t.Fatalf("invalid recovery profile = %v, want ErrInvalidProfile", err)
	}
}
