package conflict_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction/conflict"
)

func mustResourceKey(t *testing.T, segments ...string) values.ResourceKey {
	t.Helper()
	k, err := values.NewResourceKey(values.TenantId("acme-eu"), values.Kind("assignment"), segments...)
	if err != nil {
		t.Fatalf("build resource key: %v", err)
	}
	return k
}

func mustSequenceRevision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("build revision token: %v", err)
	}
	return rev
}

func mustOpenInterval(t *testing.T, startYear, startMonth, startDay int) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(startYear, time.Month(startMonth), startDay, 0, 0, 0, 0, time.UTC))
	iv, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatalf("build open interval: %v", err)
	}
	return iv
}

func mustClosedInterval(t *testing.T, start, end time.Time) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewInstantInterval(values.NewInstant(start), values.NewInstant(end))
	if err != nil {
		t.Fatalf("build closed interval: %v", err)
	}
	return iv
}

func baseFootprint(t *testing.T) conflict.WriteFootprint {
	t.Helper()
	return conflict.WriteFootprint{
		Resource:         mustResourceKey(t, "employment", "9001", "primary"),
		Field:            "employment.assignment.position_ref",
		Interval:         mustOpenInterval(t, 2026, 10, 1),
		Operation:        conflict.OperationUpdate,
		ExpectedRevision: mustSequenceRevision(t, "people.employment.9001", 42),
		Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local_master/v1"},
	}
}

// TestTodo_CONFLICT_001 is the PRIMARY test for normalizing proposal write
// footprints.
//
// RED: field aliases, parent/child paths or open-ended intervals evade
// overlap detection.
//
// GREEN: the write set contains canonical resource, field path, effective
// interval, operation, expected revision and authority scope.
func TestTodo_CONFLICT_001(t *testing.T) {
	t.Run("RED: underdeclared footprints are rejected", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*conflict.WriteFootprint)
		}{
			{"no resource", func(w *conflict.WriteFootprint) { w.Resource = values.ResourceKey{} }},
			{"no field path", func(w *conflict.WriteFootprint) { w.Field = "" }},
			{"empty path segment", func(w *conflict.WriteFootprint) { w.Field = "employment..position_ref" }},
			{"no interval", func(w *conflict.WriteFootprint) { w.Interval = values.EffectiveInterval{} }},
			{"no operation", func(w *conflict.WriteFootprint) { w.Operation = "" }},
			{"unknown operation", func(w *conflict.WriteFootprint) { w.Operation = "PATCH" }},
			{"unpinned revision", func(w *conflict.WriteFootprint) { w.ExpectedRevision = values.UnspecifiedRevision() }},
			{"no authority domain", func(w *conflict.WriteFootprint) { w.Authority.Domain = "" }},
			{"no authority policy ref", func(w *conflict.WriteFootprint) { w.Authority.PolicyRef = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := baseFootprint(t)
				tc.break_(&w)
				if err := w.Validate(); !errors.Is(err, conflict.ErrInvalidFootprint) {
					t.Fatalf("an underdeclared footprint (%s) validated: %v", tc.name, err)
				}
				if w.Canonical() != nil {
					t.Fatalf("an underdeclared footprint (%s) produced canonical bytes", tc.name)
				}
			})
		}
	})

	t.Run("RED: a field alias evades overlap detection until normalized", func(t *testing.T) {
		a := baseFootprint(t)
		a.Field = "employment.assignment.position_ref"

		// b writes the same field under its historical alias spelling.
		bRaw := baseFootprint(t)
		bRaw.Field = "assignment.position" // stale/legacy spelling of the same field

		// Before normalization the alias spelling does not overlap: the
		// evasion this RED case names.
		if overlap, err := a.Overlaps(bRaw); err != nil || overlap {
			t.Fatalf("unnormalized alias unexpectedly overlapped (or errored): overlap=%v err=%v", overlap, err)
		}

		aliases := conflict.FieldAliasTable{
			RuleRef: "employment_field_aliases", Version: "v1",
			Aliases: map[conflict.FieldPath]conflict.FieldPath{
				"assignment.position": "employment.assignment.position_ref",
			},
		}
		b, err := conflict.NormalizeFootprint(bRaw, aliases)
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		overlap, err := a.Overlaps(b)
		if err != nil {
			t.Fatalf("overlaps: %v", err)
		}
		if !overlap {
			t.Fatalf("normalized alias still evaded overlap detection")
		}
	})

	t.Run("RED: a parent/child field path evades overlap detection", func(t *testing.T) {
		parent := baseFootprint(t)
		parent.Field = "employment.assignment"
		child := baseFootprint(t)
		child.Field = "employment.assignment.grade"

		overlap, err := parent.Overlaps(child)
		if err != nil {
			t.Fatalf("overlaps: %v", err)
		}
		if !overlap {
			t.Fatalf("a write to a parent field did not overlap a write to its child")
		}

		// A field that merely shares a string prefix, but is not a real
		// dot-segment ancestor, must NOT be treated as overlapping.
		lookalike := baseFootprint(t)
		lookalike.Field = "employment.assignment2.grade"
		overlap, err = parent.Overlaps(lookalike)
		if err != nil {
			t.Fatalf("overlaps: %v", err)
		}
		if overlap {
			t.Fatalf("a string-prefix lookalike field was wrongly treated as a parent/child overlap")
		}
	})

	t.Run("RED: an open-ended interval evades overlap detection", func(t *testing.T) {
		openEnded := baseFootprint(t)
		openEnded.Interval = mustOpenInterval(t, 2026, 1, 1)

		farFuture := baseFootprint(t)
		farFuture.Interval = mustClosedInterval(t,
			time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC))

		overlap, err := openEnded.Overlaps(farFuture)
		if err != nil {
			t.Fatalf("overlaps: %v", err)
		}
		if !overlap {
			t.Fatalf("an open-ended interval failed to overlap a closed interval entirely inside its open future")
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		w := baseFootprint(t)
		if err := w.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if w.Resource.Validate() != nil {
			t.Fatalf("footprint does not carry a canonical resource")
		}
		if w.Field == "" {
			t.Fatalf("footprint does not carry a field path")
		}
		if w.Interval.Validate() != nil {
			t.Fatalf("footprint does not carry an effective interval")
		}
		if !w.Operation.Valid() {
			t.Fatalf("footprint does not carry a declared operation")
		}
		if !w.ExpectedRevision.IsSpecified() {
			t.Fatalf("footprint does not carry an expected revision")
		}
		if err := w.Authority.Validate(); err != nil {
			t.Fatalf("footprint does not carry an authority scope: %v", err)
		}
		if w.Canonical() == nil {
			t.Fatalf("a complete footprint produced no canonical bytes")
		}
		if w.Digest() == "" {
			t.Fatalf("a complete footprint produced no digest")
		}
	})
}

