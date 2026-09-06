// Package stepup produces and consumes short-lived step-up proofs for the
// sensitive operations AUTHN-005 covers - approving, repairing and exporting.
// A proof is a server-issued, signed binding of one principal, one session,
// one operation, one proposal and an explicit scope set to one assurance
// level; it expires quickly and consumes itself. The consumption is the
// single-use guarantee: a proof that has been consumed, whether by this
// process or any other over the same store, is refused on replay, and a
// verification whose commit outcome is ambiguous converges on retry to the
// replayed decision instead of executing the operation a second time.
//
// A bearer token authenticates a principal. It never authorizes a sensitive
// operation: the gate here takes only a step-up proof plus the current
// principal and there is no path by which a token alone reaches approval,
// repair or export.
package stepup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// Sensitive operation verbs a step-up proof binds.
const (
	ActionApprove = "workflow.approve"
	ActionRepair  = "data.repair"
	ActionExport  = "data.export"
)

// AllSensitiveActions is the closed verb set the gate knows.
var AllSensitiveActions = []string{ActionApprove, ActionRepair, ActionExport}

// Errors a gate or issuer returns. All are matchable with errors.Is.
var (
	ErrIssuerAssurance  = errors.New("stepup: principal assurance is below the requirement")
	ErrExpiredPrincipal = errors.New("stepup: principal credential is not current")
	ErrUnknownAction    = errors.New("stepup: operation action is outside the sensitive verb set")
	ErrProofDecoded     = errors.New("stepup: proof does not decode to a structurally valid proof")
	ErrProofSignature   = errors.New("stepup: proof signature does not verify")
	ErrProofBinding     = errors.New("stepup: proof binding does not match the operation or principal")
	ErrProofPremature   = errors.New("stepup: proof is not yet valid")
	ErrProofExpired     = errors.New("stepup: proof has expired")
	ErrProofStale       = errors.New("stepup: proof is older than the recency window")
	ErrAssuranceLow     = errors.New("stepup: step-up assurance is below the requirement")
	ErrCurrentAssurance = errors.New("stepup: current principal assurance is below the requirement")
	ErrSessionInactive  = errors.New("stepup: the bound session is not active")
)

// ErrAmbiguous is returned by a [ProofStore] when it cannot determine whether
// its own consumption committed. A gate seeing it must treat the proof as
// consumed and must not execute the operation, so that a retry converges to
// the replayed decision rather than a second execution.
var ErrAmbiguous = errors.New("stepup: consumption commit outcome is ambiguous")

// Operation is the sensitive operation a proof authorizes exactly one
// execution of.
//
// Purpose, Capability and Risk are the governance coordinates a step-up
// obligation is selected against (see [EvaluateObligation]). They are part of
// the proof binding, so a proof issued for a low-risk read of one capability
// cannot be presented for a high-risk write of another.
type Operation struct {
	Action     string
	ProposalID string
	Scopes     []string
	Tenant     values.TenantId
	Purpose    string
	Capability string
	Risk       Risk
}

// Requirement is the step-up policy a verification is checked against.
type Requirement struct {
	MinAssurance trust.Assurance
	Recency      time.Duration
}

// defaultLifetime is the validity of a proof the issuer grants. It is short
// on purpose: a step-up proof is a pass for one operation at one moment, not
// a credential.
const defaultLifetime = 5 * time.Minute

// Proof is the signed step-up statement. Every field except Signature is
// part of the signed digest.
type Proof struct {
	ID         string
	Tenant     values.TenantId
	Subject    string
	SessionRef string
	Assurance  trust.Assurance
	Action     string
	ProposalID string
	Scopes     []string
	Purpose    string
	Capability string
	Risk       Risk
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Signature  []byte
}

// ProofWire is the encoded form a proof travels in.
type ProofWire struct {
	ID         string   `json:"id"`
	Tenant     string   `json:"tenant"`
	Subject    string   `json:"subject"`
	Session    string   `json:"session"`
	Assurance  string   `json:"assurance"`
	Action     string   `json:"action"`
	Proposal   string   `json:"proposal"`
	Scopes     []string `json:"scopes"`
	Purpose    string   `json:"purpose,omitempty"`
	Capability string   `json:"capability,omitempty"`
	Risk       string   `json:"risk,omitempty"`
	IssuedAt   int64    `json:"iat"`
	ExpiresAt  int64    `json:"exp"`
	Signature  string   `json:"sig"`
}

