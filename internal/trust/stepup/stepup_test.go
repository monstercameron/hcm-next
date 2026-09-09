package stepup_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

const (
	tenantAcme = values.TenantId("acme-corp")
	subjectID  = "00000000-0000-4000-8000-000000000001"
	sessionRef = "session-stepup-1"
	proposalID = "00000000-0000-4000-8000-000000000007"
)

var testScopes = []string{"worker:00000000-0000-4000-8000-000000000001", "payroll:2026-09"}

func keyFrom(seed string) [32]byte {
	sum := sha256.Sum256([]byte(seed))
	return sum
}

func mustPrincipal(t testing.TB, assurance trust.Assurance, sessionRef string) *trust.Principal {
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

type execCapture struct {
	mu     sync.Mutex
	calls  int
	last   stepup.Proof
	lastOp stepup.Operation
}

func (c *execCapture) run(_ context.Context, proof stepup.Proof, op stepup.Operation, _ *trust.Principal) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = proof
	c.lastOp = op
	return nil
}

func (c *execCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type fixture struct {
	key      [32]byte
	issuer   *stepup.Issuer
	gate     *stepup.Gate
	store    *stepup.MemoryStore
	sessions *stepup.StaticSession
	exec     *execCapture
	req      stepup.Requirement
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key := keyFrom("stepup-issuer")
	store := stepup.NewMemoryStore()
	sessions := stepup.NewStaticSession(sessionRef)
	exec := &execCapture{}
	iss := stepup.NewIssuer(key, func() time.Time { return baseTime }, nil)
	gate := stepup.NewGate(key, store, sessions, exec.run, func() time.Time { return baseTime })
	return &fixture{
		key:      key,
		issuer:   iss,
		gate:     gate,
		store:    store,
		sessions: sessions,
		exec:     exec,
		req:      stepup.Requirement{MinAssurance: trust.AssuranceSubstantial, Recency: 10 * time.Minute},
	}
}

func highPrincipal(t testing.TB) *trust.Principal {
	return mustPrincipal(t, trust.AssuranceHigh, sessionRef)
}

func opFor(action string) stepup.Operation {
	return stepup.Operation{Action: action, ProposalID: proposalID, Scopes: testScopes, Tenant: tenantAcme}
}

// TestTodo_AUTHN_005 is the PRIMARY clause: an issued, signed proof binds one
// principal, one session, one action, one proposal and one scope set; it
// executes the bound operation exactly once, and the consumption recorded in
// the shared store refuses every later presentation.
func TestTodo_AUTHN_005(t *testing.T) {
	fx := newFixture(t)
	fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	out, err := fx.gate.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("Present: %v", err)
	}
	if out != stepup.OutcomeExecuted {
		t.Fatalf("outcome %s, want executed", out)
	}
	if fx.exec.count() != 1 {
		t.Fatalf("executor ran %d times, want 1", fx.exec.count())
	}
	// The decision binds the exact operation and the exact identity.
	if fx.exec.last.Action != stepup.ActionApprove || fx.exec.last.Action != fx.exec.lastOp.Action {
		t.Fatalf("executed action not bound: %+v / %+v", fx.exec.last, fx.exec.lastOp)
	}
	if fx.exec.last.ProposalID != proposalID || fx.exec.last.SessionRef != sessionRef || fx.exec.last.Subject != subjectID {
		t.Fatalf("proof binding does not carry the identity it executed for: %+v", fx.exec.last)
	}
	if len(fx.exec.last.Scopes) != len(testScopes) {
		t.Fatalf("proof binding lost scope set: %+v", fx.exec.last.Scopes)
	}

	replay, err := fx.gate.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("replay Present: %v", err)
	}
	if replay != stepup.OutcomeReplayed {
		t.Fatalf("replay outcome %s, want replayed", replay)
	}
	if fx.exec.count() != 1 {
		t.Fatalf("replay executed the operation again: %d calls", fx.exec.count())
	}
}

