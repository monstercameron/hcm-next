package approval_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent"
	intentapproval "github.com/monstercameron/hcm-next/internal/intent/approval"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	stepapproval "github.com/monstercameron/hcm-next/internal/workflow/steps/approval"
)

var (
	tenantID   = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	instanceID = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	itemAID    = uuid.MustParse("30000000-0000-0000-0000-000000000001")
	itemBID    = uuid.MustParse("30000000-0000-0000-0000-000000000002")
	decideBy   = values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	expiry     = values.NewInstant(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	now        = values.NewInstant(time.Date(2026, 9, 3, 11, 0, 0, 0, time.UTC))
)

func proposal() intent.ProposalRevision {
	intentID := "intent:promotion:1"
	revisionID := "proposal:promotion:1"
	return intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
}

func requirement(id string, quorum uint32, distinct bool) humanwork.ApprovalRequirement {
	return humanwork.ApprovalRequirement{
		RequirementID: id, Revision: 1,
		Quorum:           humanwork.Quorum{MinApprovals: quorum, RequireDistinctPrincipals: distinct},
		Deadline:         humanwork.Deadline{DecideBy: decideBy, Expiry: expiry},
		ExpressionDigest: "sha256:" + strings.Repeat("c", 64),
		Source: humanwork.RequirementSource{
			TableID: "table", TableVersion: "1", TableDigest: "sha256:" + strings.Repeat("d", 64),
			MatchedRowID: "row", GovernancePolicyRef: "policy:approval:1",
		},
	}
}

func pendingItem(id uuid.UUID, req humanwork.ApprovalRequirement, principal string) workitem.WorkItem {
	return workitem.WorkItem{
		TenantID: tenantID, WorkItemID: id, ItemVersion: 3,
		Kind: workitem.KindApproval, WorkType: "promotion.approval", Status: workitem.StatusAssigned,
		CorrelationID: "correlation:1", WorkflowInstanceID: instanceID, NodeID: "approve",
		ApprovalRequirementRef: req.RequirementID, ProposalRef: proposal().MaterialDigest.Digest,
		Assignment: workitem.Assignment{Resolution: humanwork.Resolution{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision,
			Candidates:        []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "role:approver"}},
			RequirementDigest: req.Digest(), ExpressionDigest: req.ExpressionDigest,
			QuorumRequired: req.Quorum.MinApprovals,
		}},
	}
}

func decision(req humanwork.ApprovalRequirement, principal, id string, outcome intentapproval.Outcome) intentapproval.ApprovalDecision {
	p := proposal()
	return intentapproval.ApprovalDecision{
		DecisionID: id,
		Binding: intentapproval.DecisionBinding{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision,
			IntentID: p.IntentID, ProposalRevisionID: p.ProposalRevisionID, ProposalDigest: p.MaterialDigest,
			TaskVersion: 1, RequirementDigest: req.Digest(), ResolutionExpressionDigest: req.ExpressionDigest,
		},
		Outcome:              outcome,
		Approver:             intentapproval.ApproverReference{PrincipalID: principal, Via: humanwork.SourceDirect},
		AuthorityDecisionRef: "authz:1", Reason: "reason:reviewed", DecidedAt: now,
		VoteDigest: "sha256:" + strings.Repeat("e", 64),
	}
}

func completed(item workitem.WorkItem, d intentapproval.ApprovalDecision) workitem.WorkItem {
	item.Status = workitem.StatusCompleted
	item.CompletedBy = d.Approver.PrincipalID
	item.CompletedOutputDigest = d.Digest()
	t := now.Time()
	item.CompletedAt = &t
	return item
}

func continuation(t *testing.T, set humanwork.RequirementSet, items []workitem.WorkItem) stepapproval.Continuation {
	t.Helper()
	c, err := stepapproval.NewContinuation(instanceID, "approve", proposal(), set, items)
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	return c
}

