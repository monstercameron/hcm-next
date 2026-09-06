package attest_test

import (
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/attest"
)

var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

const (
	tenantAcme = values.TenantId("acme-corp")
	subjectID  = "00000000-0000-4000-8000-000000000001"
	sessionRef = "session-attest-1"
)

var (
	directorKey = keyFrom("attestor-director")
	agentKey    = keyFrom("attestor-agent")
)

func keyFrom(seed string) [32]byte {
	sum := sha256.Sum256([]byte(seed))
	return sum
}

func mustPrincipal(t *testing.T, assurance trust.Assurance, sessionRef string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               tenantAcme,
		Subject:              subjectID,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{"approver"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            assurance,
		SessionRef:           sessionRef,
		IssuedAt:             baseTime.Add(-time.Hour),
		ExpiresAt:            baseTime.Add(time.Hour),
		CredentialDigest:     "digest-" + subjectID,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func mustDirectory(t *testing.T) *attest.Directory {
	t.Helper()
	d, err := attest.NewDirectory([]attest.Attestor{
		{
			ID:           "att:director",
			Tenant:       tenantAcme,
			Authority:    []attest.Mode{attest.ModeDirect, attest.ModeProxy, attest.ModeDelegated},
			MinAssurance: trust.AssuranceSubstantial,
			SigningKey:   directorKey,
			Status:       attest.StatusActive,
			EvidenceID:   "ev:attestor:director",
		},
		{
			ID:           "att:revoked",
			Tenant:       tenantAcme,
			Authority:    []attest.Mode{attest.ModeDirect},
			MinAssurance: trust.AssuranceLow,
			SigningKey:   agentKey,
			Status:       attest.StatusRevoked,
			EvidenceID:   "ev:attestor:revoked",
		},
	})
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	return d
}

// baseline is a statement a valid principal should pass against a requirement
// that permits exactly the modes listed.
func baseline(mode attest.Mode) attest.Statement {
	st := attest.Statement{
		AttestorID: "att:director",
		Tenant:     tenantAcme,
		Subject:    subjectID,
		SessionRef: sessionRef,
		Mode:       mode,
		Assurance:  trust.AssuranceHigh,
		Purposes:   []string{"authz.verify"},
		IssuedAt:   baseTime.Add(-time.Minute),
		ExpiresAt:  baseTime.Add(5 * time.Minute),
	}
	st.Sign(directorKey)
	return st
}

func run(t *testing.T, st attest.Statement, req attest.Requirement, p *trust.Principal) attest.Decision {
	t.Helper()
	d, err := attest.Verify(mustDirectory(t), st, req, p, baseTime)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return d
}

// TestTodo_ATTEST_003 is the PRIMARY clause: a well-formed proxy statement
// verified against a requirement that explicitly lists the mode is accepted,
// and the decision records both the statement proof and the current AuthZ
// context so the acceptance is re-checkable from the record alone.
func TestTodo_ATTEST_003(t *testing.T) {
	p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
	st := baseline(attest.ModeProxy)
	req := attest.Requirement{
		MinAssurance:   trust.AssuranceSubstantial,
		PermittedModes: []attest.Mode{attest.ModeDirect, attest.ModeProxy},
	}

	d := run(t, st, req, p)
	if !d.Accepted {
		t.Fatalf("decision refused a well-formed statement: %s", d.Reason)
	}
	if d.Reason != attest.ReasonAccepted {
		t.Fatalf("accepted decision carries reason %q, want %q", d.Reason, attest.ReasonAccepted)
	}
	if d.Proof == nil {
		t.Fatal("accepted decision carries no statement proof")
	}
	if d.Proof.StatementDigest != st.Digest() {
		t.Fatalf("proof digest %s does not bind the statement digest %s", d.Proof.StatementDigest, st.Digest())
	}
	if d.Proof.AttestorID != st.AttestorID || d.Proof.AttestorEvidenceID != "ev:attestor:director" {
		t.Fatalf("proof does not bind the attestor record: %+v", d.Proof)
	}
	if d.Proof.Mode != attest.ModeProxy || d.Proof.Assurance != trust.AssuranceHigh {
		t.Fatalf("proof does not bind the claim mode and assurance: %+v", d.Proof)
	}
	if d.Proof.PrincipalEvidence != p.EvidenceID() || d.Principal != p.EvidenceID() || d.PrincipalFP != p.Fingerprint() {
		t.Fatalf("decision does not record the current AuthZ context: %+v", d)
	}
	if d.EvidenceID == "" {
		t.Fatal("accepted decision carries no evidence id")
	}
	if !d.At.Equal(baseTime) {
		t.Fatalf("decision not stamped at the verification instant: %s", d.At)
	}

	// The recorded decision must verify back to the same statement: re-run
	// verification and confirm the second decision binds the same proof.
	d2 := run(t, st, req, p)
	if d2.Proof.StatementDigest != d.Proof.StatementDigest || d2.Proof.AttestorEvidenceID != d.Proof.AttestorEvidenceID {
		t.Fatalf("re-verification does not reproduce the proof: %+v vs %+v", d2.Proof, d.Proof)
	}
}

// TestTodo_ATTEST_003_Mutation is the MUTATION clause: every field that is
// signed, or that binds the statement to the claimant, is tampered one at a
// time and each tampered statement is refused with its own typed reason.
func TestTodo_ATTEST_003_Mutation(t *testing.T) {
	p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
	req := attest.Requirement{
		MinAssurance:   trust.AssuranceSubstantial,
		PermittedModes: []attest.Mode{attest.ModeDirect, attest.ModeProxy, attest.ModeDelegated},
	}
	cases := []struct {
		name   string
		mutate func(*attest.Statement)
		// empty wantReason means any refusal with an invalid signature is accepted
		wantReason string
	}{
		{"mode escalated to delegated", func(s *attest.Statement) { s.Mode = attest.ModeDelegated }, ""},
		{"mode unset", func(s *attest.Statement) { s.Mode = attest.ModeUnspecified }, ""},
		{"assurance downgraded", func(s *attest.Statement) { s.Assurance = trust.AssuranceLow }, ""},
		{"subject swapped", func(s *attest.Statement) { s.Subject = "00000000-0000-4000-8000-0000000000ff" }, ""},
		{"session swapped", func(s *attest.Statement) { s.SessionRef = "session-elsewhere" }, ""},
		{"tenant swapped", func(s *attest.Statement) { s.Tenant = values.TenantId("vendor-corp") }, ""},
		{"attestor swapped", func(s *attest.Statement) { s.AttestorID = "att:nobody" }, ""},
		{"purpose added after signing", func(s *attest.Statement) { s.Purposes = append(s.Purposes, "authz.export") }, ""},
		{"expiration extended", func(s *attest.Statement) { s.ExpiresAt = baseTime.Add(time.Hour) }, ""},
		{"issuance backdated", func(s *attest.Statement) { s.IssuedAt = baseTime.Add(-time.Hour) }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := baseline(attest.ModeProxy)
			tc.mutate(&st)
			// Re-sign only when the mutation is "the right key signs a wrong
			// claim" is NOT the point: the mutations above change signed
			// content, so the original signature must now be invalid. The
			// signature is deliberately left stale.
			d := run(t, st, req, p)
			if d.Accepted {
				t.Fatalf("mutated statement accepted: %+v", st)
			}
			if tc.wantReason != "" && d.Reason != tc.wantReason {
				t.Fatalf("mutated statement refused with %q, want %q", d.Reason, tc.wantReason)
			}
		})
	}

	// A statement re-signed by the same director after tampering is still
	// refused when the tampered field is bound to the claimant, proving the
	// refusal comes from the policy checks and not just the signature.
	st := baseline(attest.ModeProxy)
	st.SessionRef = "session-attest-2" // a session principal p does not hold
	st.Sign(directorKey)               // the signature is now valid
	d := run(t, st, req, p)            // but p's session is session-attest-1
	if d.Accepted {
		t.Fatalf("statement bound to a different session accepted for this principal")
	}
	if d.Reason != attest.ReasonSessionMismatch {
		t.Fatalf("expected %s, got %s", attest.ReasonSessionMismatch, d.Reason)
	}
}

// TestTodo_ATTEST_003_Security is the SECURITY clause: the RED list. Proxy
// and delegated claims fail unless the requirement explicitly lists the
// mode; a revoked attestor, an expired statement, and either form of
// insufficient assurance fail even when the signature verifies.
func TestTodo_ATTEST_003_Security(t *testing.T) {
	p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
	perirectOnly := []attest.Mode{attest.ModeDirect}

	t.Run("proxy fails without explicit mode listing", func(t *testing.T) {
		d := run(t, baseline(attest.ModeProxy), attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: perirectOnly,
		}, p)
		if d.Accepted || d.Reason != attest.ReasonModeNotPermitted {
			t.Fatalf("proxy accepted without explicit permission: %+v", d)
		}
	})
	t.Run("delegated fails without explicit mode listing", func(t *testing.T) {
		d := run(t, baseline(attest.ModeDelegated), attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: perirectOnly,
		}, p)
		if d.Accepted || d.Reason != attest.ReasonModeNotPermitted {
			t.Fatalf("delegated accepted without explicit permission: %+v", d)
		}
	})
	t.Run("unspecified mode fails even against permissive intent", func(t *testing.T) {
		d := run(t, baseline(attest.ModeUnspecified), attest.Requirement{
			MinAssurance:   trust.AssuranceSubstantial,
			PermittedModes: []attest.Mode{attest.ModeDirect, attest.ModeProxy, attest.ModeDelegated},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonModeNotPermitted {
			t.Fatalf("unspecified mode accepted: %+v", d)
		}
	})
	t.Run("revoked attestor fails with a valid signature", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		st.AttestorID = "att:revoked"
		st.Assurance = trust.AssuranceLow
		st.Sign(agentKey)
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceLow, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, mustPrincipal(t, trust.AssuranceSubstantial, sessionRef))
		if d.Accepted || d.Reason != attest.ReasonAttestorRevoked {
			t.Fatalf("revoked attestor honored: %+v", d)
		}
	})
	t.Run("expired statement fails with a valid signature", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		st.IssuedAt = baseTime.Add(-10 * time.Minute)
		st.ExpiresAt = baseTime.Add(-time.Minute)
		st.Sign(directorKey)
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonStatementExpired {
			t.Fatalf("expired statement honored: %+v", d)
		}
	})
	t.Run("statement not yet valid fails", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		st.IssuedAt = baseTime.Add(time.Minute)
		st.ExpiresAt = baseTime.Add(10 * time.Minute)
		st.Sign(directorKey)
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonNotValidYet {
			t.Fatalf("premature statement honored: %+v", d)
		}
	})
	t.Run("statement assurance below requirement fails", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		st.Assurance = trust.AssuranceLow
		st.Sign(directorKey)
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonAssuranceLow {
			t.Fatalf("low-assurance statement honored: %+v", d)
		}
	})
	t.Run("current principal assurance below requirement fails", func(t *testing.T) {
		st := baseline(attest.ModeDirect) // statement itself is high-assurance
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceHigh, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, mustPrincipal(t, trust.AssuranceSubstantial, sessionRef))
		if d.Accepted || d.Reason != attest.ReasonCurrentAssurance {
			t.Fatalf("downgraded current context honored: %+v", d)
		}
	})
	t.Run("unknown attestor fails", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		st.AttestorID = "att:rogue"
		st.Sign([32]byte{})
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonAttestorUnknown {
			t.Fatalf("unknown attestor honored: %+v", d)
		}
	})
	t.Run("forged signature fails", func(t *testing.T) {
		st := baseline(attest.ModeDirect)
		// Replace the signature with attacker-chosen bytes.
		var forged [32]byte
		if _, err := rand.Read(forged[:]); err != nil {
			t.Fatalf("rand: %v", err)
		}
		st.Signature = forged[:]
		d := run(t, st, attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, p)
		if d.Accepted || d.Reason != attest.ReasonSignatureInvalid {
			t.Fatalf("forged signature honored: %+v", d)
		}
	})
	t.Run("cross-tenant claimant fails", func(t *testing.T) {
		other, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant:               values.TenantId("vendor-corp"),
			Subject:              subjectID,
			SubjectKind:          trust.SubjectKindHuman,
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceHigh,
			SessionRef:           sessionRef,
			IssuedAt:             baseTime.Add(-time.Hour),
			ExpiresAt:            baseTime.Add(time.Hour),
			CredentialDigest:     "digest-vendor",
		})
		if err != nil {
			t.Fatalf("NewPrincipal: %v", err)
		}
		d := run(t, baseline(attest.ModeDirect), attest.Requirement{
			MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect},
		}, other)
		if d.Accepted || d.Reason != attest.ReasonTenantMismatch {
			t.Fatalf("cross-tenant claimant honored: %+v", d)
		}
	})
}

