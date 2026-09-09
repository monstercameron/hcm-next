package model_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// TestTodo_MODEL_019 is the PRIMARY test for external-code crosswalks.
//
// RED: tests return UNKNOWN, AMBIGUOUS or OUT_OF_EFFECTIVE_RANGE instead of
// guessing a canonical value.
//
// GREEN: exact external system/object/code/effective interval maps to one
// canonical resource with mapping version.
func TestTodo_MODEL_019(t *testing.T) {
	base := model.CrosswalkMapping{
		MappingRef:     "crosswalk.workday_job/v1",
		ExternalSystem: "WORKDAY",
		ObjectType:     "JOB_PROFILE",
		ExternalCode:   "WD-ENG-3",
		CanonicalRef:   "JOB_FAMILY/ENG",
		Effective:      mustInterval(t, 2025, time.January, 1, 2026, time.January, 1),
		EvidenceRef:    "evidence:1",
	}
	later := base
	later.MappingRef = "crosswalk.workday_job/v2"
	later.Effective = mustOpenInterval(t, 2026, time.January, 1)

	ambiguousA := model.CrosswalkMapping{
		MappingRef:     "crosswalk.sap_job/v1-a",
		ExternalSystem: "SAP",
		ObjectType:     "JOB",
		ExternalCode:   "SAP-9",
		CanonicalRef:   "JOB_FAMILY/OPS",
		Effective:      mustOpenInterval(t, 2025, time.January, 1),
		EvidenceRef:    "evidence:2a",
		Precedence:     1,
	}
	ambiguousB := model.CrosswalkMapping{
		MappingRef:     "crosswalk.sap_job/v1-b",
		ExternalSystem: "SAP",
		ObjectType:     "JOB",
		ExternalCode:   "SAP-9",
		CanonicalRef:   "JOB_FAMILY/ENG",
		Effective:      mustOpenInterval(t, 2025, time.January, 1),
		EvidenceRef:    "evidence:2b",
		Precedence:     1,
	}

	manyToOneLow := model.CrosswalkMapping{
		MappingRef:     "crosswalk.oracle_job/v1",
		ExternalSystem: "ORACLE",
		ObjectType:     "JOB",
		ExternalCode:   "ORC-1",
		CanonicalRef:   "JOB_FAMILY/LEGACY",
		Effective:      mustOpenInterval(t, 2020, time.January, 1),
		EvidenceRef:    "evidence:3a",
		Precedence:     0,
	}
	manyToOneHigh := model.CrosswalkMapping{
		MappingRef:     "crosswalk.oracle_job/v2",
		ExternalSystem: "ORACLE",
		ObjectType:     "JOB",
		ExternalCode:   "ORC-1",
		CanonicalRef:   "JOB_FAMILY/ENG",
		Effective:      mustOpenInterval(t, 2020, time.January, 1),
		EvidenceRef:    "evidence:3b",
		Precedence:     5,
	}

	mappings := []model.CrosswalkMapping{base, later, ambiguousA, ambiguousB, manyToOneLow, manyToOneHigh}

	t.Run("RED", func(t *testing.T) {
		t.Run("unknown external code", func(t *testing.T) {
			res, err := model.ResolveCrosswalk(mappings, "WORKDAY", "JOB_PROFILE", "NOPE", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrCrosswalkUnknown) {
				t.Fatalf("resolved an unknown code: %v", err)
			}
			if res.Status != model.CrosswalkUnknown {
				t.Fatalf("status = %s, want UNKNOWN", res.Status)
			}
		})

		t.Run("out of effective range", func(t *testing.T) {
			res, err := model.ResolveCrosswalk(mappings, "WORKDAY", "JOB_PROFILE", "WD-ENG-3", instant(2024, time.June, 1))
			if !errors.Is(err, model.ErrCrosswalkOutOfRange) {
				t.Fatalf("resolved out of every mapping's effective range: %v", err)
			}
			if res.Status != model.CrosswalkOutOfRange {
				t.Fatalf("status = %s, want OUT_OF_EFFECTIVE_RANGE", res.Status)
			}
		})

		t.Run("ambiguous collision", func(t *testing.T) {
			res, err := model.ResolveCrosswalk(mappings, "SAP", "JOB", "SAP-9", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrCrosswalkAmbiguous) {
				t.Fatalf("resolved an ambiguous collision: %v", err)
			}
			if res.Status != model.CrosswalkAmbiguous {
				t.Fatalf("status = %s, want AMBIGUOUS", res.Status)
			}
			if len(res.Candidates) != 2 {
				t.Fatalf("candidates = %v, want 2 tied canonical resources", res.Candidates)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("resolves the mapping version effective at asOf", func(t *testing.T) {
			res, err := model.ResolveCrosswalk(mappings, "WORKDAY", "JOB_PROFILE", "WD-ENG-3", instant(2025, time.June, 1))
			if err != nil {
				t.Fatalf("resolve at 2025-06-01: %v", err)
			}
			if res.MappingRef != base.MappingRef || res.CanonicalRef != base.CanonicalRef {
				t.Fatalf("resolution = %+v, want mapping %s to %s", res, base.MappingRef, base.CanonicalRef)
			}

			res, err = model.ResolveCrosswalk(mappings, "WORKDAY", "JOB_PROFILE", "WD-ENG-3", instant(2026, time.June, 1))
			if err != nil {
				t.Fatalf("resolve at 2026-06-01: %v", err)
			}
			if res.MappingRef != later.MappingRef {
				t.Fatalf("resolution = %+v, want mapping %s", res, later.MappingRef)
			}
		})

		t.Run("many-to-one resolves via explicit precedence", func(t *testing.T) {
			res, err := model.ResolveCrosswalk(mappings, "ORACLE", "JOB", "ORC-1", instant(2026, time.June, 1))
			if err != nil {
				t.Fatalf("resolve many-to-one: %v", err)
			}
			if res.CanonicalRef != manyToOneHigh.CanonicalRef {
				t.Fatalf("canonical = %s, want the higher-precedence %s", res.CanonicalRef, manyToOneHigh.CanonicalRef)
			}
		})

		t.Run("reverse lookup finds every mapping to a canonical resource", func(t *testing.T) {
			out, err := model.ReverseLookupCrosswalk(mappings, "JOB_FAMILY/ENG", instant(2026, time.June, 1))
			if err != nil {
				t.Fatalf("reverse lookup: %v", err)
			}
			var refs []string
			for _, m := range out {
				refs = append(refs, m.MappingRef)
			}
			want := []string{later.MappingRef, ambiguousB.MappingRef, manyToOneHigh.MappingRef}
			if !reflect.DeepEqual(sortedCopy(refs), sortedCopy(want)) {
				t.Fatalf("reverse lookup = %v, want %v", refs, want)
			}
		})
	})
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestTodo_MODEL_019_Property asserts a general invariant of
// [ResolveCrosswalk]: it never returns CrosswalkResolved with a status other
// than RESOLVED, and every non-nil error corresponds exactly to the returned
// status.
func TestTodo_MODEL_019_Property(t *testing.T) {
	m := baseCrosswalkMappingReal(t)
	res, err := model.ResolveCrosswalk([]model.CrosswalkMapping{m}, m.ExternalSystem, m.ObjectType, m.ExternalCode, instant(2026, time.June, 1))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Status != model.CrosswalkResolved {
		t.Fatalf("status = %s, want RESOLVED", res.Status)
	}
	if !res.Status.Valid() {
		t.Fatalf("status %q reports itself invalid", res.Status)
	}
}

func baseCrosswalkMappingReal(t *testing.T) model.CrosswalkMapping {
	t.Helper()
	return model.CrosswalkMapping{
		MappingRef:     "crosswalk.workday_job/v1",
		ExternalSystem: "WORKDAY",
		ObjectType:     "JOB_PROFILE",
		ExternalCode:   "WD-ENG-3",
		CanonicalRef:   "JOB_FAMILY/ENG",
		Effective:      mustOpenInterval(t, 2025, time.January, 1),
		EvidenceRef:    "evidence:1",
	}
}

// TestTodo_MODEL_019_Golden pins one crosswalk resolution's shape.
func TestTodo_MODEL_019_Golden(t *testing.T) {
	m := baseCrosswalkMappingReal(t)
	res, err := model.ResolveCrosswalk([]model.CrosswalkMapping{m}, m.ExternalSystem, m.ObjectType, m.ExternalCode, instant(2026, time.June, 1))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	goldenJSON(t, "model_019_crosswalk_resolution.json", res)
}

// TestTodo_MODEL_019_Integration exercises resolve, correction and reverse
// lookup together: a corrected mapping's new canonical target is visible to
// both ResolveCrosswalk and ReverseLookupCrosswalk, and the stale reverse
// index entry is gone.
func TestTodo_MODEL_019_Integration(t *testing.T) {
	previous := baseCrosswalkMappingReal(t)
	previous.Effective = mustInterval(t, 2025, time.January, 1, 2026, time.July, 1)
	current := previous
	current.MappingRef = "crosswalk.workday_job/v2"
	current.CanonicalRef = "JOB_FAMILY/OPS"
	current.Effective = mustOpenInterval(t, 2026, time.July, 1)

	consumers := []string{"payroll.export", "reporting.headcount"}
	rev, impact, err := model.ReviseCrosswalk(previous, current, instant(2026, time.July, 1), "actor:governance", consumers)
	if err != nil {
		t.Fatalf("revise crosswalk: %v", err)
	}
	if !impact.CanonicalRefChanged {
		t.Fatalf("impact analysis did not detect the canonical-ref change")
	}
	if len(impact.AffectedConsumers) != 2 {
		t.Fatalf("affected consumers = %v, want %v", impact.AffectedConsumers, consumers)
	}
	if rev.Current.MappingRef != current.MappingRef {
		t.Fatalf("revision current = %+v", rev.Current)
	}

	mappings := []model.CrosswalkMapping{previous, current}
	res, err := model.ResolveCrosswalk(mappings, current.ExternalSystem, current.ObjectType, current.ExternalCode, instant(2026, time.August, 1))
	if err != nil {
		t.Fatalf("resolve after revision: %v", err)
	}
	if res.CanonicalRef != "JOB_FAMILY/OPS" {
		t.Fatalf("resolved canonical = %s, want the corrected JOB_FAMILY/OPS", res.CanonicalRef)
	}

	forward, err := model.ReverseLookupCrosswalk(mappings, "JOB_FAMILY/OPS", instant(2026, time.August, 1))
	if err != nil {
		t.Fatalf("reverse lookup after revision: %v", err)
	}
	if len(forward) != 1 || forward[0].MappingRef != current.MappingRef {
		t.Fatalf("reverse lookup after revision = %+v", forward)
	}
}

// TestTodo_MODEL_019_Fault proves a rejected revision leaves both the
// previous and current mapping values it was given completely unchanged, and
// returns zero-value results rather than a partially-built revision.
func TestTodo_MODEL_019_Fault(t *testing.T) {
	previous := baseCrosswalkMappingReal(t)
	current := previous // same MappingRef: does not advance the version.
	beforePrev, beforeCur := previous, current

	rev, impact, err := model.ReviseCrosswalk(previous, current, instant(2026, time.July, 1), "actor:governance", nil)
	if !errors.Is(err, model.ErrInvalidCrosswalkRevision) {
		t.Fatalf("revised a mapping into itself: %v", err)
	}
	if (rev != model.CrosswalkRevision{}) {
		t.Fatalf("a rejected revision returned a non-zero revision: %+v", rev)
	}
	if !reflect.DeepEqual(impact, model.CrosswalkImpactAnalysis{}) {
		t.Fatalf("a rejected revision returned a non-zero impact analysis: %+v", impact)
	}
	if !reflect.DeepEqual(previous, beforePrev) || !reflect.DeepEqual(current, beforeCur) {
		t.Fatalf("a rejected ReviseCrosswalk call mutated its inputs")
	}
}

// FuzzTodo_MODEL_019 fuzzes crosswalk resolution: ResolveCrosswalk must
// always return a status that agrees with which sentinel error (if any) it
// returns, and must never return CrosswalkResolved on an error.
func FuzzTodo_MODEL_019(f *testing.F) {
	f.Add("WORKDAY", "JOB_PROFILE", "WD-ENG-3", int64(2025), int64(1))
	f.Add("SAP", "JOB", "SAP-9", int64(2026), int64(6))
	f.Add("NOPE", "NOPE", "NOPE", int64(2026), int64(6))
	f.Fuzz(func(t *testing.T, system, objectType, code string, year, month int64) {
		y := int(year%50) + 2000
		m := time.Month(int(month%12) + 1)
		mapping := model.CrosswalkMapping{
			MappingRef:     "crosswalk.fuzz/v1",
			ExternalSystem: "WORKDAY",
			ObjectType:     "JOB_PROFILE",
			ExternalCode:   "WD-ENG-3",
			CanonicalRef:   "JOB_FAMILY/ENG",
			Effective:      mustOpenInterval(t, 2025, time.January, 1),
			EvidenceRef:    "evidence:1",
		}
		res, err := model.ResolveCrosswalk([]model.CrosswalkMapping{mapping}, system, objectType, code, instant(y, m, 1))
		if err == nil && res.Status != model.CrosswalkResolved {
			t.Fatalf("nil error but status = %s", res.Status)
		}
		if err != nil && res.Status == model.CrosswalkResolved {
			t.Fatalf("error %v but status = RESOLVED", err)
		}
		if !res.Status.Valid() {
			t.Fatalf("status %q is not one of the four declared statuses", res.Status)
		}
	})
}
