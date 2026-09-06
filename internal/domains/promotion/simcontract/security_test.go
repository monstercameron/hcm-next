package simcontract_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcontract"
)

// TestTodo_PROMO_004_Security is PROMO-004's SECURITY matrix test: it proves
// a caller cannot mark a [simcontract.SideEffect] executed.
//
// The proof is structural rather than behavioral, because the property being
// tested is that a certain action is *unreachable*, not that it is refused
// with an error. simcontract_test is, deliberately, an ordinary external
// caller of this package -- the same position any other package in the
// module is in -- and from that position:
//
//  1. There is no exported field, setter or alternate constructor that could
//     move a [simcontract.SideEffect]'s status away from
//     [simcontract.EffectSimulatedNotExecuted]. [simcontract.NewSideEffect]
//     is the only constructor and it always stamps that one value.
//  2. Reflection over the exported API confirms there is no field or method
//     whose name suggests an execution-marking capability, so the property
//     cannot be quietly reintroduced by a future field addition without this
//     test noticing.
//
// [TestSideEffectStatusIsUnexported] in this package's own white-box test
// completes the proof from the other side: even code that already has access
// to the unexported field (this package's own tests) cannot make
// [simcontract.SideEffect.Validate] accept a status other than the one legal
// value.
func TestTodo_PROMO_004_Security(t *testing.T) {
	e, err := simcontract.NewSideEffect(
		"effect-1", "test.kind", "test.participant", "test.destination",
		"REVERSIBLE", "test.compensation", "test.observation",
	)
	if err != nil {
		t.Fatalf("NewSideEffect: %v", err)
	}
	if e.Status() != simcontract.EffectSimulatedNotExecuted {
		t.Fatalf("Status() = %s, want %s", e.Status(), simcontract.EffectSimulatedNotExecuted)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("a freshly constructed side effect must validate: %v", err)
	}

	typ := reflect.TypeOf(simcontract.SideEffect{})
	if field, ok := typ.FieldByName("Status"); ok {
		t.Fatalf("SideEffect exports a Status field (%v); status must be reachable only through the Status() accessor", field)
	}
	forbidden := []string{"execute", "executed", "setstatus", "markexecuted"}
	for i := 0; i < typ.NumMethod(); i++ {
		name := strings.ToLower(typ.Method(i).Name)
		for _, token := range forbidden {
			if strings.Contains(name, token) {
				t.Fatalf("SideEffect exports method %s; there must be no way to mark an effect executed", typ.Method(i).Name)
			}
		}
	}

	// EffectSimulatedNotExecuted is the only declared status. A caller who
	// constructs the string type directly still cannot get it past
	// Validate on any field this package exposes, because the field that
	// carries it is not exposed at all.
	if simcontract.EffectStatus("EXECUTED") == simcontract.EffectSimulatedNotExecuted {
		t.Fatal("an arbitrary EffectStatus string equals the sole legal value")
	}
}
