package workerids

import (
	"errors"
	"testing"
	"time"
)

func TestFormatSupportsOrganizationRules(t *testing.T) {
	p := Policy{Prefix: " hc ", Suffix: "emp", Separator: "-", SequenceDigits: 6, StartAt: 1, NextSequence: 42, IncrementBy: 5, ZeroPad: true, YearFormat: YearYYYY, IncludeUnitCode: true, CheckDigit: CheckLuhn}
	got, err := Format(p, 42, FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UnitCode: "Care Ops"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "HC-2026-CAREOP-000042-2-EMP" {
		t.Fatalf("Format() = %q", got)
	}
}

func TestPolicyRejectsOverflowAndBadRanges(t *testing.T) {
	if _, err := Format(Policy{SequenceDigits: 2, StartAt: 1, NextSequence: 1, IncrementBy: 1, YearFormat: YearNone, CheckDigit: CheckNone}, 100, FormatContext{}); !errors.Is(err, ErrExhausted) {
		t.Fatalf("overflow err=%v", err)
	}
	p := DefaultPolicy()
	p.ExcludedRanges = "10-2"
	if err := Validate(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("range err=%v", err)
	}
}

func TestExcludedRanges(t *testing.T) {
	p := DefaultPolicy()
	p.ExcludedRanges = "10-19, 42; 100-110"
	if !IsExcluded(p, 10) || !IsExcluded(p, 42) || IsExcluded(p, 43) {
		t.Fatal("excluded-range membership wrong")
	}
}
