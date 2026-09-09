package telemetry_test

import (
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

func testEvaluator(t *testing.T) *telemetry.Evaluator {
	t.Helper()
	allow := testAllowlist(t)
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

// TestTodo_OBS_004 proves the RED and GREEN clauses of planning/todos.md
// OBS-004: sensitive fixtures never pass classification, an untrusted
// caller cannot force or suppress sampling retention because Decide takes
// no such flag, metric cardinality is capped rather than left unbounded,
// a misconfigured evaluator fails closed, and every decision carries an
// exact policy receipt or drop reason.
func TestTodo_OBS_004(t *testing.T) {
	eval := testEvaluator(t)

	t.Run("RED_sensitive_fixtures_never_escape_as_metric_labels", func(t *testing.T) {
		fixtures := []string{
			"salary", "medical_diagnosis", "bank_routing_number", "case_file",
			"prompt_text", "authorization_header", "sql_query", "error_stack_trace",
		}
		for _, key := range fixtures {
			d := eval.EvaluateAttribute(telemetry.SignalMetric, key, "sensitive")
			if d.Kept {
				t.Fatalf("key %q was kept on a metric signal; want dropped", key)
			}
			if d.DropReason == "" {
				t.Fatalf("key %q: DropReason is empty; want an exact drop reason", key)
			}
		}
	})

	t.Run("RED_untrusted_caller_cannot_force_retention_or_suppress_mandatory_trace", func(t *testing.T) {
		// SuccessSampleRate 0 means "never sample on success", yet a
		// declared retention class must still retain: Decide has no
		// caller-supplied override parameter an untrusted parent could use
		// to flip this.
		neverSample := telemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0}
		for _, id := range []string{"a", "b", "attacker-chosen-id", "", "\x00\x01malformed"} {
			d := neverSample.Decide(id, telemetry.RetentionSecurityDenial)
			if !d.Retained {
				t.Fatalf("correlation id %q: security denial was not retained under a 0%% sample rate", id)
			}
		}
	})

	t.Run("RED_metric_cardinality_exceeds_budget_overflows", func(t *testing.T) {
		allow := testAllowlist(t)
		gov := telemetry.NewCardinalityGovernor(allow)
		def, ok := allow.Lookup("environment")
		if !ok {
			t.Fatal("environment is not registered")
		}
		for i := 0; i < def.MaxCardinality; i++ {
			v := fmt.Sprintf("env-%d", i)
			if got := gov.Cap("environment", v); got != v {
				t.Fatalf("value %d within budget capped to %q", i, got)
			}
		}
		overflow := gov.Cap("environment", "one-too-many")
		if overflow != "__overflow__" {
			t.Fatalf("Cap() past budget = %q, want the overflow bucket", overflow)
		}
	})

	t.Run("RED_privacy_gateway_failure_fails_closed", func(t *testing.T) {
		var broken telemetry.Evaluator // zero value: nil Allow
		d := broken.EvaluateAttribute(telemetry.SignalLog, "cell_id", "cell-1")
		if d.Kept {
			t.Fatal("a misconfigured Evaluator kept an attribute; want fail-closed drop")
		}
	})

	t.Run("GREEN_classification_aware_policy_keeps_allowlisted_attribute", func(t *testing.T) {
		d := eval.EvaluateAttribute(telemetry.SignalLog, "capability_id", "cap.workflow.start")
		if !d.Kept || d.Class != telemetry.ClassOperationalPublic {
			t.Fatalf("EvaluateAttribute() = %+v, want kept OPERATIONAL_PUBLIC", d)
		}
	})

	t.Run("GREEN_bounded_cardinality_passes_within_budget", func(t *testing.T) {
		d := eval.EvaluateAttribute(telemetry.SignalMetric, "cell_id", "cell-p1a")
		if !d.Kept || d.Value != "cell-p1a" {
			t.Fatalf("EvaluateAttribute() = %+v, want the original value kept", d)
		}
	})

	t.Run("GREEN_forced_retention_classes_all_retain", func(t *testing.T) {
		lowRate := telemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0}
		for _, class := range telemetry.DefaultForcedRetentionClasses() {
			d := lowRate.Decide("corr-1", class)
			if !d.Retained {
				t.Fatalf("retention class %q was not retained", class)
			}
		}
	})
}

