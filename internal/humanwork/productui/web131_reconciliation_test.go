package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// RED for WEB-131: reconciliation and repair presentation.
// The lifecycle surface resolves consistency and execution
// dimensions, but no governed rollup presents the
// reconcile-and-repair posture: the first surface combines
// the dimension keys by convention and a degraded
// projection can present as healthy. The compiler needs
// the governed rollup — trouble keys to attention,
// repairing to repair, consistent to healthy, everything
// else idle, with invalid dimension keys failing closed
// to attention — so posture resolves today from one
// point.
func TestTodo_WEB_131(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	dimensions := func(consistency intentsv1.ConsistencyState, execution intentsv1.ExecutionState) (statusDimension, statusDimension) {
		model := ResolveStatusDimensions(locale, StatusProjection{Available: true,
			Consistency: values.Value(consistency), Execution: values.Value(execution)})
		return model.Items[3], model.Items[1]
	}

	consistency, execution := dimensions(
		intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED,
		intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED)
	attention := ResolveReconciliation(locale, consistency, execution)
	if attention.State != ReconciliationAttention {
		t.Fatalf("degraded/blocked posture = %v", attention.State)
	}
	if attention.Text != locale.Text("status.reconciliation.attention") {
		t.Fatalf("attention text = %q", attention.Text)
	}
	if !strings.Contains(attention.Detail, "Degraded") || !strings.Contains(attention.Detail, "Blocked") {
		t.Fatalf("attention detail = %q", attention.Detail)
	}

	consistency, execution = dimensions(
		intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING,
		intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)
	if repairing := ResolveReconciliation(locale, consistency, execution); repairing.State != ReconciliationRepairing {
		t.Fatalf("repairing posture = %v", repairing.State)
	}

	consistency, execution = dimensions(
		intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT,
		intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED)
	if healthy := ResolveReconciliation(locale, consistency, execution); healthy.State != ReconciliationHealthy {
		t.Fatalf("consistent posture = %v", healthy.State)
	}

	consistency, execution = dimensions(
		intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE,
		intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED)
	if idle := ResolveReconciliation(locale, consistency, execution); idle.State != ReconciliationIdle {
		t.Fatalf("idle posture = %v", idle.State)
	}
}

// Golden: postures over consistency/execution pairs.
func TestTodo_WEB_131_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	pairs := [][2]int{{3, 5}, {4, 3}, {2, 4}, {0, 0}, {1, 0}, {5, 5}}
	var builder strings.Builder
	for _, pair := range pairs {
		model := ResolveStatusDimensions(locale, StatusProjection{Available: true,
			Consistency: values.Value(intentsv1.ConsistencyState(pair[0])),
			Execution:   values.Value(intentsv1.ExecutionState(pair[1]))})
		posture := ResolveReconciliation(locale, model.Items[3], model.Items[1])
		fmt.Fprintf(&builder, "%d|%d|%s\x00", pair[0], pair[1], posture.Text)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "8fb3a9c8146ccbb3bd0268ddc7651b87144bbba9fe204a75d585f0d8dab6abae"
	if got != want {
		t.Fatalf("reconciliation digest = %s, want %s", got, want)
	}
}

// Browser: posture resolution is deterministic and pure.
func TestTodo_WEB_131_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	model := ResolveStatusDimensions(locale, StatusProjection{Available: true,
		Consistency: values.Value(intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED)})
	beforeConsistency, beforeExecution := model.Items[3], model.Items[1]
	first := ResolveReconciliation(locale, model.Items[3], model.Items[1])
	second := ResolveReconciliation(locale, model.Items[3], model.Items[1])
	if !reflect.DeepEqual(model.Items[3], beforeConsistency) || !reflect.DeepEqual(model.Items[1], beforeExecution) {
		t.Fatal("resolution mutates its dimensions")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("resolution is nondeterministic")
	}
}

// Conformance: states are distinct, trouble keys dominate
// repair, resolution is stable.
func TestTodo_WEB_131_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	states := map[ReconciliationState]bool{}
	model := ResolveStatusDimensions(locale, StatusProjection{Available: true,
		Consistency: values.Value(intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING),
		Execution:   values.Value(intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED)})
	mixed := ResolveReconciliation(locale, model.Items[3], model.Items[1])
	if mixed.State != ReconciliationAttention {
		t.Fatal("trouble does not dominate repair")
	}
	for _, posture := range []ReconciliationStatus{
		ResolveReconciliation(locale, statusDimension{}, statusDimension{}),
		mixed,
		ResolveReconciliation(locale, model.Items[3], statusDimension{valueKey: "consistent"}),
	} {
		if states[posture.State] {
			continue
		}
		states[posture.State] = true
	}
	if len(states) != 3 {
		t.Fatalf("postures collapse: %v", states)
	}
	if !reflect.DeepEqual(mixed, ResolveReconciliation(locale, model.Items[3], model.Items[1])) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: posture tracks the projection from
// degraded through repairing to consistent.
func TestTodo_WEB_131_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	postureOf := func(consistency intentsv1.ConsistencyState) ReconciliationState {
		model := ResolveStatusDimensions(locale, StatusProjection{Available: true,
			Consistency: values.Value(consistency),
			Execution:   values.Value(intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)})
		return ResolveReconciliation(locale, model.Items[3], model.Items[1]).State
	}
	if got := postureOf(intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED); got != ReconciliationAttention {
		t.Fatalf("degraded = %v", got)
	}
	if got := postureOf(intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING); got != ReconciliationRepairing {
		t.Fatalf("repairing = %v", got)
	}
	if got := postureOf(intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT); got != ReconciliationHealthy {
		t.Fatalf("consistent = %v", got)
	}
}

// Fault: blank dimensions idle quietly while invalid
// keys fail closed to attention.
func TestTodo_WEB_131_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	if got := ResolveReconciliation(locale, statusDimension{}, statusDimension{}); got.State != ReconciliationIdle {
		t.Fatalf("blank dimensions = %v", got.State)
	}
	if got := ResolveReconciliation(locale,
		statusDimension{label: "Consistency", value: "Invalid or unspecified", valueKey: "invalid"},
		statusDimension{}); got.State != ReconciliationAttention {
		t.Fatalf("invalid consistency = %v", got.State)
	}
	if got := ResolveReconciliation(locale,
		statusDimension{},
		statusDimension{label: "Execution", value: "Invalid or unspecified", valueKey: "invalid"}); got.State != ReconciliationAttention {
		t.Fatalf("invalid execution = %v", got.State)
	}
}
