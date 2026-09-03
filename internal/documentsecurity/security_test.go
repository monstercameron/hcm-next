package documentsecurity

import (
	"context"
	"errors"
	"testing"
)

type scannerFunc func(context.Context, ScanInput, Limits) (Verdict, error)

func (f scannerFunc) Scan(c context.Context, i ScanInput, l Limits) (Verdict, error) {
	return f(c, i, l)
}
func testLimits() Limits {
	return Limits{MaxBytes: 1024, MaxDerivativeBytes: 1024, MaxCompressionRatio: 100}
}

func TestScanSafeRequiresBoundedDerivativeAndValidator(t *testing.T) {
	r := NewRegistry()
	a, err := r.Scan(context.Background(), Upload{ID: "u1", Name: "resume.pdf", Bytes: []byte("raw")}, testLimits(), scannerFunc(func(_ context.Context, in ScanInput, _ Limits) (Verdict, error) {
		return Verdict{State: Safe, Scanner: "test-av", ScannerVersion: "1", Derivative: []byte("sanitized")}, nil
	}))
	if err != nil || a.State != Safe {
		t.Fatalf("scan = %#v, %v", a, err)
	}
	if err := (Validator{}).Validate(a); err != nil {
		t.Fatalf("safe artifact rejected: %v", err)
	}
	if a.OriginalDigest == a.DerivativeDigest {
		t.Fatal("safe derivative must have its own content address")
	}
}

func TestScanFailsClosedOnProviderErrorAndNoRawArtifactIsAdmitted(t *testing.T) {
	r := NewRegistry()
	a, err := r.Scan(context.Background(), Upload{ID: "u2", Bytes: []byte("raw")}, testLimits(), scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		return Verdict{}, errors.New("scanner unavailable")
	}))
	if err != nil || a.State != Unscannable {
		t.Fatalf("scan = %#v, %v", a, err)
	}
	if err := (Validator{}).Validate(a); !errors.Is(err, ErrNotSafe) {
		t.Fatalf("Validate error = %v, want ErrNotSafe", err)
	}
}

func TestOversizedUploadIsQuarantinedFromConsumers(t *testing.T) {
	r := NewRegistry()
	lim := testLimits()
	lim.MaxBytes = 2
	a, err := r.Scan(context.Background(), Upload{ID: "u3", Bytes: []byte("large")}, lim, scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		t.Fatal("scanner called for bytes over admission limit")
		return Verdict{}, nil
	}))
	if !errors.Is(err, ErrLimitExceeded) || a.State != Unsafe {
		t.Fatalf("scan = %#v, %v", a, err)
	}
	if err := (Validator{}).Validate(a); !errors.Is(err, ErrNotSafe) {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestUnknownScannerStateFailsClosed(t *testing.T) {
	r := NewRegistry()
	a, err := r.Scan(context.Background(), Upload{ID: "u4", Bytes: []byte("x")}, testLimits(), scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		return Verdict{State: "MAYBE", Scanner: "x", ScannerVersion: "1", Derivative: []byte("y")}, nil
	}))
	if err != nil || a.State != Unscannable {
		t.Fatalf("scan = %#v, %v", a, err)
	}
}

func TestRescanRevokesPriorSafeDerivativeBeforeUnsafeVerdict(t *testing.T) {
	r := NewRegistry()
	safe := scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		return Verdict{State: Safe, Scanner: "av", ScannerVersion: "1", Derivative: []byte("clean")}, nil
	})
	a, err := r.Scan(context.Background(), Upload{ID: "u5", Bytes: []byte("raw")}, testLimits(), safe)
	if err != nil || a.State != Safe {
		t.Fatalf("initial scan = %#v, %v", a, err)
	}
	unsafe := scannerFunc(func(context.Context, ScanInput, Limits) (Verdict, error) {
		return Verdict{State: Unsafe, Scanner: "av", ScannerVersion: "2", Reason: "malware"}, nil
	})
	a, err = r.RescanUpload(context.Background(), Upload{ID: "u5", Bytes: []byte("raw")}, testLimits(), unsafe)
	if err != nil || a.State != Unsafe {
		t.Fatalf("rescan = %#v, %v", a, err)
	}
	if err := (Validator{}).Validate(a); !errors.Is(err, ErrNotSafe) {
		t.Fatalf("unsafe descendant accepted: %v", err)
	}
}
