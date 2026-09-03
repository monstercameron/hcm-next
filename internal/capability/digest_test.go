package capability

import "testing"

func TestDigest_ValidDefinition(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.digest", "test", []string{"worker"})
	d, err := Digest(def)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if d == "" {
		t.Fatal("empty digest")
	}
}

func TestDigest_Deterministic(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.digest2", "test", []string{"worker"})
	a, err := Digest(def)
	if err != nil {
		t.Fatalf("Digest a: %v", err)
	}
	b, err := Digest(def)
	if err != nil {
		t.Fatalf("Digest b: %v", err)
	}
	if a != b {
		t.Fatalf("digest not deterministic %s != %s", a, b)
	}
}

func TestDigest_DifferentDefinitionsDifferentDigests(t *testing.T) {
	def1 := bootstrapDefinition("hcmnext.test.digest1", "test", []string{"worker"})
	def2 := bootstrapDefinition("hcmnext.test.digest2", "test", []string{"worker"})
	a, _ := Digest(def1)
	b, _ := Digest(def2)
	if a == b {
		t.Fatal("different defs same digest")
	}
}
