package capability

import "testing"

func TestTypes_EffectClassValid(t *testing.T) {
	cases := []struct {
		e     EffectClass
		valid bool
	}{
		{EffectPure, true},
		{EffectReadOnly, true},
		{EffectInternalMutation, true},
		{EffectExternalMutation, true},
		{EffectIrreversibleExternalMutation, true},
		{EffectClass("UNKNOWN"), false},
	}
	for _, c := range cases {
		if c.e.Valid() != c.valid {
			t.Fatalf("%s valid %v", c.e, c.e.Valid())
		}
	}
}

func TestTypes_EffectClassIsWrite(t *testing.T) {
	if EffectPure.IsWrite() {
		t.Fatal("pure is write")
	}
	if !EffectInternalMutation.IsWrite() {
		t.Fatal("internal should be write")
	}
}

func TestTypes_SchemaRefValid(t *testing.T) {
	s := SchemaRef{SchemaID: "x", Version: 1, ProtobufFullName: "foo.Bar"}
	if !s.Valid() {
		t.Fatal("should be valid")
	}
	empty := SchemaRef{}
	if empty.Valid() {
		t.Fatal("empty should be invalid")
	}
}

func TestTypes_KeyString(t *testing.T) {
	k := Key{ID: "a.b", Version: 2}
	if k.String() != "a.b/v2" {
		t.Fatalf("string %s", k.String())
	}
}

func TestTypes_DataDomainClone(t *testing.T) {
	d := DataDomainFieldSet{DataDomains: []string{"worker"}, FieldPaths: []string{"a.b"}}
	c := d.clone()
	c.DataDomains[0] = "changed"
	if d.DataDomains[0] == "changed" {
		t.Fatal("clone aliased")
	}
}

func TestTypes_DefinitionKey(t *testing.T) {
	d := bootstrapDefinition("hcmnext.test.types", "test", []string{"worker"})
	if d.Key().ID != d.ID {
		t.Fatal("key mismatch")
	}
}
