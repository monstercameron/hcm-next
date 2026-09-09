package snapshot_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
)

func completenessSpec() snapshot.InputSpecification {
	return snapshot.InputSpecification{Inputs: []snapshot.CompletenessRequirement{
		{Name: "worker", Policy: snapshot.InputRequired},
		{Name: "optional_profile", Policy: snapshot.InputOptional},
		{Name: "manager", Policy: snapshot.InputConditional, Condition: &snapshot.CompletenessCondition{InputName: "worker", ExpectedValue: "manager"}},
	}}
}

func dispositionByName(t *testing.T, result snapshot.CompletenessResult, name string) snapshot.CompletenessDisposition {
	t.Helper()
	for _, disposition := range result.Dispositions {
		if disposition.Name == name {
			return disposition
		}
	}
	t.Fatalf("disposition %q not found in %+v", name, result.Dispositions)
	return snapshot.CompletenessDisposition{}
}

// TestTodo_SNAPSHOT_003 is the PRIMARY contract: completeness is evaluated
// from explicit presence states, and optional ABSENT is visible as MISSING
// without changing the aggregate result to a fabricated default.
func TestTodo_SNAPSHOT_003(t *testing.T) {
	result, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), []snapshot.SnapshotInput{
		{Name: "worker", Presence: snapshot.PresencePresent, Value: "manager"},
		{Name: "optional_profile", Presence: snapshot.PresenceAbsent},
		{Name: "manager", Presence: snapshot.PresencePresent, Value: "Ada"},
	})
	if err != nil {
		t.Fatalf("EvaluateCompleteness: %v", err)
	}
	if result.Overall != snapshot.VerdictSatisfied {
		t.Fatalf("overall = %s, want SATISFIED", result.Overall)
	}
	optional := dispositionByName(t, result, "optional_profile")
	if optional.Verdict != snapshot.VerdictMissing || optional.Presence != snapshot.PresenceAbsent {
		t.Fatalf("optional disposition = %+v, want explicit ABSENT/MISSING", optional)
	}
	if optional.Detail == "defaulted" || strings.Contains(result.Explain(), "default") {
		t.Fatalf("optional absence was treated as a default: %s", result.Explain())
	}
	if result.Digest == "" {
		t.Fatal("completeness digest is empty")
	}
}

// TestTodo_SNAPSHOT_003_Golden pins the verdict table and canonical digest
// for the fixed fixture above. The value is intentionally updated only when
// the completeness canonicalization contract changes.
func TestTodo_SNAPSHOT_003_Golden(t *testing.T) {
	result, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), []snapshot.SnapshotInput{
		{Name: "worker", Presence: snapshot.PresencePresent, Value: "manager"},
		{Name: "optional_profile", Presence: snapshot.PresenceAbsent},
		{Name: "manager", Presence: snapshot.PresencePresent, Value: "Ada"},
	})
	if err != nil {
		t.Fatalf("EvaluateCompleteness: %v", err)
	}
	want := []snapshot.CompletenessVerdict{snapshot.VerdictSatisfied, snapshot.VerdictMissing, snapshot.VerdictSatisfied}
	for i, disposition := range result.Dispositions {
		if disposition.Verdict != want[i] {
			t.Fatalf("verdict[%d] for %s = %s, want %s", i, disposition.Name, disposition.Verdict, want[i])
		}
	}
	const wantDigest = "sha256:0315f3617d6660393046fd09736e9b6458c911748aa8b3e0dab4358260548230"
	if result.Digest != wantDigest {
		t.Fatalf("Digest = %q, want %q", result.Digest, wantDigest)
	}
}

