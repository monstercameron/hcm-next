package dsr

import "testing"

func TestKind_ValidateClosedVocabulary(t *testing.T) {
	for _, k := range AllKinds() {
		if err := k.Validate(); err != nil {
			t.Errorf("declared kind %s failed to validate: %v", k, err)
		}
	}
	if err := Kind("SOMETHING_ELSE").Validate(); err == nil {
		t.Error("an undeclared kind validated")
	}
	if err := Kind("").Validate(); err == nil {
		t.Error("an empty kind validated")
	}
}

func TestAllKinds_ReturnsAnIndependentCopy(t *testing.T) {
	got := AllKinds()
	got[0] = Kind("MUTATED")
	if allKinds[0] == Kind("MUTATED") {
		t.Fatal("mutating AllKinds()'s result mutated the package's own vocabulary")
	}
}

func TestChannel_ValidateClosedVocabulary(t *testing.T) {
	for _, c := range allChannels {
		if err := c.Validate(); err != nil {
			t.Errorf("declared channel %s failed to validate: %v", c, err)
		}
	}
	if err := Channel("CARRIER_PIGEON").Validate(); err == nil {
		t.Error("an undeclared channel validated")
	}
}

func TestVerificationState_ValidateClosedVocabulary(t *testing.T) {
	for _, s := range allVerificationStates {
		if err := s.Validate(); err != nil {
			t.Errorf("declared verification state %s failed to validate: %v", s, err)
		}
	}
	if err := VerificationState("PENDING").Validate(); err == nil {
		t.Error("an undeclared verification state validated")
	}
}

func TestKind_StringMatchesWireSpelling(t *testing.T) {
	if KindErasure.String() != "ERASURE" {
		t.Errorf("KindErasure.String() = %q, want %q", KindErasure.String(), "ERASURE")
	}
}