// TestTodo_OBS_004_Integration exercises the Evaluator end to end: an
// allow-listed public attribute reaches a metrics backend, a restricted
// attribute never does, and a prohibited attribute reaches nothing.
func TestTodo_OBS_004_Integration(t *testing.T) {
	eval := testEvaluator(t)

	publicDecision := eval.EvaluateAttribute(telemetry.SignalMetric, "outcome", "SUCCESS")
	if !publicDecision.Kept {
		t.Fatalf("public attribute dropped: %+v", publicDecision)
	}
	if !eval.EvaluateExport(publicDecision.Class, telemetry.SinkMetricsBackend) {
		t.Fatal("OPERATIONAL_PUBLIC must reach the metrics backend")
	}

	restrictedDecision := eval.EvaluateAttribute(telemetry.SignalLog, "error_type", "VALIDATION")
	if !restrictedDecision.Kept {
		t.Fatalf("restricted attribute dropped on a log signal: %+v", restrictedDecision)
	}
	if eval.EvaluateExport(restrictedDecision.Class, telemetry.SinkMetricsBackend) {
		t.Fatal("OPERATIONAL_RESTRICTED must never reach the metrics backend")
	}
	if !eval.EvaluateExport(restrictedDecision.Class, telemetry.SinkLogBackend) {
		t.Fatal("OPERATIONAL_RESTRICTED must still reach the log backend")
	}

	prohibitedDecision := eval.EvaluateAttribute(telemetry.SignalLog, "ssn", "000-00-0000")
	if prohibitedDecision.Kept {
		t.Fatalf("prohibited attribute kept: %+v", prohibitedDecision)
	}
	for _, sink := range []telemetry.SinkClass{telemetry.SinkMetricsBackend, telemetry.SinkLogBackend, telemetry.SinkTraceBackend} {
		if eval.EvaluateExport(telemetry.ClassProhibited, sink) {
			t.Fatalf("PROHIBITED reached sink %q", sink)
		}
	}

	sampled := eval.Decide("corr-int-1", telemetry.RetentionFinancialMutation)
	if !sampled.Retained || sampled.PolicyVersion != telemetry.DefaultPolicyVersion {
		t.Fatalf("Decide() = %+v, want retained under the default policy version", sampled)
	}
}

// TestTodo_OBS_004_Security proves an export policy can never be built
// with an escape hatch for PROHIBITED content, and that an attribute
// carrying adversarial content (a SQL fragment, a secret-shaped value)
// under an unregistered key is dropped regardless of its value.
func TestTodo_OBS_004_Security(t *testing.T) {
	eval := testEvaluator(t)

	t.Run("export_policy_construction_rejects_a_prohibited_sink_mapping", func(t *testing.T) {
		_, err := telemetry.NewExportPolicy(1, map[telemetry.AttributeClass][]telemetry.SinkClass{
			telemetry.ClassProhibited: {telemetry.SinkLogBackend},
		})
		if err == nil {
			t.Fatal("NewExportPolicy() accepted a PROHIBITED->sinks mapping")
		}
	})

	t.Run("default_export_policy_never_allows_prohibited_anywhere", func(t *testing.T) {
		policy := telemetry.DefaultExportPolicy(1)
		for _, sink := range []telemetry.SinkClass{telemetry.SinkMetricsBackend, telemetry.SinkLogBackend, telemetry.SinkTraceBackend} {
			if policy.Allows(telemetry.ClassProhibited, sink) {
				t.Fatalf("default export policy allows PROHIBITED -> %q", sink)
			}
		}
	})

	t.Run("adversarial_value_under_unregistered_key_is_dropped_regardless_of_content", func(t *testing.T) {
		adversarial := []string{
			"'; DROP TABLE workers; --",
			"Bearer eyJhbGciOiJIUzI1NiJ9.secret",
			"panic: runtime error at internal/ledger/append.go:42",
		}
		for _, v := range adversarial {
			d := eval.EvaluateAttribute(telemetry.SignalLog, "unregistered_free_text", v)
			if d.Kept {
				t.Fatalf("adversarial value under an unregistered key was kept: %q", v)
			}
		}
	})
}

// TestTodo_OBS_004_Mutation flips the cardinality and sampling-rate
// boundaries by one unit and asserts the decision flips too.
func TestTodo_OBS_004_Mutation(t *testing.T) {
	t.Run("cardinality_budget_boundary", func(t *testing.T) {
		allow := testAllowlist(t)
		gov := telemetry.NewCardinalityGovernor(allow)
		def, _ := allow.Lookup("process_role")
		for i := 0; i < def.MaxCardinality-1; i++ {
			gov.Cap("process_role", fmt.Sprintf("role-%d", i))
		}
		last := fmt.Sprintf("role-%d", def.MaxCardinality-1)
		if got := gov.Cap("process_role", last); got != last {
			t.Fatalf("the %dth distinct value overflowed early: %q", def.MaxCardinality, got)
		}
		if got := gov.Cap("process_role", "one-more"); got != "__overflow__" {
			t.Fatalf("the value past budget did not overflow: %q", got)
		}
	})

	t.Run("sampling_rate_zero_versus_one", func(t *testing.T) {
		zero := telemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0}
		one := telemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 1}
		for _, id := range []string{"x", "y", "z", "correlation-123"} {
			if d := zero.Decide(id, telemetry.RetentionNone); d.Retained {
				t.Fatalf("rate 0: id %q was retained", id)
			}
			if d := one.Decide(id, telemetry.RetentionNone); !d.Retained {
				t.Fatalf("rate 1: id %q was not retained", id)
			}
		}
	})
}
