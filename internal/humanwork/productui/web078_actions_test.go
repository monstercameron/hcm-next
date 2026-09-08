package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-078: semantic action bindings validation. Compositions
// can name action bindings nothing checks: a binding naming no
// capability, a synthesized endpoint where a gateway capability
// belongs, a binding with no current action token, an unversioned
// action, a missing idempotency key, or untyped input would all
// validate today. The lifecycle's next validate step needs a pure
// binding verdict — bare capability reference, current token,
// positive expected version, idempotency key, named input type — with
// stable reasons. No static capability allowlist: availability is
// runtime gateway authorization proved by the token, and
// presentation allowlisting it would be a second authority source.
func TestTodo_WEB_078(t *testing.T) {
	valid := ActionBinding{Capability: "journeys.create", Token: "tok-9f2c", ExpectedVersion: 7, IdempotencyKey: "page-studio-1", InputType: "JourneyDraft"}
	for _, binding := range []struct {
		name    string
		binding ActionBinding
		valid   bool
		reasons []string
	}{
		{"registered binding validates", valid, true, nil},
		{"blank binding refuses", ActionBinding{}, false, []string{`missing capability`, `missing action token`, `unsupported action version 0`, `missing idempotency key`, `missing input type`}},
		{"synthesized endpoint refuses", ActionBinding{Capability: "/api/approvals/execute", Token: "tok-1", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "Approval"}, false, []string{`synthesized endpoint "/api/approvals/execute"`}},
		{"remote endpoint refuses", ActionBinding{Capability: "https://hooks.example/run", Token: "tok-1", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "Approval"}, false, []string{`synthesized endpoint "https://hooks.example/run"`}},
		{"missing token refuses", ActionBinding{Capability: "journeys.create", ExpectedVersion: 7, IdempotencyKey: "k", InputType: "JourneyDraft"}, false, []string{`missing action token`}},
		{"zero version refuses", ActionBinding{Capability: "journeys.create", Token: "tok-1", IdempotencyKey: "k", InputType: "JourneyDraft"}, false, []string{`unsupported action version 0`}},
		{"missing key refuses", ActionBinding{Capability: "journeys.create", Token: "tok-1", ExpectedVersion: 1, InputType: "JourneyDraft"}, false, []string{`missing idempotency key`}},
		{"missing input refuses", ActionBinding{Capability: "journeys.create", Token: "tok-1", ExpectedVersion: 1, IdempotencyKey: "k"}, false, []string{`missing input type`}},
	} {
		verdict := ValidateActionBinding(binding.binding)
		if verdict.Compatible != binding.valid || !reflect.DeepEqual(verdict.Reasons, binding.reasons) {
			t.Fatalf("%s = (%t, %q), want (%t, %q)", binding.name, verdict.Compatible, verdict.Reasons, binding.valid, binding.reasons)
		}
	}

	// A draft validates through its action bindings.
	draft := PageDraft{Page: "studio", Composition: PageComposition{Actions: []ActionBinding{valid}}}
	if verdict := ValidateDraftActions(draft); !verdict.Compatible {
		t.Fatalf("bound draft fails action validation: %q", verdict.Reasons)
	}
	broken := draft
	broken.Composition.Actions = []ActionBinding{{Capability: "journeys.create"}}
	if verdict := ValidateDraftActions(broken); verdict.Compatible {
		t.Fatal("tokenless draft passes action validation")
	}
}

// Golden: action validation outcomes over a binding matrix.
func TestTodo_WEB_078_Golden(t *testing.T) {
	bindings := []ActionBinding{
		{Capability: "journeys.create", Token: "tok-9f2c", ExpectedVersion: 7, IdempotencyKey: "page-studio-1", InputType: "JourneyDraft"},
		{Capability: "/api/approvals/execute", Token: "tok-1", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "Approval"},
		{},
		{Capability: "people.export", Token: "tok-2", ExpectedVersion: 1, IdempotencyKey: "k2", InputType: "ExportRequest"},
	}
	var builder strings.Builder
	for _, binding := range bindings {
		verdict := ValidateActionBinding(binding)
		builder.WriteString(binding.Capability)
		builder.WriteString("\x00")
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "850f2e8d21082c37b91f628e52b0581fefe5b9068cb89d7f24179d0851aee72f"
	if got != want {
		t.Fatalf("action matrix digest = %s, want %s", got, want)
	}
}

// Browser: canonical bindings validate across repeated calls —
// deterministically.
func TestTodo_WEB_078_Browser(t *testing.T) {
	bindings := []ActionBinding{
		{Capability: "journeys.create", Token: "tok-9f2c", ExpectedVersion: 7, IdempotencyKey: "page-studio-1", InputType: "JourneyDraft"},
		{Capability: "people.export", Token: "tok-2", ExpectedVersion: 1, IdempotencyKey: "k2", InputType: "ExportRequest"},
		{Capability: "history.read", Token: "tok-3", ExpectedVersion: 2, IdempotencyKey: "k3", InputType: "HistoryQuery"},
	}
	for _, binding := range bindings {
		first := ValidateActionBinding(binding)
		second := ValidateActionBinding(binding)
		if !first.Compatible {
			t.Fatalf("capability %q rejects its canonical binding: %q", binding.Capability, first.Reasons)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("capability %q validation is nondeterministic", binding.Capability)
		}
	}
	draft := PageDraft{Page: "studio", Composition: PageComposition{Actions: bindings}}
	if verdict := ValidateDraftActions(draft); !verdict.Compatible {
		t.Fatalf("bound draft fails action validation: %q", verdict.Reasons)
	}
}

// Conformance: empty actions validate, reason stability.
func TestTodo_WEB_078_Conformance(t *testing.T) {
	if verdict := ValidateDraftActions(PageDraft{}); !verdict.Compatible {
		t.Fatalf("empty actions fail: %q", verdict.Reasons)
	}
	first := ValidateActionBinding(ActionBinding{Capability: "/x"})
	second := ValidateActionBinding(ActionBinding{Capability: "/x"})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("action reasons are unstable")
	}
}
