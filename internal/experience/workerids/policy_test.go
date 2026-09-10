package workerids

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPreviewSkipsRangesWithoutAllocating(t *testing.T) {
	p := DefaultPolicy()
	p.SequenceDigits, p.StartAt, p.NextSequence, p.IncrementBy = 12, 1, 1, 3
	p.ExcludedRanges = "10-999999999990,1-12"
	before := p
	got, err := Preview(p, FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	want := []string{"HC-999999999991", "HC-999999999994", "HC-999999999997"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Preview = %v, %v; want %v", got, err, want)
	}
	if p != before {
		t.Fatal("preview mutated allocation state")
	}
	p.ExcludedRanges = "0-999999999999"
	got, err = Preview(p, FormatContext{})
	if err != nil || len(got) != 0 {
		t.Fatalf("exhausted preview = %v, %v", got, err)
	}
	p.ExcludedRanges = "invalid"
	if _, err := Preview(p, FormatContext{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid preview error = %v", err)
	}
}

func BenchmarkPreviewLargeExclusion(b *testing.B) {
	p := DefaultPolicy()
	p.SequenceDigits = 12
	p.ExcludedRanges = "1000-999999999990"
	ctx := FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	for b.Loop() {
		if _, err := Preview(p, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

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
