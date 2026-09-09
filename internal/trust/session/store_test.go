package session_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// fakeStore is a small, independent [session.Store] implementation over
// plain maps. It carries none of a real durable backend's transaction or
// replica machinery -- internal/trust/session/pgstore.PGStore owns proving
// that against a real PostgreSQL server (its own SECURITY, INTEGRATION and
// MUTATION suites) -- only exactly the state-transition contract
// [session.Store]'s doc comment describes. That is deliberate: these tests
// exist to prove [session.PersistentManager] delegates to and interprets
// that contract correctly, independent of which store implements it.
type fakeStore struct {
	mu          sync.Mutex
	sessions    map[session.ID]session.StoreRecord
	generations map[string]session.ID // token hash (any generation, retired or current) -> session id
	genOf       map[string]int64      // token hash -> the generation it was issued at
	evidence    map[session.ID][]session.Evidence
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions:    make(map[session.ID]session.StoreRecord),
		generations: make(map[string]session.ID),
		genOf:       make(map[string]int64),
		evidence:    make(map[session.ID][]session.Evidence),
	}
}

func (f *fakeStore) appendEvidenceLocked(id session.ID, kind session.EvidenceKind, reason string, at time.Time) {
	f.evidence[id] = append(f.evidence[id], session.Evidence{
		ID:        fmt.Sprintf("ev:fake:%s:%s:%d", id, kind, len(f.evidence[id])),
		SessionID: id,
		Kind:      kind,
		Reason:    reason,
		At:        at,
	})
}

func (f *fakeStore) applyExpiryLocked(rec *session.StoreRecord, now time.Time) {
	next, reason, ok := session.EvaluateExpiry(rec.Status, rec.LastActivityAt, rec.AbsoluteExpiresAt, rec.IdleTimeout, now)
	if !ok {
		return
	}
	rec.Status = next
	rec.RevokedReason = reason
	rec.Version++
	f.appendEvidenceLocked(rec.ID, session.EvidenceExpired, reason, now)
}

func (f *fakeStore) Create(_ context.Context, rec session.StoreRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.sessions[rec.ID]; exists {
		return errors.New("fakeStore: session already exists")
	}
	f.sessions[rec.ID] = rec
	f.generations[rec.CurrentTokenHash] = rec.ID
	f.genOf[rec.CurrentTokenHash] = rec.Generation
	f.appendEvidenceLocked(rec.ID, session.EvidenceCreated, "", rec.CreatedAt)
	return nil
}

func (f *fakeStore) Get(_ context.Context, id session.ID, now time.Time) (session.StoreRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.sessions[id]
	if !ok {
		return session.StoreRecord{}, session.ErrSessionNotFound
	}
	f.applyExpiryLocked(&rec, now)
	f.sessions[id] = rec
	return rec, nil
}

func (f *fakeStore) Touch(_ context.Context, id session.ID, now time.Time) (session.StoreRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.sessions[id]
	if !ok {
		return session.StoreRecord{}, session.ErrSessionNotFound
	}
	f.applyExpiryLocked(&rec, now)
	if rec.Status != session.StatusActive {
		f.sessions[id] = rec
		return rec, fmt.Errorf("%w: %s", session.ErrSessionNotActive, rec.Status)
	}
	rec.LastActivityAt = now
	rec.Version++
	f.sessions[id] = rec
	return rec, nil
}

func (f *fakeStore) Revoke(_ context.Context, id session.ID, reason string, now time.Time) (session.StoreRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.sessions[id]
	if !ok {
		return session.StoreRecord{}, session.ErrSessionNotFound
	}
	f.applyExpiryLocked(&rec, now)
	if rec.Status != session.StatusActive {
		f.sessions[id] = rec
		return rec, fmt.Errorf("%w: %s", session.ErrSessionNotActive, rec.Status)
	}
	rec.Status = session.StatusRevoked
	rec.RevokedReason = reason
	rec.Version++
	f.sessions[id] = rec
	f.appendEvidenceLocked(id, session.EvidenceRevoked, reason, now)
	return rec, nil
}

func (f *fakeStore) Refresh(_ context.Context, presentedHash, newHash string, now time.Time) (session.StoreRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, known := f.generations[presentedHash]
	if !known {
		return session.StoreRecord{}, session.ErrRefreshUnknown
	}
	rec := f.sessions[id]
	f.applyExpiryLocked(&rec, now)
	presentedGen := f.genOf[presentedHash]
	if presentedGen != rec.Generation {
		if rec.Status == session.StatusActive {
			rec.Status = session.StatusRevoked
			rec.RevokedReason = session.ReasonRefreshReplay
			rec.Version++
			f.appendEvidenceLocked(id, session.EvidenceRevoked, session.ReasonRefreshReplay, now)
		}
		f.sessions[id] = rec
		return session.StoreRecord{}, session.ErrRefreshReplay
	}
	if rec.Status != session.StatusActive {
		f.sessions[id] = rec
		return rec, fmt.Errorf("%w: %s", session.ErrSessionNotActive, rec.Status)
	}
	rec.CurrentTokenHash = newHash
	rec.Generation++
	rec.RotationCount++
	rec.LastActivityAt = now
	rec.Version++
	f.sessions[id] = rec
	f.generations[newHash] = id
	f.genOf[newHash] = rec.Generation
	f.appendEvidenceLocked(id, session.EvidenceRotated, "", now)
	return rec, nil
}

