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

// RED for WEB-130: execution-status dimensions. The status
// surface computes its five lifecycle dimensions inline at
// render time: the first consumer re-implements the token
// mapping, the fail-closed presence rule, and the
// accessible label join by convention, and an invalid enum
// can present as valid. The compiler needs the governed
// dimensions — all five resolved through the surface's own
// token mapping with the same accessible label — so
// dimensions resolve today from one point.
func TestTodo_WEB_130(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projection := StatusProjection{Available: true,
		Request:     values.Value(intentsv1.RequestState_REQUEST_STATE_APPROVED),
		Execution:   values.Value(intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED),
		Business:    values.Value(intentsv1.BusinessState_BUSINESS_STATE_COMPLETED),
		Consistency: values.Value(intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT),
		Obligation:  values.Value(intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED),
	}

	dimensions := ResolveStatusDimensions(locale, projection)
	if !dimensions.Available {
		t.Fatal("an available projection resolves unavailable")
	}
	if len(dimensions.Items) != 5 {
		t.Fatalf("dimensions resolve %d items", len(dimensions.Items))
	}
	kinds := []string{"request", "execution", "business", "consistency", "obligation"}
	for i, kind := range kinds {
		if dimensions.Items[i].kind != kind {
			t.Fatalf("item %d kind = %q", i, dimensions.Items[i].kind)
		}
		if dimensions.Items[i].label == "" || dimensions.Items[i].value == "" {
			t.Fatalf("item %d unlabeled: %+v", i, dimensions.Items[i])
		}
	}
	if dimensions.Items[1].value != locale.Text("status.execution.committed") {
		t.Fatalf("execution value = %q", dimensions.Items[1].value)
	}
	if !strings.Contains(dimensions.AccessibleLabel, dimensions.Items[1].label+": "+dimensions.Items[1].value) {
		t.Fatalf("accessible label = %q", dimensions.AccessibleLabel)
	}

	hidden := ResolveStatusDimensions(locale, StatusProjection{})
	if hidden.Available || len(hidden.Items) != 0 {
		t.Fatalf("unavailable projection resolves to %+v", hidden)
	}
	if hidden.Unavailable == "" {
		t.Fatal("unavailable projection states nothing")
	}
}

// Golden: dimensions over projections.
func TestTodo_WEB_130_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projections := []StatusProjection{
		{},
		{Available: true},
		{Available: true, Execution: values.Value(intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)},
		{Available: true,
			Request:     values.Value(intentsv1.RequestState_REQUEST_STATE_DRAFT),
			Execution:   values.Value(intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED),
			Business:    values.Value(intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN),
			Consistency: values.Value(intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED),
			Obligation:  values.Value(intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE)},
	}
	var builder strings.Builder
	for _, projection := range projections {
		dimensions := ResolveStatusDimensions(locale, projection)
		fmt.Fprintf(&builder, "%t|%s|%s\x00", dimensions.Available, dimensions.Unavailable, dimensions.AccessibleLabel)
		for _, item := range dimensions.Items {
			fmt.Fprintf(&builder, "%s|%s|%s|%s|%s|%s\x00", item.kind, item.label, item.value, item.valueKey, item.glyph, item.tone)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "8bb3ef9f365cef449a5df48567ae7d128ad8951f91887943f62c532a210bc503"
	if got != want {
		t.Fatalf("status dimensions digest = %s, want %s", got, want)
	}
}

// Browser: dimensions resolution is deterministic and
// never mutates its projection.
func TestTodo_WEB_130_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projection := StatusProjection{Available: true,
		Execution: values.Value(intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)}
	before := projection
	first := ResolveStatusDimensions(locale, projection)
	second := ResolveStatusDimensions(locale, projection)
	if !reflect.DeepEqual(projection, before) {
		t.Fatal("resolution mutates its projection")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("resolution is nondeterministic")
	}
}

// Conformance: dimension order is fixed, tones come from
// the token mapping, resolution is stable.
func TestTodo_WEB_130_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	dimensions := ResolveStatusDimensions(locale, StatusProjection{Available: true,
		Execution: values.Value(intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)})
	if len(dimensions.Items) != 5 {
		t.Fatalf("bare projection resolves %d items", len(dimensions.Items))
	}
	if dimensions.Items[1].tone == "" || dimensions.Items[1].glyph == "" {
		t.Fatalf("execution loses its token: %+v", dimensions.Items[1])
	}
	if !reflect.DeepEqual(dimensions, ResolveStatusDimensions(locale, StatusProjection{Available: true,
		Execution: values.Value(intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING)})) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: resolved dimensions match the rendered
// surface item for item.
func TestTodo_WEB_130_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projection := StatusProjection{Available: true,
		Obligation: values.Value(intentsv1.ObligationState_OBLIGATION_STATE_WAIVED)}
	dimensions := ResolveStatusDimensions(locale, projection)
	value, ok := projection.Obligation.Get()
	if !ok {
		t.Fatal("obligation presence lost")
	}
	token, valid := obligationToken(value)
	if !valid {
		t.Fatal("obligation token lost")
	}
	last := dimensions.Items[4]
	if last.value != locale.Text("status.obligation."+token.key) || last.valueKey != token.key ||
		last.glyph != token.glyph || last.tone != token.tone {
		t.Fatalf("resolved item diverges: %+v", last)
	}
}

// Fault: invalid enums and absent presences fail closed
// to the invalid and presence tokens.
func TestTodo_WEB_130_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	dimensions := ResolveStatusDimensions(locale, StatusProjection{Available: true,
		Request:   values.Value(intentsv1.RequestState(999)),
		Execution: values.Value(intentsv1.ExecutionState_EXECUTION_STATE_UNSPECIFIED),
	})
	if dimensions.Items[0].valueKey != "invalid" || dimensions.Items[0].value != locale.Text("status.value.invalid") {
		t.Fatalf("bad enum resolves to %+v", dimensions.Items[0])
	}
	bare := ResolveStatusDimensions(locale, StatusProjection{Available: true})
	for _, item := range bare.Items {
		if item.valueKey == "" || item.value == "" {
			t.Fatalf("absent presence resolves blank: %+v", item)
		}
	}
}
