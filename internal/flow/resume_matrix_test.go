package flow

import (
	"encoding/base64"
	"reflect"
	"sync"
	"testing"
	"time"
)

// UXFLOW-006's resume contract is intentionally exercised as small, isolated
// cases so a failure identifies the interrupted-flow guarantee that regressed.

func TestUXFLOW006ResumeLocatorIsOpaqueAndBearerFree(t *testing.T) {
	store := NewStore(func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) })
	loc, err := store.CreateLocator("flow-bearer-free", "collect", "alice", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(loc.Token)
	if err != nil {
		t.Fatalf("locator must be URL-safe opaque data: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("locator entropy length = %d, want 32 bytes", len(raw))
	}
	for _, forbidden := range []string{"alice", "flow-bearer-free", "bearer", "collect"} {
		if containsSubstring(loc.Token, forbidden) {
			t.Fatalf("locator token contains authority or flow data %q: %q", forbidden, loc.Token)
		}
	}
}

func TestUXFLOW006ExpiredResumeClearsStateAndOffersReauth(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-expiry", "collect", "alice", time.Minute)
	if _, err := store.SaveDraft(loc.FlowID, "alice", loc.StageID, 0, map[string]string{"name": "Alice", "salary": "100000"}, now); err != nil {
		t.Fatal(err)
	}
	res, err := store.Resume(loc.Token, alice, now.Add(2*time.Minute), &allowGov{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.LocatorExpired || !res.RequiresReauth {
		t.Fatalf("expired resume = %+v, want expiry and reauth", res)
	}
	if len(res.MaskedState) != 0 {
		t.Fatalf("expired resume leaked state: %#v", res.MaskedState)
	}
	if !reflect.DeepEqual(res.SafeAlternatives, []string{"reauth", "start_new"}) {
		t.Fatalf("expiry alternatives = %v", res.SafeAlternatives)
	}
	if removed := store.ClearExpired(now.Add(2 * time.Minute)); removed != 1 {
		t.Fatalf("ClearExpired removed %d locators, want 1", removed)
	}
	if _, err := store.Resume(loc.Token, alice, now.Add(2*time.Minute+time.Second), &allowGov{}); err != ErrLocatorNotFound {
		t.Fatalf("cleared locator resume error = %v, want ErrLocatorNotFound", err)
	}
}

func TestUXFLOW006StaleResumeReplacesContinueWithSafeActions(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-stale", "collect", "alice", time.Minute)
	_, _ = store.SaveDraft(loc.FlowID, "alice", loc.StageID, 0, map[string]string{"name": "v1"}, now)
	_, _ = store.SaveDraft(loc.FlowID, "alice", loc.StageID, 1, map[string]string{"name": "v2"}, now.Add(time.Second))
	res, err := store.Resume(loc.Token, alice, now.Add(10*time.Second), &allowGov{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stale {
		t.Fatal("version-advanced resume was not marked stale")
	}
	if !reflect.DeepEqual(res.SafeAlternatives, []string{"refresh", "replan"}) {
		t.Fatalf("stale actions = %v, want refresh/replan only", res.SafeAlternatives)
	}
}

func TestUXFLOW006DeviceCASAllowsOneWriter(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	store := NewStore(func() time.Time { return now })
	_, _ = store.SaveDraft("flow-device", "alice", "collect", 0, map[string]string{"value": "base"}, now)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range []string{"device-a", "device-b"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			_, err := store.SaveDraft("flow-device", "alice", "collect", 1, map[string]string{"value": value}, now.Add(time.Second))
			results <- err
		}(value)
	}
	wg.Wait()
	close(results)
	var success, conflicts int
	for err := range results {
		switch err {
		case nil:
			success++
		case ErrConflict:
			conflicts++
		default:
			t.Fatalf("unexpected device write error: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("device CAS results: success=%d conflicts=%d", success, conflicts)
	}
}

func TestUXFLOW006RefreshBackIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	store := NewStore(func() time.Time { return now })
	alice := newTestPrincipalResume(t, "alice")
	loc, _ := store.CreateLocator("flow-idempotent", "collect", "alice", time.Minute)
	_, _ = store.SaveDraft(loc.FlowID, "alice", loc.StageID, 0, map[string]string{"name": "Alice", "salary": "100000"}, now)
	first, err := store.Resume(loc.Token, alice, now.Add(10*time.Second), &allowGov{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Resume(loc.Token, alice, now.Add(10*time.Second), &allowGov{})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Idempotent || first.CurrentVersion != second.CurrentVersion || !reflect.DeepEqual(first.MaskedState, second.MaskedState) {
		t.Fatalf("refresh/back changed result: first=%+v second=%+v", first, second)
	}
}

func containsSubstring(s, needle string) bool {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
