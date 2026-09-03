package documentsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// TestTodo_DOC_MAL_001 is the primary quarantine-boundary contract.  A
// consumer can only receive a content address after a SAFE verdict and a
// bounded derivative have been recorded.
func TestTodo_DOC_MAL_001(t *testing.T) {
	r := NewRegistry()
	limits := testLimits()
	seen := false
	a, err := r.Scan(context.Background(), Upload{ID: "mal-001", Name: "upload.bin", Bytes: []byte("raw")}, limits,
		scannerFunc(func(_ context.Context, in ScanInput, got Limits) (Verdict, error) {
			seen = true
			if in.Digest != digest(in.Bytes) || got != limits {
				t.Fatalf("scanner input is not canonical: %#v %#v", in, got)
			}
			return Verdict{State: Safe, Scanner: "fixture-av", ScannerVersion: "2026.09", Derivative: []byte("sanitized")}, nil
		}))
	if err != nil || !seen || a.State != Safe {
		t.Fatalf("scan = %#v, %v", a, err)
	}
	if err := (Validator{}).Validate(a); err != nil {
		t.Fatalf("safe artifact rejected: %v", err)
	}

	unsafe, err := r.RescanUpload(context.Background(), Upload{ID: "mal-001", Bytes: []byte("raw")}, limits,
		scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
			return Verdict{State: Unsafe, Scanner: "fixture-av", ScannerVersion: "2026.09", Reason: "signature"}, nil
		}))
	if err != nil || unsafe.State != Unsafe || unsafe.DerivativeDigest != "" {
		t.Fatalf("unsafe rescan = %#v, %v", unsafe, err)
	}
	if err := (Validator{}).Validate(unsafe); !errors.Is(err, ErrNotSafe) {
		t.Fatalf("unsafe artifact admitted: %v", err)
	}
}

func TestTodo_DOC_MAL_001_Golden(t *testing.T) {
	const want = "sha256:44d8b78bec6ce60168d0849c40fd060d7ddf8d50d9194d99ce1f1240d84f7cbd"
	// Keep the oracle explicit and independently calculated; this catches
	// accidental changes to the content-address format without storing bytes.
	got := digest([]byte("sanitized"))
	sum := sha256.Sum256([]byte("sanitized"))
	if got != want || got != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %q, want %q", got, want)
	}
}

func TestTodo_DOC_MAL_001_Integration(t *testing.T) {
	r := NewRegistry()
	limits := testLimits()
	cases := []struct {
		name  string
		state State
		err   error
		v     Verdict
	}{
		{name: "infected", state: Unsafe, v: Verdict{State: Unsafe, Scanner: "av", ScannerVersion: "1", Reason: "infected"}},
		{name: "provider unavailable", state: Unscannable, v: Verdict{}, err: errors.New("offline")},
		{name: "scanner quarantine", state: Quarantined, v: Verdict{State: Quarantined, Scanner: "av", ScannerVersion: "1"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := "integration-" + string(rune('a'+i))
			a, err := r.Scan(context.Background(), Upload{ID: id, Bytes: []byte("payload")}, limits,
				scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) { return tc.v, tc.err }))
			if err != nil || a.State != tc.state {
				t.Fatalf("scan = %#v, %v", a, err)
			}
			if err := (Validator{}).Validate(a); !errors.Is(err, ErrNotSafe) {
				t.Fatalf("non-safe artifact admitted: %v", err)
			}
			stored, ok := r.Get(id)
			if !ok || stored.State != tc.state || stored.OriginalDigest == "" {
				t.Fatalf("registry record = %#v, found=%v", stored, ok)
			}
		})
	}
}

func FuzzTodo_DOC_MAL_001(f *testing.F) {
	f.Add([]byte("clean"))
	f.Add([]byte("EICAR"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) == 0 || len(payload) > 128 {
			t.Skip()
		}
		r := NewRegistry()
		a, err := r.Scan(context.Background(), Upload{ID: "fuzz", Bytes: payload}, testLimits(),
			scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
				return Verdict{State: Safe, Scanner: "fuzz-av", ScannerVersion: "1", Derivative: []byte("safe")}, nil
			}))
		if err != nil || a.State != Safe || (Validator{}).Validate(a) != nil {
			t.Fatalf("safe fixture rejected: %#v, %v", a, err)
		}
	})
}
