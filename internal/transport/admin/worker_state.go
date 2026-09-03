package admin

import (
	"context"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	adminpolicy "github.com/monstercameron/hcm-next/internal/operations/admin"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// GetWorkerState is a governed, read-only passthrough to
// internal/domains/people.ExplainWorkerState. It performs no write: a
// denied field is returned, named, and marked DENIED with no value, exactly
// as the domain function requires.
func (s *server) GetWorkerState(ctx context.Context, req *adminv1.GetWorkerStateRequest) (*adminv1.GetWorkerStateResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	evidence := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
	if s.deps.WorkerFacts == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.worker_facts_unconfigured",
			"the worker facts read port is not configured").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}
	if req.GetWorkerId() == "" {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.worker_id_required", "worker_id is required").
			WithViolation("worker_id", "must not be empty", "people.ExplainWorkerStateRequest.Validate").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	fields, fErr := adminpolicy.ParseFields(req.GetFields())
	if fErr != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "admin.invalid_fields", "one or more requested fields is not recognized").
			WithDiagnostic(fErr).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	var effectiveOn values.LocalDate
	var err error
	if req.GetEffectiveOn() != "" {
		effectiveOn, err = values.ParseLocalDate(req.GetEffectiveOn())
	} else {
		now := s.now()
		effectiveOn, err = values.NewLocalDate(now.Year(), now.Month(), now.Day())
	}
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.invalid_effective_on", "effective_on is not a valid YYYY-MM-DD date").
			WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	knownAt, tErr := resolveKnownAt(req.GetKnownAt(), s.now())
	if tErr != nil {
		return nil, tErr
	}

	domainReq := people.ExplainWorkerStateRequest{
		Tenant:        principal.Tenant(),
		Worker:        values.EntityRef{Tenant: principal.Tenant(), Kind: people.KindWorker, Id: req.GetWorkerId()},
		AsOf:          people.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt},
		Fields:        fields,
		Authorization: adminpolicy.OperatorWorkerAuthorization(fields),
	}

	explanation, explainErr := people.ExplainWorkerState(ctx, s.deps.WorkerFacts, domainReq)
	if explainErr != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.get_worker_state_failed", "the worker state could not be explained").
			WithDiagnostic(explainErr).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	resp := &adminv1.GetWorkerStateResponse{
		Disclosed:       explanation.Disclosure != people.DisclosureWithheld,
		Disclosure:      explanation.Disclosure.String(),
		WithheldReason:  explanation.WithheldReason,
		Presence:        workerStatePresence(explanation.Presence),
		Narrative:       append([]string(nil), explanation.Narrative...),
		PolicyVersion:   explanation.PolicyVersion,
		RulePackVersion: explanation.RulePackVersion,
		InputsDigest:    explanation.InputsDigest,
		ResultDigest:    explanation.ResultDigest,
		EvidenceRef: &commonv1.EvidenceRef{
			EvidenceId:   principal.EvidenceID(),
			EvidenceKind: "admin.get_worker_state",
			Digest:       explanation.ResultDigest,
		},
	}
	for _, f := range explanation.Fields {
		value, _ := f.Value.Get()
		resp.Fields = append(resp.Fields, &adminv1.FieldResult{
			Field:        string(f.Field),
			Access:       f.Access.String(),
			DenialReason: f.DenialReason,
			Presence:     f.Value.State().String(),
			Value:        value,
		})
	}
	return resp, nil
}

// workerStatePresence projects people.SubjectPresence to its wire token,
// collapsing the unspecified (withheld-only) value to empty so a withheld
// explanation never reports presence.
func workerStatePresence(p people.SubjectPresence) string {
	if p == people.SubjectPresenceUnspecified {
		return ""
	}
	return p.String()
}
