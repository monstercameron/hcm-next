package jit

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var baseNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func validRequest() Request {
	return Request{
		Principal:     "alice",
		Tenant:        values.TenantId("tenant-1"),
		Role:          RoleSupportReadOnly,
		TicketRef:     "INC-100",
		Justification: "customer requested read-only diagnostics",
		Capabilities:  []string{"support.read_diagnostics"},
		Purpose:       "hcm_operations",
		TTL:           2 * time.Hour,
	}
}

func validApproval() Approval {
	return Approval{Approver: "bob", At: baseNow}
}

// TestTodo_TRUST_021 is the primary acceptance test: a standing broad role,
// a missing ticket/justification, an expired grant, and an unrecorded action
// are all denied, while a properly bound elevation grants, is used, and
// expires with evidence at every step.
func TestTodo_TRUST_021(t *testing.T) {
	g, err := New("grant-1", validRequest(), validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Principal != "alice" || g.Approver != "bob" || g.Role != RoleSupportReadOnly {
		t.Fatalf("grant fields not bound as requested: %+v", g)
	}
	if !g.IsActive(baseNow.Add(time.Minute)) {
		t.Fatalf("freshly issued grant must be active")
	}

	if err := g.Use("read diagnostics for INC-100", baseNow.Add(time.Minute)); err != nil {
		t.Fatalf("Use: %v", err)
	}

	// Expiry: automatic once TTL elapses, with no explicit revoke call.
	afterExpiry := baseNow.Add(3 * time.Hour)
	if g.IsActive(afterExpiry) {
		t.Fatalf("grant must not be active past its ttl")
	}
	if err := g.Use("late action", afterExpiry); !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("Use after expiry: err = %v, want ErrGrantExpired", err)
	}

	ev := g.Evidence()
	kinds := map[EvidenceKind]bool{}
	for _, e := range ev {
		kinds[e.Kind] = true
	}
	for _, want := range []EvidenceKind{EvidenceGranted, EvidenceUsed, EvidenceExpired} {
		if !kinds[want] {
			t.Fatalf("evidence missing kind %s: %+v", want, ev)
		}
	}

	// RED: standing broad role is not in the closed vocabulary.
	standing := validRequest()
	standing.Role = Role("STANDING_ADMIN")
	if _, err := New("grant-2", standing, validApproval(), baseNow); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: err = %v, want ErrUnknownRole", err)
	}

	// RED: missing ticket/justification is denied.
	noTicket := validRequest()
	noTicket.TicketRef = ""
	if _, err := New("grant-3", noTicket, validApproval(), baseNow); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing ticket: err = %v, want ErrInvalidRequest", err)
	}
	noJustification := validRequest()
	noJustification.Justification = "  "
	if _, err := New("grant-4", noJustification, validApproval(), baseNow); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing justification: err = %v, want ErrInvalidRequest", err)
	}

	// RED: an expired grant denies use (covered above); a revoked grant
	// denies use too.
	g2, err := New("grant-5", validRequest(), validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := g2.Revoke("security-team", "ticket closed early", baseNow.Add(time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := g2.Use("anything", baseNow.Add(2*time.Minute)); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("Use after revoke: err = %v, want ErrGrantRevoked", err)
	}
	revokedAt, by, reason, revoked := g2.Revocation()
	if !revoked || by != "security-team" || reason != "ticket closed early" || revokedAt.IsZero() {
		t.Fatalf("revocation not recorded: at=%v by=%v reason=%v revoked=%v", revokedAt, by, reason, revoked)
	}
}

// TestTodo_TRUST_021_Security asserts the grant path cannot be used to
// create a self-approved or otherwise standing elevation: approver identity
// distinct from requester, capabilities/purpose non-empty, and a TTL that
// is always bounded are all enforced regardless of who is asking.
func TestTodo_TRUST_021_Security(t *testing.T) {
	// Self-approval is refused even with different letter casing/whitespace.
	selfApproved := []Approval{
		{Approver: "alice", At: baseNow},
		{Approver: "ALICE", At: baseNow},
		{Approver: " alice ", At: baseNow},
	}
	for _, a := range selfApproved {
		if _, err := New("grant-self", validRequest(), a, baseNow); !errors.Is(err, ErrApproverIsRequester) {
			t.Fatalf("self approval %q: err = %v, want ErrApproverIsRequester", a.Approver, err)
		}
	}

	// No capabilities named: a grant cannot authorize "whatever the role
	// can do" with an empty capability list.
	noCaps := validRequest()
	noCaps.Capabilities = nil
	if _, err := New("grant-nocaps", noCaps, validApproval(), baseNow); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("no capabilities: err = %v, want ErrInvalidRequest", err)
	}

	// There is no standing grant path: TTL of zero is refused outright, not
	// defaulted to the role maximum.
	noTTL := validRequest()
	noTTL.TTL = 0
	if _, err := New("grant-nottl", noTTL, validApproval(), baseNow); !errors.Is(err, ErrTTLRequired) {
		t.Fatalf("zero ttl: err = %v, want ErrTTLRequired", err)
	}
	negTTL := validRequest()
	negTTL.TTL = -time.Hour
	if _, err := New("grant-negttl", negTTL, validApproval(), baseNow); !errors.Is(err, ErrTTLRequired) {
		t.Fatalf("negative ttl: err = %v, want ErrTTLRequired", err)
	}

	// TTL beyond the role's hard maximum is refused, never silently capped.
	over := validRequest()
	over.Role = RoleSupportReadOnly
	max, _ := RoleSupportReadOnly.MaxTTL()
	over.TTL = max + time.Hour
	if _, err := New("grant-over", over, validApproval(), baseNow); !errors.Is(err, ErrTTLExceedsMax) {
		t.Fatalf("ttl over max: err = %v, want ErrTTLExceedsMax", err)
	}

	// Every role in the closed vocabulary has a finite ceiling.
	for role, max := range maxTTLByRole {
		if max <= 0 {
			t.Fatalf("role %s has a non-positive ttl ceiling", role)
		}
	}

	// Revocation is idempotent: a second revoke does not overwrite the
	// original actor/reason and reports the conflict.
	g, err := New("grant-doublerevoke", validRequest(), validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := g.Revoke("first-actor", "first reason", baseNow); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := g.Revoke("second-actor", "second reason", baseNow.Add(time.Minute)); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("second revoke: err = %v, want ErrGrantRevoked", err)
	}
	_, by, reason, _ := g.Revocation()
	if by != "first-actor" || reason != "first reason" {
		t.Fatalf("revocation was overwritten: by=%s reason=%s", by, reason)
	}
}