// TestTodo_AUTHN_005_Mutation is the MUTATION clause: every signed field is
// tampered. A tamper without re-sign fails the signature; a tamper the
// attacker re-signs with the issuer key fails the specific policy check -
// the refusal comes from the binding, not from the signature alone.
func TestTodo_AUTHN_005_Mutation(t *testing.T) {
	fx := newFixture(t)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Without re-signing, every field tamper fails the signature.
	unsigned := []struct {
		name   string
		mutate func(*stepup.Proof)
	}{
		{"tenant", func(pr *stepup.Proof) { pr.Tenant = values.TenantId("vendor-corp") }},
		{"subject", func(pr *stepup.Proof) { pr.Subject = "00000000-0000-4000-8000-0000000000ff" }},
		{"session", func(pr *stepup.Proof) { pr.SessionRef = "session-other" }},
		{"action", func(pr *stepup.Proof) { pr.Action = stepup.ActionExport }},
		{"proposal", func(pr *stepup.Proof) { pr.ProposalID = "other-proposal" }},
		{"scopes", func(pr *stepup.Proof) { pr.Scopes = append(pr.Scopes, "extra:scope") }},
		{"assurance", func(pr *stepup.Proof) { pr.Assurance = trust.AssuranceLow }},
		{"issued", func(pr *stepup.Proof) { pr.IssuedAt = baseTime.Add(time.Minute) }},
		{"expiry", func(pr *stepup.Proof) { pr.ExpiresAt = baseTime.Add(2 * time.Hour) }},
		{"id", func(pr *stepup.Proof) { pr.ID = "sp:attacker" }},
	}
	for _, tc := range unsigned {
		t.Run("unsigned/"+tc.name, func(t *testing.T) {
			q := p
			tc.mutate(&q)
			_, err := fx.gate.Present(context.Background(), q, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
			if err != stepup.ErrProofSignature {
				t.Fatalf("unsigned tamper %s: got %v, want ErrProofSignature", tc.name, err)
			}
		})
	}

	// Re-signed tampers: the signature is now valid, so only the policy
	// checks can refuse, and each named one must.
	resigned := []struct {
		name   string
		mutate func(*stepup.Proof)
		want   error
	}{
		{"action", func(pr *stepup.Proof) { pr.Action = stepup.ActionRepair }, stepup.ErrProofBinding},
		{"proposal", func(pr *stepup.Proof) { pr.ProposalID = "other-proposal" }, stepup.ErrProofBinding},
		{"scopes", func(pr *stepup.Proof) { pr.Scopes = append(pr.Scopes, "extra:scope") }, stepup.ErrProofBinding},
		{"subject", func(pr *stepup.Proof) { pr.Subject = "00000000-0000-4000-8000-0000000000ff" }, stepup.ErrProofBinding},
		{"session", func(pr *stepup.Proof) { pr.SessionRef = "session-other" }, stepup.ErrProofBinding},
		{"tenant", func(pr *stepup.Proof) { pr.Tenant = values.TenantId("vendor-corp") }, stepup.ErrProofBinding},
		{"assurance", func(pr *stepup.Proof) { pr.Assurance = trust.AssuranceLow }, stepup.ErrAssuranceLow},
		{"premature", func(pr *stepup.Proof) {
			pr.IssuedAt = baseTime.Add(time.Hour)
			pr.ExpiresAt = baseTime.Add(2 * time.Hour)
		}, stepup.ErrProofPremature},
		{"expired", func(pr *stepup.Proof) {
			pr.IssuedAt = baseTime.Add(-10 * time.Minute)
			pr.ExpiresAt = baseTime.Add(-time.Minute)
		}, stepup.ErrProofExpired},
		{"stale", func(pr *stepup.Proof) {
			pr.IssuedAt = baseTime.Add(-19 * time.Minute)
			pr.ExpiresAt = baseTime.Add(time.Minute)
		}, stepup.ErrProofStale},
	}
	for _, tc := range resigned {
		t.Run("resigned/"+tc.name, func(t *testing.T) {
			q := p
			tc.mutate(&q)
			q.Sign(fx.key)
			_, err := fx.gate.Present(context.Background(), q, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
			if err != tc.want {
				t.Fatalf("resigned tamper %s: got %v, want %v", tc.name, err, tc.want)
			}
		})
	}

	// No rejection above consumed the proof: the original id is still
	// unconsumed in the store.
	consumed, err := fx.store.Consume(context.Background(), p.ID, "probe")
	if err != nil {
		t.Fatalf("store probe: %v", err)
	}
	if consumed {
		t.Fatalf("a rejected presentation consumed the proof")
	}
	_ = consumed
}

// TestTodo_AUTHN_005_Security is the SECURITY clause: the RED list. A stolen
// bearer token authenticates a principal, but it carries no step-up proof, so
// it can neither approve, repair nor export; a proof issued for one
// sensitive operation is unusable for any other; and a proof presented by a
// downgraded or logged-out context is refused.
func TestTodo_AUTHN_005_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("a stolen bearer token cannot approve, repair or export", func(t *testing.T) {
		fx := newFixture(t)
		// The principal itself is legitimate: the token verifies, the
		// session is live, the assurance is high. What is missing is the
		// step-up proof, and that is what the gate requires.
		p := highPrincipal(t)
		var forged stepup.Proof // zero value: no proof at all
		for _, action := range stepup.AllSensitiveActions {
			_, err := fx.gate.Present(ctx, forged, opFor(action), p, fx.req)
			if err == nil || err != stepup.ErrProofSignature {
				t.Fatalf("action %s: token without a proof got %v, want refusal", action, err)
			}
		}
		if fx.exec.count() != 0 {
			t.Fatalf("the sensitive operation ran %d times without a step-up proof", fx.exec.count())
		}
	})

	t.Run("a proof for one operation is unusable for the others", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		for _, other := range []string{stepup.ActionRepair, stepup.ActionExport} {
			if _, err := fx.gate.Present(ctx, p, opFor(other), highPrincipal(t), fx.req); err != stepup.ErrProofBinding {
				t.Fatalf("proof for approve used for %s: got %v, want ErrProofBinding", other, err)
			}
		}
		if fx.exec.count() != 0 {
			t.Fatal("an off-binding operation was executed")
		}
	})

	t.Run("a proof for export cannot approve", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionExport), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if _, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); err != stepup.ErrProofBinding {
			t.Fatalf("got %v, want ErrProofBinding", err)
		}
	})

	t.Run("a forged signature is refused", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		p.Sign(keyFrom("stepup-attacker"))
		if _, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); err != stepup.ErrProofSignature {
			t.Fatalf("got %v, want ErrProofSignature", err)
		}
	})

	t.Run("a downgraded current context is refused", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		// The same subject and session, re-authenticated at a lower
		// assurance: the proof still says high, the current context does
		// not.
		presented, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant:               tenantAcme,
			Subject:              subjectID,
			SubjectKind:          trust.SubjectKindHuman,
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceLow,
			SessionRef:           sessionRef,
			IssuedAt:             baseTime.Add(-time.Hour),
			ExpiresAt:            baseTime.Add(time.Hour),
			CredentialDigest:     "digest-downgraded",
		})
		if err != nil {
			t.Fatalf("NewPrincipal: %v", err)
		}
		if _, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), presented, fx.req); err != stepup.ErrCurrentAssurance {
			t.Fatalf("got %v, want ErrCurrentAssurance", err)
		}
	})

	t.Run("a logged-out session is refused", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		fx.sessions.MarkInactive(sessionRef)
		if _, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); err != stepup.ErrSessionInactive {
			t.Fatalf("got %v, want ErrSessionInactive", err)
		}
	})

	t.Run("replay never re-executes", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if _, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); err != nil {
			t.Fatalf("Present: %v", err)
		}
		for i := 0; i < 3; i++ {
			out, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
			if err != nil || out != stepup.OutcomeReplayed {
				t.Fatalf("replay %d: %v %s", i, err, out)
			}
		}
		if fx.exec.count() != 1 {
			t.Fatalf("executor ran %d times", fx.exec.count())
		}
	})
}

