package dsr

import "testing"

func TestIntake_RejectsIncompleteSpec(t *testing.T) {
	valid := fixtureIntakeSpec(t, "dsr-valid", KindAccess)

	cases := map[string]IntakeSpec{
		"no id": func() IntakeSpec { s := valid; s.ID = ""; return s }(),
		"bad tenant": func() IntakeSpec {
			s := valid
			s.Tenant = "Not A Valid Tenant!"
			return s
		}(),
		"undeclared kind": func() IntakeSpec { s := valid; s.Kind = Kind("BOGUS"); return s }(),
		"empty claims":    func() IntakeSpec { s := valid; s.Claims = SubjectClaims{}; return s }(),
		"undeclared channel": func() IntakeSpec {
			s := valid
			s.Channel = Channel("BOGUS")
			return s
		}(),
	}
	for name, spec := range cases {
		if _, err := Intake(spec, DefaultClockTable(), nil, fxWindow); err == nil {
			t.Errorf("%s: Intake succeeded, want ErrIntakeInvalid", name)
		}
	}
}

func TestIntake_RejectsNegativeDuplicateWindow(t *testing.T) {
	if _, err := Intake(fixtureIntakeSpec(t, "dsr-negwin", KindAccess), DefaultClockTable(), nil, -1); err == nil {
		t.Fatal("Intake with a negative duplicate window succeeded")
	}
}

func TestDetectDuplicate_NoKeyNeverMatches(t *testing.T) {
	existing := fixtureRequest(t)
	// A candidate whose claims resolve to an empty key (nothing identifying
	// claimed) can never be linked to anything, however close the other
	// fields are.
	candidate := existing
	candidate.Claims = SubjectClaims{}
	if _, ok := DetectDuplicate([]DataSubjectRequest{existing}, candidate, fxWindow); ok {
		t.Fatal("a candidate with no identifying claim matched a duplicate")
	}
}

func TestDetectDuplicate_DifferentKindNeverMatches(t *testing.T) {
	existing := fixtureRequest(t) // KindAccess
	candidate := existing
	candidate.Kind = KindErasure
	if _, ok := DetectDuplicate([]DataSubjectRequest{existing}, candidate, fxWindow); ok {
		t.Fatal("requests of different kinds were linked as duplicates")
	}
}
