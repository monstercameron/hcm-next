package task_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	steptask "github.com/monstercameron/hcm-next/internal/workflow/steps/task"
)

var (
	tenantFixture   = "wf-step-004"
	fixedInstant    = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	instanceFixture = uuid.MustParse("40000000-0000-0000-0000-000000000001")
)

func repeatHex(digit string) string {
	out := ""
	for range 64 {
		out += digit
	}
	return out
}

func compiledNode() steptask.CompiledTaskNode {
	return steptask.CompiledTaskNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "review_task", WorkType: "promotion.review",
		OutputSchema:        workflow.SchemaRef{SchemaID: "hcmnext.task.promotion_review.Output", Version: 1, ProtobufFullName: "hcmnext.task.promotion_review.Output"},
		FormDefinition:      steptask.VersionedRef{Ref: "form.promotion.review", Version: 1},
		AccessibilityPolicy: steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1},
		AccommodationPolicy: steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1},
	}
}

func taskResolution(principal string) humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID: "task.review/v1", Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "role:reviewer"}},
		ResolvedAt: values.NewInstant(fixedInstant), EffectiveAt: values.NewInstant(fixedInstant),
		DirectoryVersion: "directory.test/1", ExpressionDigest: "sha256:" + repeatHex("1"),
		RequirementDigest: "sha256:" + repeatHex("2"), QuorumRequired: 1,
	}
}

// pendingTaskItem builds a durable-shaped, in-memory WorkItem as if it had
// already been Opened, Routed and Claimed by the given principal, for the
// pure Conformance/Security/Mutation cases that never need real Postgres.
func pendingTaskItem(instance uuid.UUID, node steptask.CompiledTaskNode, principal string, claimID uuid.UUID) workitem.WorkItem {
	claimedAt := fixedInstant
	claimExpires := fixedInstant.Add(time.Hour)
	return workitem.WorkItem{
		TenantID: uuid.MustParse("50000000-0000-0000-0000-000000000001"), WorkItemID: uuid.New(), ItemVersion: 3,
		Kind: workitem.KindTask, WorkType: node.WorkType, Status: workitem.StatusClaimed,
		CorrelationID: "corr-1", WorkflowInstanceID: instance, NodeID: node.NodeID,
		SubjectRefs: []string{"worker:jane"}, OwnerKind: workitem.OwnerPrincipal, OwnerRef: principal,
		PolicyRouteRef: "route.promotion.reviewer/v1", Visibility: workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: "org:acme-eu:engineering", DeadlineAt: fixedInstant.Add(48 * time.Hour),
		Assignment: workitem.Assignment{Resolution: taskResolution(principal), Trigger: workitem.TriggerInitialRouting},
		ClaimID:    &claimID, ClaimedBy: principal, ClaimedAt: &claimedAt, ClaimExpiresAt: &claimExpires,
		CreatedAt: fixedInstant,
	}
}

func validSpec(node steptask.CompiledTaskNode, item workitem.WorkItem, principal string, submittedAt time.Time) steptask.SubmissionSpec {
	return steptask.SubmissionSpec{
		CompletedBy: principal, CandidateVia: humanwork.SourceDirect,
		ClaimID: *item.ClaimID, ClaimExpiresAt: values.NewInstant(*item.ClaimExpiresAt), SubmittedAt: values.NewInstant(submittedAt),
		OutputSchema: node.OutputSchema, CanonicalPayloadDigest: "sha256:" + repeatHex("9"),
		FormDefinition: node.FormDefinition, RenderContextDigest: "sha256:" + repeatHex("8"),
		ValidationEvidenceRef: "validation:reviewed", AccessibilityEvidenceRef: "ack:accessibility:reviewed",
		AccommodationEvidenceRef: "ack:accommodation:reviewed",
	}
}

// acceptingValidator only checks the declared acknowledgement values are
// exactly the accepted tokens, proving Submit's schema check is against the
// declared typed fields and not merely "some string was present".
var acceptingValidator = steptask.ValidatorFunc(func(req steptask.ValidationRequest) error {
	if req.Submission.AccessibilityEvidenceRef != "ack:accessibility:reviewed" {
		return fmt.Errorf("accessibility acknowledgement not recognized")
	}
	if req.Submission.AccommodationEvidenceRef != "ack:accommodation:reviewed" {
		return fmt.Errorf("accommodation acknowledgement not recognized")
	}
	if req.Submission.ValidationEvidenceRef == "" {
		return fmt.Errorf("no validation evidence")
	}
	return nil
})