// canonical returns the framed, length-prefixed serialization of every signed
// field so no field-boundary ambiguity can let two distinct proofs share a
// digest.
func (p Proof) canonical() string {
	scopes := append([]string(nil), p.Scopes...)
	sort.Strings(scopes)
	var b strings.Builder
	write := func(label, v string) {
		b.WriteString(label)
		b.WriteString("=")
		b.WriteString(fmt.Sprintf("%d:", len(v)))
		b.WriteString(v)
		b.WriteString(";")
	}
	write("id", p.ID)
	write("tenant", p.Tenant.String())
	write("subject", p.Subject)
	write("session", p.SessionRef)
	write("assurance", p.Assurance.String())
	write("action", p.Action)
	write("proposal", p.ProposalID)
	write("purpose", p.Purpose)
	write("capability", p.Capability)
	write("risk", p.Risk.String())
	for _, s := range scopes {
		b.WriteString("scope=")
		b.WriteString(fmt.Sprintf("%d:", len(s)))
		b.WriteString(s)
		b.WriteString(";")
	}
	b.WriteString("iat=")
	b.WriteString(fmt.Sprintf("%d", p.IssuedAt.UTC().UnixNano()))
	b.WriteString(";exp=")
	b.WriteString(fmt.Sprintf("%d", p.ExpiresAt.UTC().UnixNano()))
	return b.String()
}

// Digest returns the hex-encoded SHA-256 of the proof's canonical form.
func (p Proof) Digest() string {
	sum := sha256.Sum256([]byte(p.canonical()))
	return hex.EncodeToString(sum[:])
}

// Encode serializes the proof to its wire form.
func (p Proof) Encode() ([]byte, error) {
	data, err := json.Marshal(ProofWire{
		ID:         p.ID,
		Tenant:     p.Tenant.String(),
		Subject:    p.Subject,
		Session:    p.SessionRef,
		Assurance:  p.Assurance.String(),
		Action:     p.Action,
		Proposal:   p.ProposalID,
		Scopes:     p.Scopes,
		Purpose:    p.Purpose,
		Capability: p.Capability,
		Risk:       riskWire(p.Risk),
		IssuedAt:   p.IssuedAt.UTC().UnixNano(),
		ExpiresAt:  p.ExpiresAt.UTC().UnixNano(),
		Signature:  hex.EncodeToString(p.Signature),
	})
	return data, err
}

// DecodeProof parses a wire proof. It rejects anything that is not
// structurally a proof: wrong signature shape, unparseable assurance or
// tenant, inverted validity window.
func DecodeProof(data []byte) (Proof, error) {
	var w ProofWire
	if err := json.Unmarshal(data, &w); err != nil {
		return Proof{}, fmt.Errorf("%w: %v", ErrProofDecoded, err)
	}
	sig, err := hex.DecodeString(w.Signature)
	if err != nil || len(sig) != 32 {
		return Proof{}, fmt.Errorf("%w: signature is not 32 bytes", ErrProofDecoded)
	}
	as, err := parseAssurance(w.Assurance)
	if err != nil {
		return Proof{}, fmt.Errorf("%w: assurance %q", ErrProofDecoded, w.Assurance)
	}
	tenant := values.TenantId(w.Tenant)
	if err := tenant.Validate(); err != nil {
		return Proof{}, fmt.Errorf("%w: tenant %q", ErrProofDecoded, w.Tenant)
	}
	issued := time.Unix(0, w.IssuedAt).UTC()
	expires := time.Unix(0, w.ExpiresAt).UTC()
	if issued.IsZero() || expires.IsZero() || !expires.After(issued) {
		return Proof{}, fmt.Errorf("%w: inverted or zero validity window", ErrProofDecoded)
	}
	risk, err := parseRisk(w.Risk)
	if err != nil {
		return Proof{}, fmt.Errorf("%w: risk %q", ErrProofDecoded, w.Risk)
	}
	scopes := append([]string(nil), w.Scopes...)
	sort.Strings(scopes)
	return Proof{
		ID:         w.ID,
		Tenant:     tenant,
		Subject:    w.Subject,
		SessionRef: w.Session,
		Assurance:  as,
		Action:     w.Action,
		ProposalID: w.Proposal,
		Scopes:     scopes,
		Purpose:    w.Purpose,
		Capability: w.Capability,
		Risk:       risk,
		IssuedAt:   issued,
		ExpiresAt:  expires,
		Signature:  sig,
	}, nil
}

