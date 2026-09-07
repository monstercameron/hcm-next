package covergate

import (
	"strings"
	"testing"
)

func TestParseGoTestOutputCarriesTheFailingTestLog(t *testing.T) {
	out := strings.Join([]string{
		"=== RUN   TestServiceCreate",
		"    service_test.go:42: pgtest: create schema pgtest_1: context deadline exceeded",
		"    service_test.go:43: second detail line",
		"--- FAIL: TestServiceCreate (101.13s)",
		"FAIL",
		"FAIL\tgithub.com/monstercameron/hcm-next/internal/data/ledger/checkpoint\t135.037s",
		"ok  \tgithub.com/monstercameron/hcm-next/internal/data/outbox\t278.314s\tcoverage: 67.9% of statements",
	}, "\n")
	results := ParseGoTestOutput(out)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	fail := results[0]
	if fail.Status != "fail" || len(fail.FailedTests) != 1 || fail.FailedTests[0] != "TestServiceCreate" {
		t.Fatalf("failing result = %+v", fail)
	}
	if len(fail.FailureLog) != 2 || !strings.HasPrefix(fail.FailureLog[0], "service_test.go:42: pgtest: create schema") {
		t.Fatalf("failure log = %v, want the two indented test log lines", fail.FailureLog)
	}
	if !strings.Contains(fail.Line, "context deadline exceeded") {
		t.Fatalf("the finding line %q does not carry the failure reason", fail.Line)
	}
	if ok := results[1]; ok.Status != "ok" || len(ok.FailureLog) != 0 {
		t.Fatalf("the following ok package inherited log lines: %+v", ok)
	}
}

func TestParseGoTestOutputBoundsTheFailureLog(t *testing.T) {
	var lines []string
	for i := 0; i < MaxFailureLogLines+20; i++ {
		lines = append(lines, "    noisy_test.go:1: line")
	}
	lines = append(lines, "--- FAIL: TestNoisy (0.00s)", "FAIL\tgithub.com/monstercameron/hcm-next/internal/noisy\t0.1s")
	results := ParseGoTestOutput(strings.Join(lines, "\n"))
	if len(results) != 1 || len(results[0].FailureLog) != MaxFailureLogLines {
		t.Fatalf("failure log length = %d, want the cap %d", len(results[0].FailureLog), MaxFailureLogLines)
	}
}
