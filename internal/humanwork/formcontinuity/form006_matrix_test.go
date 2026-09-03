package formcontinuity

import (
	"errors"
	"testing"
	"time"
)

// TestFORM006RouteMatrix exercises the supported alternate routes against the
// same canonical snapshot.  The route labels intentionally describe the
// operator-facing routes: manual is in-person, message is postal/asynchronous,
// and interpreter is an assisted phone route.
func TestFORM006RouteMatrix(t *testing.T) {
	tests := []struct {
		name       string
		channel    Channel
		accom      string
		transcript string
		readBack   bool
	}{
		{name: "manual", channel: ChannelInPerson, transcript: "transcription:manual", readBack: true},
		{name: "phone", channel: ChannelPhone, transcript: "transcription:phone", readBack: true},
		{name: "message", channel: ChannelPostal, transcript: "transcription:message", readBack: true},
		{name: "interpreter", channel: ChannelPhone, transcript: "transcription:interpreter", readBack: true},
		{name: "accessible", channel: ChannelAccessible, accom: "accommodation:large-print"},
		{name: "rtl", channel: ChannelRTL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			r.AlternateChannel = tc.channel
			r.AccommodationRef = tc.accom
			r.TranscriptionRef = tc.transcript
			r.ReadBackConfirmed = tc.readBack
			got, err := Establish(r)
			if err != nil {
				t.Fatalf("route %s rejected: %v", tc.name, err)
			}
			if got.Outcome != OutcomeSafe || got.Channel != tc.channel {
				t.Fatalf("route %s changed outcome/channel: %#v", tc.name, got)
			}
		})
	}
}

func TestFORM006ExactAttributionValidationAndPrivacy(t *testing.T) {
	base := validRequest()
	got, err := Establish(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.RespondentID != base.Identity.PrincipalID || got.AssistantID != base.Attribution.AssistantID {
		t.Fatalf("attribution drifted: got %#v", got)
	}
	if got.AuthorityRef != base.Authority.AuthorityRef || got.Purpose != base.Privacy.Purpose || got.Compartment != base.Privacy.Compartment {
		t.Fatalf("authority/privacy drifted: got %#v", got)
	}
	if got.FormRevision != base.Validation.FormRevision || got.SchemaDigest != base.Validation.SchemaDigest {
		t.Fatalf("validation identity drifted: got %#v", got)
	}

	for name, mutate := range map[string]func(*ContinuityRequest){
		"respondent": func(r *ContinuityRequest) { r.Attribution.RespondentID = "principal:other" },
		"assistant":  func(r *ContinuityRequest) { r.Attribution.AssistantID = "principal:other" },
		"schema":     func(r *ContinuityRequest) { r.Validation.SchemaDigest = "" },
		"proof":      func(r *ContinuityRequest) { r.Validation.ProofDigest = "" },
		"privacy":    func(r *ContinuityRequest) { r.Privacy.Compartment = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			mutate(&r)
			if _, err := Establish(r); err == nil {
				t.Fatal("expected continuity refusal")
			}
		})
	}
}

func TestFORM006DeadlineAndNoWeakerAuthority(t *testing.T) {
	r := validRequest()
	r.Authority.DecisionRight = "submit.leave"
	r.Authority.Scope = "worker:worker-1"
	got, err := Establish(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorityRef != r.Authority.AuthorityRef || got.RespondentID != r.Identity.PrincipalID {
		t.Fatalf("alternate route weakened authority/identity: %#v", got)
	}

	for name, now := range map[string]time.Time{
		"at-deadline":    r.OriginalDeadline,
		"after-deadline": r.OriginalDeadline.Add(time.Nanosecond),
	} {
		t.Run(name, func(t *testing.T) {
			candidate := r
			candidate.Now = now
			_, err := Establish(candidate)
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("deadline bypass returned %v", err)
			}
		})
	}

	assistant := r
	assistant.Attribution.RespondentID = assistant.Identity.AssistantID
	if _, err := Establish(assistant); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("assistant obtained respondent authority: %v", err)
	}
}