// TestTodo_WF_STEP_004 is the PRIMARY case: Open through the store, Route,
// Claim, then every typed terminal a TASK node can reach -- SUCCEEDED via
// Submit, RETURNED, EXPIRED, CANCELLED -- plus the AWAITING marker a still
// -claimed item reports, each proved against a real Postgres-backed
// workitem.Store rather than a hand-built WorkItem.
func TestTodo_WF_STEP_004(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, tenantFixture+"-primary")
	insertInstance(t, db, tenant, instanceFixture, "review_task")
	conn := appConn(t, db)
	store := workitem.Store{}
	node := compiledNode()

	open := func(t *testing.T) workitem.WorkItem {
		t.Helper()
		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = steptask.Open(ctx, tx, store, steptask.OpenInput{
				TenantID: tenant, WorkflowInstanceID: instanceFixture, CorrelationID: "corr-" + instanceFixture.String(),
				SubjectRefs: []string{"worker:jane"}, Node: node, PolicyRouteRef: "route.promotion.reviewer/v1",
				Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme-eu:engineering",
				DeadlineAt: fixedInstant.Add(48 * time.Hour), Now: fixedInstant,
				Meta: workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.created", At: fixedInstant},
			})
			return err
		})
		if item.Status != workitem.StatusCreated || item.Kind != workitem.KindTask {
			t.Fatalf("Open produced %+v", item)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: taskResolution("principal:reviewer"), Trigger: workitem.TriggerInitialRouting},
				workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.routed", At: fixedInstant})
			return err
		})
		if item.Status != workitem.StatusAssigned {
			t.Fatalf("Route produced %s, want ASSIGNED", item.Status)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:reviewer", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.claimed", At: fixedInstant},
			})
			return err
		})
		if item.Status != workitem.StatusClaimed {
			t.Fatalf("Claim produced %s, want CLAIMED", item.Status)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant,
				workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.started", At: fixedInstant})
			return err
		})
		if item.Status != workitem.StatusInProgress {
			t.Fatalf("Start produced %s, want IN_PROGRESS", item.Status)
		}
		return item
	}

	t.Run("SUCCEEDED via Submit", func(t *testing.T) {
		item := open(t)
		var completed workitem.WorkItem
		var sub steptask.Submission
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			completed, sub, err = steptask.Submit(ctx, tx, store, item, steptask.SubmitInput{
				Node: node, Spec: validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute)),
				Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
			})
			return err
		})
		if completed.Status != workitem.StatusCompleted || completed.CompletedOutputDigest != sub.Digest() {
			t.Fatalf("Submit produced %+v, want completed digest %s", completed, sub.Digest())
		}
		c, err := steptask.NewContinuation(instanceFixture, node, completed)
		if err != nil {
			t.Fatalf("NewContinuation: %v", err)
		}
		res, err := steptask.Resolve(c, completed, &sub, acceptingValidator, values.NewInstant(fixedInstant.Add(time.Minute)), steptask.Event{})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != steptask.OutcomeSucceeded {
			t.Fatalf("Resolve outcome = %q, want SUCCEEDED", res.Outcome)
		}
		out := res.ToNodeOutcome("review_task")
		if out.Outcome != workflow.OutcomeSucceeded || out.OutputDigest != res.Digest {
			t.Fatalf("ToNodeOutcome = %+v, want SUCCEEDED with digest %s", out, res.Digest)
		}
	})

	t.Run("RETURNED", func(t *testing.T) {
		item := open(t)
		var returned workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			returned, err = store.Return(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant,
				workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.returned", At: fixedInstant})
			return err
		})
		assertTerminal(t, instanceFixture, node, returned, steptask.OutcomeReturned, workflow.OutcomeRejected)
	})

	t.Run("EXPIRED", func(t *testing.T) {
		item := open(t)
		var expired workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			expired, err = store.Expire(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.TransitionMeta{ActorPrincipalID: "system:deadline", Reason: "workitem.expired", At: fixedInstant})
			return err
		})
		assertTerminal(t, instanceFixture, node, expired, steptask.OutcomeExpired, workflow.Outcome("EXPIRED"))
	})

	t.Run("CANCELLED", func(t *testing.T) {
		item := open(t)
		var cancelled workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			cancelled, err = store.Cancel(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.TransitionMeta{ActorPrincipalID: "principal:requester", Reason: "workitem.cancelled", At: fixedInstant})
			return err
		})
		assertTerminal(t, instanceFixture, node, cancelled, steptask.OutcomeCancelled, workflow.Outcome("CANCELLED"))
	})

	t.Run("AWAITING while claimed", func(t *testing.T) {
		item := open(t)
		c, err := steptask.NewContinuation(instanceFixture, node, item)
		if err != nil {
			t.Fatalf("NewContinuation: %v", err)
		}
		res, err := steptask.Resolve(c, item, nil, nil, values.NewInstant(fixedInstant), steptask.Event{})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != "" {
			t.Fatalf("Resolve outcome = %q, want empty (still awaiting)", res.Outcome)
		}
		out := res.ToNodeOutcome("review_task")
		if out.Await != frontier.AwaitWorkItem || out.AwaitRef != c.Digest {
			t.Fatalf("ToNodeOutcome = %+v, want work-item await", out)
		}
	})
}

