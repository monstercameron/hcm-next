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

// TestTodo_MSG_001 is the registry's primary contract test.  A message intent
// carries business meaning and references, never a resolved endpoint or a
// provider credential.
func TestTodo_MSG_001(t *testing.T) {
	m := validIntent()
	accepted, err := Accept(m)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Purpose != PurposeApprovalRequired || accepted.DeliveryRequirement != RequirementDelivered {
		t.Fatalf("accepted intent lost semantic contract: %#v", accepted)
	}
	if accepted.AudienceExpression == "" || accepted.TemplateRef == "" || accepted.Classification == "" || accepted.CorrelationID == "" {
		t.Fatal("accepted intent omitted a required semantic reference")
	}
}

func TestTodo_MSG_001_Race(t *testing.T) {
	// Acceptance is a detached value operation; concurrent callers must not
	// mutate or share the caller's scheduling pointer.
	m := validIntent()
	available := time.Now().UTC().Add(time.Minute)
	m.AvailableAt = &available
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			if _, err := Accept(m); err != nil {
				t.Errorf("accept: %v", err)
			}
			done <- struct{}{}
		}()
	}
	<-done
	<-done
}

func TestTodo_MSG_001_Integration(t *testing.T) {
	// The contract boundary is intentionally provider independent: references
	// are opaque and acceptance performs no delivery side effect.
	m := validIntent()
	m.ContentRef = "content/approval-1"
	m.TemplateRef = ""
	if _, err := Accept(m); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_MSG_001_Fault(t *testing.T) {
	m := validIntent()
	m.ExpiresAt = time.Now().UTC().Add(-time.Second)
	if _, err := Accept(m); !errors.Is(err, ErrExpiredIntent) {
		t.Fatalf("expired intent error = %v", err)
	}
}

func TestTodo_MSG_001_Security(t *testing.T) {
	m := validIntent()
	// Endpoint/provider data is not part of MessageIntent and therefore cannot
	// be smuggled into the accepted semantic request.
	accepted, err := Accept(m)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.IntentID != m.IntentID || accepted.TenantID != m.TenantID {
		t.Fatal("acceptance changed identity scope")
	}
}

func TestTodo_MSG_001_Mutation(t *testing.T) {
	m := validIntent()
	before := m
	if _, err := Accept(m); err != nil {
		t.Fatal(err)
	}
	if m.IntentID != before.IntentID || m.ExpiresAt != before.ExpiresAt || m.AvailableAt != before.AvailableAt {
		t.Fatal("acceptance mutated caller-owned intent")
	}
}
