package latencygate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEvaluateUsesNearestRankWithoutMutatingCorpus(t *testing.T) {
	durations := make([]time.Duration, 100)
	for index := range durations {
		durations[index] = time.Duration(100-index) * time.Millisecond
	}
	result := Evaluate("fixture", durations)
	if result.Samples != 100 || result.P50 != 50*time.Millisecond || result.P95 != 95*time.Millisecond || result.Max != 100*time.Millisecond {
		t.Fatalf("result = %+v", result)
	}
	if durations[0] != 100*time.Millisecond || durations[99] != time.Millisecond {
		t.Fatal("Evaluate mutated the caller's duration corpus")
	}
}

func TestCheckReportsActionableBreach(t *testing.T) {
	budget := Budget{Name: "people sort", P95: 50 * time.Millisecond, Samples: 20}
	err := Check(budget, Result{Name: budget.Name, Samples: 20, P50: 20 * time.Millisecond, P95: 51 * time.Millisecond, Max: 70 * time.Millisecond})
	if err == nil {
		t.Fatal("expected breached budget to fail")
	}
	for _, want := range []string{"people sort", "51ms", "50ms", "p50=20ms", "max=70ms", "n=20"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("breach error %q missing %q", err, want)
		}
	}
}

func TestMeasureWarmsUpAndPropagatesOperationFailure(t *testing.T) {
	budget := Budget{Name: "fixture", P95: time.Second, Warmups: 2, Samples: 20}
	calls := 0
	result, err := Measure(budget, func() error {
		calls++
		if calls == 7 {
			return errors.New("render failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "sample 5") || !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBudgetValidationRejectsStatisticallyWeakGate(t *testing.T) {
	_, err := Measure(Budget{Name: "fixture", P95: time.Second, Samples: 19}, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "at least 20 samples") {
		t.Fatalf("err = %v", err)
	}
}
