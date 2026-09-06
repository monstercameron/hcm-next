package replay

import (
	"errors"
	"strings"
	"testing"
)

// TestErrors_CodesAreDistinctAndStable pins the refusal vocabulary. The codes
// are a contract other packages and operators match on, so a rename is a
// breaking change and a duplicate would make two different refusals
// indistinguishable.
func TestErrors_CodesAreDistinctAndStable(t *testing.T) {
	codes := map[string]string{
		"artifact": CodeArtifactUnavailable,
		"effect":   CodeEffectForbidden,
		"mode":     CodeModeRefused,
		"causal":   CodeCausalSeparation,
		"diverge":  CodeDivergence,
		"record":   CodeRecordInvalid,
		"plan":     CodePlanMismatch,
		"options":  CodeInvalidOptions,
		"source":   CodeSourceFailed,
		"budget":   CodeStepBudgetExceeded,
	}
	seen := map[string]string{}
	for name, code := range codes {
		if code == "" {
			t.Fatalf("%s code is empty", name)
		}
		if !strings.HasPrefix(code, "REPLAY_") {
			t.Fatalf("%s code %q is not namespaced", name, code)
		}
		if other, dup := seen[code]; dup {
			t.Fatalf("%s and %s share the code %q", name, other, code)
		}
		seen[code] = name
	}
	// The one code the ticket's GREEN clause spells verbatim.
	if CodeArtifactUnavailable != "REPLAY_ARTIFACT_UNAVAILABLE" {
		t.Fatalf("CodeArtifactUnavailable = %q", CodeArtifactUnavailable)
	}
}

// TestErrors_UnwrapAndAccessors checks that a refusal is classifiable without
// reading its message: the sentinel, the code and the node it names.
func TestErrors_UnwrapAndAccessors(t *testing.T) {
	plain := refuse(CodeEffectForbidden, "node_a", "reached for %s", "a webhook")
	if !errors.Is(plain, ErrReplay) {
		t.Fatalf("refusal does not unwrap to ErrReplay")
	}
	if CodeOf(plain) != CodeEffectForbidden || NodeOf(plain) != "node_a" {
		t.Fatalf("code=%q node=%q", CodeOf(plain), NodeOf(plain))
	}
	if !strings.Contains(plain.Error(), "at node node_a") || !strings.Contains(plain.Error(), "a webhook") {
		t.Fatalf("message = %q", plain.Error())
	}

	cause := errors.New("underlying")
	wrapped := wrap(CodeSourceFailed, "", cause, "load")
	if !errors.Is(wrapped, ErrReplay) || !errors.Is(wrapped, cause) {
		t.Fatalf("wrapped refusal does not unwrap to both the sentinel and the cause")
	}
	if strings.Contains(wrapped.Error(), "at node") {
		t.Fatalf("a refusal naming no node rendered a node: %q", wrapped.Error())
	}

	// A foreign error carries neither.
	if CodeOf(cause) != "" || NodeOf(cause) != "" {
		t.Fatalf("a foreign error reported a code or node")
	}
}