// flakyStore simulates a consumption whose commit outcome was lost in transit:
// the underlying store commits the row, but the first call reports the
// ambiguity instead of the fresh-consumption answer.
type flakyStore struct {
	inner  stepup.ProofStore
	mu     sync.Mutex
	failed bool
}

func (f *flakyStore) Consume(ctx context.Context, id, outcome string) (bool, error) {
	consumed, err := f.inner.Consume(ctx, id, outcome)
	if consumed || err != nil {
		return consumed, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.failed {
		f.failed = true
		return false, stepup.ErrAmbiguous
	}
	return false, nil
}

// TestTodo_AUTHN_005_Recovery is the RECOVERY clause: a gate whose
// consumption commit comes back ambiguous executes nothing and reports
// unknown_commit; the retry converges to replayed; and an executor failure
// burns the proof so the operation cannot run twice.
func TestTodo_AUTHN_005_Recovery(t *testing.T) {
	ctx := context.Background()

	t.Run("an ambiguous commit never executes twice", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		g := stepup.NewGate(fx.key, &flakyStore{inner: fx.store}, fx.sessions, fx.exec.run, func() time.Time { return baseTime })

		out, err := g.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
		if err != nil {
			t.Fatalf("ambiguous Present: %v", err)
		}
		if out != stepup.OutcomeUnknownCommit {
			t.Fatalf("ambiguous commit reported %s, want unknown_commit", out)
		}
		if fx.exec.count() != 0 {
			t.Fatalf("the operation ran %d times on an ambiguous commit", fx.exec.count())
		}

		// The retry sees the committed consumption and converges.
		out, err = g.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
		if err != nil {
			t.Fatalf("retry Present: %v", err)
		}
		if out != stepup.OutcomeReplayed {
			t.Fatalf("retry reported %s, want replayed", out)
		}
		if fx.exec.count() != 0 {
			t.Fatalf("the operation ran %d times after recovery", fx.exec.count())
		}
	})

	t.Run("an executor failure burns the proof", func(t *testing.T) {
		fx := newFixture(t)
		p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		failOnce := &struct {
			mu    sync.Mutex
			calls int
			inner *execCapture
		}{inner: fx.exec}
		g := stepup.NewGate(fx.key, fx.store, fx.sessions, func(context.Context, stepup.Proof, stepup.Operation, *trust.Principal) error {
			failOnce.mu.Lock()
			failOnce.calls++
			failOnce.mu.Unlock()
			fx.exec.run(context.Background(), stepup.Proof{}, stepup.Operation{}, nil)
			return context.DeadlineExceeded
		}, func() time.Time { return baseTime })

		if _, err := g.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); err == nil {
			t.Fatal("an executor failure was reported as success")
		}
		out, err := fx.gate.Present(ctx, p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
		if err != nil || out != stepup.OutcomeReplayed {
			t.Fatalf("after a failed execution the proof is still usable: %v %s", err, out)
		}
		failOnce.mu.Lock()
		calls := failOnce.calls
		failOnce.mu.Unlock()
		if calls != 1 {
			t.Fatalf("the operation was attempted %d times, want exactly one", calls)
		}
	})
}

