package demoworkforce

import "testing"

// TestPromotionPathsCoverEveryStaffedJobCode proves the ladder actually
// reaches the roles a real employee holds: PROMOUX-001's defect was that the
// live journey service's job-architecture catalog never mentioned any of
// this company's own job codes at all, so every seeded worker looked
// structurally ineligible for the same reason regardless of their real
// grade. Every job code appearing in staffing must appear as either a
// source or a target edge, except the roles that are genuinely the top of
// their unit's ladder (nothing in the unit outranks them).
func TestPromotionPathsCoverEveryStaffedJobCode(t *testing.T) {
	edges := PromotionPaths()
	if len(edges) == 0 {
		t.Fatal("no promotion paths computed for the staffed job codes")
	}
	named := map[string]bool{}
	for _, edge := range edges {
		named[edge.SourceJobCode] = true
		named[edge.TargetJobCode] = true
	}
	topOfLadder := map[string]bool{
		// Every executive role: E7 is the top rank in gradeRank, so none of
		// these can have an edge -- there is nothing above them to promote
		// into within this demo company.
		"EXEC-CEO": true, "EXEC-COO": true, "EXEC-CTO": true, "EXEC-CPO": true,
		// Every unit director/manager role is the top of its own unit;
		// EXEC-* is the only rank above M4 and directors are not eligible
		// to become an executive within this ladder's published scope.
		"CLN-DIR": true, "CARE-MGR": true, "QLT-DIR": true, "ENG-DIR": true,
		"PRD-DIR": true, "DAT-DIR": true, "SEC-DIR": true, "CS-DIR": true,
		"SAL-DIR": true, "MKT-DIR": true, "PPL-DIR": true, "FIN-DIR": true,
		"LEG-GC": true, "WRK-MGR": true,
	}
	for _, group := range staffing {
		for _, role := range group.Roles {
			if named[role.Code] || topOfLadder[role.Code] {
				continue
			}
			t.Errorf("job code %s (%s) in unit %s is neither a ladder edge nor a declared top-of-ladder role", role.Code, role.Title, group.Code)
		}
	}
}

// TestPromotionPathsAreDeterministicAndAcyclic proves the ladder is pure
// (repeated calls agree) and every edge is a genuine upward move: the
// target always outranks the source, and a job code never targets itself.
func TestPromotionPathsAreDeterministicAndAcyclic(t *testing.T) {
	first, second := PromotionPaths(), PromotionPaths()
	if len(first) != len(second) {
		t.Fatalf("PromotionPaths is nondeterministic: %d edges then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("PromotionPaths edge %d differs between calls: %+v vs %+v", i, first[i], second[i])
		}
	}
	for _, edge := range first {
		if edge.SourceJobCode == edge.TargetJobCode {
			t.Fatalf("edge %+v targets its own source", edge)
		}
		sourceRank, targetRank := gradeRank[edge.SourceGrade], gradeRank[edge.TargetGrade]
		if targetRank <= sourceRank {
			t.Fatalf("edge %+v does not move to a strictly higher grade (source rank %d, target rank %d)", edge, sourceRank, targetRank)
		}
		if edge.TargetTitle == "" {
			t.Fatalf("edge %+v has no target title", edge)
		}
	}
}

// TestPayZonesMatchesLocationTable proves PayZones is the exact distinct set
// used by the location table, not a hand-maintained list that can drift from
// it.
func TestPayZonesMatchesLocationTable(t *testing.T) {
	zones := PayZones()
	if len(zones) == 0 {
		t.Fatal("no pay zones computed")
	}
	want := map[string]bool{}
	for _, location := range locations {
		want[location.Zone] = true
	}
	if len(zones) != len(want) {
		t.Fatalf("PayZones returned %d zones, want %d distinct zones", len(zones), len(want))
	}
	for i, zone := range zones {
		if !want[zone] {
			t.Fatalf("PayZones returned %q, which is not in the location table", zone)
		}
		if i > 0 && zones[i-1] >= zone {
			t.Fatalf("PayZones is not sorted: %q before %q", zones[i-1], zone)
		}
	}
}
