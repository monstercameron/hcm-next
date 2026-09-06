package documentsecurity

import (
	"context"
	"errors"
	"testing"
)

func TestLimitsValidatorAndDigestBoundaries(t *testing.T) {
	if !testLimits().valid() || (Limits{}).valid() || (Limits{MaxBytes: 1}).valid() {
		t.Fatal("invalid limits accepted")
	}
	validator := Validator{}
	valid := Artifact{ID: "a", OriginalDigest: digest([]byte("raw")), DerivativeDigest: digest([]byte("safe")), State: Safe, Scanner: "av", ScannerVersion: "1", Limits: testLimits()}
	if err := validator.Validate(valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*Artifact)
		want error
	}{
		{"state", func(a *Artifact) { a.State = Unsafe }, ErrNotSafe},
		{"id", func(a *Artifact) { a.ID = "" }, ErrNotSafe},
		{"original", func(a *Artifact) { a.OriginalDigest = "" }, ErrNotSafe},
		{"derivative", func(a *Artifact) { a.DerivativeDigest = "" }, ErrNotSafe},
		{"scanner", func(a *Artifact) { a.Scanner = "" }, ErrNotSafe},
		{"scanner version", func(a *Artifact) { a.ScannerVersion = "" }, ErrNotSafe},
		{"limits", func(a *Artifact) { a.Limits = Limits{} }, ErrNotSafe},
		{"original digest shape", func(a *Artifact) { a.OriginalDigest = "bad" }, ErrDigestMismatch},
		{"derivative digest shape", func(a *Artifact) { a.DerivativeDigest = "bad" }, ErrDigestMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			tc.edit(&bad)
			if err := validator.Validate(bad); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
	if !digestString(digest([]byte("x"))) || digestString("sha256:xyz") || digestString("sha256:") {
		t.Fatal("digestString boundary mismatch")
	}
}

func TestRegistryScanAdmissionAndScannerVerdicts(t *testing.T) {
	limits := testLimits()
	scanner := scannerFunc(func(_ context.Context, in ScanInput, got Limits) (Verdict, error) {
		if in.Digest != digest(in.Bytes) || got != limits {
			t.Fatalf("scanner input not bound: %+v, %+v", in, got)
		}
		in.Bytes[0] = 'x'
		return Verdict{State: Safe, Scanner: "av", ScannerVersion: "2", Derivative: []byte("clean")}, nil
	})
	r := NewRegistry()
	a, err := r.Scan(context.Background(), Upload{ID: "safe", Name: "x", ContentType: "text/plain", Bytes: []byte("raw")}, limits, scanner)
	if err != nil || a.State != Safe || a.DerivativeDigest != digest([]byte("clean")) {
		t.Fatalf("safe scan = %+v, %v", a, err)
	}
	stored, ok := r.Get("safe")
	if !ok || stored.State != Safe {
		t.Fatalf("stored safe artifact = %+v, %v", stored, ok)
	}
	cases := []struct {
		name      string
		u         Upload
		lim       Limits
		s         Scanner
		wantState State
		wantErr   error
	}{
		{"nil scanner", Upload{ID: "bad", Bytes: []byte("x")}, limits, nil, "", ErrInvalidUpload},
		{"nil registry", Upload{ID: "bad", Bytes: []byte("x")}, limits, scanner, "", ErrInvalidUpload},
		{"missing id", Upload{Bytes: []byte("x")}, limits, scanner, "", ErrInvalidUpload},
		{"empty bytes", Upload{ID: "bad"}, limits, scanner, "", ErrInvalidUpload},
		{"bad limits", Upload{ID: "bad", Bytes: []byte("x")}, Limits{}, scanner, "", ErrInvalidUpload},
	}
	var nilRegistry *Registry
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Artifact
			var err error
			if tc.name == "nil registry" {
				got, err = nilRegistry.Scan(context.Background(), tc.u, tc.lim, tc.s)
			} else {
				got, err = r.Scan(context.Background(), tc.u, tc.lim, tc.s)
			}
			if !errors.Is(err, tc.wantErr) || got.State != tc.wantState {
				t.Fatalf("scan = %+v, %v", got, err)
			}
		})
	}
	oversized := limits
	oversized.MaxBytes = 1
	got, err := r.Scan(context.Background(), Upload{ID: "large", Bytes: []byte("raw")}, oversized, scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		t.Fatal("scanner called")
		return Verdict{}, nil
	}))
	if !errors.Is(err, ErrLimitExceeded) || got.State != Unsafe {
		t.Fatalf("oversized = %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		v    Verdict
		want State
	}{
		{"unsafe", Verdict{State: Unsafe, Scanner: "av", ScannerVersion: "1"}, Unsafe},
		{"unscannable", Verdict{State: Unscannable, Scanner: "av", ScannerVersion: "1"}, Unscannable},
		{"quarantined", Verdict{State: Quarantined, Scanner: "av", ScannerVersion: "1"}, Quarantined},
		{"unknown", Verdict{State: "MAYBE", Scanner: "av", ScannerVersion: "1"}, Unscannable},
		{"safe missing identity", Verdict{State: Safe, Derivative: []byte("d")}, Unscannable},
		{"safe missing derivative", Verdict{State: Safe, Scanner: "av", ScannerVersion: "1"}, Unscannable},
		{"safe oversized derivative", Verdict{State: Safe, Scanner: "av", ScannerVersion: "1", Derivative: []byte("large")}, Unscannable},
		{"safe bad derivative digest", Verdict{State: Safe, Scanner: "av", ScannerVersion: "1", Derivative: []byte("d"), DerivativeDigest: "bad"}, Unscannable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lim := limits
			if tc.name == "safe oversized derivative" {
				lim.MaxDerivativeBytes = 1
			}
			a, err := r.Scan(context.Background(), Upload{ID: "v-" + tc.name, Bytes: []byte("raw")}, lim, scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) { return tc.v, nil }))
			if err != nil || a.State != tc.want {
				t.Fatalf("verdict = %+v, %v", a, err)
			}
		})
	}
	providerError, err := r.Scan(context.Background(), Upload{ID: "provider-error", Bytes: []byte("raw")}, limits, scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) { return Verdict{}, errors.New("offline") }))
	if err != nil || providerError.State != Unscannable || providerError.Reason != "offline" {
		t.Fatalf("provider error = %+v, %v", providerError, err)
	}
}

