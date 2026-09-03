package customobject

import "testing"

func validSource() CustomObjectType {
	return CustomObjectType{
		Name: "Vehicle", Namespace: "tenant.fleet", Owner: "OPERATIONS", Version: 1,
		Fields: []Field{{Name: "registration", Type: "string", Required: true}, {Name: "active", Type: "bool"}},
	}
}

func TestCompileProducesStableSortedSchemaFluxDigest(t *testing.T) {
	a := validSource()
	b := validSource()
	b.Fields[0], b.Fields[1] = b.Fields[1], b.Fields[0]
	ca, err := Compile(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Compile(b)
	if err != nil {
		t.Fatal(err)
	}
	if ca.Digest == "" || ca.Digest != cb.Digest {
		t.Fatalf("digest not stable: %q != %q", ca.Digest, cb.Digest)
	}
	if ca.Fields[0].Name != "active" {
		t.Fatalf("fields not sorted: %+v", ca.Fields)
	}
	if ca.Limits.MaxFields != DefaultMaxFields {
		t.Fatalf("default limit = %d", ca.Limits.MaxFields)
	}
}

// TestTodo_CUSTOM_001 is the primary test named by the planning registry.
// Keep the assertions at the contract boundary: callers receive a typed,
// bounded descriptor and a SchemaFlux content digest, never executable input.
func TestTodo_CUSTOM_001(t *testing.T) {

	schema, err := Compile(CustomObjectType{
		Name: "Vehicle", Namespace: "tenant.fleet", Owner: "OPERATIONS", Version: 7,
		Fields: []Field{
			{Name: "registration", Type: "string", Required: true},
			{Name: "active", Type: "bool"},
		},
		Limits: Limits{MaxFields: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if schema.Name != "Vehicle" || schema.Namespace != "tenant.fleet" || schema.Owner != "OPERATIONS" || schema.Version != 7 {
		t.Fatalf("compiled metadata = %+v", schema)
	}
	if schema.Limits.MaxFields != 8 || len(schema.Fields) != 2 {
		t.Fatalf("compiled bounds/fields = %+v", schema)
	}
	if schema.Digest == "" || len(schema.Digest) != len("sha256:")+64 || schema.Digest[:len("sha256:")] != "sha256:" {
		t.Fatalf("invalid SchemaFlux digest %q", schema.Digest)
	}
}

// TestTodo_CUSTOM_001_Golden is the golden matrix case: equivalent source
// definitions compile to the same canonical descriptor and digest.
func TestTodo_CUSTOM_001_Golden(t *testing.T) {
	first := validSource()
	second := validSource()
	second.Fields[0], second.Fields[1] = second.Fields[1], second.Fields[0]
	a, err := CompileSchema(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileSchema(second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("canonical digest changed with field order: %q != %q", a.Digest, b.Digest)
	}
	if a.Fields[0].Name != "active" || a.Fields[1].Name != "registration" {
		t.Fatalf("golden field order = %+v", a.Fields)
	}
}

func TestCompileRejectsUnsafeDefinitions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CustomObjectType)
	}{
		{"arbitrary name", func(s *CustomObjectType) { s.Name = "vehicle;drop" }},
		{"missing owner", func(s *CustomObjectType) { s.Owner = "" }},
		{"reserved field", func(s *CustomObjectType) { s.Fields[0].Name = "tenant_id" }},
		{"unsupported primitive", func(s *CustomObjectType) { s.Fields[0].Type = "map[string]any" }},
		{"unbounded fields", func(s *CustomObjectType) { s.Limits.MaxFields = DefaultMaxFields + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSource()
			tc.mutate(&s)
			if _, err := Compile(s); err == nil {
				t.Fatal("Compile accepted invalid definition")
			}
		})
	}
}
