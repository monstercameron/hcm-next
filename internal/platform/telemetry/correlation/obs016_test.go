package correlation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/correlation"
)

// TestTelemetryCorrelationJoinsSignalsButNeverUsesTraceIdentityAsBusinessAuthority
// is the PRIMARY test for planning/todos.md OBS-016: one owned Correlation
// joins structured logs, traces and metrics across two traces (the second
// sampled out) through stable business causation, every join carries its
// own tenant/purpose scope, and the trace identity never becomes business
// authority — rebinding the trace linkage leaves the idempotency key
// unchanged.
func TestTelemetryCorrelationJoinsSignalsButNeverUsesTraceIdentityAsBusinessAuthority(t *testing.T) {
	c, err := correlation.New("intent-1", "corr-1", "tenant-a")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first, err := c.WithTrace("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	if err != nil {
		t.Fatalf("WithTrace: %v", err)
	}

	keyBefore := first.IdempotencyKey()

	fields := first.LogFields()
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["correlation_id"] != "corr-1" || got["intent_id"] != "intent-1" {
		t.Fatalf("log fields lack owned correlation: %+v", got)
	}
	if got["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || got["span_id"] != "00f067aa0ba902b7" || got["trace_sampled"] != "true" {
		t.Fatalf("log fields lack active trace/span linkage: %+v", got)
	}

	// The intent outlives its first trace: a second, sampled-out trace
	// carries the same business causation.
	second, err := first.WithTrace("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", false)
	if err != nil {
		t.Fatalf("WithTrace: %v", err)
	}
	if second.IdempotencyKey() != keyBefore {
		t.Fatalf("rebinding trace linkage changed the idempotency key: %q -> %q", keyBefore, second.IdempotencyKey())
	}
	if strings.Contains(second.IdempotencyKey(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatal("idempotency key embeds trace identity")
	}
	if got := fieldValue(second.LogFields(), "trace_sampled"); got != "false" {
		t.Fatalf("sampled-out trace reports trace_sampled = %q, want false", got)
	}
	if got := fieldValue(second.LogFields(), "correlation_id"); got != "corr-1" {
		t.Fatalf("sampled-out trace broke BusinessIntent correlation: correlation_id = %q", got)
	}

	// Every join is scoped independently: one scope per signal, each
	// carrying tenant and purpose.
	for _, purpose := range []string{"incident", "slo"} {
		ticket, err := second.Join(correlation.JoinScope{TenantToken: "tenant-a", Purpose: purpose})
		if err != nil {
			t.Fatalf("Join(%s): %v", purpose, err)
		}
		if ticket.CorrelationID != "corr-1" || ticket.IntentID != "intent-1" || ticket.TenantToken != "tenant-a" || ticket.Purpose != purpose {
			t.Fatalf("join ticket = %+v, want owned correlation bound to scope", ticket)
		}
	}
}

// TestTodo_OBS_016_Property mirrors the OBS-013 property shape: every
// owned field is required and bounded, and trace linkage admits only
// well-formed sampled-or-not hex identities.
func TestTodo_OBS_016_Property(t *testing.T) {
	base, err := correlation.New("intent-1", "corr-1", "tenant-a")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, field := range []struct {
		name string
		bad  func(c correlation.Correlation) correlation.Correlation
	}{
		{"intent missing", func(c correlation.Correlation) correlation.Correlation { c.IntentID = " "; return c }},
		{"intent bounded", func(c correlation.Correlation) correlation.Correlation {
			c.IntentID = strings.Repeat("x", 129)
			return c
		}},
		{"correlation missing", func(c correlation.Correlation) correlation.Correlation { c.CorrelationID = ""; return c }},
		{"correlation bounded", func(c correlation.Correlation) correlation.Correlation {
			c.CorrelationID = strings.Repeat("x", 129)
			return c
		}},
		{"tenant missing", func(c correlation.Correlation) correlation.Correlation { c.TenantToken = ""; return c }},
		{"tenant bounded", func(c correlation.Correlation) correlation.Correlation {
			c.TenantToken = strings.Repeat("x", 129)
			return c
		}},
		{"tenant no whitespace", func(c correlation.Correlation) correlation.Correlation { c.TenantToken = "tenant a"; return c }},
	} {
		t.Run(field.name, func(t *testing.T) {
			if err := field.bad(base).Validate(); !errors.Is(err, correlation.ErrCorrelation) {
				t.Fatalf("Validate() = %v, want ErrCorrelation", err)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		traceID string
		spanID  string
		wantErr bool
	}{
		{"valid sampled", "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", false},
		{"valid unsampled", "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", false},
		{"short trace", "4bf92f35", "00f067aa0ba902b7", true},
		{"non-hex trace", strings.Repeat("z", 32), "00f067aa0ba902b7", true},
		{"zero trace", strings.Repeat("0", 32), "00f067aa0ba902b7", true},
		{"short span", "4bf92f3577b34da6a3ce929d0e0e4736", "00f067", true},
		{"non-hex span", "4bf92f3577b34da6a3ce929d0e0e4736", strings.Repeat("z", 16), true},
	} {
		t.Run("trace/"+tc.name, func(t *testing.T) {
			_, err := base.WithTrace(tc.traceID, tc.spanID, true)
			if tc.wantErr && !errors.Is(err, correlation.ErrCorrelation) {
				t.Fatalf("WithTrace() = %v, want ErrCorrelation", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("WithTrace() = %v, want nil", err)
			}
		})
	}
}

// TestTodo_OBS_016_Golden pins the exact log-field rendering and
// idempotency key for fixed inputs: a rendering change is a deliberate
// encoding change, never silent drift.
func TestTodo_OBS_016_Golden(t *testing.T) {
	c, err := correlation.New("intent-golden", "corr-golden", "tenant-golden")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err = c.WithTrace("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	if err != nil {
		t.Fatalf("WithTrace: %v", err)
	}
	var sb strings.Builder
	for _, f := range c.LogFields() {
		sb.WriteString(f.Key + "=" + f.Value + "\n")
	}
	want := "correlation_id=corr-golden\n" +
		"intent_id=intent-golden\n" +
		"span_id=00f067aa0ba902b7\n" +
		"trace_id=4bf92f3577b34da6a3ce929d0e0e4736\n" +
		"trace_sampled=true\n"
	if sb.String() != want {
		t.Fatalf("LogFields() =\n%s\nwant\n%s", sb.String(), want)
	}
	if got, want := c.IdempotencyKey(), "intent-golden:corr-golden"; got != want {
		t.Fatalf("IdempotencyKey() = %q, want %q", got, want)
	}
}

// TestTodo_OBS_016_Security proves joins are authorized per join and per
// tenant: a scope for another tenant cannot ride a shared correlation ID,
// a purposeless scope cannot join, and authorizing one join does not
// authorize the next.
func TestTodo_OBS_016_Security(t *testing.T) {
	c, err := correlation.New("intent-1", "shared-corr", "tenant-a")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Join(correlation.JoinScope{TenantToken: "tenant-b", Purpose: "incident"}); !errors.Is(err, correlation.ErrJoinScope) {
		t.Fatalf("cross-tenant Join() = %v, want ErrJoinScope", err)
	}
	if _, err := c.Join(correlation.JoinScope{TenantToken: "tenant-a"}); !errors.Is(err, correlation.ErrJoinScope) {
		t.Fatalf("purposeless Join() = %v, want ErrJoinScope", err)
	}
	if _, err := c.Join(correlation.JoinScope{TenantToken: " tenant-a ", Purpose: "incident"}); !errors.Is(err, correlation.ErrJoinScope) {
		t.Fatalf("padded-tenant Join() = %v, want ErrJoinScope", err)
	}
	if _, err := c.Join(correlation.JoinScope{TenantToken: "tenant-a", Purpose: "incident"}); err != nil {
		t.Fatalf("authorized Join() = %v, want nil", err)
	}
	// No authorization is cached between joins: the next join re-checks.
	if _, err := c.Join(correlation.JoinScope{TenantToken: "tenant-b", Purpose: "incident"}); !errors.Is(err, correlation.ErrJoinScope) {
		t.Fatalf("second Join() after an authorized one = %v, want ErrJoinScope", err)
	}
}

// TestTodo_OBS_016_Conformance proves the metric-label guard: owned
// correlation and trace identity are never metric labels (unbounded
// cardinality), while the catalog's bounded keys pass.
func TestTodo_OBS_016_Conformance(t *testing.T) {
	for _, key := range []string{"correlation_id", "intent_id", "trace_id", "span_id", "tenant_token"} {
		if err := correlation.CheckMetricLabel(key); !errors.Is(err, correlation.ErrMetricLabel) {
			t.Fatalf("CheckMetricLabel(%q) = %v, want ErrMetricLabel", key, err)
		}
	}
	for _, key := range []string{"cell_id", "tenant_class", "outcome", "edge"} {
		if err := correlation.CheckMetricLabel(key); err != nil {
			t.Fatalf("CheckMetricLabel(%q) = %v, want nil", key, err)
		}
	}
	if err := correlation.CheckMetricLabel(""); !errors.Is(err, correlation.ErrMetricLabel) {
		t.Fatalf("CheckMetricLabel(\"\") = %v, want ErrMetricLabel", err)
	}
}

// TestTodo_OBS_016_Mutation flips exactly one input and asserts the
// behavior flips with it: business key follows owned identity, never the
// trace; join follows the scope, never a previous verdict.
func TestTodo_OBS_016_Mutation(t *testing.T) {
	c, err := correlation.New("intent-1", "corr-1", "tenant-a")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err = c.WithTrace("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	if err != nil {
		t.Fatalf("WithTrace: %v", err)
	}
	before := c.IdempotencyKey()

	mutated := c
	mutated.IntentID = "intent-2"
	if mutated.IdempotencyKey() == before {
		t.Fatal("changed IntentID left the idempotency key unchanged")
	}
	mutated = c
	mutated.CorrelationID = "corr-2"
	if mutated.IdempotencyKey() == before {
		t.Fatal("changed CorrelationID left the idempotency key unchanged")
	}

	ctx := correlation.WithCorrelation(context.Background(), c)
	back, ok := correlation.FromContext(ctx)
	if !ok || back.IdempotencyKey() != before {
		t.Fatalf("context round trip = %+v, %v; want key %q", back, ok, before)
	}
	if _, ok := correlation.FromContext(context.Background()); ok {
		t.Fatal("empty context yielded a correlation")
	}
}

func fieldValue(fields []correlation.LogField, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}
