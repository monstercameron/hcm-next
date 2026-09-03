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