// TestTodo_CONFLICT_001_Property drives a spread of resource/field/interval
// combinations through Overlaps and requires the answer to be exactly what
// the resource, field ancestry and interval half-open semantics predict.
func TestTodo_CONFLICT_001_Property(t *testing.T) {
	key1 := mustResourceKey(t, "employment", "9001", "primary")
	key2 := mustResourceKey(t, "employment", "9002", "primary")
	rev := mustSequenceRevision(t, "people.employment.9001", 1)
	authority := conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local_master/v1"}

	mk := func(resource values.ResourceKey, field conflict.FieldPath, iv values.EffectiveInterval) conflict.WriteFootprint {
		return conflict.WriteFootprint{
			Resource: resource, Field: field, Interval: iv,
			Operation: conflict.OperationUpdate, ExpectedRevision: rev, Authority: authority,
		}
	}

	touching := []struct {
		start, mid, end time.Time
	}{{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}}[0]

	ivEarly := mustClosedInterval(t, touching.start, touching.mid)
	ivLate := mustClosedInterval(t, touching.mid, touching.end) // starts exactly where ivEarly ends
	ivOverlapping := mustClosedInterval(t,
		touching.start.AddDate(0, 1, 0), touching.end)

	cases := []struct {
		name string
		a, b conflict.WriteFootprint
		want bool
	}{
		{"different resources never overlap", mk(key1, "f", ivEarly), mk(key2, "f", ivEarly), false},
		{"same resource, same field, same interval overlaps", mk(key1, "f", ivEarly), mk(key1, "f", ivEarly), true},
		{"same resource, disjoint fields do not overlap", mk(key1, "f1", ivEarly), mk(key1, "f2", ivEarly), false},
		{"same resource, same field, half-open adjacent intervals do not overlap", mk(key1, "f", ivEarly), mk(key1, "f", ivLate), false},
		{"same resource, same field, genuinely overlapping intervals overlap", mk(key1, "f", ivEarly), mk(key1, "f", ivOverlapping), true},
		{"ancestor field overlaps descendant field", mk(key1, "a.b", ivEarly), mk(key1, "a.b.c", ivEarly), true},
		{"descendant field overlaps ancestor field", mk(key1, "a.b.c", ivEarly), mk(key1, "a.b", ivEarly), true},
		{"sibling fields do not overlap", mk(key1, "a.b", ivEarly), mk(key1, "a.c", ivEarly), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.a.Overlaps(tc.b)
			if err != nil {
				t.Fatalf("overlaps: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Overlaps = %v, want %v", got, tc.want)
			}
			// Overlaps must be symmetric.
			got2, err := tc.b.Overlaps(tc.a)
			if err != nil {
				t.Fatalf("overlaps (reversed): %v", err)
			}
			if got2 != tc.want {
				t.Fatalf("Overlaps(b, a) = %v, want %v (symmetry violated)", got2, tc.want)
			}
		})
	}
}