// TestTodo_WF_STEP_003 exercises the five closed APPROVAL routes and proves a
// pending quorum remains a durable WorkItem await rather than guessing success.
func TestTodo_WF_STEP_003(t *testing.T) {
	req := requirement("approval.manager", 1, false)
	pending := pendingItem(itemAID, req, "principal:manager")
	approved := decision(req, "principal:manager", "decision:approved", intentapproval.OutcomeApproved)
	rejected := decision(req, "principal:manager", "decision:rejected", intentapproval.OutcomeRejected)

	tests := []struct {
		name      string
		item      workitem.WorkItem
		decisions []intentapproval.ApprovalDecision
		at        values.Instant
		event     stepapproval.Event
		want      workflow.Outcome
		await     bool
	}{
		{"APPROVED", completed(pending, approved), []intentapproval.ApprovalDecision{approved}, now, stepapproval.Event{}, "APPROVED", false},
		{"REJECTED", completed(pending, rejected), []intentapproval.ApprovalDecision{rejected}, now, stepapproval.Event{}, workflow.OutcomeRejected, false},
		{"INVALIDATED", pending, nil, now, stepapproval.Event{Kind: stepapproval.EventInvalidated, Reason: "material proposal changed"}, "INVALIDATED", false},
		{"EXPIRED", pending, nil, values.NewInstant(expiry.Time().Add(time.Second)), stepapproval.Event{}, "EXPIRED", false},
		{"CANCELLED", func() workitem.WorkItem { i := pending; i.Status = workitem.StatusCancelled; return i }(), nil, now, stepapproval.Event{}, "CANCELLED", false},
		{"AWAITING", pending, nil, now, stepapproval.Event{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{pending})
			res, err := stepapproval.Resolve(c, []workitem.WorkItem{tc.item}, tc.decisions, tc.at, tc.event)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if res.Outcome != tc.want || res.Digest == "" {
				t.Fatalf("resolution = %+v, want outcome %q", res, tc.want)
			}
			out := res.ToNodeOutcome("approve")
			if tc.await {
				if out.Await != frontier.AwaitWorkItem || out.AwaitRef != c.Digest {
					t.Fatalf("ToNodeOutcome = %+v, want work-item await", out)
				}
			} else if out.Outcome != tc.want || out.OutputDigest != res.Digest {
				t.Fatalf("ToNodeOutcome = %+v, want %s", out, tc.want)
			}
		})
	}

	// Open and Complete are the store-integration half of WF-STEP-003: the
	// table above proves the pure evaluation over already-durable evidence,
	// and this subtest proves that evidence is exactly what Open and Complete
	// produce against a real Postgres-backed workitem.Store -- CREATED through
	// Route, Claim, Start and Complete -- rather than evidence a test built by
	// hand.
	t.Run("store round trip: Open, Route, Claim, Start, Complete, Resolve", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := insertTenant(t, db, "wf-step-003-store")
		instance := instanceID // the package fixture NewContinuation binds against
		insertInstance(t, db, tenant, instance, "approve")
		conn := appConn(t, db)
		store := workitem.Store{}
		ctx := context.Background()

		req := requirement("approval.manager", 1, false)
		prop := proposal()
		node := stepapproval.CompiledApprovalNode{
			WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "approve",
			WorkType: "promotion.approval", PolicyRouteRef: "route.promotion.manager/v1",
			Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme-eu:engineering",
		}

		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = stepapproval.Open(ctx, tx, store, stepapproval.OpenInput{
				TenantID: tenant, WorkflowInstanceID: instance, CorrelationID: "corr-" + instance.String(),
				SubjectRefs: []string{"worker:jane"}, Node: node, Requirement: req, Proposal: prop,
				Now: now.Time(), Meta: workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.created", At: now.Time()},
			})
			return err
		})
		if item.Status != workitem.StatusCreated || item.Kind != workitem.KindApproval || item.ApprovalRequirementRef != req.RequirementID {
			t.Fatalf("Open produced %+v", item)
		}

		resolution := humanwork.Resolution{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision, Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: "principal:manager", Via: humanwork.SourceDirect, TermRef: "role:approver"}},
			ResolvedAt: now, EffectiveAt: now,
			DirectoryVersion: "directory.test/1", ExpressionDigest: req.ExpressionDigest,
			RequirementDigest: req.Digest(), QuorumRequired: req.Quorum.MinApprovals,
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: resolution, GovernancePolicyRef: req.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting},
				workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.routed", At: now.Time()})
			return err
		})
		if item.Status != workitem.StatusAssigned {
			t.Fatalf("Route produced status %s, want ASSIGNED", item.Status)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:manager", ClaimExpiresAt: now.Time().Add(time.Hour), Now: now.Time(),
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:manager", Reason: "workitem.claimed", At: now.Time()},
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, now.Time(),
				workitem.TransitionMeta{ActorPrincipalID: "principal:manager", Reason: "workitem.started", At: now.Time()})
			return err
		})
		if item.Status != workitem.StatusInProgress {
			t.Fatalf("Start produced status %s, want IN_PROGRESS", item.Status)
		}

		d := decision(req, "principal:manager", "decision:store-1", intentapproval.OutcomeApproved)
		var completedItem workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			completedItem, err = stepapproval.Complete(ctx, tx, store, item, d, now.Time(),
				workitem.TransitionMeta{ActorPrincipalID: "principal:manager", Reason: "workitem.completed", At: now.Time()})
			return err
		})
		if completedItem.Status != workitem.StatusCompleted || completedItem.CompletedOutputDigest != d.Digest() {
			t.Fatalf("Complete produced %+v, want digest %s", completedItem, d.Digest())
		}

		c := continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{completedItem})
		res, err := stepapproval.Resolve(c, []workitem.WorkItem{completedItem}, []intentapproval.ApprovalDecision{d}, now, stepapproval.Event{})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != "APPROVED" {
			t.Fatalf("Resolve outcome = %q, want APPROVED", res.Outcome)
		}
		out := res.ToNodeOutcome("approve")
		if out.Outcome != "APPROVED" || out.OutputDigest != res.Digest {
			t.Fatalf("ToNodeOutcome = %+v, want APPROVED with digest %s", out, res.Digest)
		}

		// A second Complete attempt with a conflicting decision is refused: the
		// item is already terminal, so the store's own LegalTransition guard
		// (backed by migration 00017's immutable-output trigger) refuses it
		// rather than silently overwriting the recorded decision.
		d2 := decision(req, "principal:manager", "decision:store-2", intentapproval.OutcomeRejected)
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := stepapproval.Complete(ctx, tx, store, completedItem, d2, now.Time(),
				workitem.TransitionMeta{ActorPrincipalID: "principal:manager", Reason: "workitem.completed", At: now.Time()})
			return err
		})
		if err == nil {
			t.Fatalf("second Complete on a terminal item succeeded, want refusal")
		}
	})
}

