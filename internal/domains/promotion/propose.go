package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	CapPromotionProposeID      = "people.promote.propose"
	CapPromotionProposeVersion = 1
	RejectionCodePromo007      = "PROMO_007_REJECTED"
	EvidenceKindPropose        = "promotion.propose"
)

var (
	ErrProposeInvalid          = errors.New("promotion.propose: invalid request")
	ErrProposeForbiddenField   = errors.New("promotion.propose: forbidden field")
	ErrProposeMissingField     = errors.New("promotion.propose: missing required field")
	ErrProposePrincipalInvalid = errors.New("promotion.propose: principal not injectable from payload")
	ErrProposeTenantInvalid    = errors.New("promotion.propose: tenant not injectable from payload")
)

var allowedProposeFields = map[string]bool{
	"worker_id":                 true,
	"job_code":                  true,
	"grade":                     true,
	"org_unit":                  true,
	"position_id":               true,
	"pay_zone":                  true,
	"manager_id":                true,
	"compensation_amount":       true,
	"compensation_currency":     true,
	"compensation_pay_basis":    true,
	"effective_date":            true,
	"reason":                    true,
	"expected_subject_revision": true,
	"client_request_id":         true,
}

var forbiddenProposeFields = map[string]bool{
	"current_salary":   true,
	"current_manager":  true,
	"position_vacancy": true,
	"vacancy":          true,
	"budget":           true,
	"budget_authority": true,
	"authority":        true,
	"principal":        true,
	"principal_id":     true,
	"tenant_id":        true,
	"tenant":           true,
	"session_id":       true,
	"session":          true,
	"locale":           true,
	"legal_context":    true,
	"salary":           true,
	"pay_band":         true,
}

var reWorkerID = regexp.MustCompile(`^worker:[a-z0-9-]{1,64}$`)
var reClientReqID = regexp.MustCompile(`^[a-zA-Z0-9-_]{8,128}$`)
var reRevision = regexp.MustCompile(`^[a-zA-Z0-9-_:.]{1,128}$`)

type PromotionProposeRequest struct {
	WorkerID                string
	JobCode                 string
	Grade                   string
	OrgUnit                 string
	PositionID              string
	PayZone                 string
	ManagerID               string
	CompensationAmount      string
	CompensationCurrency    string
	CompensationPayBasis    string
	EffectiveDate           string
	Reason                  string
	ExpectedSubjectRevision string
	ClientRequestID         string
}

type PromotionProposeResponse struct {
	IntentID      string
	ProposalID    string
	EvidenceID    string
	Digest        string
	ZeroMutation  bool
	CapabilityID  string
	IntentType    string
	IntentVersion string
}

type ProposePrincipal struct {
	TenantID    string
	SubjectID   string
	SubjectKind string
	Scopes      []string
}

type ProposeEvidence struct {
	CapabilityID string
	IntentID     string
	Principal    string
	Tenant       string
	Kind         string
}

type EvidenceSink interface {
	Record(ctx context.Context, e ProposeEvidence) (string, error)
}

type GatewayInvoker interface {
	Invoke(ctx context.Context, capability string, version int, payload any, principal ProposePrincipal) (any, error)
}

type IntentCreator interface {
	CreateIntent(ctx context.Context, req PromotionProposeRequest, principal ProposePrincipal) (string, string, error)
}

type PreflightRunner interface {
	RunPreflight(ctx context.Context, req PromotionProposeRequest, intentID string) error
}

type ProposeService struct {
	Gateway   GatewayInvoker
	Intents   IntentCreator
	Preflight PreflightRunner
	Evidence  EvidenceSink
	Now       func() string
}

func ValidateRawProposeFields(raw map[string]string) error {
	for k := range raw {
		lk := strings.ToLower(k)
		if forbiddenProposeFields[lk] {
			return fmt.Errorf("%w: %s field %q", ErrProposeForbiddenField, RejectionCodePromo007, k)
		}
		if !allowedProposeFields[lk] {
			return fmt.Errorf("%w: %s unknown field %q", ErrProposeInvalid, RejectionCodePromo007, k)
		}
	}
	if strings.TrimSpace(raw["worker_id"]) == "" {
		return fmt.Errorf("%w: %s missing worker_id", ErrProposeMissingField, RejectionCodePromo007)
	}
	if strings.TrimSpace(raw["expected_subject_revision"]) == "" {
		return fmt.Errorf("%w: %s missing expected_subject_revision", ErrProposeMissingField, RejectionCodePromo007)
	}
	if strings.TrimSpace(raw["client_request_id"]) == "" {
		return fmt.Errorf("%w: %s missing client_request_id", ErrProposeMissingField, RejectionCodePromo007)
	}
	return nil
}

