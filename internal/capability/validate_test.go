package capability

import "testing"

func TestValidate_MissingID(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.val", "test", []string{"worker"})
	def.ID = ""
	if err := validate(def); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_MissingOwner(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.val2", "test", []string{"worker"})
	def.OwnerDomain = ""
	if err := validate(def); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_InvalidEffect(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.val3", "test", []string{"worker"})
	def.EffectClass = EffectClass("BOGUS")
	if err := validate(def); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_Valid(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.valid", "test", []string{"worker"})
	if err := validate(def); err != nil {
		t.Fatalf("valid: %v", err)
	}
}

func TestValidate_MissingRisk(t *testing.T) {
	def := bootstrapDefinition("hcmnext.test.val4", "test", []string{"worker"})
	def.RiskClass = ""
	if err := validate(def); err == nil {
		t.Fatal("expected error")
	}
}
