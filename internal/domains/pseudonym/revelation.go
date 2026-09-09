package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

const MaxRevelationTTL = 24 * time.Hour

// RevelationPurpose is deliberately closed. Adding a purpose is a policy and
// code change, rather than a caller-controlled string becoming authority.
type RevelationPurpose string

const (
	RevelationPurposeSafety             RevelationPurpose = "safety"
	RevelationPurposeLegalObligation    RevelationPurpose = "legal_obligation"
	RevelationPurposeCaseInvestigation  RevelationPurpose = "case_investigation"
	RevelationPurposeRetaliationConcern RevelationPurpose = "retaliation_concern"
	RevelationPurposeCredibleThreat     RevelationPurpose = "credible_threat"
)

var (
	ErrRevelationDenied       = errors.New("pseudonym: revelation denied")
	ErrRevelationPurpose      = errors.New("pseudonym: revelation purpose is not cleared")
	ErrRevelationScope        = errors.New("pseudonym: revelation scope is not one pseudonym generation")
	ErrRevelationCustodian    = errors.New("pseudonym: approver is not an escrow custodian")
	ErrRevelationStale        = errors.New("pseudonym: revelation authorization is stale or expired")
	ErrRevelationEvidence     = errors.New("pseudonym: revelation evidence is invalid")
	ErrRevelationEvidenceUsed = errors.New("pseudonym: revelation evidence was already used")
)

// RevelationPolicy declares the only purposes and scopes for which a
// revelation can be authorized. The alias fields allow policy composition to
// use either the older ClearedScopes spelling or the more descriptive
// AllowedScopes spelling; a missing declaration denies by default.
type RevelationPolicy struct {
	ID      string
	Version string

	ClearedScopes map[string][]RevelationPurpose
	AllowedScopes map[string][]RevelationPurpose
	ScopePurposes map[string]map[RevelationPurpose]bool

	CustodianRole       string
	CustodianRoles      []string
	CustodianPrincipals map[string]bool
	MaxTTL              time.Duration
	Clock               func() time.Time
}

// RevelationRequest is a request to reveal one exact pseudonym generation.
// It carries no subject identity; the escrow provider remains the only place
// where the mapping can be opened.
type RevelationRequest struct {
	Pseudonym          Pseudonym
	Pseudonyms         []Pseudonym
	Generations        []int
	RequestedBy        string
	Approver           string
	ApproverRole       string
	Purpose            RevelationPurpose
	LegalBasisRef      string
	Scope              string
	TTL                time.Duration
	ExpiresAt          time.Time
	Recipients         []string
	Fields             []string
	NotificationPolicy string
	CaseRef            string
	RequestedAt        time.Time
}

// RevelationEvidence is a digest-bearing disclosure receipt. PseudonymRef is
// the reference to what may be revealed; it is not an identity mapping.
type RevelationEvidence struct {
	RequestedBy        string            `json:"requested_by"`
	Approver           string            `json:"approver"`
	ApproverRole       string            `json:"approver_role"`
	Purpose            RevelationPurpose `json:"purpose"`
	LegalBasisRef      string            `json:"legal_basis_ref"`
	PseudonymRef       string            `json:"pseudonym_ref"`
	WhatRevealedRef    string            `json:"what_revealed_ref"`
	Tenant             string            `json:"tenant"`
	Scope              string            `json:"scope"`
	Generation         int               `json:"generation"`
	Recipients         []string          `json:"recipients"`
	Fields             []string          `json:"fields"`
	NotificationPolicy string            `json:"notification_policy"`
	PolicyID           string            `json:"policy_id"`
	PolicyVersion      string            `json:"policy_version"`
	AuthorizedAt       time.Time         `json:"authorized_at"`
	ExpiresAt          time.Time         `json:"expires_at"`
	Digest             string            `json:"digest"`
}

// RevelationDecision is the policy result and, only when allowed, its
// one-time receipt. Denial reasons are stable policy tokens and do not expose
// whether a pseudonym maps to an identity.
type RevelationDecision struct {
	Allowed       bool
	Reason        string
	PolicyID      string
	PolicyVersion string
	Evidence      RevelationEvidence
}

func (p RevelationPurpose) valid() bool {
	switch p {
	case RevelationPurposeSafety, RevelationPurposeLegalObligation, RevelationPurposeCaseInvestigation, RevelationPurposeRetaliationConcern, RevelationPurposeCredibleThreat:
		return true
	default:
		return false
	}
}

func (p RevelationPolicy) maxTTL() time.Duration {
	if p.MaxTTL <= 0 {
		return MaxRevelationTTL
	}
	return p.MaxTTL
}

func (p RevelationPolicy) now() time.Time {
	if p.Clock != nil {
		return p.Clock().UTC()
	}
	return time.Now().UTC()
}