func TestTodo_WF_STEP_003_Conformance(t *testing.T) {
	t.Run("exact proposal binding", func(t *testing.T) {
		req := requirement("approval.manager", 1, false)
		item := pendingItem(itemAID, req, "principal:manager")
		d := decision(req, "principal:manager", "decision:1", intentapproval.OutcomeApproved)
		item = completed(item, d)
		d.Binding.ProposalDigest.Digest = strings.Repeat("f", 64)
		item.CompletedOutputDigest = d.Digest()
		_, err := stepapproval.Resolve(continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{item}), []workitem.WorkItem{item}, []intentapproval.ApprovalDecision{d}, now, stepapproval.Event{})
		if !errors.Is(err, stepapproval.ErrInvalidEvidence) && !errors.Is(err, stepapproval.ErrBindingMismatch) {
			t.Fatalf("Resolve error = %v, want binding/evidence refusal", err)
		}
	})

	t.Run("wrong candidate cannot approve", func(t *testing.T) {
		req := requirement("approval.manager", 1, false)
		item := pendingItem(itemAID, req, "principal:manager")
		d := decision(req, "principal:intruder", "decision:1", intentapproval.OutcomeApproved)
		item = completed(item, d)
		_, err := stepapproval.Resolve(continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{item}), []workitem.WorkItem{item}, []intentapproval.ApprovalDecision{d}, now, stepapproval.Event{})
		if !errors.Is(err, stepapproval.ErrBindingMismatch) {
			t.Fatalf("Resolve error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("distinct quorum cannot reuse a principal", func(t *testing.T) {
		req := requirement("approval.executive", 2, true)
		one := pendingItem(itemAID, req, "principal:executive")
		two := pendingItem(itemBID, req, "principal:executive")
		d1 := decision(req, "principal:executive", "decision:1", intentapproval.OutcomeApproved)
		d2 := decision(req, "principal:executive", "decision:2", intentapproval.OutcomeApproved)
		d2.VoteDigest = "sha256:" + strings.Repeat("9", 64)
		one, two = completed(one, d1), completed(two, d2)
		c := continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{one, two})
		res, err := stepapproval.Resolve(c, []workitem.WorkItem{one, two}, []intentapproval.ApprovalDecision{d1, d2}, now, stepapproval.Event{})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != "" || res.ToNodeOutcome("approve").Await != frontier.AwaitWorkItem {
			t.Fatalf("same principal satisfied distinct quorum: %+v", res)
		}
	})
}