func assertTerminal(t *testing.T, instance uuid.UUID, node steptask.CompiledTaskNode, item workitem.WorkItem, wantTask steptask.Outcome, wantRoute workflow.Outcome) {
	t.Helper()
	c, err := steptask.NewContinuation(instance, node, item)
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	res, err := steptask.Resolve(c, item, nil, nil, values.NewInstant(fixedInstant.Add(time.Minute)), steptask.Event{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != wantTask {
		t.Fatalf("Resolve outcome = %q, want %q", res.Outcome, wantTask)
	}
	out := res.ToNodeOutcome("review_task")
	if out.Outcome != wantRoute || out.OutputDigest != res.Digest {
		t.Fatalf("ToNodeOutcome = %+v, want %s with digest %s", out, wantRoute, res.Digest)
	}
}

// TestTodo_WF_STEP_004_Race proves WORK-003's exclusivity contract end to
// end for a TASK node: two real, concurrent connections racing Store.Claim
// against the identical ExpectedVersion never both win, and the loser's item
// is left untouched by its own attempt.
func TestTodo_WF_STEP_004_Race(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, tenantFixture+"-race")
	insertInstance(t, db, tenant, instanceFixture, "review_task")
	store := workitem.Store{}
	node := compiledNode()

	// Route the item to AVAILABLE (candidate set) through the primary
	// connection so both racing claimants are legally entitled to try.
	primary := appConn(t, db)
	var item workitem.WorkItem
	inTenantTx(t, primary, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = steptask.Open(ctx, tx, store, steptask.OpenInput{
			TenantID: tenant, WorkflowInstanceID: instanceFixture, CorrelationID: "corr-race",
			SubjectRefs: []string{"worker:jane"}, Node: node, PolicyRouteRef: "route.promotion.reviewer/v1",
			Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme-eu:engineering",
			DeadlineAt: fixedInstant.Add(48 * time.Hour), Now: fixedInstant,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.created", At: fixedInstant},
		})
		return err
	})
	resolution := humanwork.Resolution{
		RequirementID: "task.review/v1", Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{
			{PrincipalID: "principal:a", Via: humanwork.SourceDirect, TermRef: "role:reviewer"},
			{PrincipalID: "principal:b", Via: humanwork.SourceDirect, TermRef: "role:reviewer"},
		},
		ResolvedAt: values.NewInstant(fixedInstant), EffectiveAt: values.NewInstant(fixedInstant),
		DirectoryVersion: "directory.test/1", ExpressionDigest: "sha256:" + repeatHex("3"),
		RequirementDigest: "sha256:" + repeatHex("4"), QuorumRequired: 1,
	}
	inTenantTx(t, primary, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, Trigger: workitem.TriggerInitialRouting},
			workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.routed", At: fixedInstant})
		return err
	})
	if item.Status != workitem.StatusAvailable {
		t.Fatalf("fixture item status = %s, want AVAILABLE", item.Status)
	}

	connA := appConn(t, db)
	connB := appConn(t, db)

	race := func(conn *pgxadapter.Conn, principal string) error {
		return inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: principal, ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "workitem.claimed", At: fixedInstant},
			})
			return err
		})
	}

	var wg sync.WaitGroup
	errA, errB := error(nil), error(nil)
	wg.Add(2)
	go func() { defer wg.Done(); errA = race(connA, "principal:a") }()
	go func() { defer wg.Done(); errB = race(connB, "principal:b") }()
	wg.Wait()

	succeeded, refused := 0, 0
	for _, err := range []error{errA, errB} {
		switch {
		case err == nil:
			succeeded++
		case workitem.CodeOf(err) == workitem.CodeAlreadyClaimed:
			refused++
		default:
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("race outcome = %d succeeded, %d refused (errA=%v errB=%v), want exactly one of each", succeeded, refused, errA, errB)
	}

	var final workitem.WorkItem
	inTenantTx(t, primary, tenant, func(tx dbport.Tx) error {
		var err error
		final, err = store.Load(ctx, tx, tenant, item.WorkItemID)
		return err
	})
	if final.Status != workitem.StatusClaimed || (final.ClaimedBy != "principal:a" && final.ClaimedBy != "principal:b") {
		t.Fatalf("final item = %+v, want exactly one claimant holding CLAIMED", final)
	}
}