// TestTodo_TRUST_021_Mutation flips boundary TTL values to confirm the
// comparison against the role ceiling is inclusive-at-max, exclusive-over.
func TestTodo_TRUST_021_Mutation(t *testing.T) {
	max, _ := RoleSupportReadOnly.MaxTTL()

	atMax := validRequest()
	atMax.TTL = max
	if _, err := New("grant-atmax", atMax, validApproval(), baseNow); err != nil {
		t.Fatalf("ttl exactly at max must be accepted: %v", err)
	}

	overMax := validRequest()
	overMax.TTL = max + time.Nanosecond
	if _, err := New("grant-overmax", overMax, validApproval(), baseNow); !errors.Is(err, ErrTTLExceedsMax) {
		t.Fatalf("ttl one tick over max: err = %v, want ErrTTLExceedsMax", err)
	}

	// Expiry boundary: exactly at ExpiresAt is inactive (exclusive).
	g, err := New("grant-boundary", validRequest(), validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.IsActive(g.ExpiresAt) {
		t.Fatalf("grant must not be active exactly at its expiry instant")
	}
	if !g.IsActive(g.ExpiresAt.Add(-time.Nanosecond)) {
		t.Fatalf("grant must still be active one tick before expiry")
	}
}

// TestTodo_TRUST_021_Fault covers a malformed grant id and a use with an
// empty action, both of which must be refused without corrupting the
// evidence trail already recorded.
func TestTodo_TRUST_021_Fault(t *testing.T) {
	if _, err := New("  ", validRequest(), validApproval(), baseNow); err == nil {
		t.Fatalf("blank grant id must be refused")
	}

	g, err := New("grant-fault", validRequest(), validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := g.Use("   ", baseNow.Add(time.Minute)); err == nil {
		t.Fatalf("blank action must be refused")
	}
	// The refused blank-action attempt must not have been recorded as a use.
	for _, e := range g.Evidence() {
		if e.Kind == EvidenceUsed {
			t.Fatalf("blank action must not produce a USED evidence record: %+v", e)
		}
	}
}

// FuzzTodo_TRUST_021 checks that no fuzzed TTL, role, or approver/principal
// pair can produce a grant that violates the role's hard ceiling or that is
// self-approved.
func FuzzTodo_TRUST_021(f *testing.F) {
	f.Add("alice", "bob", "SUPPORT_READ_ONLY", int64(time.Hour))
	f.Add("alice", "alice", "SUPPORT_READ_ONLY", int64(time.Hour))
	f.Add("alice", "bob", "SUPPORT_READ_ONLY", int64(100*time.Hour))
	f.Fuzz(func(t *testing.T, principal, approver, role string, ttl int64) {
		req := validRequest()
		req.Principal = principal
		req.Role = Role(role)
		req.TTL = time.Duration(ttl)
		appr := Approval{Approver: approver, At: baseNow}

		g, err := New("grant-fuzz", req, appr, baseNow)
		if err != nil {
			return
		}
		// Any grant that was actually issued must satisfy every invariant:
		if strings.EqualFold(strings.TrimSpace(g.Approver), strings.TrimSpace(g.Principal)) {
			t.Fatalf("issued a self-approved grant: principal=%q approver=%q", g.Principal, g.Approver)
		}
		max, ok := g.Role.MaxTTL()
		if !ok {
			t.Fatalf("issued a grant with an unrecognized role %q", g.Role)
		}
		if g.ExpiresAt.Sub(g.IssuedAt) > max {
			t.Fatalf("issued grant ttl %s exceeds role max %s", g.ExpiresAt.Sub(g.IssuedAt), max)
		}
		if g.ExpiresAt.Sub(g.IssuedAt) <= 0 {
			t.Fatalf("issued a non-expiring (standing) grant")
		}
	})
}

func TestJIT_MaxTTLAndRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		role Role
		want time.Duration
	}{
		{RoleSupportReadOnly, 4 * time.Hour},
		{RoleIncidentResponder, 8 * time.Hour},
		{RoleIntegrityRepair, 4 * time.Hour},
		{RolePayrollEmergency, 2 * time.Hour},
		{RoleAccessRevocation, 2 * time.Hour},
	} {
		got, ok := tc.role.MaxTTL()
		if !ok || got != tc.want {
			t.Errorf("%s.MaxTTL() = (%s, %v), want (%s, true)", tc.role, got, ok, tc.want)
		}
	}
	if got, ok := Role("not-a-role").MaxTTL(); ok || got != 0 {
		t.Fatalf("unknown role MaxTTL() = (%s, %v), want (0, false)", got, ok)
	}

	cases := []struct {
		name   string
		mutate func(*Request)
		want   error
	}{
		{"principal required", func(r *Request) { r.Principal = " " }, ErrInvalidRequest},
		{"tenant required", func(r *Request) { r.Tenant = "" }, ErrInvalidRequest},
		{"unknown role", func(r *Request) { r.Role = Role("UNKNOWN") }, ErrUnknownRole},
		{"ticket required", func(r *Request) { r.TicketRef = "\t" }, ErrInvalidRequest},
		{"justification required", func(r *Request) { r.Justification = " " }, ErrInvalidRequest},
		{"purpose required", func(r *Request) { r.Purpose = "" }, ErrInvalidRequest},
		{"capability required", func(r *Request) { r.Capabilities = nil }, ErrInvalidRequest},
		{"capability cannot be blank", func(r *Request) { r.Capabilities = []string{" "} }, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)
			grant, err := New("invalid-"+tc.name, req, validApproval(), baseNow)
			if !errors.Is(err, tc.want) || grant != nil {
				t.Fatalf("New() = grant=%v err=%v, want nil/%v", grant, err, tc.want)
			}
		})
	}

	approvalCases := []struct {
		name     string
		approval Approval
	}{
		{"approver required", Approval{At: baseNow}},
		{"approval timestamp required", Approval{Approver: "bob"}},
	}
	for _, tc := range approvalCases {
		t.Run(tc.name, func(t *testing.T) {
			if grant, err := New("invalid-approval-"+tc.name, validRequest(), tc.approval, baseNow); !errors.Is(err, ErrInvalidRequest) || grant != nil {
				t.Fatalf("New() = grant=%v err=%v, want nil/ErrInvalidRequest", grant, err)
			}
		})
	}
}

