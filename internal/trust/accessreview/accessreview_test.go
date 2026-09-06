package accessreview

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/trust/jit"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fakeAuthz struct {
	grants []Grant
}

func (f fakeAuthz) EffectiveGrants(time.Time) ([]Grant, error) {
	return append([]Grant(nil), f.grants...), nil
}

type fakeJIT struct {
	grants []Grant
}

func (f fakeJIT) JITRoles(time.Time) ([]Grant, error) { return append([]Grant(nil), f.grants...), nil }

func reviewManager(t *testing.T, now time.Time, authzGrants, jitGrants []Grant, policy Policy) (*Manager, *MemoryEvidenceStore) {
	t.Helper()
	store := NewMemoryEvidenceStore()
	m, err := New(Config{
		Authz:    fakeAuthz{grants: authzGrants},
		JIT:      fakeJIT{grants: jitGrants},
		Evidence: store,
		Clock:    fixedClock{at: now},
		Policy:   policy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, store
}

func grant(id, holder string, class GrantClass, grantedAt time.Time) Grant {
	g := Grant{ID: id, Holder: holder, Class: class, Source: "test-source", GrantedAt: grantedAt,
		ScopeDigest: digest("test-scope", id)}
	if class == GrantClassJIT {
		g.ExpiresAt = grantedAt.Add(time.Hour)
	}
	return g
}

func standardPolicy() Policy {
	return Policy{Version: "test.policy.v1", Intervals: map[GrantClass]time.Duration{
		GrantClassAuthz: 24 * time.Hour,
		GrantClassJIT:   2 * time.Hour,
	}}
}

// TestTodo_SECARCH_003 is the PRIMARY test for periodic access review: every
// source grant receives a policy-derived due instant, overdue access is
// reported, and a distinct reviewer can complete an immutable review.
func TestTodo_SECARCH_003(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	authzGrant := grant("standing-1", "holder-1", GrantClassAuthz, now.Add(-48*time.Hour))
	jitGrant := grant("jit-1", "holder-2", GrantClassJIT, now.Add(-3*time.Hour))
	m, store := reviewManager(t, now, []Grant{authzGrant}, []Grant{jitGrant}, standardPolicy())

	schedule, err := m.Schedule()
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(schedule.Entries) != 2 {
		t.Fatalf("schedule entries = %d, want 2", len(schedule.Entries))
	}
	for _, entry := range schedule.Entries {
		if entry.ReviewDue.IsZero() {
			t.Fatal("grant has no review due instant")
		}
	}
	report, err := m.Overdue()
	if err != nil {
		t.Fatalf("Overdue: %v", err)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("overdue entries = %d, want 2", len(report.Entries))
	}

	record, err := m.CompleteReview(ReviewRequest{
		GrantID: "standing-1", Reviewer: "independent-reviewer", Action: Continue,
		Justification: "current business need confirmed",
	})
	if err != nil {
		t.Fatalf("CompleteReview: %v", err)
	}
	if record.Action != Continue || record.Digest == "" || record.GrantDigest != authzGrant.Digest() {
		t.Fatalf("review record = %+v, want digested CONTINUE record", record)
	}
	if got := record.Explain(); strings.Contains(got, "standing-1") || strings.Contains(got, "independent-reviewer") {
		t.Fatalf("Explain leaked an identifier: %q", got)
	}
	report, err = m.Overdue()
	if err != nil {
		t.Fatalf("Overdue after review: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Grant.ID != "jit-1" {
		t.Fatalf("overdue after review = %+v, want only jit-1", report.Entries)
	}
	evidence := store.Evidence()
	if len(evidence) < 3 || evidence[len(evidence)-1].Digest == "" || evidence[len(evidence)-1].PreviousDigest == "" {
		t.Fatalf("evidence = %+v, want chained schedule/review revisions", evidence)
	}
}

// TestTodo_SECARCH_003_Golden proves the schedule digest is deterministic and
// the explanation remains a stable redaction-safe summary.
func TestTodo_SECARCH_003_Golden(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	g := grant("g-1", "holder", GrantClassAuthz, now.Add(-time.Hour))
	m, _ := reviewManager(t, now, []Grant{g}, nil, standardPolicy())
	a, err := m.Schedule()
	if err != nil {
		t.Fatalf("first Schedule: %v", err)
	}
	b, err := m.Schedule()
	if err != nil {
		t.Fatalf("second Schedule: %v", err)
	}
	if a.Digest != b.Digest || a.Explain() != b.Explain() {
		t.Fatalf("schedule is not deterministic: a=%q/%q b=%q/%q", a.Digest, a.Explain(), b.Digest, b.Explain())
	}
}

// TestTodo_SECARCH_003_Security proves separation of duties, closed action
// handling, typed field refusals, and identifier-safe explanations.
func TestTodo_SECARCH_003_Security(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	g := grant("secret-grant-id", "same-holder", GrantClassAuthz, now.Add(-48*time.Hour))
	m, _ := reviewManager(t, now, []Grant{g}, nil, standardPolicy())
	_, err := m.CompleteReview(ReviewRequest{GrantID: g.ID, Reviewer: g.Holder, Action: Continue, Justification: "not allowed"})
	if err == nil {
		t.Fatal("same holder was accepted as reviewer")
	}
	var refusal Refusal
	if !errors.As(err, &refusal) || refusal.Field != "reviewer" {
		t.Fatalf("error = %v, want typed reviewer refusal", err)
	}
	_, err = m.CompleteReview(ReviewRequest{GrantID: g.ID, Reviewer: "independent", Action: ReviewAction("DELETE"), Justification: "invalid action"})
	if err == nil {
		t.Fatal("unknown action was accepted")
	}
	if !errors.As(err, &refusal) || refusal.Field != "action" {
		t.Fatalf("error = %v, want typed action refusal", err)
	}
	entry, err := m.Schedule()
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if strings.Contains(entry.Explain(), g.ID) || strings.Contains(entry.Explain(), g.Holder) {
		t.Fatalf("schedule Explain leaked identifier: %q", entry.Explain())
	}
}

// TestTodo_SECARCH_003_Integration wires the real authz field resolver and
// jit.New constructor through the package's read ports. Only the evidence
// store is an in-memory I/O fake.
func TestTodo_SECARCH_003_Integration(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("acme"), Subject: "operator-1", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{string(authz.RoleManager)}, Purposes: []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	authzReader, err := NewAuthzReader(principal, []authz.FieldID{authz.FieldBaseSalary}, []string{authz.PurposeCompensationReview})
	if err != nil {
		t.Fatalf("NewAuthzReader: %v", err)
	}
	jitGrant, err := jit.New("jit-constructed", jit.Request{
		Principal: "operator-2", Tenant: values.TenantId("acme"), Role: jit.RoleSupportReadOnly,
		TicketRef: "ticket-1", Justification: "bounded support investigation", Capabilities: []string{"read.audit"},
		Fields: []string{"audit.event"}, Purpose: "audit_review", TTL: time.Hour,
	}, jit.Approval{Approver: "approver-1", At: now.Add(-time.Minute)}, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("jit.New: %v", err)
	}
	jitReader, err := NewJITReader(jitGrant)
	if err != nil {
		t.Fatalf("NewJITReader: %v", err)
	}
	store := NewMemoryEvidenceStore()
	m, err := New(Config{Authz: authzReader, JIT: jitReader, Evidence: store, Clock: fixedClock{at: now}, Policy: standardPolicy()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	schedule, err := m.Schedule()
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(schedule.Entries) != 2 {
		t.Fatalf("real collaborator schedule entries = %d, want 2", len(schedule.Entries))
	}
	classes := map[GrantClass]bool{}
	for _, entry := range schedule.Entries {
		classes[entry.Grant.Class] = true
	}
	if !classes[GrantClassAuthz] || !classes[GrantClassJIT] {
		t.Fatalf("real collaborator classes = %v, want AUTHZ and JIT", classes)
	}
}

// TestTodo_SECARCH_003_Mutation proves the load-bearing policy and action
// checks: removing a class interval or narrowing without a scope cannot pass.
func TestTodo_SECARCH_003_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	g := grant("g-1", "holder", GrantClassJIT, now.Add(-time.Hour))
	missing := Policy{Version: "mutation.v1", Intervals: map[GrantClass]time.Duration{GrantClassAuthz: time.Hour}}
	m, _ := reviewManager(t, now, nil, []Grant{g}, missing)
	if _, err := m.Schedule(); err == nil {
		t.Fatal("missing JIT policy interval was accepted")
	}
	m, _ = reviewManager(t, now, nil, []Grant{g}, standardPolicy())
	if _, err := m.CompleteReview(ReviewRequest{GrantID: g.ID, Reviewer: "reviewer", Action: Narrow, Justification: "narrow", NarrowTo: nil}); err == nil {
		t.Fatal("NARROW without narrow_to was accepted")
	}
}
