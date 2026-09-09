package accessreview

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
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

type failingReviewPort struct{ err error }

func (f failingReviewPort) EffectiveGrants(time.Time) ([]Grant, error) { return nil, f.err }
func (f failingReviewPort) JITRoles(time.Time) ([]Grant, error)        { return nil, f.err }

type stagedEvidence struct {
	count int
}

func (s *stagedEvidence) Append(in Evidence) (Evidence, error) {
	s.count++
	if s.count > 1 {
		return Evidence{}, errors.New("evidence unavailable")
	}
	in.Revision = uint64(s.count)
	return in, nil
}

func TestAccessReview_PublicValueAPIsAndRefusals(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clockCalled := false
	clock := ClockFunc(func() time.Time { clockCalled = true; return now })
	if !clock.Now().Equal(now) || !clockCalled {
		t.Fatal("ClockFunc did not return its source instant")
	}
	g := grant("g", "holder", GrantClassAuthz, now.Add(-time.Hour))
	if g.Digest() == "" {
		t.Fatal("Grant.Digest returned empty digest")
	}
	policy := standardPolicy()
	store := NewMemoryEvidenceStore()
	if _, err := store.Append(Evidence{}); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero evidence err=%v", err)
	}
	if _, err := store.Append(Evidence{Kind: EvidenceKind("BAD"), At: now}); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad evidence kind err=%v", err)
	}
	if _, err := (*MemoryEvidenceStore)(nil).Append(Evidence{Kind: EvidenceSchedule, At: now}); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil evidence store err=%v", err)
	}
	if got := (*MemoryEvidenceStore)(nil).Evidence(); got != nil {
		t.Fatalf("nil store evidence=%v, want nil", got)
	}
	entry := ScheduleEntry{Grant: g, ReviewDue: now}
	schedule := Schedule{AsOf: now, Policy: policy, Entries: []ScheduleEntry{entry}, Digest: "schedule-digest"}
	if !strings.Contains(schedule.Explain(), "entries=1") || !strings.Contains(schedule.Explain(), "overdue_at_schedule=1") {
		t.Fatalf("schedule explanation=%q", schedule.Explain())
	}
	report := OverdueReport{AsOf: now, Policy: policy.Version, Entries: []ScheduleEntry{entry}, Digest: "overdue-digest"}
	if !strings.Contains(report.Explain(), "entries=1") {
		t.Fatalf("overdue explanation=%q", report.Explain())
	}
	record := ReviewRecord{GrantID: g.ID, GrantClass: g.Class, Reviewer: "reviewer", Action: Continue, ReviewedAt: now, ReviewDue: now.Add(-time.Hour), PolicyVersion: policy.Version, Digest: "review-digest"}
	if record.DigestValue() != record.Digest || !strings.Contains(record.Explain(), "overdue=true") {
		t.Fatalf("review record helpers failed: %q", record.Explain())
	}
	evidence := Evidence{Revision: 1, Kind: EvidenceReview, At: now, PolicyVersion: policy.Version, GrantClass: g.Class, Action: Continue, Digest: "evidence-digest"}
	if !strings.Contains(evidence.Explain(), "revision=1") {
		t.Fatalf("evidence explanation=%q", evidence.Explain())
	}
	copyEvidence, err := store.Append(Evidence{Kind: EvidenceSchedule, At: now, PolicyVersion: policy.Version, GrantDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	rows := store.Evidence()
	rows[0].Digest = "mutated"
	if store.Evidence()[0].Digest != copyEvidence.Digest {
		t.Fatal("Evidence did not return copies")
	}
}

func TestAccessReview_NewAndScheduleFailures(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := Config{Authz: fakeAuthz{}, JIT: fakeJIT{}, Evidence: NewMemoryEvidenceStore(), Clock: fixedClock{at: now}, Policy: standardPolicy()}
	for name, mutate := range map[string]func(*Config){
		"authz": func(c *Config) { c.Authz = nil }, "jit": func(c *Config) { c.JIT = nil }, "evidence": func(c *Config) { c.Evidence = nil }, "clock": func(c *Config) { c.Clock = nil },
		"policy version": func(c *Config) { c.Policy.Version = " " }, "policy intervals": func(c *Config) { c.Policy.Intervals = nil }, "policy class": func(c *Config) { c.Policy.Intervals = map[GrantClass]time.Duration{" ": time.Hour} }, "policy interval": func(c *Config) { c.Policy.Intervals = map[GrantClass]time.Duration{GrantClassAuthz: 0} },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if _, err := New(cfg); err == nil || !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("New err=%v", err)
			}
		})
	}
	if _, err := (*Manager)(nil).Schedule(); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil manager schedule err=%v", err)
	}
	zeroClock := fixedClock{}
	cfg := base
	cfg.Clock = zeroClock
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Schedule(); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero clock schedule err=%v", err)
	}
	for name, ports := range map[string]Config{
		"authz error": func() Config { c := base; c.Authz = failingReviewPort{err: errors.New("authz down")}; return c }(),
		"jit error":   func() Config { c := base; c.JIT = failingReviewPort{err: errors.New("jit down")}; return c }(),
	} {
		m, err := New(ports)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Schedule(); err == nil {
			t.Fatalf("%s schedule unexpectedly succeeded", name)
		}
	}
	dup := grant("same", "h", GrantClassAuthz, now)
	cfg = base
	cfg.Authz = fakeAuthz{grants: []Grant{dup}}
	cfg.JIT = fakeJIT{grants: []Grant{dup}}
	m, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Schedule(); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate schedule err=%v", err)
	}
	unknown := grant("unknown", "h", GrantClass("UNKNOWN"), now)
	cfg = base
	cfg.Authz = fakeAuthz{grants: []Grant{unknown}}
	m, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Schedule(); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing interval schedule err=%v", err)
	}
}

