package intentdefinitions

import "testing"

func TestRegistryHasExactDraftCoverage(t *testing.T) {
	rows := Registry()
	if err := Validate(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 14 {
		t.Fatalf("got %d rows, want 14", len(rows))
	}
	var found bool
	for _, d := range rows {
		if d.IntentTypeID == "hcmnext.people.change_manager" {
			found = d.ConformanceOnly
		}
	}
	if !found {
		t.Fatal("change_manager must be marked conformance-only")
	}
}

func TestDigestIsStable(t *testing.T) {
	a, err := ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Digest(Registry())
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("digest = %q, %q", a, b)
	}
}

func TestRejectsUndraftedAndIncompleteRows(t *testing.T) {
	rows := Registry()
	rows[0].IntentTypeID = "hcmnext.people.not_drafted"
	if err := Validate(rows); err == nil {
		t.Fatal("undrafted row accepted")
	}
	rows = Registry()
	rows[0].Evidence = nil
	if err := Validate(rows); err == nil {
		t.Fatal("incomplete row accepted")
	}
}
