package confidentialactor

import (
	"errors"
	"reflect"
	"testing"
)

func TestModeValidAndIntakeValidation_CoversDisclosureBoundaries(t *testing.T) {
	for _, mode := range []Mode{ModeKnown, ModePseudonymous, ModeAnonymous, ModeEscrowed} {
		if !mode.Valid() {
			t.Errorf("Mode %q reported invalid", mode)
		}
	}
	if Mode("UNKNOWN").Valid() || Mode("").Valid() {
		t.Fatal("unknown or empty mode reported valid")
	}
	cases := []struct {
		name   string
		intake Intake
		want   error
	}{
		{"known evidence required", validIntake(ModeKnown), ErrEvidenceRequired},
		{"escrow evidence required", validIntake(ModeEscrowed), ErrEvidenceRequired},
		{"invalid mode", validIntake(ModeKnown), ErrModeInvalid},
		{"anonymous evidence forbidden", validIntake(ModeAnonymous), ErrEvidenceForbidden},
		{"abuse required", validIntake(ModeKnown), ErrAbuseRequired},
		{"revelation required", validIntake(ModeKnown), ErrRevelationRequired},
		{"revelation evidence required", validIntake(ModeKnown), ErrRevelationEvidenceRequired},
		{"downstream required", validIntake(ModeKnown), ErrDownstreamRequired},
		{"expiry invalid", validIntake(ModeKnown), ErrExpiryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := tc.intake
			switch tc.name {
			case "known evidence required", "escrow evidence required":
				i.IdentityEvidence = nil
			case "invalid mode":
				i.Mode = "OTHER"
			case "anonymous evidence forbidden":
				i = validIntake(ModeAnonymous)
				i.IdentityEvidence = []IdentityEvidence{{Kind: "id", Ref: "ref"}}
			case "abuse required":
				i.Abuse = AbuseControls{}
			case "revelation required":
				i.Revelation.Trigger = ""
			case "revelation evidence required":
				i.Revelation.RequiresEvidence = false
			case "downstream required":
				i.Downstream = DisclosureLimit{}
			case "expiry invalid":
				i.Downstream.Expiry = "not-a-time"
			}
			if err := i.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
	invalidEvidence := validIntake(ModeKnown)
	invalidEvidence.IdentityEvidence = []IdentityEvidence{{Kind: " ", Ref: "ref"}}
	if err := invalidEvidence.Validate(); err == nil {
		t.Fatal("blank evidence was accepted")
	}
	anonymous := validIntake(ModeAnonymous)
	anonymous.Revelation.Allowed = true
	if err := anonymous.Validate(); err == nil || !stringsContains(err.Error(), "cannot be revealed") {
		t.Fatalf("anonymous revelation = %v", err)
	}
}

func TestIntakeStringAndCanonicalRecipients_AreDeterministicCopies(t *testing.T) {
	i := validIntake(ModeKnown)
	i.Downstream.Recipients = []string{"z-team", "a-team"}
	if got := i.String(); got != "KNOWN confidential actor intake" {
		t.Fatalf("String = %q", got)
	}
	got := i.CanonicalRecipients()
	if !reflect.DeepEqual(got, []string{"a-team", "z-team"}) || i.Downstream.Recipients[0] != "z-team" {
		t.Fatalf("canonical recipients = %v input = %v", got, i.Downstream.Recipients)
	}
	got[0] = "tampered"
	if i.Downstream.Recipients[0] == "tampered" {
		t.Fatal("CanonicalRecipients exposed input storage")
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
