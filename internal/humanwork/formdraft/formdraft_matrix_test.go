package formdraft

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func matrixStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := NewStore([]byte("form-005 matrix key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := s.SetClock(func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	return s, &now
}

func TestFORM005Matrix_EncryptionAtRestAndResumeAuthentication(t *testing.T) {
	s, _ := matrixStore(t)
	plain := json.RawMessage(`"private-value"`)
	if _, err := s.Put(PutRequest{ID: "auth", Form: "f", Subject: "s", Requested: []string{"answer"}, Data: map[string]json.RawMessage{"answer": plain}, TTL: time.Hour}, 0); err != nil {
		t.Fatal(err)
	}

	s.mu.RLock()
	r := s.drafts["auth"]
	s.mu.RUnlock()
	if string(r.ciphertext) == "" || string(r.ciphertext) == string(plain) || containsBytes(r.ciphertext, []byte("private-value")) {
		t.Fatalf("draft plaintext exposed at rest: %q", r.ciphertext)
	}

	foreign, err := NewStore([]byte("different key"))
	if err != nil {
		t.Fatal(err)
	}
	foreign.drafts["auth"] = r
	if _, err := foreign.Resume("auth"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resume with foreign key = %v, want ErrInvalid", err)
	}
	if _, err := s.Resume(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty resume id = %v, want ErrInvalid", err)
	}

	s.mu.Lock()
	r.ciphertext[0] ^= 1
	s.drafts["auth"] = r
	s.mu.Unlock()
	if _, err := s.Resume("auth"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered resume = %v, want ErrInvalid", err)
	}
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestFORM005Matrix_ConcurrentCASHasSingleWinner(t *testing.T) {
	s, _ := matrixStore(t)
	req := PutRequest{ID: "cas", Form: "f", Subject: "s", Requested: []string{"x"}, Data: map[string]json.RawMessage{"x": json.RawMessage(`1`)}, TTL: time.Hour}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Put(req, 0)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Errorf("concurrent create = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 || conflicts != 15 || s.Effects() != 1 {
		t.Fatalf("wins=%d conflicts=%d effects=%d", wins, conflicts, s.Effects())
	}
	if _, err := s.Put(req, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(req, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update = %v, want ErrConflict", err)
	}
}

func TestFORM005Matrix_ExpiryClearRequestedOnlyAndZeroEffects(t *testing.T) {
	s, now := matrixStore(t)
	bad := PutRequest{ID: "", Form: "f", TTL: time.Hour}
	if _, err := s.Put(bad, 0); !errors.Is(err, ErrInvalid) || s.Effects() != 0 {
		t.Fatalf("invalid put err=%v effects=%d", err, s.Effects())
	}

	req := PutRequest{ID: "expiry", Form: "f", Subject: "s", Requested: []string{"kept"}, Data: map[string]json.RawMessage{"kept": json.RawMessage(`"yes"`), "discarded": json.RawMessage(`"no"`)}, TTL: time.Hour}
	d, err := s.Put(req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Data) != 1 || string(d.Data["kept"]) != `"yes"` {
		t.Fatalf("requested-only data=%v", d.Data)
	}
	req.Data["kept"] = json.RawMessage(`"mutated-after-put"`)
	resumed, err := s.Get("expiry")
	if err != nil || string(resumed.Data["kept"]) != `"yes"` {
		t.Fatalf("stored data aliased caller input: draft=%v err=%v", resumed.Data, err)
	}
	*now = d.ExpiresAt
	if _, err := s.Get("expiry"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired get=%v", err)
	}
	if got := s.IDs(); len(got) != 0 {
		t.Fatalf("expired IDs=%v", got)
	}
	if err := s.Clear("expiry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clear after expiry=%v", err)
	}
	if s.Effects() != 1 {
		t.Fatalf("expiry/misses changed effects=%d", s.Effects())
	}
	if _, err := s.Put(req, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("expiry"); err != nil {
		t.Fatal(err)
	}
	if s.Effects() != 3 {
		t.Fatalf("successful put/clear effects=%d", s.Effects())
	}
}
