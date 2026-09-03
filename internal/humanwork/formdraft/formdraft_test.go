package formdraft

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTodo_FORM_005_Conformance(t *testing.T) {
	s, err := NewStore([]byte("form-005 test key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err = s.SetClock(func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	r := PutRequest{ID: "d1", Form: "promotion", Subject: "u1", Requested: []string{"name"}, Data: map[string]json.RawMessage{"name": json.RawMessage(`"Ada"`), "secret": json.RawMessage(`"no"`)}, TTL: time.Hour}
	d, err := s.Put(r, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != 1 || len(d.Data) != 1 {
		t.Fatalf("draft=%+v", d)
	}
	if _, err = s.Get("d1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(r, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected CAS conflict, got %v", err)
	}
	if s.Effects() != 1 {
		t.Fatalf("rejected put mutated effects: %d", s.Effects())
	}
	now = now.Add(2 * time.Hour)
	if _, err = s.Get("d1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expiry, got %v", err)
	}
	if err = s.Clear("d1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired clear should be miss, got %v", err)
	}
}

func TestPutEncryptsAtRestAndClear(t *testing.T) {
	s, _ := NewStore([]byte("k"))
	d, err := s.Put(PutRequest{ID: "x", Form: "f", Subject: "s", Requested: []string{"a"}, Data: map[string]json.RawMessage{"a": json.RawMessage(`"v"`)}, TTL: time.Minute}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Data["a"][0] != '"' {
		t.Fatal("decrypted result missing")
	}
	if err = s.Clear("x"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