// TestTodo_SNAPSHOT_003_Fault proves that UNKNOWN is not collapsed into
// MISSING or false, and that a required UNKNOWN returns a named refusal.
func TestTodo_SNAPSHOT_003_Fault(t *testing.T) {
	result, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, snapshot.InputSpecification{Inputs: []snapshot.CompletenessRequirement{{Name: "worker", Policy: snapshot.InputRequired}}}, []snapshot.SnapshotInput{{Name: "worker", Presence: snapshot.PresenceUnknown}})
	if !errors.Is(err, snapshot.ErrCompletenessRefused) || snapshot.CompletenessInputOf(err) != "worker" {
		t.Fatalf("error = %v, want named completeness refusal for worker", err)
	}
	if result.Overall != snapshot.VerdictUnknown || result.Dispositions[0].Verdict != snapshot.VerdictUnknown {
		t.Fatalf("unknown result = %+v, want UNKNOWN overall and per input", result)
	}

	conditional := snapshot.InputSpecification{Inputs: []snapshot.CompletenessRequirement{
		{Name: "condition", Policy: snapshot.InputRequired},
		{Name: "dependent", Policy: snapshot.InputConditional, Condition: &snapshot.CompletenessCondition{InputName: "condition", ExpectedValue: "enabled"}},
	}}
	conditionalResult, conditionalErr := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, conditional, []snapshot.SnapshotInput{{Name: "condition", Presence: snapshot.PresenceUnknown}})
	if !errors.Is(conditionalErr, snapshot.ErrCompletenessRefused) || snapshot.CompletenessInputOf(conditionalErr) != "condition" {
		t.Fatalf("conditional UNKNOWN error = %v, want refusal naming condition", conditionalErr)
	}
	if got := dispositionByName(t, conditionalResult, "dependent"); got.Verdict != snapshot.VerdictUnknown {
		t.Fatalf("conditional disposition = %+v, want UNKNOWN", got)
	}
}

// TestTodo_SNAPSHOT_003_Security proves that Explain is safe for refusal and
// evidence paths: it names protected inputs and states, never their values.
func TestTodo_SNAPSHOT_003_Security(t *testing.T) {
	result, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, snapshot.InputSpecification{Inputs: []snapshot.CompletenessRequirement{{Name: "protected_worker", Policy: snapshot.InputRequired}}}, []snapshot.SnapshotInput{{Name: "protected_worker", Presence: snapshot.PresenceUnknown, Value: ""}})
	if err == nil {
		t.Fatal("required UNKNOWN input was accepted")
	}
	explanation := result.Explain()
	if !strings.Contains(explanation, "protected_worker") || strings.Contains(explanation, "secret-value") {
		t.Fatalf("unsafe explanation = %q", explanation)
	}
}

// TestTodo_SNAPSHOT_003_Property proves verdict noninterference: adding an
// unrelated resolved input cannot change any declared input's verdict.
func TestTodo_SNAPSHOT_003_Property(t *testing.T) {
	base := []snapshot.SnapshotInput{{Name: "worker", Presence: snapshot.PresencePresent, Value: "manager"}, {Name: "manager", Presence: snapshot.PresencePresent, Value: "Ada"}}
	without, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), base)
	if err != nil {
		t.Fatal(err)
	}
	with, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), append(base, snapshot.SnapshotInput{Name: "unrelated", Presence: snapshot.PresenceUnknown}))
	if err != nil {
		t.Fatal(err)
	}
	for _, before := range without.Dispositions {
		after := dispositionByName(t, with, before.Name)
		if before.Verdict != after.Verdict {
			t.Fatalf("unrelated input changed %s: %s -> %s", before.Name, before.Verdict, after.Verdict)
		}
	}
}

// TestTodo_SNAPSHOT_003_Mutation proves that changing any material presence
// or condition value changes the canonical result, while input ordering does
// not change it.
func TestTodo_SNAPSHOT_003_Mutation(t *testing.T) {
	base := []snapshot.SnapshotInput{{Name: "worker", Presence: snapshot.PresencePresent, Value: "manager"}, {Name: "manager", Presence: snapshot.PresencePresent, Value: "Ada"}}
	first, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), base)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), []snapshot.SnapshotInput{base[1], base[0]})
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Digest != first.Digest {
		t.Fatalf("observation order changed digest: %q vs %q", first.Digest, reordered.Digest)
	}
	changed := []snapshot.SnapshotInput{{Name: "worker", Presence: snapshot.PresencePresent, Value: "individual"}, {Name: "manager", Presence: snapshot.PresencePresent, Value: "Ada"}}
	second, err := snapshot.EvaluateCompleteness(snapshot.ReadSnapshot{Digest: "sha256:resolved"}, completenessSpec(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest == first.Digest {
		t.Fatal("changing a condition value did not change the canonical digest")
	}
}