// EvaluateRevelation evaluates a request against a declared policy without
// opening escrow or retaining an identity.
func EvaluateRevelation(policy RevelationPolicy, request RevelationRequest) (RevelationDecision, error) {
	decision := RevelationDecision{PolicyID: policy.ID, PolicyVersion: policy.Version}
	deny := func(reason error) (RevelationDecision, error) {
		decision.Reason = reason.Error()
		return decision, fmt.Errorf("%w: %w", ErrRevelationDenied, reason)
	}
	if strings.TrimSpace(policy.ID) == "" || strings.TrimSpace(policy.Version) == "" {
		return deny(ErrRevelationDenied)
	}

	p := request.Pseudonym
	if strings.TrimSpace(p.ID) == "" {
		p.ID = strings.TrimSpace(p.Value)
	}
	if strings.TrimSpace(p.ID) == "" || p.Generation <= 0 || strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Scope) == "" {
		return deny(ErrRevelationScope)
	}
	if len(request.Pseudonyms) > 1 || (len(request.Pseudonyms) == 1 && request.Pseudonyms[0].ID != p.ID) || len(request.Generations) > 1 || (len(request.Generations) == 1 && request.Generations[0] != p.Generation) {
		return deny(ErrRevelationScope)
	}
	if strings.TrimSpace(request.Scope) == "" || request.Scope != p.Scope {
		return deny(ErrRevelationScope)
	}
	if !request.Purpose.valid() || !scopeAllows(policy, request.Scope, request.Purpose) {
		return deny(ErrRevelationPurpose)
	}
	if strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.Approver) == "" || request.RequestedBy == request.Approver {
		return deny(ErrRevelationDenied)
	}
	if !custodianAllows(policy, request.Approver, request.ApproverRole) {
		return deny(ErrRevelationCustodian)
	}
	if strings.TrimSpace(request.LegalBasisRef) == "" || len(cleanList(request.Recipients)) == 0 || len(cleanList(request.Fields)) == 0 || strings.TrimSpace(request.NotificationPolicy) == "" {
		return deny(ErrRevelationDenied)
	}
	if containsWildcard(request.Fields) {
		return deny(ErrRevelationScope)
	}

	now := policy.now()
	at := request.RequestedAt.UTC()
	if at.IsZero() {
		at = now
	}
	if at.After(now) || now.Sub(at) > policy.maxTTL() {
		return deny(ErrRevelationStale)
	}
	ttl := request.TTL
	if ttl <= 0 || ttl > policy.maxTTL() {
		return deny(ErrRevelationStale)
	}
	expires := at.Add(ttl)
	if !request.ExpiresAt.IsZero() {
		if !request.ExpiresAt.Equal(expires) {
			return deny(ErrRevelationStale)
		}
		expires = request.ExpiresAt.UTC()
	}
	if !now.Before(expires) {
		return deny(ErrRevelationStale)
	}

	evidence := RevelationEvidence{
		RequestedBy:        request.RequestedBy,
		Approver:           request.Approver,
		ApproverRole:       request.ApproverRole,
		Purpose:            request.Purpose,
		LegalBasisRef:      request.LegalBasisRef,
		PseudonymRef:       p.ID,
		WhatRevealedRef:    "pseudonym-generation:" + p.ID,
		Tenant:             p.Tenant,
		Scope:              p.Scope,
		Generation:         p.Generation,
		Recipients:         cleanList(request.Recipients),
		Fields:             cleanList(request.Fields),
		NotificationPolicy: request.NotificationPolicy,
		PolicyID:           policy.ID,
		PolicyVersion:      policy.Version,
		AuthorizedAt:       at,
		ExpiresAt:          expires,
	}
	evidence.Digest = digestRevelationEvidence(evidence)
	decision.Allowed = true
	decision.Evidence = evidence
	return decision, nil
}

// AuthorizeRevelation is the command-shaped alias for EvaluateRevelation.
func AuthorizeRevelation(policy RevelationPolicy, request RevelationRequest) (RevelationDecision, error) {
	return EvaluateRevelation(policy, request)
}

// Validate checks the receipt digest and its bounded disclosure fields.
func (e RevelationEvidence) Validate(now time.Time) error {
	if strings.TrimSpace(e.RequestedBy) == "" || strings.TrimSpace(e.Approver) == "" || e.RequestedBy == e.Approver || !e.Purpose.valid() || strings.TrimSpace(e.LegalBasisRef) == "" || strings.TrimSpace(e.PseudonymRef) == "" || e.Generation <= 0 || strings.TrimSpace(e.Scope) == "" || len(e.Recipients) == 0 || len(e.Fields) == 0 || strings.TrimSpace(e.NotificationPolicy) == "" || e.AuthorizedAt.IsZero() || e.ExpiresAt.IsZero() || !e.AuthorizedAt.Before(e.ExpiresAt) || !now.Before(e.ExpiresAt) {
		return ErrRevelationEvidence
	}
	if e.Digest == "" || e.Digest != digestRevelationEvidence(e) {
		return ErrRevelationEvidence
	}
	return nil
}

func scopeAllows(policy RevelationPolicy, scope string, purpose RevelationPurpose) bool {
	if allowed, ok := policy.ScopePurposes[scope]; ok {
		return allowed[purpose]
	}
	allowed := policy.ClearedScopes[scope]
	if len(allowed) == 0 {
		allowed = policy.AllowedScopes[scope]
	}
	for _, candidate := range allowed {
		if candidate == purpose {
			return true
		}
	}
	return false
}