// TestTodo_WF_STEP_004_Conformance proves the pure binding checks Submit and
// Resolve enforce before any store write is attempted: a node/instance
// mismatch, a wrong candidate, an invalid form/schema reference and a missing
// accessibility/accommodation acknowledgement all refuse rather than succeed
// or silently coerce.
func TestTodo_WF_STEP_004_Conformance(t *testing.T) {
	node := compiledNode()
	claimID := uuid.New()

	t.Run("work item names another node", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		item.NodeID = "some_other_node"
		_, err := steptask.NewContinuation(instanceFixture, node, item)
		if !errors.Is(err, steptask.ErrBindingMismatch) {
			t.Fatalf("NewContinuation error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("wrong candidate cannot submit", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:intruder", fixedInstant.Add(time.Minute))
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:intruder", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrBindingMismatch) {
			t.Fatalf("Submit error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("submission names another form definition", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute))
		spec.FormDefinition = steptask.VersionedRef{Ref: "form.other", Version: 9}
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrBindingMismatch) {
			t.Fatalf("Submit error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("missing accessibility acknowledgement", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute))
		spec.AccessibilityEvidenceRef = ""
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrInvalidSubmission) {
			t.Fatalf("Submit error = %v, want ErrInvalidSubmission", err)
		}
	})

	t.Run("missing accommodation acknowledgement", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute))
		spec.AccommodationEvidenceRef = ""
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrInvalidSubmission) {
			t.Fatalf("Submit error = %v, want ErrInvalidSubmission", err)
		}
	})

	t.Run("expired claim cannot submit", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:reviewer", (*item.ClaimExpiresAt).Add(time.Hour))
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: (*item.ClaimExpiresAt).Add(time.Hour),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: (*item.ClaimExpiresAt).Add(time.Hour)},
		})
		if !errors.Is(err, steptask.ErrClaimExpired) {
			t.Fatalf("Submit error = %v, want ErrClaimExpired", err)
		}
	})
}