func TestAccessReview_GrantAndReviewRequestBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := grant("g", "holder", GrantClassAuthz, now)
	for name, mutate := range map[string]func(*Grant){
		"id": func(g *Grant) { g.ID = " " }, "holder": func(g *Grant) { g.Holder = " holder" }, "class": func(g *Grant) { g.Class = " " }, "granted": func(g *Grant) { g.GrantedAt = time.Time{} }, "expired ordering": func(g *Grant) { g.ExpiresAt = g.GrantedAt }, "jit expiry": func(g *Grant) { g.Class = GrantClassJIT; g.ExpiresAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			g := base
			mutate(&g)
			m, _ := reviewManager(t, now, []Grant{g}, nil, standardPolicy())
			if _, err := m.Schedule(); err == nil || !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Schedule err=%v", err)
			}
		})
	}
	valid := ReviewRequest{GrantID: "g", Reviewer: "reviewer", Action: Continue, Justification: "justified"}
	cases := []struct {
		name   string
		mutate func(*ReviewRequest)
	}{
		{"grant", func(r *ReviewRequest) { r.GrantID = " " }}, {"reviewer", func(r *ReviewRequest) { r.Reviewer = " " }}, {"justification", func(r *ReviewRequest) { r.Justification = " " }}, {"continue scope", func(r *ReviewRequest) { r.NarrowTo = []string{"x"} }}, {"revoke scope", func(r *ReviewRequest) { r.Action = Revoke; r.NarrowTo = []string{"x"} }}, {"narrow empty", func(r *ReviewRequest) { r.Action = Narrow }}, {"narrow padded", func(r *ReviewRequest) { r.Action = Narrow; r.NarrowTo = []string{" x"} }}, {"narrow wildcard", func(r *ReviewRequest) { r.Action = Narrow; r.NarrowTo = []string{"*"} }}, {"narrow duplicate", func(r *ReviewRequest) { r.Action = Narrow; r.NarrowTo = []string{"x", "x"} }}, {"action", func(r *ReviewRequest) { r.Action = "UNKNOWN" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			m, _ := reviewManager(t, now, []Grant{base}, nil, standardPolicy())
			if _, err := m.CompleteReview(r); err == nil || !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("CompleteReview err=%v", err)
			}
		})
	}
	missing, _ := reviewManager(t, now, []Grant{base}, nil, standardPolicy())
	if _, err := missing.CompleteReview(ReviewRequest{GrantID: "missing", Reviewer: "reviewer", Action: Continue, Justification: "justified"}); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("missing grant err=%v", err)
	}
	staged := &stagedEvidence{}
	m, err := New(Config{Authz: fakeAuthz{grants: []Grant{base}}, JIT: fakeJIT{}, Evidence: staged, Clock: fixedClock{at: now}, Policy: standardPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CompleteReview(valid); err == nil {
		t.Fatal("evidence append failure was ignored")
	}
	store := NewMemoryEvidenceStore()
	m, err = New(Config{Authz: fakeAuthz{grants: []Grant{base}}, JIT: fakeJIT{}, Evidence: store, Clock: fixedClock{at: now}, Policy: standardPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CompleteReview(valid); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CompleteReview(valid); !errors.Is(err, ErrReviewAlreadyRecorded) {
		t.Fatalf("duplicate review err=%v", err)
	}
}

func TestAccessReview_ReadersRejectInvalidInputsAndCopy(t *testing.T) {
	if _, err := NewAuthzReader(nil, nil, nil); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil principal err=%v", err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("acme"), Subject: "subject", SubjectKind: trust.SubjectKindHuman, Roles: []string{string(authz.RoleManager)}, Purposes: nil, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuthzReader(principal, []authz.FieldID{authz.FieldBaseSalary}, nil); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing purposes err=%v", err)
	}
	principalWithPurpose, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("acme"), Subject: "subject", SubjectKind: trust.SubjectKindHuman, Roles: []string{string(authz.RoleManager)}, Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest-2"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewAuthzReader(principalWithPurpose, []authz.FieldID{authz.FieldBaseSalary}, []string{authz.PurposeCompensationReview})
	if err != nil {
		t.Fatal(err)
	}
	grants, err := reader.EffectiveGrants(time.Now())
	if err != nil || len(grants) == 0 {
		t.Fatalf("effective grants=%+v err=%v", grants, err)
	}
	if _, err := (*AuthzReader)(nil).EffectiveGrants(time.Now()); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil authz reader err=%v", err)
	}
	if _, err := NewJITReader(nil); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil JIT grant err=%v", err)
	}
	jitReader, err := NewJITReader(&jit.Grant{ID: "jit", Principal: "machine", Role: jit.RoleSupportReadOnly, IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), TicketRef: "ticket", Purpose: "audit"})
	if err != nil {
		t.Fatal(err)
	}
	jitGrants, err := jitReader.JITRoles(time.Now())
	if err != nil || len(jitGrants) != 1 || jitGrants[0].Class != GrantClassJIT {
		t.Fatalf("JIT grants=%+v err=%v", jitGrants, err)
	}
	if _, err := (*JITReader)(nil).JITRoles(time.Now()); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil JIT reader err=%v", err)
	}
}
