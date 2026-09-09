package quarantine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// compile-time proof that fakeScanner satisfies quarantine.Scanner.
var _ quarantine.Scanner = (*fakeScanner)(nil)

func TestFakeScannerSatisfiesScannerPort(t *testing.T) {
	s := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
	v, err := s.Scan(context.Background(), "deadbeef", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !v.Safe {
		t.Error("expected the configured Safe verdict")
	}
	if len(s.calls) != 1 || s.calls[0] != "deadbeef" {
		t.Fatalf("calls = %v, want a single call recording the digest", s.calls)
	}
}

func TestFakeScannerReportsConfiguredError(t *testing.T) {
	s := &fakeScanner{err: errScannerUnavailable}
	v, err := s.Scan(context.Background(), "deadbeef", strings.NewReader("content"))
	if err == nil {
		t.Fatal("expected the configured error")
	}
	if v.Safe {
		t.Error("a scanner failure must not also report Safe")
	}
}

func TestVerdictZeroValueIsUnsafe(t *testing.T) {
	var v quarantine.Verdict
	if v.Safe {
		t.Error("the zero Verdict must not be Safe: an unconfigured scanner never defaults to admitting content")
	}
}
