package search_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/search"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// TestBuildSearchTextIncludesOnlyClassificationClearedFields is the unit-level
// half of RETRIEVAL-001's "a classified field never enters the projection"
// requirement: given every people.FieldID at once, including several this
// package has declared classified, the rendered text contains every cleared
// value and not one byte of any classified one.
func TestBuildSearchTextIncludesOnlyClassificationClearedFields(t *testing.T) {
	t.Parallel()
	const classifiedMarker = "PAY-ZONE-SECRET-BAND-9"
	fields := map[people.FieldID]string{
		people.FieldLegalName:     "Ada Lovelace",
		people.FieldPreferredName: "Ada",
		people.FieldWorkerNumber:  "W-9001",
		people.FieldJobCode:       "OPS-HRBP2",
		people.FieldOrgUnit:       "people-ops",
		people.FieldLocation:      "Boston, MA",
		people.FieldPositionID:    "POS-HRBP-900",
		// Classified: must never appear in the rendered text.
		people.FieldPayZone:          classifiedMarker,
		people.FieldFTE:              "1.0000",
		people.FieldManagerRelation:  "rel_mgr_secret",
		people.FieldEmploymentStatus: "ACTIVE",
		people.FieldGrade:            "GRADE-CLASSIFIED-XQ7",
		people.FieldHireDate:         "2021-04-05",
	}

	text := search.BuildSearchText(fields)

	for _, cleared := range []string{
		"Ada Lovelace", "Ada", "W-9001", "OPS-HRBP2", "people-ops", "Boston, MA", "POS-HRBP-900",
	} {
		if !strings.Contains(text, cleared) {
			t.Errorf("search text %q is missing cleared value %q", text, cleared)
		}
	}
	for _, classified := range []string{
		classifiedMarker, "1.0000", "rel_mgr_secret", "GRADE-CLASSIFIED-XQ7",
	} {
		if strings.Contains(text, classified) {
			t.Errorf("search text %q leaked classified value %q", text, classified)
		}
	}
}

// TestBuildSearchTextIsDeterministic proves the join order is a property of
// [search.ClearedFields], not of the caller's map iteration: two maps built
// with the same key/value pairs in different insertion order render
// byte-identical text. This is the pure-function half of the rebuild proof
// -- [search.Project] persists exactly what this function returns.
func TestBuildSearchTextIsDeterministic(t *testing.T) {
	t.Parallel()
	a := map[people.FieldID]string{
		people.FieldLegalName:    "Ada Lovelace",
		people.FieldJobCode:      "OPS-HRBP2",
		people.FieldOrgUnit:      "people-ops",
		people.FieldWorkerNumber: "W-9001",
	}
	b := map[people.FieldID]string{
		people.FieldWorkerNumber: "W-9001",
		people.FieldOrgUnit:      "people-ops",
		people.FieldJobCode:      "OPS-HRBP2",
		people.FieldLegalName:    "Ada Lovelace",
	}
	textA := search.BuildSearchText(a)
	textB := search.BuildSearchText(b)
	if textA != textB {
		t.Fatalf("BuildSearchText is not order-independent: %q vs %q", textA, textB)
	}
	for i := 0; i < 5; i++ {
		if got := search.BuildSearchText(a); got != textA {
			t.Fatalf("BuildSearchText(a) is not stable across calls: %q then %q", textA, got)
		}
	}
}

// TestBuildSearchTextSkipsAbsentAndBlankFields proves a field the input never
// mentions, and a field present with only whitespace, are both silently
// absent -- never a placeholder token that would itself become a spurious
// search match.
func TestBuildSearchTextSkipsAbsentAndBlankFields(t *testing.T) {
	t.Parallel()
	text := search.BuildSearchText(map[people.FieldID]string{
		people.FieldLegalName:     "Ada Lovelace",
		people.FieldPreferredName: "   ",
	})
	if text != "Ada Lovelace" {
		t.Fatalf("BuildSearchText = %q, want %q", text, "Ada Lovelace")
	}
}
