package admin

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

// OperatorRole is the reserved role a caller's authenticated trust.Principal
// must carry for any AdminService method to run. It is minted only into an
// operator's own credential (a JIT/HMAC token in development, a federation
// claim in production); no ordinary end-user or first-party service
// credential issued for the intents/registry surface ever carries it. This
// is the whole of AdminService's "distinct trust/route policy" - every other
// admission step (authentication, deadline capping, strict validation,
// trusted-field derivation) is the identical shared chain every other
// service on the process uses (internal/transport/admin, the wire adapter
// hosting this package's capabilities, reuses that chain unchanged).
const OperatorRole = "hcmnext.trust.role.operator"

// Operator route-policy errors. All are matchable with errors.Is.
var (
	ErrNoPrincipal          = errors.New("admin: no authenticated principal")
	ErrOperatorRoleRequired = errors.New("admin: this method requires the operator profile")
)

// RequireOperator evaluates AdminService's distinct trust/route policy
// against an already-authenticated principal: it fails closed on a nil
// principal and on one that does not carry [OperatorRole]. It is a pure
// function of its argument - no context, no clock, no I/O - so the transport
// adapter that reads the principal off a request context can call it
// without this package ever knowing what carried the request.
func RequireOperator(principal *trust.Principal) error {
	if principal == nil {
		return ErrNoPrincipal
	}
	if !principal.HasRole(OperatorRole) {
		return ErrOperatorRoleRequired
	}
	return nil
}

// operatorPolicyVersion and operatorPurpose name the fixed authorization
// policy AdminService's read passthroughs evaluate under. They are the
// admin surface's own coarse-grained policy: an authenticated operator
// principal is, by definition of holding OperatorRole, entitled to full
// disclosure of the read-only diagnostic surfaces this package exposes.
// This is deliberately coarser than the fine-grained, subject-specific
// tenant AuthZ evaluation internal/trust/authz performs for an ordinary
// end-user read; ADMIN-006 (support-safe diagnostic sessions, time/purpose-
// bound and allowlisted) is where a narrower, session-scoped operator grant
// belongs. SVC-011/ADMIN-001 requires only that a caller without
// OperatorRole cannot reach these methods at all.
const (
	operatorPolicyVersion = "hcmnext.admin.operator-profile/1"
	operatorPurpose       = "operator_diagnostics"
)

// ParseFields validates tokens as internal/domains/people.FieldID values,
// rejecting an unknown or duplicate token rather than silently narrowing or
// widening the projection. An empty tokens list means every known field.
func ParseFields(tokens []string) ([]people.FieldID, error) {
	if len(tokens) == 0 {
		return people.AllFields(), nil
	}
	out := make([]people.FieldID, 0, len(tokens))
	seen := make(map[people.FieldID]bool, len(tokens))
	for i, t := range tokens {
		f := people.FieldID(t)
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("admin: field at index %d is not recognized: %w", i, err)
		}
		if seen[f] {
			return nil, fmt.Errorf("admin: field at index %d is requested twice", i)
		}
		seen[f] = true
		out = append(out, f)
	}
	return out, nil
}

// ParseSections validates tokens as internal/domains/intelligence.Section
// values, with the same fail-closed discipline as [ParseFields].
func ParseSections(tokens []string) ([]intelligence.Section, error) {
	if len(tokens) == 0 {
		return intelligence.AllSections(), nil
	}
	out := make([]intelligence.Section, 0, len(tokens))
	seen := make(map[intelligence.Section]bool, len(tokens))
	for i, t := range tokens {
		s := intelligence.Section(t)
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("admin: section at index %d is not recognized: %w", i, err)
		}
		if seen[s] {
			return nil, fmt.Errorf("admin: section at index %d is requested twice", i)
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// OperatorWorkerAuthorization builds the full-disclosure
// people.AuthorizationDecision the operator profile evaluates under for a
// GetWorkerState call projecting exactly fields.
func OperatorWorkerAuthorization(fields []people.FieldID) people.AuthorizationDecision {
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = people.FieldRuling{Effect: people.EffectAllow}
	}
	return people.AuthorizationDecision{
		PolicyVersion:      operatorPolicyVersion,
		Purpose:            operatorPurpose,
		SubjectDisclosable: true,
		Fields:             rulings,
	}
}

// OperatorTransactionAuthorization builds the full-disclosure
// intelligence.AuthorizationDecision the operator profile evaluates under
// for an ExplainTransaction call projecting exactly sections.
func OperatorTransactionAuthorization(sections []intelligence.Section) intelligence.AuthorizationDecision {
	rulings := make(map[intelligence.Section]intelligence.Ruling, len(sections))
	for _, s := range sections {
		rulings[s] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	return intelligence.AuthorizationDecision{
		PolicyVersion:          operatorPolicyVersion,
		Purpose:                operatorPurpose,
		TransactionDisclosable: true,
		Sections:               rulings,
	}
}

// OperatorWorkflowInstanceAuthorization builds the full-disclosure
// inspect.Authorization the operator profile evaluates a GetWorkflowInstance
// call's traversal (definition/instance/node/governance/transaction/
// connector/observation/trace) under: every stage and every protected field
// allowed, the same coarse, full-disclosure operator profile
// [OperatorWorkerAuthorization] and [OperatorTransactionAuthorization] grant.
// subject names the caller the rendered view records as having asked.
func OperatorWorkflowInstanceAuthorization(subject string) inspect.Authorization {
	return inspect.AllowAll(operatorPolicyVersion, operatorPurpose, subject)
}

// OperatorWorkItemAuthorization builds the full-disclosure
// inspect.WorkItemAuthorization the operator profile evaluates a
// GetWorkflowInstance call's work-item-and-transitions section under.
func OperatorWorkItemAuthorization() inspect.WorkItemAuthorization {
	return inspect.WorkItemAuthorization{Disclosed: true}
}
