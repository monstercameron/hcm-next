package admin

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// ExplainTransaction is a governed, read-only passthrough to
// internal/domains/intelligence.ExplainTransaction: this method resolves
// the caller's tenant and the operator's full-disclosure authorization
// decision, then hands both to the identical domain function every future
// GWC/HTTP surface for the same capability will call. It never touches a
// table or a queue directly, and it performs no write.
func (s *server) ExplainTransaction(ctx context.Context, req *adminv1.ExplainTransactionRequest) (*adminv1.ExplainTransactionResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	evidence := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
	if s.deps.TransactionHistory == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.transaction_history_unconfigured",
			"the transaction history read port is not configured").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}
	if req.GetTransaction().GetId() == "" {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.transaction_id_required", "transaction.id is required").
			WithViolation("transaction.id", "must not be empty", "intelligence.ExplainTransactionRequest.Validate").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	sections, sErr := adminpolicy.ParseSections(req.GetSections())
	if sErr != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "admin.invalid_sections", "one or more requested sections is not recognized").
			WithDiagnostic(sErr).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	knownAt, tErr := resolveKnownAt(req.GetKnownAt(), s.now())
	if tErr != nil {
		return nil, tErr
	}

	domainReq := intelligence.ExplainTransactionRequest{
		Tenant:        principal.Tenant(),
		Transaction:   values.EntityRef{Tenant: principal.Tenant(), Kind: intelligence.KindTransaction, Id: req.GetTransaction().GetId()},
		AsKnownAt:     knownAt,
		Sections:      sections,
		Authorization: adminpolicy.OperatorTransactionAuthorization(sections),
	}

	explanation, err := intelligence.ExplainTransaction(ctx, s.deps.TransactionHistory, domainReq)
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.explain_transaction_failed", "the transaction could not be explained").
			WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	resp := &adminv1.ExplainTransactionResponse{
		Disclosed:       explanation.Disclosure != intelligence.DisclosureWithheld,
		Disclosure:      explanation.Disclosure.String(),
		WithheldReason:  explanation.WithheldReason,
		Presence:        explainTransactionPresence(explanation.Presence),
		Narrative:       append([]string(nil), explanation.Narrative...),
		PolicyVersion:   explanation.PolicyVersion,
		RulePackVersion: explanation.RulePackVersion,
		InputsDigest:    explanation.InputsDigest,
		Complete:        explanation.Completeness.Complete,
		Redactions:      append([]string(nil), explanation.Completeness.Redactions...),
		EvidenceRef: &commonv1.EvidenceRef{
			EvidenceId:   principal.EvidenceID(),
			EvidenceKind: "admin.explain_transaction",
			Digest:       explanation.InputsDigest,
		},
	}
	for _, sec := range explanation.Sections {
		resp.Sections = append(resp.Sections, &adminv1.SectionResult{
			Section:      sec.Section.String(),
			Access:       sec.Access.String(),
			DenialReason: sec.DenialReason,
			EntryCount:   int32(sec.Entries),
		})
	}
	return resp, nil
}

// resolveKnownAt converts an optional wire timestamp to a values.KnownAt,
// defaulting to now when ts is nil or unset.
func resolveKnownAt(ts *timestamppb.Timestamp, now time.Time) (values.KnownAt, *envelope.Error) {
	t := now
	if ts != nil {
		t = ts.AsTime()
	}
	instant := values.NewInstant(t)
	knownAt, err := values.NewKnownAt(instant)
	if err != nil {
		return values.KnownAt{}, envelope.New(envelope.CodeInvalidArgument,
			"admin.invalid_known_at", "known_at is not a valid instant").WithDiagnostic(err)
	}
	return knownAt, nil
}

// explainTransactionPresence projects intelligence.Presence to its wire
// token, collapsing the unspecified (withheld-only) value to empty so a
// withheld explanation never reports presence.
func explainTransactionPresence(p intelligence.Presence) string {
	if p == intelligence.PresenceUnspecified {
		return ""
	}
	return p.String()
}

// now returns the server's configured clock reading, defaulting to time.Now.
func (s *server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now().UTC()
	}
	return time.Now().UTC()
}
