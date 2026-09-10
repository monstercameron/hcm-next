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

// RED for WEB-127: policy and obligation explanations. The
// lifecycle surface renders dimension values and the
// simulation panel renders its outcome, but no governed
// explanation says what an obligation state or a policy
// outcome means: the first surface invents the wording by
// convention and an unspecified state can present as
// explained. The compiler needs the governed explainers —
// one explanation per obligation state through reviewed
// copy with unspecified failing closed, plus the policy
// outcome explanation — so meanings resolve today without
// evaluating policy.
func TestTodo_WEB_127(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	cases := map[intentsv1.ObligationState]string{
		intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE: "status.obligation.explain.not_applicable",
		intentsv1.ObligationState_OBLIGATION_STATE_PENDING:        "status.obligation.explain.pending",
		intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED:      "status.obligation.explain.satisfied",
		intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE:        "status.obligation.explain.overdue",
		intentsv1.ObligationState_OBLIGATION_STATE_WAIVED:         "status.obligation.explain.waived",
		intentsv1.ObligationState_OBLIGATION_STATE_UNKNOWN:        "status.obligation.explain.unknown",
	}
	for state, key := range cases {
		explanation, ok := ExplainObligation(locale, state)
		if !ok {
			t.Fatalf("obligation %v unexplained", state)
		}
		if explanation != locale.Text(key) {
			t.Fatalf("obligation %v = %q", state, explanation)
		}
	}
	if _, ok := ExplainObligation(locale, intentsv1.ObligationState_OBLIGATION_STATE_UNSPECIFIED); ok {
		t.Fatal("unspecified obligation presents as explained")
	}

	if got := ExplainPolicyOutcome(locale, true); got != locale.Text("view_as.explain.allowed") {
		t.Fatalf("allowed policy = %q", got)
	}
	if got := ExplainPolicyOutcome(locale, false); got != locale.Text("view_as.explain.denied") {
		t.Fatalf("denied policy = %q", got)
	}
}

// Golden: explanations over states and outcomes.
func TestTodo_WEB_127_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	states := []intentsv1.ObligationState{0, 1, 2, 3, 4, 5, 6, 7, 99}
	var builder strings.Builder
	for _, state := range states {
		explanation, ok := ExplainObligation(locale, state)
		fmt.Fprintf(&builder, "%d|%t|%s\x00", int32(state), ok, explanation)
	}
	for _, allowed := range []bool{true, false} {
		fmt.Fprintf(&builder, "policy-%t|%s\x00", allowed, ExplainPolicyOutcome(locale, allowed))
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "d5068ec51fdfc9732b3597039264fe52a4ca3f7836ba9c7e9ccd936cd7c0feac"
	if got != want {
		t.Fatalf("explanation digest = %s, want %s", got, want)
	}
}

// Browser: explanation is deterministic and pure.
func TestTodo_WEB_127_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	first, firstOK := ExplainObligation(locale, intentsv1.ObligationState_OBLIGATION_STATE_PENDING)
	second, secondOK := ExplainObligation(locale, intentsv1.ObligationState_OBLIGATION_STATE_PENDING)
	if first != second || firstOK != secondOK {
		t.Fatal("obligation explanation is nondeterministic")
	}
	firstExplanation, secondExplanation := ExplainPolicyOutcome(locale, true), ExplainPolicyOutcome(locale, true)
	if firstExplanation != secondExplanation {
		t.Fatal("policy explanation is nondeterministic")
	}
}

// Conformance: every tokenized obligation state is
// explained, explanations are distinct, resolution is
// stable.
func TestTodo_WEB_127_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	seen := map[string]intentsv1.ObligationState{}
	for _, state := range []intentsv1.ObligationState{1, 2, 3, 4, 5, 7} {
		explanation, ok := ExplainObligation(locale, state)
		if !ok || explanation == "" {
			t.Fatalf("obligation %v unexplained", state)
		}
		if prev, dup := seen[explanation]; dup {
			t.Fatalf("obligations %v and %v share %q", prev, state, explanation)
		}
		seen[explanation] = state
	}
	if ExplainPolicyOutcome(locale, true) == ExplainPolicyOutcome(locale, false) {
		t.Fatal("policy outcomes share an explanation")
	}
	stable, _ := ExplainObligation(locale, intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED)
	repeat, _ := ExplainObligation(locale, intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED)
	if stable != repeat || !reflect.DeepEqual(stable, repeat) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: explanations pair with the rendered value
// and outcome copy they describe.
func TestTodo_WEB_127_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projection := StatusProjection{Obligation: values.Value(intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE)}
	value, ok := projection.Obligation.Get()
	if !ok {
		t.Fatal("obligation presence lost")
	}
	token, valid := obligationToken(value)
	if !valid {
		t.Fatal("obligation token lost")
	}
	rendered := locale.Text("status.obligation." + token.key)
	explanation, explained := ExplainObligation(locale, value)
	if !explained || explanation != locale.Text("status.obligation.explain."+token.key) {
		t.Fatalf("value %q pairs with %q", rendered, explanation)
	}
	panel := PolicySimulationProps{Subject: "Avery", Allowed: false}
	outcome := locale.Text("view_as.denied")
	if panel.Allowed {
		outcome = locale.Text("view_as.allowed")
	}
	if ExplainPolicyOutcome(locale, panel.Allowed) != locale.Text("view_as.explain.denied") || outcome != locale.Text("view_as.denied") {
		t.Fatal("policy explanation diverges from the panel outcome")
	}
}

// Fault: out-of-range and unspecified states fail closed;
// explanations never evaluate.
func TestTodo_WEB_127_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	for _, state := range []intentsv1.ObligationState{-1, 0, 99} {
		if explanation, ok := ExplainObligation(locale, state); ok || explanation != "" {
			t.Fatalf("obligation %d explains as %q", int32(state), explanation)
		}
	}
}
