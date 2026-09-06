package survey

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type surveyResponsePseudonymFake struct {
	called  []string
	scope   string
	purpose string
}

func (f *surveyResponsePseudonymFake) Pseudonymize(subject, scope, purpose string) (string, error) {
	f.called = append(f.called, subject)
	f.scope = scope
	f.purpose = purpose
	sum := sha256.Sum256([]byte(subject + "\x00" + scope + "\x00" + purpose))
	return "psn:" + hex.EncodeToString(sum[:]), nil
}

func surveyResponseForm() FormDefinitionRef {
	return FormDefinitionRef{Ref: "survey-feedback", Version: 3, Digest: "sha256:form-v3"}
}

func surveyResponseCampaign(t *testing.T, mode DisclosureMode, allowRevision bool) ResponseCampaign {
	t.Helper()
	return ResponseCampaign{
		Campaign:          validCampaign(t),
		FormDefinition:    surveyResponseForm(),
		Disclosure:        mode,
		AllowRevision:     allowRevision,
		MinimumCohortSize: 5,
	}
}

func surveyResponseInput(subject string, value string) ResponseInput {
	return ResponseInput{
		SubjectRef:     subject,
		FormDefinition: surveyResponseForm(),
		Answers:        []ResponseAnswer{{QuestionID: "q1", Value: value}},
		SubmittedAt:    values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)),
	}
}

func surveyResponseValidator() FormValidator {
	return FormValidatorFunc(func(form FormDefinitionRef, answers []ResponseAnswer) (string, error) {
		if form != surveyResponseForm() || len(answers) != 1 || answers[0].QuestionID != "q1" || answers[0].Value == "" {
			return "", errors.New("invalid answer")
		}
		return "sha256:validated", nil
	})
}

// TestTodo_SURVEY_004 is the primary acceptance case for exact form binding,
// scoped pseudonymous/anonymous recording, duplicate protection, revisions,
// closure, and minimum-cohort aggregate suppression.
func TestTodo_SURVEY_004(t *testing.T) {
	forms := surveyResponseValidator()
	pseudonyms := &surveyResponsePseudonymFake{}
	campaign := surveyResponseCampaign(t, DisclosurePseudonymous, false)
	store := NewResponseStore()

	first, err := store.RecordResponse(campaign, surveyResponseInput("subject-1", "yes"), pseudonyms, forms)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResponseKey == "" || first.ResponseRevision != 1 || first.Digest == "" {
		t.Fatalf("incomplete response record: %+v", first)
	}
	if pseudonyms.scope != campaignScope(campaign) || pseudonyms.purpose != responsePurpose {
		t.Fatalf("pseudonym port scope/purpose = %q/%q", pseudonyms.scope, pseudonyms.purpose)
	}
	if got := store.Records(campaign); len(got) != 1 {
		t.Fatalf("record count = %d, want one", len(got))
	}

	if _, err := store.RecordResponse(campaign, surveyResponseInput("subject-1", "no"), pseudonyms, forms); !errors.Is(err, ErrDuplicateResponse) {
		t.Fatalf("duplicate response error = %v, want ErrDuplicateResponse", err)
	}
	if _, err := store.RecordResponse(campaign, surveyResponseInput("subject-2", "no"), pseudonyms, forms); err != nil {
		t.Fatalf("second pseudonym response: %v", err)
	}
	if _, err := store.Aggregate(campaign); !errors.Is(err, ErrAggregateCohortTooSmall) {
		t.Fatalf("small aggregate error = %v, want ErrAggregateCohortTooSmall", err)
	}

	revisionCampaign := surveyResponseCampaign(t, DisclosurePseudonymous, true)
	revisionStore := NewResponseStore()
	original, err := revisionStore.RecordResponse(revisionCampaign, surveyResponseInput("subject-1", "yes"), pseudonyms, forms)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := revisionStore.RecordResponse(revisionCampaign, surveyResponseInput("subject-1", "no"), pseudonyms, forms)
	if err != nil {
		t.Fatal(err)
	}
	if revised.ResponseRevision != 2 || revised.Digest == original.Digest {
		t.Fatalf("revision = %+v, original = %+v", revised, original)
	}
	history := revisionStore.Records(revisionCampaign)
	if len(history) != 2 || history[0].Digest != original.Digest {
		t.Fatalf("revision overwrote history: %+v", history)
	}

	for i := 3; i <= 5; i++ {
		if _, err := store.RecordResponse(campaign, surveyResponseInput("subject-"+string(rune('0'+i)), "yes"), pseudonyms, forms); err != nil {
			t.Fatal(err)
		}
	}
	aggregate, err := store.Aggregate(campaign)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Count != 5 || aggregate.Breakdown["q1"]["yes"] != 4 || aggregate.Breakdown["q1"]["no"] != 1 || aggregate.Digest == "" {
		t.Fatalf("aggregate = %+v", aggregate)
	}

	if err := store.CloseCampaign(campaign); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordResponse(campaign, surveyResponseInput("subject-6", "yes"), pseudonyms, forms); !errors.Is(err, ErrCampaignClosed) {
		t.Fatalf("closed campaign error = %v, want ErrCampaignClosed", err)
	}

	anonymous := surveyResponseCampaign(t, DisclosureAnonymous, false)
	anonymousStore := NewResponseStore()
	anonymousInput := surveyResponseInput("", "yes")
	anonymousRecord, err := anonymousStore.RecordResponse(anonymous, anonymousInput, nil, forms)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(anonymousRecord.ResponseKey, "response:v1:") {
		t.Fatalf("anonymous key = %q, want random response token", anonymousRecord.ResponseKey)
	}
	if _, err := anonymousStore.RecordResponse(anonymous, surveyResponseInput("subject-should-not-link", "yes"), nil, forms); !errors.Is(err, ErrSubjectLinkageRefused) {
		t.Fatalf("anonymous subject linkage error = %v, want ErrSubjectLinkageRefused", err)
	}

	if got := ExplainResponses(); got == "" || strings.Contains(got, "subject-") || strings.Contains(got, "response:v1:") {
		t.Fatalf("Explain leaked response material: %q", got)
	}
}

