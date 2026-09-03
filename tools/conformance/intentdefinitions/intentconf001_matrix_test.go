package intentdefinitions

import "testing"

// TestTodo_INTENT_CONF_001 is the primary contract test for the reviewed
// fourteen-row intent definition manifest.
func TestTodo_INTENT_CONF_001(t *testing.T) {
	rows := Registry()
	if got := len(rows); got != 14 {
		t.Fatalf("registry rows = %d, want 14", got)
	}
	if err := Validate(rows); err != nil {
		t.Fatalf("registry validation: %v", err)
	}
	result := Check(rows)
	if !result.Valid || len(result.Errors) != 0 || len(result.Digest) != 64 {
		t.Fatalf("check result = %+v, want valid 64-character digest", result)
	}

	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.key()] {
			t.Fatalf("duplicate row %s", row.key())
		}
		seen[row.key()] = true
		if row.Family == "" || row.SideEffectProfile == "" || len(row.Entities) == 0 || len(row.Properties) == 0 || len(row.Reads) == 0 || len(row.Writes) == 0 || len(row.Effects) == 0 || len(row.Authority) == 0 || len(row.Time) == 0 || len(row.Evidence) == 0 {
			t.Fatalf("%s is missing a required manifest dimension", row.key())
		}
		if row.Lifecycle.Request == "" || row.Lifecycle.Execution == "" || row.Lifecycle.Business == "" || row.Lifecycle.Consistency == "" || row.Lifecycle.Obligation == "" {
			t.Fatalf("%s has incomplete five-dimension lifecycle", row.key())
		}
		if row.Scenario.Name == "" || row.Scenario.Given == "" || row.Scenario.When == "" || row.Scenario.Then == "" || len(row.NegativePolicy) == 0 {
			t.Fatalf("%s is missing scenario or negative-policy matrix", row.key())
		}
	}
	if !seen["hcmnext.people.change_manager/v1"] {
		t.Fatal("change_manager row missing")
	}
	for _, row := range rows {
		if row.IntentTypeID == "hcmnext.people.change_manager" && !row.ConformanceOnly {
			t.Fatal("change_manager must be conformance-only")
		}
	}
	for _, id := range ExpectedIDs() {
		if !seen[id+"/v1"] {
			t.Fatalf("expected row %s/v1 missing", id)
		}
	}
}

// TestTodo_INTENT_CONF_001_Golden pins the manifest digest and the special
// conformance-only row marker against silent registry drift.
func TestTodo_INTENT_CONF_001_Golden(t *testing.T) {
	rows := Registry()
	digest, err := ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	if digest != "4f0bbcea8874725e480682bdc21ec64418d779316392eb8843f54ddfc3b4814a" {
		t.Fatalf("manifest digest = %q, want the reviewed golden digest", digest)
	}
	for _, row := range rows {
		if row.IntentTypeID == "hcmnext.people.change_manager" && !row.ConformanceOnly {
			t.Fatal("golden change_manager row lost conformance-only marker")
		}
	}
}

// TestTodo_INTENT_CONF_001_Conformance proves that the generated manifest is
// deterministic and contains no row for an undrafted intent name.
func TestTodo_INTENT_CONF_001_Conformance(t *testing.T) {
	a := Registry()
	b := Registry()
	da, err := Digest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Digest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db || da != mustManifestDigest(t) {
		t.Fatalf("manifest digest is not deterministic: %q vs %q", da, db)
	}
	for _, row := range a {
		if row.IntentTypeID == "hcmnext.people.not_drafted" {
			t.Fatal("undrafted intent was published")
		}
	}
}

// TestTodo_INTENT_CONF_001_Mutation ensures each mandatory contract class is
// enforced rather than merely present in the happy-path fixture.
func TestTodo_INTENT_CONF_001_Mutation(t *testing.T) {
	mutate := func(name string, f func(*Descriptor)) {
		t.Run(name, func(t *testing.T) {
			rows := Registry()
			f(&rows[0])
			if err := Validate(rows); err == nil {
				t.Fatal("mutation accepted")
			}
		})
	}
	mutate("undrafted_name", func(d *Descriptor) { d.IntentTypeID = "hcmnext.people.not_drafted" })
	mutate("missing_entities", func(d *Descriptor) { d.Entities = nil })
	mutate("missing_authority", func(d *Descriptor) { d.Authority = nil })
	mutate("missing_lifecycle", func(d *Descriptor) { d.Lifecycle.Obligation = "" })
	mutate("missing_negative_policy", func(d *Descriptor) { d.NegativePolicy = nil })
	mutate("missing_scenario", func(d *Descriptor) { d.Scenario.Then = "" })
	t.Run("wrong_analytical_effect", func(t *testing.T) {
		rows := Registry()
		for i := range rows {
			if rows[i].Family == "ANALYTICAL_REQUEST" {
				rows[i].SideEffectProfile = "PURE"
				if err := Validate(rows); err == nil {
					t.Fatal("family/effect mutation accepted")
				}
				return
			}
		}
		t.Fatal("no analytical row")
	})
}

func mustManifestDigest(t *testing.T) string {
	t.Helper()
	d, err := ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