func parseAssurance(s string) (trust.Assurance, error) {
	for _, a := range []trust.Assurance{trust.AssuranceLow, trust.AssuranceSubstantial, trust.AssuranceHigh} {
		if a.String() == s {
			return a, nil
		}
	}
	return trust.AssuranceUnspecified, fmt.Errorf("unknown assurance %q", s)
}

// Issuer signs fresh step-up proofs under the issuer key.
type Issuer struct {
	key [32]byte
	now func() time.Time
	id  func(Proof) string
}

// NewIssuer builds an issuer with the signing key and a clock. The id
// generator assigns the proof identifier; the default derives it from the
// proof content so two issues of the same binding at different instants get
// different ids.
func NewIssuer(key [32]byte, now func() time.Time, id func(Proof) string) *Issuer {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if id == nil {
		id = func(p Proof) string {
			sum := sha256.Sum256([]byte("stepup:" + p.canonical()))
			return "sp:" + hex.EncodeToString(sum[:])
		}
	}
	return &Issuer{key: key, now: now, id: id}
}

// Issue produces the proof that authorizes op for the principal, when the
// principal's current credential and assurance satisfy the requirement. It
// refuses to issue for an action outside the sensitive verb set, so a step-up
// proof can never bind an arbitrary action string.
func (iss *Issuer) Issue(p *trust.Principal, op Operation, req Requirement) (Proof, error) {
	if !sensitiveAction(op.Action) {
		return Proof{}, fmt.Errorf("%w: %s", ErrUnknownAction, op.Action)
	}
	now := iss.now()
	if !p.ExpiresAt().After(now) {
		return Proof{}, fmt.Errorf("%w: expired %s", ErrExpiredPrincipal, p.ExpiresAt().UTC().Format(time.RFC3339))
	}
	if !p.Assurance().AtLeast(req.MinAssurance) {
		return Proof{}, ErrIssuerAssurance
	}
	proof := Proof{
		Tenant:     p.Tenant(),
		Subject:    p.Subject(),
		SessionRef: p.SessionRef(),
		Assurance:  p.Assurance(),
		Action:     op.Action,
		ProposalID: op.ProposalID,
		Scopes:     sortedCopy(op.Scopes),
		Purpose:    op.Purpose,
		Capability: op.Capability,
		Risk:       op.Risk,
		IssuedAt:   now,
		ExpiresAt:  now.Add(defaultLifetime),
	}
	proof.ID = iss.id(proof)
	proof.Sign(iss.key)
	return proof, nil
}

// Sign fills Signature with the HMAC-SHA256 of the proof digest under key.
// Tests use it to re-sign tampered proofs and prove a refusal came from the
// policy checks rather than the signature.
func (p *Proof) Sign(key [32]byte) {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(p.Digest()))
	p.Signature = mac.Sum(nil)
}

func (p Proof) verifies(key [32]byte) bool {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(p.Digest()))
	return subtle.ConstantTimeCompare(p.Signature, mac.Sum(nil)) == 1
}

func sensitiveAction(a string) bool {
	for _, v := range AllSensitiveActions {
		if a == v {
			return true
		}
	}
	return false
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// SessionChecker reports whether the server-side session a proof is bound to
// is still active at verification time.
type SessionChecker interface {
	Active(ctx context.Context, sessionRef string, at time.Time) (bool, error)
}

// StaticSession is a [SessionChecker] over a fixed set of active sessions.
// Tests and single-tenant deployments with an in-process session cache use it
// directly.
type StaticSession struct {
	mu     sync.Mutex
	active map[string]bool
}

// NewStaticSession builds a checker over the active session references.
func NewStaticSession(active ...string) *StaticSession {
	m := make(map[string]bool, len(active))
	for _, s := range active {
		m[s] = true
	}
	return &StaticSession{active: m}
}

// MarkInactive records a session as no longer active.
func (s *StaticSession) MarkInactive(sessionRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[sessionRef] = false
}

// Active implements [SessionChecker].
func (s *StaticSession) Active(_ context.Context, sessionRef string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[sessionRef], nil
}

// ProofStore consumes proofs and records that they were consumed. The store
// is the single-use authority: it must be safe for any number of gate
// instances to share one. Consume reports whether the proof had already been
// consumed (consumed=true) or was consumed now (consumed=false, err=nil). A
// store that cannot tell whether its own write committed returns
// (false, [ErrAmbiguous]).
type ProofStore interface {
	Consume(ctx context.Context, proofID, outcome string) (consumed bool, err error)
}

// MemoryStore is a [ProofStore] in process memory.
type MemoryStore struct {
	mu       sync.Mutex
	consumed map[string]string
}

// NewMemoryStore returns an empty in-memory proof store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{consumed: make(map[string]string)}
}

