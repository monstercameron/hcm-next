package survey

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMemoryRepositorySatisfiesSurveyPort(t *testing.T) {
	var _ Repository = NewMemoryStore()
	store := NewMemoryStore()
	if err := store.SaveQuestionBank(context.Background(), "tenant-a", QuestionBankRevision{QuestionBankID: "bank", Revision: 1, CreatedAt: testInstant(), Questions: []Question{{QuestionID: "q", Kind: QuestionKindScale, Text: "score", ScaleMin: 1, ScaleMax: 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadQuestionBank(context.Background(), "tenant-b", "bank", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant memory read = %v", err)
	}
}

func TestAggregateResponsesKeepsCohortGuardInDomain(t *testing.T) {
	campaign := ResponseCampaign{Campaign: CampaignRevision{CampaignID: "campaign", Revision: 1, SurveyRef: testRef("survey"), PopulationSnapshotRef: testRef("population"), WindowStart: testInstant(), WindowEnd: values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), AnonymityThreshold: 5, ChannelRefs: []values.EntityRef{testRef("channel")}, CreatedAt: testInstant()}, FormDefinition: FormDefinitionRef{Ref: "form", Version: 1}, Disclosure: DisclosureAnonymous}
	_, err := AggregateResponses(campaign, nil)
	if !errors.Is(err, ErrAggregateCohortTooSmall) {
		t.Fatalf("aggregate error = %v", err)
	}
}

func testInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
}
func testRef(_ string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: "survey", Id: "550e8400-e29b-41d4-a716-446655440001"}
}
