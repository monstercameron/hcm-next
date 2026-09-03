package messaging

import (
	"errors"
	"testing"
	"time"
)

func validIntent() MessageIntent {
	expires := time.Now().UTC().Add(time.Hour)
	return MessageIntent{IntentID: "mi-1", TenantID: "tenant-1", OrganizationScope: "org-1", Purpose: PurposeApprovalRequired,
		AudienceExpression: "ManagerOf(worker-1)", AudienceResolutionPolicy: "DELIVERY_TIME", TemplateRef: "approval/v1",
		ParametersRef: "params-1", Classification: "INTERNAL", Urgency: "NORMAL", DeliveryRequirement: RequirementDelivered,
		ReplyMode: ReplyRequired, CorrelationID: "corr-1", ExpiresAt: expires}
}

func TestMessageIntentValidate(t *testing.T) {
	if err := validIntent().Validate(); err != nil {
		t.Fatalf("valid intent rejected: %v", err)
	}
	for name, mutate := range map[string]func(*MessageIntent){
		"missing purpose":     func(m *MessageIntent) { m.Purpose = "" },
		"missing audience":    func(m *MessageIntent) { m.AudienceExpression = "" },
		"missing parameters":  func(m *MessageIntent) { m.ParametersRef = "" },
		"missing expiry":      func(m *MessageIntent) { m.ExpiresAt = time.Time{} },
		"unknown requirement": func(m *MessageIntent) { m.DeliveryRequirement = "PROVIDER_ACCEPTED" },
	} {
		t.Run(name, func(t *testing.T) {
			m := validIntent()
			mutate(&m)
			if err := m.Validate(); !errors.Is(err, ErrInvalidIntent) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAcceptRejectsExpiredAndDoesNotMutateInput(t *testing.T) {
	m := validIntent()
	m.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if _, err := Accept(m); !errors.Is(err, ErrExpiredIntent) {
		t.Fatalf("error = %v", err)
	}
}

func TestAcceptNormalizesDetachedTime(t *testing.T) {
	m := validIntent()
	available := time.Now().Add(10 * time.Minute)
	m.AvailableAt = &available
	accepted, err := Accept(m)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.AvailableAt == m.AvailableAt {
		t.Fatal("accepted intent aliases caller time pointer")
	}
	if accepted.ExpiresAt.Location() != time.UTC {
		t.Fatal("expiry was not normalized")
	}
}