func TestTodo_WF_STEP_003_Mutation(t *testing.T) {
	reqA := requirement("approval.a", 1, false)
	reqB := requirement("approval.b", 1, false)
	itemA := pendingItem(itemAID, reqA, "principal:a")
	itemB := pendingItem(itemBID, reqB, "principal:b")
	dA := decision(reqA, "principal:a", "decision:a", intentapproval.OutcomeApproved)
	dB := decision(reqB, "principal:b", "decision:b", intentapproval.OutcomeApproved)
	itemA, itemB = completed(itemA, dA), completed(itemB, dB)

	setAB := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{reqA, reqB}}
	setBA := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{reqB, reqA}}
	c1 := continuation(t, setAB, []workitem.WorkItem{itemA, itemB})
	c2 := continuation(t, setBA, []workitem.WorkItem{itemB, itemA})
	if c1.Digest != c2.Digest || !reflect.DeepEqual(c1.Requirements, c2.Requirements) {
		t.Fatalf("continuation depends on input ordering: %+v vs %+v", c1, c2)
	}
	r1, err := stepapproval.Resolve(c1, []workitem.WorkItem{itemA, itemB}, []intentapproval.ApprovalDecision{dA, dB}, now, stepapproval.Event{})
	if err != nil {
		t.Fatalf("Resolve ordered: %v", err)
	}
	r2, err := stepapproval.Resolve(c1, []workitem.WorkItem{itemB, itemA}, []intentapproval.ApprovalDecision{dB, dA}, now, stepapproval.Event{})
	if err != nil {
		t.Fatalf("Resolve reversed: %v", err)
	}
	if r1.Digest != r2.Digest || !reflect.DeepEqual(r1, r2) {
		t.Fatalf("resolution depends on input ordering: %+v vs %+v", r1, r2)
	}

	replayed, err := stepapproval.Resolve(c1, nil, nil, values.NewInstant(now.Time().Add(24*time.Hour)), stepapproval.Event{Kind: stepapproval.EventInvalidated, Reason: "would differ", Prior: &r1})
	if err != nil {
		t.Fatalf("Resolve replay: %v", err)
	}
	if !reflect.DeepEqual(replayed, r1) {
		t.Fatalf("replay changed original resolution: %+v vs %+v", replayed, r1)
	}
}
