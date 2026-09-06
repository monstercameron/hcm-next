package promotionterminal_test

import (
	"errors"
	"testing"

	domaincommit "github.com/monstercameron/hcm-next/internal/domains/promotion/commit"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/hcm-next/internal/transaction"
)

func domainEffect(id string) domaincommit.ExternalEffect {
	return domaincommit.ExternalEffect{EffectID: id, DestinationRef: "destination", SchemaRef: "schema/v1", Payload: []byte(`{}`)}
}

func resolvedPlan(t *testing.T, cmdEffects []string) transaction.Resolution {
	t.Helper()
	participants := []intent.PlanParticipant{
		{ParticipantID: "people.assignment", StreamID: "people.assignment/1", StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: "position.occupancy", StreamID: "position.occupancy/1", StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: "rewards.compensation", StreamID: "rewards.compensation/1", StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: "rewards.budget_reservation", StreamID: "rewards.budget/1", StorageClass: "LOCAL_POSTGRES", Local: true},
	}
	for _, effectID := range cmdEffects {
		participants = append(participants, intent.PlanParticipant{ParticipantID: effectID, StreamID: "external/" + effectID, StorageClass: "REMOTE", Local: false})
	}
	boundary := transaction.ConsistencyBoundary{
		BoundaryID: "boundary:test", Tenant: values.TenantId("tenant-test"), CellID: "cell-test", CoordinatorID: "coordinator:test",
		Admitted: []transaction.AdmissionSelector{{StorageClass: "LOCAL_POSTGRES"}}, Isolation: transaction.IsolationSerializable,
		Protocol: transaction.CommitProtocolSingleDatabaseACID, CoordinatorEpoch: 1,
		CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
	}
	resolution, err := transaction.ResolveConsistencyBoundary(boundary, intent.TransactionPlan{
		PlanID: "promotion-plan", Tenant: values.TenantId("tenant-test"), Participants: participants,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return resolution
}

func TestBindResolutionUsesTheTransactionPlanParticipantSet(t *testing.T) {
	_, cmd := request()
	cmd.Effects = append(cmd.Effects,
		domainEffect("payroll:1"), domainEffect("iam:1"))
	bound, err := promotionterminal.BindResolution(resolvedPlan(t, []string{"payroll:1", "iam:1"}), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if bound.PlanID != "promotion-plan" || bound.PlanDigest == "" || len(bound.PlanParticipants) != 4 {
		t.Fatalf("bound command = %+v", bound)
	}
}

func TestBindResolutionRefusesDroppedLocalOrRemoteParticipants(t *testing.T) {
	_, cmd := request()
	cmd.Effects = append(cmd.Effects, domainEffect("payroll:1"))
	resolution := resolvedPlan(t, []string{"payroll:1", "iam:missing"})
	if _, err := promotionterminal.BindResolution(resolution, cmd); !errors.Is(err, promotionterminal.ErrPlanBinding) {
		t.Fatalf("remote mismatch error = %v", err)
	}

	resolution = resolvedPlan(t, []string{"payroll:1"})
	resolution.Admitted = resolution.Admitted[:3]
	if _, err := promotionterminal.BindResolution(resolution, cmd); !errors.Is(err, promotionterminal.ErrPlanBinding) {
		t.Fatalf("tampered resolution error = %v", err)
	}
}