func TestJIT_GrantAccessorsAndEvidenceAreStateful(t *testing.T) {
	req := validRequest()
	req.Capabilities = []string{"support.read_diagnostics"}
	req.Fields = []string{"worker.worker_number"}
	g, err := New("grant-state", req, validApproval(), baseNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.ID != "grant-state" || g.Tenant != req.Tenant || g.TicketRef != req.TicketRef || g.Justification != req.Justification || g.Purpose != req.Purpose || !g.IssuedAt.Equal(baseNow) || !g.ApprovedAt.Equal(baseNow) || !g.ExpiresAt.Equal(baseNow.Add(req.TTL)) {
		t.Fatalf("issued grant did not preserve request and approval state: %+v", g)
	}
	req.Capabilities[0] = "forged"
	req.Fields[0] = "forged"
	if g.Capabilities[0] == "forged" || g.Fields[0] == "forged" {
		t.Fatal("New retained caller-owned capability or field slices")
	}
	if at, by, reason, revoked := g.Revocation(); revoked || !at.IsZero() || by != "" || reason != "" {
		t.Fatalf("fresh grant Revocation() = %v, %q, %q, %v, want zero/not revoked", at, by, reason, revoked)
	}
	if len(g.Evidence()) != 1 || g.Evidence()[0].Kind != EvidenceGranted {
		t.Fatalf("fresh evidence = %+v, want one GRANTED event", g.Evidence())
	}

	if err := g.Use("diagnostic read", baseNow.Add(time.Minute)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	copyOfEvidence := g.Evidence()
	copyOfEvidence[0].Actor = "forged"
	if g.Evidence()[0].Actor == "forged" {
		t.Fatal("Evidence returned a slice backed by grant state")
	}
	if err := g.Revoke("security", "incident closed", baseNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if g.IsActive(baseNow.Add(3 * time.Minute)) {
		t.Fatal("revoked grant remained active")
	}
	if err := g.Revoke("", "ignored", baseNow.Add(4*time.Minute)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("blank revoker error = %v, want ErrInvalidRequest", err)
	}
	if err := g.Use("late use", baseNow.Add(3*time.Minute)); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("Use after revoke = %v, want ErrGrantRevoked", err)
	}
	if at, by, reason, revoked := g.Revocation(); !revoked || !at.Equal(baseNow.Add(2*time.Minute)) || by != "security" || reason != "incident closed" {
		t.Fatalf("final Revocation() = %v, %q, %q, %v, want original revocation", at, by, reason, revoked)
	}
}