func (f *fakeStore) RecordEvidence(_ context.Context, id session.ID, kind session.EvidenceKind, reason string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.sessions[id]; !ok {
		return session.ErrSessionNotFound
	}
	f.appendEvidenceLocked(id, kind, reason, at)
	return nil
}

func (f *fakeStore) Evidence(_ context.Context, id session.ID) ([]session.Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]session.Evidence, len(f.evidence[id]))
	copy(out, f.evidence[id])
	return out, nil
}

var _ session.Store = (*fakeStore)(nil)

func clockAt(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestTodo_SECARCH_002 is the RED case made concrete: a session created
// through one [session.PersistentManager] instance must remain fully usable
// -- readable, touchable, refreshable, revocable, and still subject to
// replay detection -- through a second, independent instance sharing only
// the [session.Store] and nothing else, standing in for "a process restart
// or a second API replica" (the RED clause's own words).
func TestTodo_SECARCH_002(t *testing.T) {
	store := newFakeStore()
	clock := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	first, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(clock), Store: store})
	if err != nil {
		t.Fatalf("NewPersistentManager (first replica): %v", err)
	}
	second, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(clock), Store: store})
	if err != nil {
		t.Fatalf("NewPersistentManager (second replica): %v", err)
	}

	rec, tok1, err := first.Create(context.Background(), session.CreateSpec{
		Tenant: "11111111-1111-1111-1111-111111111111", Subject: "user:1",
		PrincipalFingerprint: "fp:1", Assurance: trust.AssuranceSubstantial,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A second replica, sharing only the store, sees the exact same session
	// -- the RED case ("a second API replica loses an active session") does
	// not happen.
	got, err := second.Get(context.Background(), rec.ID())
	if err != nil {
		t.Fatalf("second replica Get: %v", err)
	}
	if got.ID() != rec.ID() || got.Status() != session.StatusActive || got.Tenant() != rec.Tenant() {
		t.Fatalf("second replica saw %+v, want the first replica's own record %+v", got, rec)
	}

	// The second replica touches it.
	touched, err := second.Touch(context.Background(), rec.ID())
	if err != nil {
		t.Fatalf("second replica Touch: %v", err)
	}
	if touched.Status() != session.StatusActive {
		t.Fatalf("touched status = %s, want ACTIVE", touched.Status())
	}

	// The first replica rotates the refresh token the second replica has
	// never seen.
	rotated, tok2, err := first.Refresh(context.Background(), tok1)
	if err != nil {
		t.Fatalf("first replica Refresh: %v", err)
	}
	if rotated.RotationCount() != 1 {
		t.Fatalf("rotation count = %d, want 1", rotated.RotationCount())
	}

	// The second replica now sees the rotation: presenting the retired
	// (first) token is a replay it alone must catch and revoke, having
	// never itself performed the rotation.
	if _, _, err := second.Refresh(context.Background(), tok1); !errors.Is(err, session.ErrRefreshReplay) {
		t.Fatalf("second replica Refresh(retired token) error = %v, want ErrRefreshReplay", err)
	}
	after, err := first.Get(context.Background(), rec.ID())
	if err != nil {
		t.Fatalf("first replica Get after replay: %v", err)
	}
	if after.Status() != session.StatusRevoked || after.RevokedReason() != session.ReasonRefreshReplay {
		t.Fatalf("after replay: status=%s reason=%q, want REVOKED/refresh_replay_detected", after.Status(), after.RevokedReason())
	}

	// The still-current (second-generation) token is now also dead, because
	// the whole family was revoked -- not because it was itself replayed.
	if _, _, err := second.Refresh(context.Background(), tok2); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("Refresh with the current-but-now-revoked token error = %v, want ErrSessionNotActive", err)
	}
}