// TestTodo_CONFLICT_001_Golden pins the exact canonical digest of a fixed
// footprint so a silent field reordering, an encoding change, or a dropped
// dimension is caught by a test diff, not discovered downstream. The literal
// hash below was computed once from the encoding this file documents; a
// deliberate encoding change updates it in the same commit that changes the
// encoding.
func TestTodo_CONFLICT_001_Golden(t *testing.T) {
	const wantDigest = "82bfc9b8ecdfa3290f8aebc505f7fcf94d8ea1c6718388ef478a50fc85e5a2ef"

	w := baseFootprint(t)
	got := w.Digest()
	if got == "" {
		t.Fatalf("golden footprint produced no digest")
	}
	if got != wantDigest {
		t.Fatalf("footprint digest drifted: got %s, want %s", got, wantDigest)
	}

	// A second, independently constructed but semantically identical
	// footprint must hash identically regardless of construction path.
	w2 := conflict.WriteFootprint{
		Resource:         mustResourceKey(t, "employment", "9001", "primary"),
		Field:            conflict.FieldPath("employment.assignment.position_ref"),
		Interval:         mustOpenInterval(t, 2026, 10, 1),
		Operation:        conflict.OperationUpdate,
		ExpectedRevision: mustSequenceRevision(t, "people.employment.9001", 42),
		Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local_master/v1"},
	}
	if got2 := w2.Digest(); got2 != got {
		t.Fatalf("two semantically identical footprints hashed differently: %q vs %q", got, got2)
	}
}

// TestTodo_CONFLICT_001_Mutation perturbs one dimension of a footprint at a
// time and requires the digest to move: a digest that ignored a dimension
// would let a footprint be substituted after a domain compiler generated it.
func TestTodo_CONFLICT_001_Mutation(t *testing.T) {
	baseline := baseFootprint(t)
	baselineDigest := baseline.Digest()
	if baselineDigest == "" {
		t.Fatalf("baseline footprint produced no digest")
	}

	mutations := []struct {
		name   string
		mutate func(*conflict.WriteFootprint)
	}{
		{"the resource", func(w *conflict.WriteFootprint) { w.Resource = mustResourceKey(t, "employment", "9002", "primary") }},
		{"the field path", func(w *conflict.WriteFootprint) { w.Field = "employment.assignment.grade" }},
		{"the interval", func(w *conflict.WriteFootprint) { w.Interval = mustOpenInterval(t, 2027, 1, 1) }},
		{"the operation", func(w *conflict.WriteFootprint) { w.Operation = conflict.OperationDelete }},
		{"the expected revision", func(w *conflict.WriteFootprint) {
			w.ExpectedRevision = mustSequenceRevision(t, "people.employment.9001", 43)
		}},
		{"the authority domain", func(w *conflict.WriteFootprint) { w.Authority.Domain = "POSITION" }},
		{"the authority policy ref", func(w *conflict.WriteFootprint) { w.Authority.PolicyRef = "authority.external/v1" }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			w := baseFootprint(t)
			m.mutate(&w)
			if err := w.Validate(); err != nil {
				t.Fatalf("mutated footprint is invalid: %v", err)
			}
			if got := w.Digest(); got == baselineDigest {
				t.Fatalf("changing %s left the footprint digest unchanged", m.name)
			}
		})
	}
}
