package survey

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/messagetemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func launchTemplate() messagetemplate.Template {
	return messagetemplate.Template{
		Key:            "survey-launch",
		Version:        1,
		Purpose:        messagetemplate.PurposeNotice,
		Channel:        messagetemplate.ChannelEmail,
		Locale:         "en-US",
		Classification: "PUBLIC",
		LegalBasis:     "employee-feedback",
		Subject:        "Your survey is ready",
		Body:           "Please share your feedback.",
	}
}

func launchAudienceAndPlan(principal values.EntityRef) (audience.Resolution, audience.DeliveryPlan) {
	resolved := audience.Resolution{Principals: []values.EntityRef{principal}, ResultDigest: "sha256:audience"}
	plan := audience.DeliveryPlan{
		Purpose:        string(messagetemplate.PurposeNotice),
		AudienceDigest: resolved.ResultDigest,
		Plans:          []audience.RecipientDeliveryPlan{{Principal: principal}},
		ResultDigest:   "sha256:delivery",
	}
	return resolved, plan
}

func launchRequest(t *testing.T, protected bool) LaunchRequest {
	t.Helper()
	campaign := validCampaign(t)
	principal := values.EntityRef{Tenant: "test-tenant", Kind: "worker", Id: "550e8400-e29b-41d4-a716-446655440006"}
	resolved, plan := launchAudienceAndPlan(principal)
	memberIDs := []string{principal.Id}
	count := 1
	if protected {
		memberIDs = nil
		count = 1
	}
	sample, err := FreezeSample(campaign.CampaignID, validPopulationBindingRef(), validWholeRule(), memberIDs, protected, count, campaign.WindowStart)
	if err != nil {
		t.Fatal(err)
	}
	return LaunchRequest{
		Campaign:       campaign,
		Purpose:        messagetemplate.PurposeNotice,
		Sample:         sample,
		Template:       launchTemplate(),
		Audience:       resolved,
		DeliveryPlan:   plan,
		LaunchedBy:     "campaign-owner",
		Approver:       "survey-approver",
		LaunchedAt:     campaign.WindowStart,
		ReminderPolicy: ReminderPolicy{MaxCount: 2, Interval: 24 * time.Hour},
	}
}

// TestTodo_SURVEY_003 is the PRIMARY acceptance case for governed launch,
// protected membership and bounded follow-up reminders.
func TestTodo_SURVEY_003(t *testing.T) {
	request := launchRequest(t, false)
	record, err := LaunchCampaign(request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Digest == "" || record.SampleDigest == "" || record.TemplateDigest == "" || record.DeliveryPlanDigest == "" {
		t.Fatalf("launch did not produce complete digested evidence: %+v", record)
	}
	if record.Approver == record.LaunchedBy {
		t.Fatal("launch accepted a self-approval")
	}
	reminders, err := ScheduleReminders(record, request.ReminderPolicy, 2)
	if err != nil || len(reminders) != 2 {
		t.Fatalf("expected two governed reminders, got %d/%v", len(reminders), err)
	}
	if !reminders[1].ScheduledAt.After(reminders[0].ScheduledAt) || reminders[1].Digest == "" {
		t.Fatal("reminders were not deterministically scheduled")
	}
	if _, err := ScheduleReminders(record, request.ReminderPolicy, 3); !errors.Is(err, ErrReminderLimitExceeded) {
		t.Fatalf("expected reminder limit refusal, got %v", err)
	}

	protectedRequest := launchRequest(t, true)
	protectedRecord, err := Launch(protectedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !protectedRecord.MembershipProtected {
		t.Fatal("protected sample was not marked protected")
	}
	if strings.Contains(protectedRecord.Explain().SampleDigest, "emp-1") || strings.Contains(strings.TrimSpace(strings.Join([]string{protectedRecord.LaunchedBy, protectedRecord.Approver}, " ")), "emp-1") {
		t.Fatal("protected launch explanation surfaced member material")
	}
}

// TestTodo_SURVEY_003_Security verifies that stale/unfrozen samples, purpose
// mismatches, self-approval and out-of-sample delivery cannot launch.
func TestTodo_SURVEY_003_Security(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LaunchRequest)
		want   error
	}{
		{"unfrozen sample", func(r *LaunchRequest) { r.Sample = CampaignSample{} }, ErrSampleUnfrozen},
		{"superseded sample", func(r *LaunchRequest) { r.SampleState = SampleStateSuperseded }, ErrSampleSuperseded},
		{"template purpose mismatch", func(r *LaunchRequest) { r.Template.Purpose = messagetemplate.PurposeLeave }, ErrTemplatePurposeMismatch},
		{"self approval", func(r *LaunchRequest) { r.Approver = r.LaunchedBy }, ErrDistinctApproverRequired},
		{"member outside sample", func(r *LaunchRequest) {
			r.Sample, _ = FreezeSample(r.Campaign.CampaignID, validPopulationBindingRef(), validWholeRule(), []string{"emp-2"}, false, 0, r.Campaign.WindowStart)
		}, ErrDeliveryPlanOutsideSample},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := launchRequest(t, false)
			tc.mutate(&request)
			if _, err := Launch(request); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestTodo_SURVEY_003_Mutation verifies that every bound launch input changes
// the launch digest, and that the registry returns one record on replay.
func TestTodo_SURVEY_003_Mutation(t *testing.T) {
	base := launchRequest(t, false)
	first, err := Launch(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*LaunchRequest)
	}{
		{"campaign revision", func(r *LaunchRequest) { r.Campaign.Revision = 2 }},
		{"sample", func(r *LaunchRequest) {
			r.Sample, _ = FreezeSample(r.Campaign.CampaignID, validPopulationBindingRef(), validWholeRule(), []string{r.Sample.MemberIDs[0], "550e8400-e29b-41d4-a716-446655440009"}, false, 0, r.Campaign.WindowStart)
		}},
		{"template version", func(r *LaunchRequest) { r.Template.Version = 2 }},
		{"audience", func(r *LaunchRequest) {
			r.Audience.ResultDigest = "sha256:other-audience"
			r.DeliveryPlan.AudienceDigest = r.Audience.ResultDigest
		}},
		{"delivery plan", func(r *LaunchRequest) { r.DeliveryPlan.ResultDigest = "sha256:other-delivery" }},
		{"approver", func(r *LaunchRequest) { r.Approver = "another-approver" }},
		{"launched at", func(r *LaunchRequest) { r.LaunchedAt = values.NewInstant(r.LaunchedAt.Time().Add(time.Hour)) }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.mutate(&request)
			changed, err := Launch(request)
			if err != nil {
				t.Fatal(err)
			}
			if changed.Digest == first.Digest {
				t.Fatal("launch mutation did not change digest")
			}
		})
	}
	registry := NewLaunchRegistry()
	one, err := registry.Launch(base)
	if err != nil {
		t.Fatal(err)
	}
	two, err := registry.Launch(base)
	if err != nil || two.Digest != one.Digest {
		t.Fatalf("replay was not idempotent: %v, %v", one.Digest, err)
	}
	if got := registry.Records(); len(got) != 1 {
		t.Fatalf("replay created %d records, want one", len(got))
	}
}
