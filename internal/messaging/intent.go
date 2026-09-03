package messaging

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Purpose is the business reason for a communication. It is part of the
// authorization boundary and must never be inferred from a delivery channel.
type Purpose string

const (
	PurposeApprovalRequired Purpose = "APPROVAL_REQUIRED"
	PurposeTaskAssigned     Purpose = "TASK_ASSIGNED"
	PurposeDetermination    Purpose = "DETERMINATION"
	PurposeReminder         Purpose = "REMINDER"
	PurposeWorkflowUpdate   Purpose = "WORKFLOW_UPDATE"
	PurposeEmployeeMessage  Purpose = "EMPLOYEE_MESSAGE"
	PurposeNotice           Purpose = "NOTICE"
	PurposeLegalNotice      Purpose = "LEGAL_NOTICE"
	PurposeIncident         Purpose = "INCIDENT"
)

// Short aliases keep call sites readable while the prefixed names remain
// unambiguous in packages that import several purpose vocabularies.
const (
	ApprovalRequired = PurposeApprovalRequired
	TaskAssigned     = PurposeTaskAssigned
	Determination    = PurposeDetermination
	Reminder         = PurposeReminder
	WorkflowUpdate   = PurposeWorkflowUpdate
	EmployeeMessage  = PurposeEmployeeMessage
	Notice           = PurposeNotice
	LegalNotice      = PurposeLegalNotice
	Incident         = PurposeIncident
)

func (p Purpose) valid() bool {
	switch p {
	case PurposeApprovalRequired, PurposeTaskAssigned, PurposeDetermination, PurposeReminder,
		PurposeWorkflowUpdate, PurposeEmployeeMessage, PurposeNotice,
		PurposeLegalNotice, PurposeIncident:
		return true
	default:
		return false
	}
}

// DeliveryRequirement describes the business state a communication may need
// to reach. Provider acceptance is intentionally not a requirement here.
type DeliveryRequirement string

const (
	RequirementBestEffort        DeliveryRequirement = "BEST_EFFORT"
	RequirementSubmitted         DeliveryRequirement = "SUBMITTED"
	RequirementDelivered         DeliveryRequirement = "DELIVERED"
	RequirementVerifiedRecipient DeliveryRequirement = "VERIFIED_RECIPIENT"
	RequirementRead              DeliveryRequirement = "READ"
	RequirementAcknowledged      DeliveryRequirement = "ACKNOWLEDGED"
	RequirementResponded         DeliveryRequirement = "RESPONDED"
	RequirementSigned            DeliveryRequirement = "SIGNED"
	RequirementLegalEvidence     DeliveryRequirement = "LEGAL_EVIDENCE"
)

func (r DeliveryRequirement) valid() bool {
	switch r {
	case RequirementBestEffort, RequirementSubmitted, RequirementDelivered,
		RequirementVerifiedRecipient, RequirementRead, RequirementAcknowledged,
		RequirementResponded, RequirementSigned, RequirementLegalEvidence:
		return true
	default:
		return false
	}
}

// ReplyMode states whether a recipient response is expected.
type ReplyMode string

const (
	ReplyNotAllowed ReplyMode = "NOT_ALLOWED"
	ReplyOptional   ReplyMode = "OPTIONAL"
	ReplyRequired   ReplyMode = "REQUIRED"
)

func (r ReplyMode) valid() bool {
	return r == ReplyNotAllowed || r == ReplyOptional || r == ReplyRequired
}

// MessageIntent is an immutable-after-acceptance, provider-independent
// request to communicate business meaning. References identify content held
// by separately authorized artifact/template services; they are not rendered
// payloads, addresses, or provider credentials.
type MessageIntent struct {
	IntentID          string
	TenantID          string
	OrganizationScope string

	Purpose                  Purpose
	AudienceExpression       string
	AudienceResolutionPolicy string

	ContentRef    string
	TemplateRef   string
	ParametersRef string

	Classification      string
	Urgency             string
	DeliveryRequirement DeliveryRequirement
	ReplyMode           ReplyMode

	WorkflowInstanceID    string
	HumanTaskID           string
	CaseID                string
	BusinessTransactionID string
	CorrelationID         string
	AvailableAt           *time.Time
	ExpiresAt             time.Time
}

var (
	ErrInvalidIntent = errors.New("messaging: invalid message intent")
	ErrExpiredIntent = errors.New("messaging: message intent is expired")
)

// Validate checks the minimum semantic contract. It does not contact a
// provider and cannot validate audience membership or referenced artifacts.
func (m MessageIntent) Validate() error {
	checks := []struct{ name, value string }{
		{"intent_id", m.IntentID}, {"tenant_id", m.TenantID},
		{"organization_scope", m.OrganizationScope}, {"audience_expression", m.AudienceExpression},
		{"audience_resolution_policy", m.AudienceResolutionPolicy}, {"parameters_ref", m.ParametersRef},
		{"classification", m.Classification}, {"urgency", m.Urgency}, {"correlation_id", m.CorrelationID},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidIntent, c.name)
		}
	}
	if strings.TrimSpace(m.ContentRef) == "" && strings.TrimSpace(m.TemplateRef) == "" {
		return fmt.Errorf("%w: content_ref or template_ref is required", ErrInvalidIntent)
	}
	if !m.Purpose.valid() {
		return fmt.Errorf("%w: unsupported purpose %q", ErrInvalidIntent, m.Purpose)
	}
	if !m.DeliveryRequirement.valid() {
		return fmt.Errorf("%w: unsupported delivery requirement %q", ErrInvalidIntent, m.DeliveryRequirement)
	}
	if !m.ReplyMode.valid() {
		return fmt.Errorf("%w: unsupported reply mode %q", ErrInvalidIntent, m.ReplyMode)
	}
	if m.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expires_at is required", ErrInvalidIntent)
	}
	if m.ExpiresAt.Before(m.CreatedAt()) {
		return fmt.Errorf("%w: expires_at precedes available_at", ErrInvalidIntent)
	}
	return nil
}

// CreatedAt returns the earliest known intent time. MessageIntent intentionally
// has no mutable creation timestamp; availability is the scheduling boundary.
func (m MessageIntent) CreatedAt() time.Time {
	if m.AvailableAt != nil {
		return *m.AvailableAt
	}
	return time.Time{}
}

// Accept validates and returns a detached value suitable for durable storage.
// The returned value contains only semantic fields, so a provider cannot be
// smuggled into an accepted intent through this API.
func Accept(m MessageIntent) (MessageIntent, error) {
	if err := m.Validate(); err != nil {
		return MessageIntent{}, err
	}
	if !m.ExpiresAt.After(time.Now().UTC()) {
		return MessageIntent{}, ErrExpiredIntent
	}
	if m.AvailableAt != nil {
		v := m.AvailableAt.UTC()
		m.AvailableAt = &v
	}
	m.ExpiresAt = m.ExpiresAt.UTC()
	return m, nil
}