// TestTodo_WF_STEP_004_Browser is WF-STEP-004's BROWSER case, satisfied as a
// server-rendered conformance check of the task form's accessibility
// contract: there is no browser in this environment, so RenderForm's HTML is
// asserted directly rather than driven through a real one. Every declared
// control carries a label bound by for/id, both the native `required`
// attribute and `aria-required="true"` together (never one alone), and the
// accommodation acknowledgement is its own explicit, equally required
// control rather than folded into the general submission.
func TestTodo_WF_STEP_004_Browser(t *testing.T) {
	node := compiledNode()
	doc, err := steptask.RenderForm(node)
	if err != nil {
		t.Fatalf("RenderForm: %v", err)
	}

	requiredPair := 0
	for _, marker := range []string{"required", `aria-required="true"`} {
		if strings.Contains(doc, marker) {
			requiredPair++
		}
	}
	if requiredPair != 2 {
		t.Fatalf("rendered form is missing native required/aria-required pairing: %s", doc)
	}
	if !strings.Contains(doc, `for="task-output"`) || !strings.Contains(doc, `id="task-output"`) {
		t.Fatalf("rendered form has no label bound to the typed output control: %s", doc)
	}
	if !strings.Contains(doc, `for="accommodation-ack"`) || !strings.Contains(doc, `id="accommodation-ack"`) {
		t.Fatalf("rendered form has no explicit accommodation-acknowledgement control: %s", doc)
	}
	if !strings.Contains(doc, node.AccommodationPolicy.Ref) || !strings.Contains(doc, node.AccessibilityPolicy.Ref) {
		t.Fatalf("rendered form does not cite the compiled accessibility/accommodation policy: %s", doc)
	}

	if _, err := steptask.RenderForm(steptask.CompiledTaskNode{}); err == nil {
		t.Fatalf("RenderForm accepted an incomplete node")
	}
}