// FuzzTodo_AUTHN_005 is the FUZZ clause: arbitrary byte strings are treated
// as candidate wire proofs. Decoding and presenting them must never panic,
// only structurally valid and correctly signed proofs may reach an outcome,
// and the executor must run at most once per unique proof id.
func FuzzTodo_AUTHN_005(f *testing.F) {
	key := keyFrom("stepup-issuer")
	req := stepup.Requirement{MinAssurance: trust.AssuranceSubstantial, Recency: 10 * time.Minute}
	iss := stepup.NewIssuer(key, func() time.Time { return baseTime }, nil)
	seed, err := iss.Issue(highPrincipal(f), opFor(stepup.ActionApprove), req)
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}
	wire, err := seed.Encode()
	if err != nil {
		f.Fatalf("Encode: %v", err)
	}
	f.Add(wire)

	f.Fuzz(func(t *testing.T, data []byte) {
		exec := &execCapture{}
		gate := stepup.NewGate(
			key,
			stepup.NewMemoryStore(),
			stepup.NewStaticSession(sessionRef),
			exec.run,
			func() time.Time { return baseTime },
		)
		proof, err := stepup.DecodeProof(data)
		if err != nil {
			return
		}
		out, err := gate.Present(context.Background(), proof, opFor(stepup.ActionApprove), highPrincipal(t), req)
		if err == nil {
			switch out {
			case stepup.OutcomeExecuted, stepup.OutcomeReplayed, stepup.OutcomeUnknownCommit:
			default:
				t.Fatalf("unrecognized outcome %s", out)
			}
		}
		if exec.count() > 1 {
			t.Fatalf("the executor ran %d times for one presentation", exec.count())
		}
	})
}

