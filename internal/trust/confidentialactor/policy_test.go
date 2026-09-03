package confidentialactor

import (
	"errors"
	"strings"
	"testing"
)

func validIntake(mode Mode) Intake {
	i := Intake{
		Mode:       mode,
		Abuse:      AbuseControls{RateLimitPerHour: 3, ReportChannel: "safety://reports", BlockOnRisk: true},
		Revelation: RevelationPolicy{Allowed: mode != ModeAnonymous, Trigger: "credible-threat", ApproverRole: "safety-reviewer", RequiresEvidence: true},
		Downstream: DisclosureLimit{Recipients: []string{"case-team"}, Fields: []string{"status"}, Expiry: "2026-12-31T00:00:00Z", Notify: true},
	}
	if mode == ModeKnown || mode == ModePseudonymous || mode == ModeEscrowed {
		i.IdentityEvidence = []IdentityEvidence{{Kind: "identity-proof", Ref: "evidence-1"}}
	}
	return i
}

func TestTodo_ANON_001(t *testing.T) {
	for _, mode := range []Mode{ModeKnown, ModePseudonymous, ModeAnonymous, ModeEscrowed} {
		i := validIntake(mode)
		if err := i.Validate(); err != nil {
			t.Errorf("%s: valid intake rejected: %v", mode, err)
		}
	}
}

func TestTodo_ANON_001_Fault(t *testing.T) {
	i := validIntake(ModeKnown)
	i.Mode = ""
	i.Abuse = AbuseControls{}
	i.Revelation = RevelationPolicy{}
	i.Downstream = DisclosureLimit{}
	err := i.Validate()
	if err == nil || !strings.Contains(err.Error(), ErrModeRequired.Error()) {
		t.Fatalf("missing explicit mode not rejected: %v", err)
	}
	if !strings.Contains(err.Error(), ErrAbuseRequired.Error()) || !strings.Contains(err.Error(), ErrRevelationRequired.Error()) || !strings.Contains(err.Error(), ErrDownstreamRequired.Error()) {
		t.Fatalf("incomplete declaration lost violations: %v", err)
	}
}

func TestTodo_ANON_001_Security(t *testing.T) {
	i := validIntake(ModeAnonymous)
	i.IdentityEvidence = []IdentityEvidence{{Kind: "government-id", Ref: "secret"}}
	if err := i.Validate(); err == nil || !errors.Is(err, ErrEvidenceForbidden) {
		t.Fatalf("anonymous intake accepted identity evidence: %v", err)
	}
	i = validIntake(ModeAnonymous)
	i.Revelation.Allowed = true
	if err := i.Validate(); err == nil || !strings.Contains(err.Error(), "cannot be revealed") {
		t.Fatalf("anonymous revelation accepted: %v", err)
	}
}

func TestCanonicalRecipientsDoesNotMutateInput(t *testing.T) {
	i := validIntake(ModeKnown)
	i.Downstream.Recipients = []string{"z", "a"}
	got := i.CanonicalRecipients()
	if got[0] != "a" || i.Downstream.Recipients[0] != "z" {
		t.Fatalf("recipient canonicalization mutated input: got %v input %v", got, i.Downstream.Recipients)
	}
}
