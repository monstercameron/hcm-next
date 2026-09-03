package racecheck

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCheckRecordsPassAndFailureWithoutRunningGo(t *testing.T) {
	var calls [][]string
	report, err := Check(context.Background(), "root", []string{"./internal/workflow", "./internal/platform/cache"}, func(_ context.Context, _ string, args []string) ([]byte, error) {
		calls = append(calls, args)
		if args[len(args)-1] == "./internal/platform/cache" {
			return []byte("FAIL: concurrent test"), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete() {
		t.Fatal("failed package incorrectly reported complete")
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("runner calls = %d, want %d", got, want)
	}
	wantArgs := []string{"test", "-race", "-count=1", "./internal/workflow"}
	if !reflect.DeepEqual(calls[0], wantArgs) {
		t.Fatalf("arguments = %#v, want %#v", calls[0], wantArgs)
	}
}

func TestCheckDoesNotTreatUnsupportedAsCoverage(t *testing.T) {
	report, err := Check(context.Background(), ".", []string{"./internal/workflow"}, func(context.Context, string, []string) ([]byte, error) {
		return []byte("go: -race is not supported on windows/arm64"), errors.New("exit status 2")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := report.Results[0]
	if !got.Unsupported || got.Eligible || got.Covered || report.Complete() {
		t.Fatalf("unsupported result = %#v; report=%v", got, report.Complete())
	}
}

func TestCheckRejectsEmptyPatterns(t *testing.T) {
	if _, err := Check(context.Background(), ".", nil, nil); err == nil {
		t.Fatal("empty package set accepted")
	}
	if _, err := Check(context.Background(), ".", []string{"  "}, nil); err == nil {
		t.Fatal("blank package pattern accepted")
	}
}

func TestIsUnsupportedVariants(t *testing.T) {
	for _, output := range []string{
		"go: -race is not supported on windows/arm64",
		"race detector is not supported on this platform",
		"flag -race is not supported",
		"not supported on linux/386: race",
	} {
		if !IsUnsupported(output) {
			t.Errorf("IsUnsupported(%q) = false", output)
		}
	}
	if IsUnsupported("--- FAIL: Test (DATA RACE)") {
		t.Fatal("ordinary race failure classified as unsupported")
	}
}

func TestExplain(t *testing.T) {
	r := Report{Results: []PackageResult{{Pattern: "./ok", Eligible: true, Covered: true}, {Pattern: "./skip", Unsupported: true}, {Pattern: "./fail", Eligible: true}}}
	if got, want := r.Explain(), "PASS ./ok\nUNSUPPORTED ./skip\nFAIL ./fail"; got != want {
		t.Fatalf("Explain() = %q, want %q", got, want)
	}
}