func TestIssuerAndGate_MalformedPrincipalFailsClosed(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.issuer.Issue(nil, opFor(stepup.ActionApprove), fx.req); !errors.Is(err, stepup.ErrIssuerAssurance) {
		t.Fatalf("Issue(nil) = %v, want ErrIssuerAssurance", err)
	}
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := fx.gate.Present(context.Background(), p, opFor(stepup.ActionApprove), nil, fx.req); !errors.Is(err, stepup.ErrProofBinding) {
		t.Fatalf("Present(nil principal) = %v, want ErrProofBinding", err)
	}
	if fx.exec.count() != 0 {
		t.Fatalf("malformed principal reached executor %d times", fx.exec.count())
	}
}

func TestProofWire_RejectsMalformedInputAndPreservesCanonicalScopes(t *testing.T) {
	fx := newFixture(t)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	reordered := p
	reordered.Scopes = append([]string(nil), p.Scopes...)
	for i, j := 0, len(reordered.Scopes)-1; i < j; i, j = i+1, j-1 {
		reordered.Scopes[i], reordered.Scopes[j] = reordered.Scopes[j], reordered.Scopes[i]
	}
	if reordered.Digest() != p.Digest() {
		t.Fatal("scope ordering changed the canonical proof digest")
	}
	wire, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := stepup.DecodeProof(wire)
	if err != nil || decoded.Digest() != p.Digest() || !decoded.IssuedAt.Equal(p.IssuedAt) || !decoded.ExpiresAt.Equal(p.ExpiresAt) {
		t.Fatalf("DecodeProof = %+v, err=%v, want a digest-preserving round trip", decoded, err)
	}

	base := map[string]any{
		"id": "sp:test", "tenant": string(tenantAcme), "subject": subjectID, "session": sessionRef,
		"assurance": "high", "action": stepup.ActionApprove, "proposal": proposalID, "scopes": testScopes,
		"iat": baseTime.UnixNano(), "exp": baseTime.Add(time.Minute).UnixNano(), "sig": "0000000000000000000000000000000000000000000000000000000000000000",
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"invalid json", nil},
		{"invalid signature hex", func(m map[string]any) { m["sig"] = "not-hex" }},
		{"wrong signature length", func(m map[string]any) { m["sig"] = "00" }},
		{"unknown assurance", func(m map[string]any) { m["assurance"] = "root" }},
		{"invalid tenant", func(m map[string]any) { m["tenant"] = "Tenant" }},
		{"missing expiry window", func(m map[string]any) { m["exp"] = int64(0) }},
		{"inverted window", func(m map[string]any) { m["exp"] = baseTime.Add(-time.Minute).UnixNano() }},
		{"unknown risk", func(m map[string]any) { m["risk"] = "critical-ish" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data []byte
			if tc.mutate == nil {
				data = []byte("{")
			} else {
				candidate := make(map[string]any, len(base))
				for k, v := range base {
					candidate[k] = v
				}
				tc.mutate(candidate)
				var err error
				data, err = json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := stepup.DecodeProof(data); !errors.Is(err, stepup.ErrProofDecoded) {
				t.Fatalf("DecodeProof error = %v, want ErrProofDecoded", err)
			}
		})
	}
	base["risk"] = ""
	data, _ := json.Marshal(base)
	decoded, err = stepup.DecodeProof(data)
	if err != nil || decoded.Risk != stepup.RiskUnspecified {
		t.Fatalf("empty risk decode = %+v, err=%v, want unspecified risk", decoded, err)
	}
}

