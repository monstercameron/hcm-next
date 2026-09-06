package telemetry

import (
	"strings"
	"sync"
	"testing"
)

func testPolicy() Policy {
	return Policy{ID: "pilot-telemetry", Version: "v1", Owner: "privacy-ops", SigningKey: []byte("test-signing-key"), Sampling: SamplingPolicy{DefaultRate: 1, CriticalRate: 1}, AllowedAttributes: map[string]AttributeRule{"capability": {Allowed: true, MetricLabel: true}, "worker": {Allowed: true, MetricLabel: true}, "salary": {Allowed: true, Redact: true}, "reason": {Allowed: true}}, CardinalityBudgets: []CardinalityBudget{{Signal: SignalMetric, Attribute: "worker", Limit: 2}, {Signal: SignalMetric, Attribute: "capability", Limit: 2}}}
}

func testEvents() []Event {
	return []Event{{ID: "e-1", TenantID: "tenant-a", Signal: SignalMetric, Name: "operation", Attributes: map[string]string{"capability": "promotion", "worker": "w-1", "salary": "100000", "unknown": "secret"}}, {ID: "e-2", TenantID: "tenant-a", Signal: SignalMetric, Name: "operation", Attributes: map[string]string{"capability": "promotion", "worker": "w-2"}}, {ID: "e-3", TenantID: "tenant-a", Signal: SignalMetric, Name: "operation", Attributes: map[string]string{"capability": "promotion", "worker": "w-3"}}, {ID: "e-4", TenantID: "tenant-a", Signal: SignalSecurity, Name: "failure", Critical: true, FailureClass: "security", Attributes: map[string]string{"reason": "denied"}}}
}

func TestTodo_OPS_002(t *testing.T) {
	result, err := Export(testPolicy(), testEvents())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 4 || result.Receipt.Signature == "" || result.Receipt.CriticalFailuresKept != 1 || result.Receipt.Aggregated == 0 {
		t.Fatalf("result=%+v", result)
	}
	if !VerifyReceipt(testPolicy(), result.Receipt) {
		t.Fatal("receipt signature did not verify")
	}
}

func TestTodo_OPS_002_Race(t *testing.T) {
	policy := testPolicy()
	events := testEvents()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Export(policy, events); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_OPS_002_Security(t *testing.T) {
	result, err := Export(testPolicy(), testEvents())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range result.Events {
		if strings.Contains(event.Attributes["salary"], "100000") || event.Attributes["unknown"] != "" {
			t.Fatalf("sensitive or unallowlisted data escaped: %+v", event)
		}
	}
}

func TestTodo_OPS_002_Mutation(t *testing.T) {
	policy := testPolicy()
	events := testEvents()
	original := events[0].Attributes["worker"]
	if _, err := Export(policy, events); err != nil {
		t.Fatal(err)
	}
	if events[0].Attributes["worker"] != original || policy.AllowedAttributes["worker"].MetricLabel != true {
		t.Fatal("export mutated caller-owned policy or event")
	}
	bad := policy
	bad.SigningKey = nil
	if _, err := Export(bad, events); err == nil {
		t.Fatal("unsigned receipt must be rejected")
	}
}