func custodianAllows(policy RevelationPolicy, approver, role string) bool {
	if len(policy.CustodianPrincipals) > 0 && !policy.CustodianPrincipals[approver] {
		return false
	}
	roles := append([]string(nil), policy.CustodianRoles...)
	if strings.TrimSpace(policy.CustodianRole) != "" {
		roles = append(roles, policy.CustodianRole)
	}
	for _, candidate := range roles {
		if strings.TrimSpace(candidate) == role && strings.TrimSpace(role) != "" {
			return true
		}
	}
	return false
}

func cleanList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsWildcard(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

func digestRevelationEvidence(e RevelationEvidence) string {
	h := sha256.New()
	write := func(label, value string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value) }
	write("requested_by", e.RequestedBy)
	write("approver", e.Approver)
	write("approver_role", e.ApproverRole)
	write("purpose", string(e.Purpose))
	write("legal_basis", e.LegalBasisRef)
	write("pseudonym_ref", e.PseudonymRef)
	write("what_ref", e.WhatRevealedRef)
	write("tenant", e.Tenant)
	write("scope", e.Scope)
	write("generation", fmt.Sprint(e.Generation))
	write("recipients", strings.Join(cleanList(e.Recipients), "\x00"))
	write("fields", strings.Join(cleanList(e.Fields), "\x00"))
	write("notification", e.NotificationPolicy)
	write("policy_id", e.PolicyID)
	write("policy_version", e.PolicyVersion)
	write("authorized_at", e.AuthorizedAt.UTC().Format(time.RFC3339Nano))
	write("expires_at", e.ExpiresAt.UTC().Format(time.RFC3339Nano))
	return hex.EncodeToString(h.Sum(nil))
}

type revelationUseState struct {
	mu   sync.Mutex
	used map[string]struct{}
}

var revelationUseStates sync.Map // map[*IdentityEscrow]*revelationUseState

func useStateFor(escrow *IdentityEscrow) *revelationUseState {
	state, _ := revelationUseStates.LoadOrStore(escrow, &revelationUseState{used: make(map[string]struct{})})
	return state.(*revelationUseState)
}

// ReleaseWithEvidence is the governed escrow path. It verifies the receipt
// against the exact pseudonym generation, then consumes its digest once before
// opening the provider-held mapping.
func (e *IdentityEscrow) ReleaseWithEvidence(ctx custody.Context, request EscrowReleaseRequest, evidence RevelationEvidence) (string, EscrowEvent, error) {
	if e == nil {
		return "", EscrowEvent{}, ErrEscrowReleaseDenied
	}
	if err := validateReleaseEvidence(ctx, request, evidence, e.clock().UTC()); err != nil {
		return "", EscrowEvent{}, err
	}
	state := useStateFor(e)
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, used := state.used[evidence.Digest]; used {
		return "", EscrowEvent{}, ErrRevelationEvidenceUsed
	}
	request.EvidenceRef = evidence.Digest
	subject, event, err := e.release(ctx, request)
	if err != nil {
		return "", event, err
	}
	state.used[evidence.Digest] = struct{}{}
	return subject, event, nil
}

// ReleaseWithEvidence exposes the same governed release path on the service
// that owns both pseudonym generation and the identity escrow.
func (s *EscrowedService) ReleaseWithEvidence(ctx custody.Context, request EscrowReleaseRequest, evidence RevelationEvidence) (string, EscrowEvent, error) {
	if s == nil || s.escrow == nil {
		return "", EscrowEvent{}, ErrEscrowReleaseDenied
	}
	return s.escrow.ReleaseWithEvidence(ctx, request, evidence)
}

func validateReleaseEvidence(ctx custody.Context, request EscrowReleaseRequest, evidence RevelationEvidence, now time.Time) error {
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: invalid context", ErrRevelationEvidence)
	}
	if err := evidence.Validate(now); err != nil {
		return err
	}
	p := request.Pseudonym
	if p.ID == "" {
		p.ID = p.Value
	}
	if evidence.PseudonymRef != p.ID || evidence.Tenant != p.Tenant || evidence.Scope != p.Scope || evidence.Generation != p.Generation || evidence.RequestedBy != request.RequestedBy || evidence.Approver != request.EscrowCustodian || evidence.ExpiresAt.Before(now) {
		return ErrRevelationEvidence
	}
	if request.EvidenceRef != "" && request.EvidenceRef != evidence.Digest {
		return ErrRevelationEvidence
	}
	if request.TTL <= 0 || now.Add(request.TTL).After(evidence.ExpiresAt) {
		return ErrRevelationEvidence
	}
	return nil
}

// ExplainRevelation describes the governed boundary without carrying a
// pseudonym, subject, requester, approver, or identity mapping.
func ExplainRevelation() string {
	return "Identity revelation requires a closed purpose, exact one-generation scope, distinct requester and custodian approver, legal-basis reference, bounded expiry, notification policy, and a one-time digested receipt before escrow release."
}