func TestIssuerAndMemoryAuthorities_EnforceInputs(t *testing.T) {
	key := keyFrom("issuer")
	issuer := stepup.NewIssuer(key, func() time.Time { return baseTime }, func(stepup.Proof) string { return "sp:custom" })
	p := highPrincipal(t)
	proof, err := issuer.Issue(p, opFor(stepup.ActionApprove), stepup.Requirement{MinAssurance: trust.AssuranceSubstantial, Recency: time.Minute})
	if err != nil || proof.ID != "sp:custom" || len(proof.Signature) != 32 {
		t.Fatalf("custom issuer output = %+v, err=%v", proof, err)
	}
	for _, action := range []string{"", "ordinary.read"} {
		if _, err := issuer.Issue(p, opFor(action), fxReq()); !errors.Is(err, stepup.ErrUnknownAction) {
			t.Fatalf("Issue(%q) = %v, want ErrUnknownAction", action, err)
		}
	}
	if _, err := issuer.Issue(mustPrincipal(t, trust.AssuranceSubstantial, sessionRef), opFor(stepup.ActionApprove), stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute}); !errors.Is(err, stepup.ErrIssuerAssurance) {
		t.Fatalf("insufficient requirement = %v, want ErrIssuerAssurance", err)
	}
	expired, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenantAcme, Subject: subjectID, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: sessionRef, IssuedAt: baseTime.Add(-time.Hour), ExpiresAt: baseTime,
		CredentialDigest: "expired-digest",
	})
	if err != nil {
		t.Fatalf("expired principal fixture: %v", err)
	}
	if _, err := issuer.Issue(expired, opFor(stepup.ActionApprove), fxReq()); !errors.Is(err, stepup.ErrExpiredPrincipal) {
		t.Fatalf("expired principal Issue = %v, want ErrExpiredPrincipal", err)
	}

	sessions := stepup.NewStaticSession("live")
	if active, err := sessions.Active(context.Background(), "live", baseTime); err != nil || !active {
		t.Fatalf("live session = %v, %v", active, err)
	}
	if active, err := sessions.Active(context.Background(), "missing", baseTime); err != nil || active {
		t.Fatalf("missing session = %v, %v, want inactive", active, err)
	}
	sessions.MarkInactive("live")
	if active, _ := sessions.Active(context.Background(), "live", baseTime); active {
		t.Fatal("MarkInactive left session active")
	}
	store := stepup.NewMemoryStore()
	if consumed, err := store.Consume(context.Background(), "proof", "executed"); err != nil || consumed {
		t.Fatalf("first memory consume = %v, %v", consumed, err)
	}
	if consumed, err := store.Consume(context.Background(), "proof", "executed"); err != nil || !consumed {
		t.Fatalf("replayed memory consume = %v, %v", consumed, err)
	}
}

func fxReq() stepup.Requirement {
	return stepup.Requirement{MinAssurance: trust.AssuranceSubstantial, Recency: time.Minute}
}

type errorSessionChecker struct{ err error }

func (s errorSessionChecker) Active(context.Context, string, time.Time) (bool, error) {
	return false, s.err
}

type errorProofStore struct{ err error }

func (s errorProofStore) Consume(context.Context, string, string) (bool, error) { return false, s.err }

func TestGate_PreservesAuthorityErrorsAndExecutesUnrequiredOperations(t *testing.T) {
	fx := newFixture(t)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	sessionErr := errors.New("session authority unavailable")
	g := stepup.NewGate(fx.key, fx.store, errorSessionChecker{err: sessionErr}, fx.exec.run, func() time.Time { return baseTime })
	if _, err := g.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); !errors.Is(err, sessionErr) {
		t.Fatalf("session checker error = %v, want %v", err, sessionErr)
	}
	storeErr := errors.New("consumption authority unavailable")
	g = stepup.NewGate(fx.key, errorProofStore{err: storeErr}, fx.sessions, fx.exec.run, func() time.Time { return baseTime })
	if _, err := g.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req); !errors.Is(err, storeErr) {
		t.Fatalf("proof store error = %v, want %v", err, storeErr)
	}
	if fx.exec.count() != 0 {
		t.Fatalf("authority errors reached executor %d times", fx.exec.count())
	}

	op := opFor(stepup.ActionApprove)
	op.Purpose, op.Capability, op.Risk = stepup.PurposeHCMOperations, "ordinary.read", stepup.RiskRoutine
	proof, err := fx.issuer.Issue(highPrincipal(t), op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
	if err != nil {
		t.Fatalf("Issue unrequired operation: %v", err)
	}
	out, ob, err := fx.gate.PresentUnderObligation(context.Background(), stepup.DefaultObligationPolicy(), proof, op, highPrincipal(t), stepup.ObligationRequest{At: baseTime})
	if err != nil || out != stepup.OutcomeExecuted || ob.Required || !ob.Satisfied || fx.exec.count() != 1 {
		t.Fatalf("unrequired gated operation = out=%s ob=%+v err=%v calls=%d", out, ob, err, fx.exec.count())
	}
}
