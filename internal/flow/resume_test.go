package flow

import (
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

func newTestPrincipalResume(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	now := time.Now().UTC()
	spec := trust.PrincipalSpec{
		Tenant:               values.TenantId("acme"),
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-1",
		Roles:                []string{"employee"},
		Purposes:             []string{"hr:manage"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "sess_" + subject,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-" + subject,
	}
	p, err := trust.NewPrincipal(spec)
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	return p
}

type allowGov struct {
	allowed map[string]bool
}

func (a *allowGov) Authorized(principal, flowID string, at time.Time) bool {
	if a.allowed == nil {
		return true
	}
	return a.allowed[principal+">"+flowID]
}

func TestFlowResumeReauthorizesAndRestoresOnlySafeCurrentState(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, err := store.CreateLocator("flow-1", "stage-collect", "alice", 15*time.Minute)
	if err != nil {
		t.Fatalf("create locator: %v", err)
	}
	if loc.Token == "" || loc.FlowID != "flow-1" {
		t.Fatalf("locator invalid")
	}
	if len(loc.Token) < 20 {
		t.Fatalf("token too short opaque")
	}
	_, err = store.SaveDraft("flow-1", "alice", "stage-collect", 0, map[string]string{"name": "Alice", "salary": "100000"}, now)
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	res, err := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res.MaskedState["name"] != "Alice" {
		t.Fatalf("masked state name wrong")
	}
	if res.MaskedState["salary"] != "***" {
		t.Fatalf("sensitive field must be masked, got %q", res.MaskedState["salary"])
	}
	if res.Stale {
		t.Fatalf("should not be stale")
	}
	if len(res.SafeAlternatives) == 0 {
		t.Fatalf("safe alternatives required")
	}
	govDeny := &allowGov{allowed: map[string]bool{"alice>flow-1": false}}
	_, err = store.Resume(loc.Token, alice, now.Add(2*time.Minute), govDeny)
	if err == nil {
		t.Fatalf("authority loss must deny even with valid token")
	}
	expiredNow := now.Add(20 * time.Minute)
	res2, err := store.Resume(loc.Token, alice, expiredNow, &allowGov{})
	if err != nil {
		t.Fatalf("expired resume err: %v", err)
	}
	if !res2.RequiresReauth || !res2.LocatorExpired {
		t.Fatalf("expired locator must require reauth")
	}
	if len(res2.MaskedState) != 0 {
		t.Fatalf("expired must clear sensitive state")
	}
	_, err = store.SaveDraft("flow-1", "alice", "stage-collect", 1, map[string]string{"name": "Alice2"}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("save v2: %v", err)
	}
	res3, _ := store.Resume(loc.Token, alice, now.Add(4*time.Minute), &allowGov{})
	if !res3.Stale {
		t.Fatalf("stale token after draft advance must be stale")
	}
	_, err = store.SaveDraft("flow-1", "alice", "stage-collect", 1, map[string]string{"name": "conflict"}, now.Add(5*time.Minute))
	if err != ErrConflict {
		t.Fatalf("two devices overwriting same version must expose conflict, got %v", err)
	}
	res4, _ := store.Resume(loc.Token, alice, now.Add(5*time.Minute), &allowGov{})
	res5, _ := store.Resume(loc.Token, alice, now.Add(5*time.Minute), &allowGov{})
	if !res5.Idempotent {
		t.Fatalf("refresh/back must be idempotent")
	}
	if res4.CurrentVersion != res5.CurrentVersion {
		t.Fatalf("idempotent resume must return same version")
	}
	_, err = store.SaveDraft("flow-1", "alice", "stage-collect", 2, map[string]string{"ssn": "123-45-6789"}, now.Add(6*time.Minute))
	if err != ErrProhibitedField {
		t.Fatalf("offline draft with prohibited fields unprotected must be rejected, got %v", err)
	}
	if containsBearer(loc.Token) {
		t.Fatalf("locator must not contain bearer authority")
	}
}

func containsBearer(token string) bool {
	for _, sub := range []string{"alice", "acme", "bearer", "salary", "ssn"} {
		if len(token) >= len(sub) {
			for i := 0; i <= len(token)-len(sub); i++ {
				if token[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func TestTodo_UXFLOW_006_Property(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-p", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-p", "alice", "s1", 0, map[string]string{"a": "1"}, now)
	r1, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	r2, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	if r1.CurrentVersion != r2.CurrentVersion || r1.MaskedState["a"] != r2.MaskedState["a"] {
		t.Fatalf("property: same inputs must return same masked state")
	}
	_, _ = store.SaveDraft("flow-p", "alice", "s1", 1, map[string]string{"a": "2"}, now.Add(2*time.Minute))
	r3, _ := store.Resume(loc.Token, alice, now.Add(3*time.Minute), &allowGov{})
	if !r3.Stale {
		t.Fatalf("stale must be exposed after version advance")
	}
}

func TestTodo_UXFLOW_006_Security(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	bob := newTestPrincipalResume(t, "bob")
	loc, _ := store.CreateLocator("flow-sec", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-sec", "alice", "s1", 0, map[string]string{"name": "Alice"}, now)
	gov := &allowGov{allowed: map[string]bool{"alice>flow-sec": true, "bob>flow-sec": false}}
	_, err := store.Resume(loc.Token, bob, now.Add(time.Minute), gov)
	if err == nil {
		t.Fatalf("cross-user resume must deny")
	}
	_, err = store.Resume("invalid-token", alice, now.Add(time.Minute), gov)
	if err != ErrLocatorNotFound {
		t.Fatalf("unknown locator must not disclose existence differently")
	}
	if loc.Token == "flow-sec" || loc.Token == "alice" {
		t.Fatalf("locator must be opaque")
	}
}

func TestTodo_UXFLOW_006_Fault(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-f", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-f", "alice", "s1", 0, map[string]string{"x": "1"}, now)
	expired := now.Add(20 * time.Minute)
	res, _ := store.Resume(loc.Token, alice, expired, &allowGov{})
	if !res.LocatorExpired || len(res.MaskedState) != 0 {
		t.Fatalf("fault: expired must clear")
	}
	n := store.ClearExpired(expired)
	if n == 0 {
		t.Fatalf("clear expired should remove")
	}
	_, err := store.Resume(loc.Token, alice, expired.Add(time.Minute), &allowGov{})
	if err != ErrLocatorNotFound {
		t.Fatalf("after clear locator gone")
	}
}

func TestTodo_UXFLOW_006_Race(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	loc, _ := store.CreateLocator("flow-race", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-race", "alice", "s1", 0, map[string]string{"v": "1"}, now)
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = store.SaveDraft("flow-race", "alice", "s1", 1, map[string]string{"v": string(rune('a' + idx))}, now.Add(time.Duration(idx)*time.Millisecond))
		}(i)
	}
	wg.Wait()
	conflicts := 0
	success := 0
	for _, e := range errs {
		if e == ErrConflict {
			conflicts++
		} else if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("exactly one CAS must succeed, got %d success %d conflicts", success, conflicts)
	}
	alice := newTestPrincipalResume(t, "alice")
	var wg2 sync.WaitGroup
	results := make([]ResumeResult, 5)
	for i := 0; i < 5; i++ {
		wg2.Add(1)
		go func(idx int) {
			defer wg2.Done()
			r, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
			results[idx] = r
		}(i)
	}
	wg2.Wait()
	for i := 1; i < len(results); i++ {
		if results[i].CurrentVersion != results[0].CurrentVersion {
			t.Fatalf("concurrent resume must be consistent")
		}
	}
}

func TestTodo_UXFLOW_006_Golden(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-golden", "stage-1", "alice", 10*time.Minute)
	_, _ = store.SaveDraft("flow-golden", "alice", "stage-1", 0, map[string]string{"name": "Alice", "salary": "99999", "notes": "hello"}, now)
	res, _ := store.Resume(loc.Token, alice, now.Add(5*time.Minute), &allowGov{})
	if res.MaskedState["name"] != "Alice" || res.MaskedState["notes"] != "hello" || res.MaskedState["salary"] != "***" {
		t.Fatalf("golden masked mismatch: %v", res.MaskedState)
	}
	if res.CurrentVersion != 1 {
		t.Fatalf("golden version")
	}
}

func TestTodo_UXFLOW_006_Conformance(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-conf", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-conf", "alice", "s1", 0, map[string]string{"a": "1"}, now)
	r1, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	if r1.FlowID != "flow-conf" || r1.StageID != "s1" {
		t.Fatalf("conformance locator must resolve flow/stage")
	}
	r2, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	if !r2.Idempotent {
		t.Fatalf("second resume with same key must be idempotent")
	}
}

func TestTodo_UXFLOW_006_Browser(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-b", "s1", "alice", 15*time.Minute)
	_, _ = store.SaveDraft("flow-b", "alice", "s1", 0, map[string]string{"field": "value"}, now)
	r1, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	r2, _ := store.Resume(loc.Token, alice, now.Add(time.Minute), &allowGov{})
	if r1.MaskedState["field"] != r2.MaskedState["field"] {
		t.Fatalf("browser refresh must be idempotent and return same masked state")
	}
}

func TestTodo_UXFLOW_006_Mutation(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(func() time.Time { return now })
	_, err := store.CreateLocator("", "s1", "alice", 15*time.Minute)
	if err == nil {
		t.Fatalf("empty flow must error")
	}
	_, err = store.SaveDraft("flow-m", "alice", "s1", 5, map[string]string{"a": "1"}, now)
	if err != ErrConflict {
		t.Fatalf("wrong expected version must conflict")
	}
	_, err = store.Resume("nope", newTestPrincipalResume(t, "alice"), now, &allowGov{})
	if err != ErrLocatorNotFound {
		t.Fatalf("mutated token must not be found")
	}
}