// Consume implements [ProofStore].
func (s *MemoryStore) Consume(_ context.Context, proofID, outcome string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.consumed[proofID]; ok {
		return true, nil
	}
	s.consumed[proofID] = outcome
	return false, nil
}

// Executor performs the sensitive operation. It is invoked at most once per
// proof: the gate consumes the proof before it invokes the executor, and a
// consumed proof is never verified a second time into an execution.
type Executor func(ctx context.Context, proof Proof, op Operation, p *trust.Principal) error

// Outcome is the gate's decision for a presented proof.
type Outcome string

// Gate outcomes.
const (
	OutcomeExecuted      Outcome = "executed"
	OutcomeReplayed      Outcome = "replayed"
	OutcomeUnknownCommit Outcome = "unknown_commit"
)

// Gate verifies proofs and executes the bound operation exactly once.
type Gate struct {
	key      [32]byte
	store    ProofStore
	sessions SessionChecker
	exec     Executor
	now      func() time.Time
}

// NewGate builds a gate over the consumption store, the session checker and
// the executor, signing under the same key as the [Issuer] that produced the
// proofs it verifies.
func NewGate(key [32]byte, store ProofStore, sessions SessionChecker, exec Executor, now func() time.Time) *Gate {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Gate{key: key, store: store, sessions: sessions, exec: exec, now: now}
}

// Present verifies proof against op and the current principal and, when the
// proof is authentic, current, sufficiently assured, still session-bound and
// unconsumed, consumes it and executes the operation once. The returned
// outcome is one of [OutcomeExecuted], [OutcomeReplayed] or
// [OutcomeUnknownCommit]; a non-nil error means the proof was refused
// outright and nothing was consumed or executed.
func (g *Gate) Present(ctx context.Context, proof Proof, op Operation, p *trust.Principal, req Requirement) (Outcome, error) {
	if !proof.verifies(g.key) {
		return "", ErrProofSignature
	}
	now := g.now()

	bindings := []struct {
		proof, want string
	}{
		{proof.Tenant.String(), op.Tenant.String()},
		{proof.Action, op.Action},
		{proof.ProposalID, op.ProposalID},
		{proof.Purpose, op.Purpose},
		{proof.Capability, op.Capability},
		{proof.Risk.String(), op.Risk.String()},
		{proof.Subject, p.Subject()},
		{proof.SessionRef, p.SessionRef()},
	}
	for _, b := range bindings {
		if b.proof != b.want {
			return "", ErrProofBinding
		}
	}
	if !equalScopes(proof.Scopes, op.Scopes) {
		return "", ErrProofBinding
	}

	if now.Before(proof.IssuedAt) {
		return "", ErrProofPremature
	}
	if !now.Before(proof.ExpiresAt) {
		return "", ErrProofExpired
	}
	if now.Sub(proof.IssuedAt) > req.Recency {
		return "", ErrProofStale
	}
	if !proof.Assurance.AtLeast(req.MinAssurance) {
		return "", ErrAssuranceLow
	}
	if !p.Assurance().AtLeast(req.MinAssurance) {
		return "", ErrCurrentAssurance
	}

	active, err := g.sessions.Active(ctx, proof.SessionRef, now)
	if err != nil {
		return "", err
	}
	if !active {
		return "", ErrSessionInactive
	}

	consumed, err := g.store.Consume(ctx, proof.ID, string(OutcomeExecuted))
	if err == ErrAmbiguous {
		// The proof may already be consumed; assume the conservative reading
		// and execute nothing. A retry converges on [OutcomeReplayed].
		return OutcomeUnknownCommit, nil
	}
	if err != nil {
		return "", err
	}
	if consumed {
		return OutcomeReplayed, nil
	}

	if err := g.exec(ctx, proof, op, p); err != nil {
		return "", err
	}
	return OutcomeExecuted, nil
}

func equalScopes(a, b []string) bool {
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
