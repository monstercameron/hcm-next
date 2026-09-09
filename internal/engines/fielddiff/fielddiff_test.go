package fielddiff

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// at builds an instant for a fixture side.
func at(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return values.NewInstant(parsed)
}

// side builds one comparison side.
func side(kind ValueKind, v values.Presence[string], updated values.Instant) Side {
	return Side{Kind: kind, Value: v, UpdatedAt: updated}
}

// TestCompareRelatesTwoSidesWithoutJudgingThem covers the whole relation
// vocabulary and the order in which the checks apply.
func TestCompareRelatesTwoSidesWithoutJudgingThem(t *testing.T) {
	early := at(t, "2026-01-01T00:00:00Z")
	late := at(t, "2026-06-01T00:00:00Z")

	for _, tc := range []struct {
		name      string
		canonical Side
		external  Side
		relation  Relation
		reason    string
		ordered   bool
	}{
		{
			"equal values match",
			side(KindEnum, values.Value("P3"), early),
			side(KindEnum, values.Value("P3"), late),
			RelationMatch, ReasonValuesEqual, false,
		},
		{
			"both vacant match",
			side(KindEnum, values.Absent[string](), values.Instant{}),
			side(KindEnum, values.Null[string](), values.Instant{}),
			RelationMatch, ReasonBothVacant, false,
		},
		{
			"canonical changed last",
			side(KindEnum, values.Value("P4"), late),
			side(KindEnum, values.Value("P3"), early),
			RelationCanonicalAhead, ReasonCanonicalNewer, true,
		},
		{
			"external changed last",
			side(KindEnum, values.Value("P3"), early),
			side(KindEnum, values.Value("P4"), late),
			RelationExternalAhead, ReasonExternalNewer, true,
		},
		{
			"simultaneous change is a conflict",
			side(KindEnum, values.Value("P4"), late),
			side(KindEnum, values.Value("P5"), late),
			RelationConflict, ReasonSimultaneous, false,
		},
		{
			"one side with no update time cannot be ordered",
			side(KindEnum, values.Value("P4"), late),
			side(KindEnum, values.Value("P5"), values.Instant{}),
			RelationConflict, ReasonUnordered, false,
		},
		{
			"canonical side has no value",
			side(KindEnum, values.Absent[string](), values.Instant{}),
			side(KindEnum, values.Value("P3"), late),
			RelationMissingLeft, ReasonCanonicalVacant, false,
		},
		{
			"external side has no value",
			side(KindEnum, values.Value("P3"), late),
			side(KindEnum, values.Null[string](), values.Instant{}),
			RelationMissingRight, ReasonExternalVacant, false,
		},
		{
			"declared kinds differ",
			side(KindEnum, values.Value("3"), late),
			side(KindDecimal, values.Value("3"), late),
			RelationTypeMismatch, ReasonKindsDiffer, false,
		},
		{
			"a kind disagreement outranks an unreadable side",
			side(KindEnum, values.Unknown[string]("not_loaded"), values.Instant{}),
			side(KindDecimal, values.Value("3"), late),
			RelationTypeMismatch, ReasonKindsDiffer, false,
		},
	} {
		got, err := Compare(tc.canonical, tc.external)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Relation != tc.relation || got.Reason != tc.reason || got.Ordered != tc.ordered {
			t.Fatalf("%s: relation=%s reason=%q ordered=%t, want %s/%q/%t",
				tc.name, got.Relation, got.Reason, got.Ordered,
				tc.relation, tc.reason, tc.ordered)
		}
		if got.Validate() != nil {
			t.Fatalf("%s: outcome does not validate", tc.name)
		}
		if got.Canonical() == nil {
			t.Fatalf("%s: outcome has no canonical encoding", tc.name)
		}
	}
}

// TestCompareRefusesToReadWhatItCannotSee proves the engine never turns an
// epistemic gap into agreement or disagreement.
func TestCompareRefusesToReadWhatItCannotSee(t *testing.T) {
	readable := side(KindEnum, values.Value("P3"), values.Instant{})
	for _, unreadable := range []values.Presence[string]{
		values.Unknown[string]("not_loaded"),
		values.Redacted[string]("restricted"),
		values.Unavailable[string]("source_down"),
	} {
		blind := side(KindEnum, unreadable, values.Instant{})
		if _, err := Compare(blind, readable); !errors.Is(err, ErrNotComparable) {
			t.Fatalf("canonical %s: err = %v, want ErrNotComparable", unreadable.State(), err)
		}
		if _, err := Compare(readable, blind); !errors.Is(err, ErrNotComparable) {
			t.Fatalf("external %s: err = %v, want ErrNotComparable", unreadable.State(), err)
		}
	}
}