func TestRegistryRescanRevokeAndFailClosedState(t *testing.T) {
	r := NewRegistry()
	limits := testLimits()
	if _, err := r.Rescan(context.Background(), "missing", scannerFunc(nil)); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("missing rescan = %v", err)
	}
	if _, err := r.RescanUpload(context.Background(), Upload{ID: "x", Bytes: []byte("raw")}, limits, scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		return Verdict{State: Safe, Scanner: "av", ScannerVersion: "1", Derivative: []byte("d")}, nil
	})); err != nil {
		t.Fatal(err)
	}
	pending, err := r.Rescan(context.Background(), "x", scannerFunc(nil))
	if !errors.Is(err, ErrNotSafe) || pending.State != Quarantined || pending.DerivativeDigest != "" {
		t.Fatalf("rescan pending = %+v, %v", pending, err)
	}
	if got, ok := r.Get("x"); !ok || got.State != Quarantined {
		t.Fatalf("pending state not stored = %+v, %v", got, ok)
	}
	if !r.Revoke("x", "manual review") {
		t.Fatal("existing artifact not revoked")
	}
	if got, _ := r.Get("x"); got.State != Quarantined || got.Reason != "manual review" || got.DerivativeDigest != "" {
		t.Fatalf("revoked artifact = %+v", got)
	}
	if r.Revoke("missing", "reason") {
		t.Fatal("missing artifact revoked")
	}
	var nilRegistry *Registry
	if _, err := nilRegistry.RescanUpload(context.Background(), Upload{ID: "x"}, limits, nil); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("nil rescan upload = %v", err)
	}
}