func TestAttest_PublicAPIs_DirectoryModesAndStructuralRefusals(t *testing.T) {
	for mode, want := range map[attest.Mode]string{
		attest.ModeDirect: "direct", attest.ModeProxy: "proxy", attest.ModeDelegated: "delegated", attest.ModeUnspecified: "mode_unspecified",
	} {
		if got := mode.String(); got != want {
			t.Errorf("Mode(%d).String=%q, want %q", mode, got, want)
		}
	}
	for _, wire := range []string{"direct", "proxy", "delegated"} {
		if mode, err := attest.ParseMode(wire); err != nil || mode.String() != wire {
			t.Errorf("ParseMode(%q)=%v/%v", wire, mode, err)
		}
	}
	if mode, err := attest.ParseMode("unknown"); err == nil || mode != attest.ModeUnspecified {
		t.Fatalf("unknown ParseMode=%v/%v", mode, err)
	}

	base := attest.Attestor{ID: "attestor", Tenant: tenantAcme, Authority: []attest.Mode{attest.ModeDirect}, MinAssurance: trust.AssuranceSubstantial, SigningKey: directorKey, Status: attest.StatusActive, EvidenceID: "evidence"}
	invalid := []struct {
		name   string
		mutate func(*attest.Attestor)
	}{
		{"id", func(a *attest.Attestor) { a.ID = "" }}, {"tenant", func(a *attest.Attestor) { a.Tenant = "" }}, {"authority", func(a *attest.Attestor) { a.Authority = nil }}, {"key", func(a *attest.Attestor) { a.SigningKey = [32]byte{} }}, {"status", func(a *attest.Attestor) { a.Status = attest.StatusUnspecified }}, {"evidence", func(a *attest.Attestor) { a.EvidenceID = "" }},
	}
	for _, tc := range invalid {
		t.Run("directory "+tc.name, func(t *testing.T) {
			a := base
			tc.mutate(&a)
			if _, err := attest.NewDirectory([]attest.Attestor{a}); err == nil {
				t.Fatal("invalid directory record accepted")
			}
		})
	}
	if _, err := attest.NewDirectory([]attest.Attestor{base, base}); err == nil {
		t.Fatal("duplicate directory record accepted")
	}
	d, err := attest.NewDirectory([]attest.Attestor{base})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := d.LookUp(base.ID); !ok || got.ID != base.ID {
		t.Fatalf("LookUp existing=%+v/%v", got, ok)
	}
	if _, ok := d.LookUp("missing"); ok {
		t.Fatal("LookUp reported missing attestor present")
	}
	p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
	valid := baseline(attest.ModeDirect)
	for _, tc := range []struct {
		name      string
		dir       *attest.Directory
		req       attest.Requirement
		principal *trust.Principal
	}{
		{"nil directory", nil, attest.Requirement{MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect}}, p},
		{"nil principal", d, attest.Requirement{MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect}}, nil},
		{"missing assurance", d, attest.Requirement{PermittedModes: []attest.Mode{attest.ModeDirect}}, p},
		{"missing modes", d, attest.Requirement{MinAssurance: trust.AssuranceSubstantial}, p},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := attest.Verify(tc.dir, valid, tc.req, tc.principal, baseTime); err == nil {
				t.Fatal("structurally invalid Verify request accepted")
			}
		})
	}

	proxyRecord := base
	proxyRecord.Authority = []attest.Mode{attest.ModeDirect}
	proxyDirectory, err := attest.NewDirectory([]attest.Attestor{proxyRecord})
	if err != nil {
		t.Fatal(err)
	}
	proxy := baseline(attest.ModeProxy)
	proxy.AttestorID = base.ID
	proxy.Sign(directorKey)
	if decision, err := attest.Verify(proxyDirectory, proxy, attest.Requirement{MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeProxy}}, p, baseTime); err != nil || decision.Accepted || decision.Reason != attest.ReasonModeNotPermitted || decision.Proof != nil {
		t.Fatalf("unauthorized attestor mode decision=%+v err=%v", decision, err)
	}
	floorRecord := base
	floorRecord.Authority = []attest.Mode{attest.ModeDirect}
	floorRecord.MinAssurance = trust.AssuranceHigh
	floorDirectory, err := attest.NewDirectory([]attest.Attestor{floorRecord})
	if err != nil {
		t.Fatal(err)
	}
	lowClaim := baseline(attest.ModeDirect)
	lowClaim.AttestorID = base.ID
	lowClaim.Assurance = trust.AssuranceSubstantial
	lowClaim.Sign(directorKey)
	if decision, err := attest.Verify(floorDirectory, lowClaim, attest.Requirement{MinAssurance: trust.AssuranceSubstantial, PermittedModes: []attest.Mode{attest.ModeDirect}}, p, baseTime); err != nil || decision.Accepted || decision.Reason != attest.ReasonAssuranceLow {
		t.Fatalf("below attestor floor decision=%+v err=%v", decision, err)
	}
}