func ValidatePromotionProposeRequest(r PromotionProposeRequest) error {
	if !reWorkerID.MatchString(r.WorkerID) {
		return fmt.Errorf("%w: %s worker_id invalid", ErrProposeInvalid, RejectionCodePromo007)
	}
	if strings.TrimSpace(r.ExpectedSubjectRevision) == "" || !reRevision.MatchString(r.ExpectedSubjectRevision) {
		return fmt.Errorf("%w: %s expected_subject_revision invalid", ErrProposeInvalid, RejectionCodePromo007)
	}
	if !reClientReqID.MatchString(r.ClientRequestID) {
		return fmt.Errorf("%w: %s client_request_id invalid", ErrProposeInvalid, RejectionCodePromo007)
	}
	if strings.TrimSpace(r.EffectiveDate) == "" {
		return fmt.Errorf("%w: %s effective_date required", ErrProposeMissingField, RejectionCodePromo007)
	}
	if strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%w: %s reason required", ErrProposeMissingField, RejectionCodePromo007)
	}
	if strings.TrimSpace(r.JobCode) == "" && strings.TrimSpace(r.Grade) == "" && strings.TrimSpace(r.OrgUnit) == "" && strings.TrimSpace(r.PositionID) == "" {
		return fmt.Errorf("%w: %s at least one desired placement field required", ErrProposeMissingField, RejectionCodePromo007)
	}
	return nil
}

func CanonicalProposeBytes(r PromotionProposeRequest) []byte {
	parts := []string{
		r.WorkerID,
		r.JobCode,
		r.Grade,
		r.OrgUnit,
		r.PositionID,
		r.PayZone,
		r.ManagerID,
		r.CompensationAmount,
		r.CompensationCurrency,
		r.CompensationPayBasis,
		r.EffectiveDate,
		r.Reason,
		r.ExpectedSubjectRevision,
		r.ClientRequestID,
	}
	return []byte(strings.Join(parts, "|"))
}

func DigestPromotionPropose(r PromotionProposeRequest) string {
	h := sha256.Sum256(CanonicalProposeBytes(r))
	return hex.EncodeToString(h[:])
}

func ParseProposeFromGRPC(m map[string]string) (PromotionProposeRequest, error) {
	if err := ValidateRawProposeFields(m); err != nil {
		return PromotionProposeRequest{}, err
	}
	r := PromotionProposeRequest{
		WorkerID:                m["worker_id"],
		JobCode:                 m["job_code"],
		Grade:                   m["grade"],
		OrgUnit:                 m["org_unit"],
		PositionID:              m["position_id"],
		PayZone:                 m["pay_zone"],
		ManagerID:               m["manager_id"],
		CompensationAmount:      m["compensation_amount"],
		CompensationCurrency:    m["compensation_currency"],
		CompensationPayBasis:    m["compensation_pay_basis"],
		EffectiveDate:           m["effective_date"],
		Reason:                  m["reason"],
		ExpectedSubjectRevision: m["expected_subject_revision"],
		ClientRequestID:         m["client_request_id"],
	}
	if err := ValidatePromotionProposeRequest(r); err != nil {
		return PromotionProposeRequest{}, err
	}
	return r, nil
}

func ParseProposeFromHTTP(m map[string]string) (PromotionProposeRequest, error) {
	normalized := make(map[string]string, len(m))
	for k, v := range m {
		normalized[strings.ToLower(k)] = v
	}
	return ParseProposeFromGRPC(normalized)
}

func (s *ProposeService) Propose(ctx context.Context, req PromotionProposeRequest, principal ProposePrincipal) (PromotionProposeResponse, error) {
	if err := ValidatePromotionProposeRequest(req); err != nil {
		return PromotionProposeResponse{}, err
	}
	if strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.SubjectID) == "" {
		return PromotionProposeResponse{}, fmt.Errorf("%w: %s principal/tenant must be injected by trusted interceptor", ErrProposePrincipalInvalid, RejectionCodePromo007)
	}
	hasScope := false
	for _, sc := range principal.Scopes {
		if sc == "people.promote.propose" || sc == "*" {
			hasScope = true
			break
		}
	}
	if !hasScope {
		return PromotionProposeResponse{}, fmt.Errorf("%w: %s missing scope people.promote.propose", ErrProposeInvalid, RejectionCodePromo007)
	}
	if s.Gateway != nil {
		if _, err := s.Gateway.Invoke(ctx, CapPromotionProposeID, CapPromotionProposeVersion, req, principal); err != nil {
			return PromotionProposeResponse{}, err
		}
	}
	intentID := ""
	proposalID := ""
	if s.Intents != nil {
		var err error
		intentID, proposalID, err = s.Intents.CreateIntent(ctx, req, principal)
		if err != nil {
			return PromotionProposeResponse{}, err
		}
	} else {
		intentID = "intent-" + req.ClientRequestID
		proposalID = "proposal-" + req.ClientRequestID
	}
	if s.Preflight != nil {
		if err := s.Preflight.RunPreflight(ctx, req, intentID); err != nil {
			return PromotionProposeResponse{}, err
		}
	}
	evID := ""
	if s.Evidence != nil {
		id, err := s.Evidence.Record(ctx, ProposeEvidence{
			CapabilityID: CapPromotionProposeID,
			IntentID:     intentID,
			Principal:    principal.SubjectID,
			Tenant:       principal.TenantID,
			Kind:         EvidenceKindPropose,
		})
		if err != nil {
			return PromotionProposeResponse{}, err
		}
		evID = id
	} else {
		evID = "ev-" + req.ClientRequestID
	}
	digest := DigestPromotionPropose(req)
	return PromotionProposeResponse{
		IntentID:      intentID,
		ProposalID:    proposalID,
		EvidenceID:    evID,
		Digest:        digest,
		ZeroMutation:  true,
		CapabilityID:  CapPromotionProposeID,
		IntentType:    IntentType,
		IntentVersion: IntentVersion,
	}, nil
}