// TestCompareRejectsUndeclaredKinds proves a side must say what type it is
// carrying.
func TestCompareRejectsUndeclaredKinds(t *testing.T) {
	good := side(KindEnum, values.Value("P3"), values.Instant{})
	bad := side(KindUnspecified, values.Value("P3"), values.Instant{})
	if _, err := Compare(bad, good); !errors.Is(err, ErrKindUnspecified) {
		t.Fatalf("err = %v, want ErrKindUnspecified", err)
	}
	unset := Side{Kind: KindEnum}
	if _, err := Compare(unset, good); !errors.Is(err, ErrSideInvalid) {
		t.Fatalf("unset presence: err = %v, want ErrSideInvalid", err)
	}
}

// TestCompareIsSymmetricInItsVocabulary proves that swapping the two sides
// swaps the directional relations and leaves the symmetric ones alone.
func TestCompareIsSymmetricInItsVocabulary(t *testing.T) {
	early := at(t, "2026-01-01T00:00:00Z")
	late := at(t, "2026-06-01T00:00:00Z")
	a := side(KindEnum, values.Value("P4"), late)
	b := side(KindEnum, values.Value("P3"), early)

	forward, err := Compare(a, b)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reverse, err := Compare(b, a)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if forward.Relation != RelationCanonicalAhead || reverse.Relation != RelationExternalAhead {
		t.Fatalf("forward=%s reverse=%s", forward.Relation, reverse.Relation)
	}
}

// TestOutcomeExplainsItselfWithoutTheValues proves the engine contract:
// a version, and an explanation that never carries a compared value.
func TestOutcomeExplainsItselfWithoutTheValues(t *testing.T) {
	if Version() < 1 {
		t.Fatalf("engine version = %d", Version())
	}
	late := at(t, "2026-06-01T00:00:00Z")
	early := at(t, "2026-01-01T00:00:00Z")
	got, err := Compare(
		side(KindEnum, values.Value("P4"), late),
		side(KindEnum, values.Value("P3"), early),
	)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	lines := got.Explain()
	if len(lines) != 3 {
		t.Fatalf("explanation = %v", lines)
	}
	for _, line := range lines {
		for _, secret := range []string{"P3", "P4"} {
			if len(line) >= len(secret) && containsSubstring(line, secret) {
				t.Fatalf("the explanation leaked %q: %q", secret, line)
			}
		}
	}
	if (Outcome{}).Explain() != nil {
		t.Fatal("an invalid outcome produced an explanation")
	}
}

// containsSubstring reports whether s contains sub.
func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// FuzzCompare drives both sides with arbitrary values, kinds and timestamps.
// No input may panic, and every accepted comparison must produce a legal,
// reasoned relation.
func FuzzCompare(f *testing.F) {
	f.Add("P3", "P4", "ENUM", "ENUM", int64(1), int64(2))
	f.Add("", "", "", "", int64(0), int64(0))
	f.Add("3", "3.0", "DECIMAL", "MONEY", int64(-1), int64(1))

	f.Fuzz(func(t *testing.T, left, right, leftKind, rightKind string, leftAt, rightAt int64) {
		build := func(v, kind string, unix int64) Side {
			s := Side{Kind: ValueKind(kind), Value: values.Value(v)}
			if unix != 0 {
				if instant, err := values.NewInstantFromUnix(unix, 0); err == nil {
					s.UpdatedAt = instant
				}
			}
			return s
		}
		got, err := Compare(build(left, leftKind, leftAt), build(right, rightKind, rightAt))
		if err != nil {
			return
		}
		if !got.Relation.Valid() || got.Reason == "" {
			t.Fatalf("relation=%s reason=%q", got.Relation, got.Reason)
		}
		if got.Ordered && got.Relation != RelationCanonicalAhead &&
			got.Relation != RelationExternalAhead {
			t.Fatalf("relation %s claims an ordering", got.Relation)
		}
		if got.Relation == RelationMatch && left != right {
			t.Fatalf("different values %q and %q matched", left, right)
		}
	})
}