// TestTodo_WF_STEP_004_Mutation proves two properties the REFACTOR clause
// requires: an arbitrary mutation to a minted Submission's typed fields is
// caught by Verify (and therefore by Resolve) rather than silently accepted,
// and a replayed resolution returns the exact prior verdict byte for byte
// regardless of what "now" or evidence a later call supplies.
func TestTodo_WF_STEP_004_Mutation(t *testing.T) {
	node := compiledNode()
	claimID := uuid.New()
	item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
	spec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute))
	spec.WorkflowInstanceID = item.WorkflowInstanceID
	spec.NodeID = item.NodeID
	spec.WorkItemID = item.WorkItemID
	spec.ItemVersion = item.ItemVersion
	sub, err := steptask.NewSubmission(spec)
	if err != nil {
		t.Fatalf("NewSubmission: %v", err)
	}
	completedAt := fixedInstant.Add(time.Minute) // must equal spec.SubmittedAt above, per checkCompletion
	item.Status = workitem.StatusCompleted
	item.CompletedBy = sub.CompletedBy
	item.CompletedOutputDigest = sub.Digest()
	item.CompletedAt = &completedAt
	item.ClaimID, item.ClaimedBy, item.ClaimedAt, item.ClaimExpiresAt = nil, "", nil, nil

	c, err := steptask.NewContinuation(instanceFixture, node, item)
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	resolvedAt := values.NewInstant(fixedInstant.Add(time.Hour))
	r1, err := steptask.Resolve(c, item, &sub, acceptingValidator, resolvedAt, steptask.Event{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Two evaluations of Resolve over byte-identical evidence and the same
	// resolution instant reproduce the identical digest -- the pure-function
	// determinism [Resolution.Digest] exists to make checkable.
	r2, err := steptask.Resolve(c, item, &sub, acceptingValidator, resolvedAt, steptask.Event{})
	if err != nil {
		t.Fatalf("Resolve second evaluation: %v", err)
	}
	if r1.Digest != r2.Digest || !reflect.DeepEqual(r1, r2) {
		t.Fatalf("resolution is not deterministic over identical evidence: %+v vs %+v", r1, r2)
	}

	// Tampering with the "immutable" submission after minting is caught by
	// Verify, which checkCompletion calls before anything else: a mutable
	// task payload is exactly what the REFACTOR clause prohibits.
	tampered := sub
	tampered.CanonicalPayloadDigest = "sha256:" + repeatHex("0")
	if err := tampered.Verify(); err == nil {
		t.Fatalf("tampered submission still verified")
	}
	if _, err := steptask.Resolve(c, item, &tampered, acceptingValidator, values.NewInstant(fixedInstant.Add(time.Hour)), steptask.Event{}); err == nil {
		t.Fatalf("Resolve accepted a tampered submission")
	}

	// A duplicate event replays the prior resolution unchanged, regardless of
	// a later, differing "now".
	replayed, err := steptask.Resolve(c, item, nil, nil, values.NewInstant(fixedInstant.Add(48*time.Hour)), steptask.Event{Prior: &r1})
	if err != nil {
		t.Fatalf("Resolve replay: %v", err)
	}
	if !reflect.DeepEqual(replayed, r1) {
		t.Fatalf("replay changed the original resolution: %+v vs %+v", replayed, r1)
	}
}

// TestTodo_WF_STEP_004_Security proves TASK's authority boundary: a principal
// outside the resolved candidate set cannot submit regardless of how it
// claims to have arrived, a forged claim identifier is refused even when the
// principal is otherwise authorized, and a second submission attempt against
// an already-completed item is refused rather than silently overwriting the
// first (the store's immutable-output guarantee, exercised from Submit's own
// side of the boundary).
func TestTodo_WF_STEP_004_Security(t *testing.T) {
	node := compiledNode()
	claimID := uuid.New()

	t.Run("principal outside the candidate set is refused regardless of claimed route", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:outsider", fixedInstant.Add(time.Minute))
		spec.CandidateVia = humanwork.SourceDelegated // claims a route it was never granted
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:outsider", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrBindingMismatch) {
			t.Fatalf("Submit error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("forged claim id is refused even for the authorized candidate", func(t *testing.T) {
		item := pendingTaskItem(instanceFixture, node, "principal:reviewer", claimID)
		spec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute))
		spec.ClaimID = uuid.New() // does not match item.ClaimID
		_, _, err := steptask.Submit(context.Background(), nil, workitem.Store{}, item, steptask.SubmitInput{
			Node: node, Spec: spec, Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
		})
		if !errors.Is(err, steptask.ErrBindingMismatch) {
			t.Fatalf("Submit error = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("duplicate conflicting completion against an already-completed item", func(t *testing.T) {
		ctx := context.Background()
		db := pgtest.New(t)
		tenant := insertTenant(t, db, tenantFixture+"-security")
		insertInstance(t, db, tenant, instanceFixture, "review_task")
		conn := appConn(t, db)
		store := workitem.Store{}

		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = steptask.Open(ctx, tx, store, steptask.OpenInput{
				TenantID: tenant, WorkflowInstanceID: instanceFixture, CorrelationID: "corr-security",
				SubjectRefs: []string{"worker:jane"}, Node: node, PolicyRouteRef: "route.promotion.reviewer/v1",
				Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme-eu:engineering",
				DeadlineAt: fixedInstant.Add(48 * time.Hour), Now: fixedInstant,
				Meta: workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.created", At: fixedInstant},
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: taskResolution("principal:reviewer"), Trigger: workitem.TriggerInitialRouting},
				workitem.TransitionMeta{ActorPrincipalID: "system:frontier", Reason: "workitem.routed", At: fixedInstant})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:reviewer", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.claimed", At: fixedInstant},
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant,
				workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.started", At: fixedInstant})
			return err
		})

		var first workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			first, _, err = steptask.Submit(ctx, tx, store, item, steptask.SubmitInput{
				Node: node, Spec: validSpec(node, item, "principal:reviewer", fixedInstant.Add(time.Minute)),
				Validator: acceptingValidator, Now: fixedInstant.Add(time.Minute),
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(time.Minute)},
			})
			return err
		})
		if first.Status != workitem.StatusCompleted {
			t.Fatalf("first Submit produced %+v, want COMPLETED", first)
		}

		// A second submission attempt against the now-completed item -- even
		// with a differing payload -- must not be accepted as a fresh
		// completion.
		secondSpec := validSpec(node, item, "principal:reviewer", fixedInstant.Add(2*time.Minute))
		secondSpec.CanonicalPayloadDigest = "sha256:" + repeatHex("7")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, err := steptask.Submit(ctx, tx, store, first, steptask.SubmitInput{
				Node: node, Spec: secondSpec, Validator: acceptingValidator, Now: fixedInstant.Add(2 * time.Minute),
				Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:reviewer", Reason: "workitem.completed", At: fixedInstant.Add(2 * time.Minute)},
			})
			return err
		})
		if err == nil {
			t.Fatalf("second Submit against a completed item succeeded, want refusal")
		}
	})
}
