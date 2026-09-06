package modelgen

import "testing"

func TestPascalCase(t *testing.T) {
	cases := map[string]string{
		"manager_relationship": "ManagerRelationship",
		"effective_interval":   "EffectiveInterval",
		"status":               "Status",
		"a.b":                  "AB",
		"":                     "",
	}
	for in, want := range cases {
		if got := pascalCase(in); got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveGoTypeKnownTypes(t *testing.T) {
	for goType, wantKind := range map[string]fieldKind{
		"string":                   kindString,
		"uint32":                   kindUint32,
		"[]string":                 kindStringSlice,
		"values.Decimal":           kindDecimal,
		"values.EffectiveInterval": kindInterval,
		"values.Instant":           kindInstant,
		"values.KnownAt":           kindKnownAt,
		"values.RecordedAt":        kindRecordedAt,
		"lifecycle.StateID":        kindStateID,
		"lifecycle.Dimensions":     kindDimensions,
	} {
		info, err := resolveGoType(goType)
		if err != nil {
			t.Fatalf("resolveGoType(%q): %v", goType, err)
		}
		if info.kind != wantKind {
			t.Errorf("resolveGoType(%q).kind = %d, want %d", goType, info.kind, wantKind)
		}
		if info.goType != goType {
			t.Errorf("resolveGoType(%q).goType = %q, want %q", goType, info.goType, goType)
		}
	}
}

// TestResolveGoTypeUnknownTypeFails is the MSRC-007 RED case: an
// unrecognized modeled Go type must fail generation, never fall back to
// map[string]any or any.
func TestResolveGoTypeUnknownTypeFails(t *testing.T) {
	for _, bad := range []string{"", "any", "map[string]any", "interface{}", "MadeUpType"} {
		if _, err := resolveGoType(bad); err == nil {
			t.Errorf("resolveGoType(%q) succeeded; want an error", bad)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	got := sortedKeys(m)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("sortedKeys length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys = %v, want %v", got, want)
		}
	}
}