// TestTodo_SECARCH_002_Golden pins the exact evidence trail one lifecycle
// produces: created, rotated, then revoked-by-replay -- the durable
// audit shape SECARCH-002's GREEN clause depends on being available "within
// a bounded propagation window" across every replica, here proven available
// through the one [session.PersistentManager] that wrote it.
func TestTodo_SECARCH_002_Golden(t *testing.T) {
	store := newFakeStore()
	clock := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(clock), Store: store})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}

	rec, tok, err := mgr.Create(context.Background(), session.CreateSpec{
		Tenant: "22222222-2222-2222-2222-222222222222", Subject: "user:golden",
		PrincipalFingerprint: "fp:golden", Assurance: trust.AssuranceHigh,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, _, err = mgr.Refresh(context.Background(), tok)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Replay the already-retired token.
	if _, _, err := mgr.Refresh(context.Background(), tok); !errors.Is(err, session.ErrRefreshReplay) {
		t.Fatalf("Refresh(retired) error = %v, want ErrRefreshReplay", err)
	}

	got, err := mgr.Evidence(context.Background(), rec.ID())
	if err != nil {
		t.Fatalf("Evidence: %v", err)
	}
	wantKinds := []session.EvidenceKind{session.EvidenceCreated, session.EvidenceRotated, session.EvidenceRevoked}
	if len(got) != len(wantKinds) {
		t.Fatalf("evidence trail = %+v, want %d entries", got, len(wantKinds))
	}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("evidence[%d].Kind = %s, want %s (full trail: %+v)", i, got[i].Kind, want, got)
		}
		if got[i].SessionID != rec.ID() {
			t.Fatalf("evidence[%d].SessionID = %s, want %s", i, got[i].SessionID, rec.ID())
		}
	}
	if got[2].Reason != session.ReasonRefreshReplay {
		t.Fatalf("final evidence reason = %q, want %q", got[2].Reason, session.ReasonRefreshReplay)
	}
}

// TestTodo_SECARCH_002_EvaluateExpiry pins [session.EvaluateExpiry] against
// the exact three cases [Manager.applyExpiryLocked] distinguishes: still
// active, absolute timeout (checked first), and idle timeout -- the shared
// pure decision every [session.Store] implementation must apply identically
// to a durably loaded row.
func TestTodo_SECARCH_002_EvaluateExpiry(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	idle := 30 * time.Minute
	absoluteAt := created.Add(12 * time.Hour)

	tests := []struct {
		name           string
		status         session.Status
		lastActivityAt time.Time
		now            time.Time
		wantOK         bool
		wantNext       session.Status
		wantReason     string
	}{
		{"not active is left alone", session.StatusRevoked, created, created.Add(time.Hour), false, session.StatusRevoked, ""},
		{"still within both windows", session.StatusActive, created, created.Add(time.Minute), false, session.StatusActive, ""},
		{"absolute timeout wins even if idle window is also open", session.StatusActive, absoluteAt.Add(-time.Second), absoluteAt, true, session.StatusExpiredAbsolute, session.ReasonAbsoluteTimeout},
		{"idle timeout", session.StatusActive, created, created.Add(idle + time.Second), true, session.StatusExpiredIdle, session.ReasonIdleTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, reason, ok := session.EvaluateExpiry(tt.status, tt.lastActivityAt, absoluteAt, idle, tt.now)
			if ok != tt.wantOK || next != tt.wantNext || reason != tt.wantReason {
				t.Fatalf("EvaluateExpiry() = (%s, %q, %v), want (%s, %q, %v)", next, reason, ok, tt.wantNext, tt.wantReason, tt.wantOK)
			}
		})
	}
}

// TestTodo_SECARCH_002_InvalidStore proves [session.NewPersistentManager]
// refuses to construct without a [session.Store]: a durable manager with
// nowhere durable to write would silently behave like an in-memory one
// while claiming otherwise.
func TestTodo_SECARCH_002_InvalidStore(t *testing.T) {
	if _, err := session.NewPersistentManager(session.PersistentManagerConfig{}); err == nil {
		t.Fatal("NewPersistentManager with no Store: want an error, got nil")
	}
}

func TestPersistentManager_RejectsInvalidConfigurationAndSpecs(t *testing.T) {
	store := newFakeStore()
	if _, err := session.NewPersistentManager(session.PersistentManagerConfig{Store: store, IdleTimeout: 2 * time.Hour, AbsoluteTimeout: time.Hour}); !errors.Is(err, session.ErrInvalidCreateSpec) {
		t.Fatalf("invalid persistent timeout config = %v, want ErrInvalidCreateSpec", err)
	}
	mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(baseTime), Store: store, IdleTimeout: time.Minute, AbsoluteTimeout: time.Hour})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	for name, mutate := range map[string]func(*session.CreateSpec){
		"tenant":      func(s *session.CreateSpec) { s.Tenant = "" },
		"subject":     func(s *session.CreateSpec) { s.Subject = "" },
		"fingerprint": func(s *session.CreateSpec) { s.PrincipalFingerprint = "" },
		"assurance":   func(s *session.CreateSpec) { s.Assurance = trust.AssuranceUnspecified },
		"timeout":     func(s *session.CreateSpec) { s.IdleTimeout, s.AbsoluteTimeout = 2*time.Hour, time.Hour },
	} {
		t.Run(name, func(t *testing.T) {
			spec := session.CreateSpec{Tenant: "33333333-3333-3333-3333-333333333333", Subject: "user", PrincipalFingerprint: "fp", Assurance: trust.AssuranceHigh}
			mutate(&spec)
			if _, _, err := mgr.Create(context.Background(), spec); !errors.Is(err, session.ErrInvalidCreateSpec) {
				t.Fatalf("Create invalid %s = %v, want ErrInvalidCreateSpec", name, err)
			}
		})
	}
	if len(store.sessions) != 0 {
		t.Fatalf("invalid persistent creates changed store state: %d sessions", len(store.sessions))
	}
}
