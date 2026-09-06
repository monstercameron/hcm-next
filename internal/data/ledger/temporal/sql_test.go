package temporal

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSortedUniquePreservesTheNilDistinction(t *testing.T) {
	if got := sortedUnique(nil); got != nil {
		t.Fatalf("sortedUnique(nil) = %v, want nil: a nil list means \"not restricted\"", got)
	}
	if got := sortedUnique([]string{}); got == nil || len(got) != 0 {
		t.Fatalf("sortedUnique([]) = %v, want an empty non-nil list: it means \"restricted to nothing\"", got)
	}
	if got := sortedUnique([]string{"b", "a", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("sortedUnique = %v, want [a b]", got)
	}
}

func TestNormalizeDecisionMakesOrderingIrrelevant(t *testing.T) {
	tenant := uuid.New()
	a := normalizeDecision(Decision{Tenant: tenant, AllowFields: []string{"y", "x"}, DenySubjects: []string{"s2", "s1", "s2"}})
	b := normalizeDecision(Decision{Tenant: tenant, AllowFields: []string{"x", "y"}, DenySubjects: []string{"s1", "s2"}})
	if !slices.Equal(a.AllowFields, b.AllowFields) || !slices.Equal(a.DenySubjects, b.DenySubjects) {
		t.Fatalf("normalize left %+v and %+v different", a, b)
	}
}

func TestBuildReconstructSQL(t *testing.T) {
	tenant := uuid.New()
	coord := Coordinate{
		EffectiveAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		KnownAt:     time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	}
	base := Request{Tenant: tenant, Mode: ModeReconstruct, Subject: "worker:1"}

	t.Run("both temporal bounds are inclusive at a point coordinate", func(t *testing.T) {
		sqlText, args, err := BuildReconstructSQL(base, Decision{Tenant: tenant}, coord, 10)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !strings.Contains(sqlText, "e.effective_at <= ") || !strings.Contains(sqlText, "e.recorded_at <= ") {
			t.Fatalf("statement does not bound both axes inclusively:\n%s", sqlText)
		}
		if !slices.Contains(args, any(coord.EffectiveAt)) || !slices.Contains(args, any(coord.KnownAt)) {
			t.Fatalf("args %v do not carry the coordinate", args)
		}
		// limit+1 so the caller can tell "exactly the limit" from "more".
		if last := args[len(args)-1]; last != 11 {
			t.Fatalf("limit parameter is %v, want limit+1 = 11", last)
		}
	})

	t.Run("every authorization boundary becomes a predicate", func(t *testing.T) {
		dec := Decision{
			Tenant:        tenant,
			AllowSubjects: []string{"worker:1"},
			DenySubjects:  []string{"worker:9"},
			AllowFields:   []string{"a@1"},
			DenyFields:    []string{"b@1"},
		}
		sqlText, _, err := BuildReconstructSQL(base, dec, coord, 10)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		for _, want := range []string{
			"e.stream_key = ANY(",
			"NOT (e.stream_key = ANY(",
			"e.schema_ref = ANY(",
			"NOT (e.schema_ref = ANY(",
		} {
			if !strings.Contains(sqlText, want) {
				t.Fatalf("statement is missing %q:\n%s", want, sqlText)
			}
		}
	})

	t.Run("an unresolved coordinate is refused", func(t *testing.T) {
		_, _, err := BuildReconstructSQL(base, Decision{Tenant: tenant}, Coordinate{}, 10)
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) || invalid.Field != "Coordinate" {
			t.Fatalf("build with a zero coordinate = %v, want ErrRequestInvalid on Coordinate", err)
		}
	})

	t.Run("a delegated mode is refused", func(t *testing.T) {
		req := base
		req.Mode = ModeHistory
		_, _, err := BuildReconstructSQL(req, Decision{Tenant: tenant}, coord, 10)
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) || invalid.Field != "Mode" {
			t.Fatalf("build for HISTORY = %v, want ErrRequestInvalid on Mode", err)
		}
	})

	t.Run("a decision for another tenant is refused", func(t *testing.T) {
		_, _, err := BuildReconstructSQL(base, Decision{Tenant: uuid.New()}, coord, 10)
		var mismatch ErrTenantMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("build across tenants = %v, want ErrTenantMismatch", err)
		}
	})

	t.Run("the corrected assertion's class is never selected", func(t *testing.T) {
		sqlText, _, err := BuildReconstructSQL(base, Decision{Tenant: tenant}, coord, 10)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if strings.Contains(sqlText, "target.assertion_class") {
			t.Fatalf("statement reads the target's class, which a decision may withhold:\n%s", sqlText)
		}
	})
}
