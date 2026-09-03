package content

import (
	"context"
	"errors"
	"testing"
	"time"
)

func policy() Policy {
	return Policy{Limits: Limits{MaxBytes: 1024}, Extensions: map[string]struct{}{".pdf": {}}, MIME: map[string]struct{}{"application/pdf": {}}}
}
func scanner(v Verdict) Scanner {
	return func(context.Context, []byte) (Inspection, error) {
		return Inspection{Verdict: v, ScannerVersion: "scanner-1"}, nil
	}
}
func transformer(context.Context, []byte) ([]byte, error) { return []byte("safe derivative"), nil }

func TestTodo_TRUST_019(t *testing.T) {
	a, err := New(Ingress{ID: "obj-1", Tenant: "tenant-a", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, []byte("%PDF-hostile"), time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().State != Quarantined {
		t.Fatalf("state=%s", a.Snapshot().State)
	}
	if err := a.Validate(policy(), time.Unix(11, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.Scan(context.Background(), scanner(VerdictSafe), time.Unix(12, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.Transform(context.Background(), transformer, time.Unix(13, 0)); err != nil {
		t.Fatal(err)
	}
	d, err := a.Promoted()
	if err != nil || d.Classification != "SAFE" || d.SourceDigest == "" {
		t.Fatalf("promote=%+v err=%v", d, err)
	}
}

func TestTodo_TRUST_019_Security(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mime, file string
		body       []byte
	}{
		{"oversize", "application/pdf", "x.pdf", []byte("%PDF-too-large")},
		{"polyglot signature mismatch", "application/pdf", "x.pdf", []byte("PK\\x03\\x04")},
		{"bad extension", "application/pdf", "x.exe", []byte("%PDF-data")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := policy()
			if tc.name == "oversize" {
				p.Limits.MaxBytes = 2
			}
			a, _ := New(Ingress{ID: tc.name, Tenant: "t", Source: "upload", Filename: tc.file, DeclaredMIME: tc.mime}, tc.body, time.Unix(1, 0))
			if err := a.Validate(p, time.Unix(2, 0)); err == nil {
				t.Fatal("unsafe content accepted")
			}
			if _, err := a.Promoted(); !errors.Is(err, ErrNotPromotable) {
				t.Fatalf("promoted unsafe: %v", err)
			}
		})
	}
	// Scanner outage is fail-closed and leaves the object quarantined.
	a, _ := New(Ingress{ID: "outage", Tenant: "t", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, []byte("%PDF-data"), time.Unix(1, 0))
	_ = a.Validate(policy(), time.Unix(2, 0))
	if err := a.Scan(context.Background(), nil, time.Unix(3, 0)); err == nil || a.Snapshot().State != Quarantined {
		t.Fatalf("outage state=%s err=%v", a.Snapshot().State, err)
	}
}

func TestTodo_TRUST_019_RescanRevokesOldDerivative(t *testing.T) {
	a, _ := New(Ingress{ID: "obj", Tenant: "t", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, []byte("%PDF-data"), time.Unix(1, 0))
	if err := a.Rescan(context.Background(), policy(), scanner(VerdictSafe), transformer, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	old, _ := a.Promoted()
	if old.Revoked {
		t.Fatal("initial derivative revoked")
	}
	if err := a.Rescan(context.Background(), policy(), scanner(VerdictUnsafe), transformer, time.Unix(3, 0)); err == nil {
		t.Fatal("unsafe rescan accepted")
	}
	if _, err := a.Promoted(); !errors.Is(err, ErrNotPromotable) {
		t.Fatalf("revoked derivative promoted: %v", err)
	}
	if got := len(a.Derivatives()); got != 1 {
		t.Fatalf("history lost: %d", got)
	}
}

// TestTodo_TRUST_019_Mutation protects the fail-closed quarantine boundary:
// changing any processing dependency must not make untrusted bytes promotable.
func TestTodo_TRUST_019_Mutation(t *testing.T) {
	cases := []struct {
		name      string
		scan      Scanner
		transform Transformer
		state     State
	}{
		{"scanner unavailable", nil, transformer, Quarantined},
		{"scanner unsafe", scanner(VerdictUnsafe), transformer, Rejected},
		{"scanner unscannable", scanner(VerdictUnscannable), transformer, Rejected},
		{"transformer unavailable", scanner(VerdictSafe), nil, Quarantined},
		{"transformer failure", scanner(VerdictSafe), func(context.Context, []byte) ([]byte, error) {
			return nil, errors.New("transform failed")
		}, Rejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(Ingress{ID: tc.name, Tenant: "t", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, []byte("%PDF-data"), time.Unix(1, 0))
			if err != nil {
				t.Fatal(err)
			}
			err = a.Rescan(context.Background(), policy(), tc.scan, tc.transform, time.Unix(2, 0))
			if err == nil || a.Snapshot().State != tc.state {
				t.Fatalf("state=%s err=%v", a.Snapshot().State, err)
			}
			if _, err := a.Promoted(); !errors.Is(err, ErrNotPromotable) {
				t.Fatalf("quarantined artifact promoted: %v", err)
			}
		})
	}
}

func FuzzTodo_TRUST_019(f *testing.F) {
	f.Add([]byte("%PDF-data"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) == 0 {
			return
		}
		a, err := New(Ingress{ID: "fuzz", Tenant: "t", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, payload, time.Unix(1, 0))
		if err != nil {
			return
		}
		_ = a.Rescan(context.Background(), policy(), scanner(VerdictSafe), transformer, time.Unix(2, 0))
		if a.Snapshot().State == Safe {
			if _, err := a.Promoted(); err != nil {
				t.Fatalf("safe artifact not promotable: %v", err)
			}
		} else if _, err := a.Promoted(); !errors.Is(err, ErrNotPromotable) {
			t.Fatalf("non-safe artifact promoted: %v", err)
		}
	})
}