// TestTodo_SURVEY_004_Mutation verifies exact form-version binding, validator
// refusal, defensive copies, token uniqueness, and the no-subject record and
// aggregate shape.
func TestTodo_SURVEY_004_Mutation(t *testing.T) {
	campaign := surveyResponseCampaign(t, DisclosurePseudonymous, false)
	store := NewResponseStore()
	pseudonyms := &surveyResponsePseudonymFake{}
	forms := surveyResponseValidator()

	badForm := surveyResponseInput("subject-1", "yes")
	badForm.FormDefinition.Version++
	if _, err := store.RecordResponse(campaign, badForm, pseudonyms, forms); !errors.Is(err, ErrFormVersionMismatch) {
		t.Fatalf("wrong form version error = %v, want ErrFormVersionMismatch", err)
	}
	badAnswers := surveyResponseInput("subject-1", "yes")
	badAnswers.Answers = []ResponseAnswer{{QuestionID: "q2", Value: "injected"}}
	if _, err := store.RecordResponse(campaign, badAnswers, pseudonyms, forms); !errors.Is(err, ErrFormValidationFailed) {
		t.Fatalf("invalid answer error = %v, want ErrFormValidationFailed", err)
	}
	if _, err := store.RecordResponse(campaign, surveyResponseInput("", "yes"), nil, forms); !errors.Is(err, ErrPseudonymRequired) {
		t.Fatalf("missing pseudonym error = %v, want ErrPseudonymRequired", err)
	}

	record, err := store.RecordResponse(campaign, surveyResponseInput("subject-1", "yes"), pseudonyms, forms)
	if err != nil {
		t.Fatal(err)
	}
	record.Answers[0].Value = "tampered"
	if store.Records(campaign)[0].Answers[0].Value != "yes" {
		t.Fatal("Records exposed mutable answer storage")
	}

	anonymous := surveyResponseCampaign(t, DisclosureAnonymous, false)
	first, err := store.RecordResponse(anonymous, surveyResponseInput("", "yes"), nil, forms)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RecordResponse(anonymous, surveyResponseInput("", "yes"), nil, forms)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResponseKey == second.ResponseKey {
		t.Fatal("anonymous response tokens repeated")
	}

	recordType := reflect.TypeOf(ResponseRecord{})
	if _, ok := recordType.FieldByName("SubjectRef"); ok {
		t.Fatal("ResponseRecord carries a subject reference field")
	}
	aggregateType := reflect.TypeOf(Aggregate{})
	if _, ok := aggregateType.FieldByName("ResponseKey"); ok {
		t.Fatal("Aggregate carries a response key")
	}
	if strings.Contains(record.Explain(), "subject-1") || strings.Contains(aggregateExplanationWithoutData(), "subject-1") {
		t.Fatal("response explanation carried a subject reference")
	}
}

func aggregateExplanationWithoutData() string { return (Aggregate{}).Explain() }
