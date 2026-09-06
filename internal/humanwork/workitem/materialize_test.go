package workitem_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/engines/rules"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// approvalBinding builds a well-formed [workitem.ApprovalProposalBinding] for
// the promotion fixture's SUBJECT/ORGANIZATION frames.
func approvalBinding(tenant, instance uuid.UUID, proposalRef string) workitem.ApprovalProposalBinding {
	return workitem.ApprovalProposalBinding{
		TenantID:            tenant,
		WorkflowInstanceID:  instance,
		NodeID:              "approval_node",
		CorrelationID:       "corr-" + instance.String(),
		ProposalRef:         proposalRef,
		SubjectRefs:         []string{"worker:jane"},
		OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		PolicyRouteRef:      "route.promotion.approval-committee/v1",
		CreatedAt:           fixedInstant,
	}
}

// deterministicWorkItemID mints a stable, name-derived id per requirement so
// a test can assert on identity, and so a retry with the same seed can be
// proved to fail rather than silently duplicate.
func deterministicWorkItemID(seed string) func(string) uuid.UUID {
	return func(requirementID string) uuid.UUID {
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte("approval007/"+seed+"/"+requirementID))
	}
}

// TestTodo_APPROVAL_007 is the PRIMARY case: APPROVAL-007's RED clause names
// a resolved requirement and its WorkItem diverging, a candidate treated as
// decision authority, an omitted proposal digest, or an unfillable slot left
// stranded. These subtests exercise the standard-tier baseline (every slot
// resolves to one candidate) and the executive-tier exhausted case (a slot
// nobody can fill escalates rather than stranding), through the real
// promotion fixture and the real resolver -- never a stand-in.
func TestTodo_APPROVAL_007(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "approval007-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	store := workitem.Store{}

	t.Run("one governed work item per required decision slot, each carrying a real resolution", func(t *testing.T) {
		scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
		if err != nil {
			t.Fatalf("NewPromotionScenario: %v", err)
		}
		proposalRef := "sha256:" + repeatHex("a")
		in := workitem.MaterializationInput{
			Requirements:     scenario.Requirements,
			Proposal:         approvalBinding(tenant, instance, proposalRef),
			Resolution:       scenario.Resolution,
			Directory:        scenario.Directory,
			Clock:            scenario.Clock,
			WorkItemID:       deterministicWorkItemID("standard"),
			ActorPrincipalID: "system:approval-materializer",
		}

		var items []workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			items, err = workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
			return err
		})

		if len(items) != len(scenario.Requirements.Requirements) {
			t.Fatalf("materialized %d items, want %d", len(items), len(scenario.Requirements.Requirements))
		}
		for i, item := range items {
			req := scenario.Requirements.Requirements[i]
			if item.Kind != workitem.KindApproval {
				t.Errorf("%s: kind = %s, want APPROVAL", req.RequirementID, item.Kind)
			}
			if item.ApprovalRequirementRef != req.RequirementID {
				t.Errorf("%s: requirement ref = %q", req.RequirementID, item.ApprovalRequirementRef)
			}
			if item.ProposalRef != proposalRef {
				t.Errorf("%s: proposal ref = %q, want %q", req.RequirementID, item.ProposalRef, proposalRef)
			}
			if !item.DeadlineAt.Equal(req.Deadline.Expiry.Time()) {
				t.Errorf("%s: deadline = %s, want %s", req.RequirementID, item.DeadlineAt, req.Deadline.Expiry.Time())
			}
			if item.Assignment.Resolution.RequirementDigest != req.Digest() {
				t.Errorf("%s: stored requirement digest does not match the compiled requirement", req.RequirementID)
			}
			// The current-manager slot is the one baseline requirement with a
			// live delegation in the fixture (PrincipalDelegate holds an
			// active, in-scope delegation from PrincipalManager), so it
			// genuinely resolves to two candidates and is left AVAILABLE for
			// either of them; HRBP and the compensation partner have no
			// delegate in the fixture and resolve singly.
			if req.RequirementID == humanwork.RequirementCurrentManager {
				if item.Status != workitem.StatusAvailable || item.OwnerKind != workitem.OwnerCandidateSet {
					t.Errorf("%s: status/owner = %s/%s, want AVAILABLE/CANDIDATE_SET (manager + delegate both resolve)",
						req.RequirementID, item.Status, item.OwnerKind)
				}
				if item.Visibility != workitem.VisibilityCandidateSet {
					t.Errorf("%s: visibility = %s, want CANDIDATE_SET", req.RequirementID, item.Visibility)
				}
				for _, want := range []string{humanwork.PrincipalManager, humanwork.PrincipalDelegate} {
					if !item.Assignment.IsCandidate(want) {
						t.Errorf("%s: %s does not round-trip as a recorded candidate", req.RequirementID, want)
					}
				}
				continue
			}
			if item.Status != workitem.StatusAssigned || item.OwnerKind != workitem.OwnerPrincipal {
				t.Errorf("%s: status/owner = %s/%s, want ASSIGNED/PRINCIPAL",
					req.RequirementID, item.Status, item.OwnerKind)
			}
			if item.Visibility != workitem.VisibilityAssigneeOnly {
				t.Errorf("%s: visibility = %s, want ASSIGNEE_ONLY", req.RequirementID, item.Visibility)
			}
			// Recording an owner grants nothing: the same WORK-002 REFACTOR
			// clause applies here. IsCandidate answers "was this principal in
			// the set when it resolved", never "may they decide now".
			if !item.Assignment.IsCandidate(item.OwnerRef) {
				t.Errorf("%s: the routed owner does not round-trip as a recorded candidate", req.RequirementID)
			}
		}

		// The evidence round-trips through storage: reloading each item
		// reproduces the same owner, proving the recorded assignment is what
		// was actually resolved, not something reconstructed after the fact.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			for _, item := range items {
				reloaded, err := store.Load(ctx, tx, tenant, item.WorkItemID)
				if err != nil {
					return err
				}
				if reloaded.OwnerRef != item.OwnerRef || reloaded.Status != item.Status {
					t.Errorf("%s: reload = %s/%s, want %s/%s",
						item.ApprovalRequirementRef, reloaded.OwnerRef, reloaded.Status, item.OwnerRef, item.Status)
				}
			}
			return nil
		})
	})

	t.Run("an unfillable slot is escalated with its full exclusion list, never stranded", func(t *testing.T) {
		scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputExecutive())
		if err != nil {
			t.Fatalf("NewPromotionScenario: %v", err)
		}
		// Both executives unavailable exhausts the primary set. The
		// escalation fallback (the deputy group) does not broaden authority:
		// neither deputy nor intern holds role.executive_approver, so the
		// fallback fails too, and the slot has no authorized candidate at
		// all -- exactly the case that must escalate rather than strand.
		scenario.Directory.SetAvailable(humanwork.PrincipalExecutiveA, false)
		scenario.Directory.SetAvailable(humanwork.PrincipalExecutiveB, false)

		in := workitem.MaterializationInput{
			Requirements:     scenario.Requirements,
			Proposal:         approvalBinding(tenant, instance, "sha256:"+repeatHex("b")),
			Resolution:       scenario.Resolution,
			Directory:        scenario.Directory,
			Clock:            scenario.Clock,
			WorkItemID:       deterministicWorkItemID("executive-unfillable"),
			ActorPrincipalID: "system:approval-materializer",
		}
		var items []workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			items, err = workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
			return err
		})
		if len(items) != len(scenario.Requirements.Requirements) {
			t.Fatalf("materialized %d items, want %d -- an unfillable slot must still get its accountable work item",
				len(items), len(scenario.Requirements.Requirements))
		}

		var committee workitem.WorkItem
		found := false
		for _, item := range items {
			if item.ApprovalRequirementRef == humanwork.RequirementExecutiveCommittee {
				committee, found = item, true
			}
		}
		if !found {
			t.Fatal("no work item materialized for the executive committee requirement")
		}
		if committee.Status != workitem.StatusEscalated || committee.OwnerKind != workitem.OwnerPolicyRoute {
			t.Fatalf("committee slot = %s/%s, want ESCALATED/POLICY_ROUTE", committee.Status, committee.OwnerKind)
		}
		if committee.OwnerRef != in.Proposal.PolicyRouteRef {
			t.Fatalf("committee owner ref = %q, want the item's own policy route %q",
				committee.OwnerRef, in.Proposal.PolicyRouteRef)
		}
		if committee.Visibility != workitem.VisibilityTenantGovernance {
			t.Fatalf("committee visibility = %s, want TENANT_GOVERNANCE", committee.Visibility)
		}
		if len(committee.Assignment.Resolution.Excluded) == 0 {
			t.Fatal("an unfillable requirement recorded no exclusions -- the item was stranded silently")
		}
	})

	t.Run("retrying the same materialization fails rather than duplicating the work", func(t *testing.T) {
		scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
		if err != nil {
			t.Fatalf("NewPromotionScenario: %v", err)
		}
		in := workitem.MaterializationInput{
			Requirements:     scenario.Requirements,
			Proposal:         approvalBinding(tenant, instance, "sha256:"+repeatHex("c")),
			Resolution:       scenario.Resolution,
			Directory:        scenario.Directory,
			Clock:            scenario.Clock,
			WorkItemID:       deterministicWorkItemID("retry"),
			ActorPrincipalID: "system:approval-materializer",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
			return err
		})

		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
			return err
		})
		if err == nil {
			t.Fatal("a retried materialization against the same deterministic ids silently succeeded")
		}
	})
}

// TestTodo_APPROVAL_007_Golden pins the standard-tier promotion scenario's
// exact routing outcome -- work type, status, owner kind/ref and visibility
// per requirement -- against testdata/approval007_golden.json. A change to
// requirement derivation order, [workitem.RouteFromAssignment] or
// [workitem.VisibilityForAssignment] fails this test even when the specific
// assertions in the PRIMARY case still happen to pass.
func TestTodo_APPROVAL_007_Golden(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/approval007_golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Slots []struct {
			RequirementID string `json:"requirement_id"`
			WorkType      string `json:"work_type"`
			Status        string `json:"status"`
			OwnerKind     string `json:"owner_kind"`
			// OwnerRefPrefix is matched with strings.HasPrefix rather than
			// equality: a CANDIDATE_SET owner ref embeds the requirement's own
			// expression digest, which this fixture does not hand-pin.
			OwnerRefPrefix string `json:"owner_ref_prefix"`
			Visibility     string `json:"visibility"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "approval007-golden")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	store := workitem.Store{}

	scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
	if err != nil {
		t.Fatalf("NewPromotionScenario: %v", err)
	}
	in := workitem.MaterializationInput{
		Requirements:     scenario.Requirements,
		Proposal:         approvalBinding(tenant, instance, "sha256:"+repeatHex("d")),
		Resolution:       scenario.Resolution,
		Directory:        scenario.Directory,
		Clock:            scenario.Clock,
		WorkItemID:       deterministicWorkItemID("golden"),
		ActorPrincipalID: "system:approval-materializer",
	}
	var items []workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		items, err = workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
		return err
	})

	if len(items) != len(golden.Slots) {
		t.Fatalf("materialized %d items, want %d per the golden fixture", len(items), len(golden.Slots))
	}
	for i, want := range golden.Slots {
		got := items[i]
		if got.ApprovalRequirementRef != want.RequirementID {
			t.Fatalf("slot %d requirement = %q, want %q (order drifted)", i, got.ApprovalRequirementRef, want.RequirementID)
		}
		if got.WorkType != want.WorkType {
			t.Errorf("%s: work type = %q, want %q", want.RequirementID, got.WorkType, want.WorkType)
		}
		if string(got.Status) != want.Status {
			t.Errorf("%s: status = %q, want %q", want.RequirementID, got.Status, want.Status)
		}
		if string(got.OwnerKind) != want.OwnerKind {
			t.Errorf("%s: owner kind = %q, want %q", want.RequirementID, got.OwnerKind, want.OwnerKind)
		}
		if !strings.HasPrefix(got.OwnerRef, want.OwnerRefPrefix) {
			t.Errorf("%s: owner ref = %q, want prefix %q", want.RequirementID, got.OwnerRef, want.OwnerRefPrefix)
		}
		if string(got.Visibility) != want.Visibility {
			t.Errorf("%s: visibility = %q, want %q", want.RequirementID, got.Visibility, want.Visibility)
		}
	}
}

// TestTodo_APPROVAL_007_Race drives two overlapping concurrent
// materializations that both mint the same deterministic work item id for
// every requirement. Every writer opens and commits its own transaction, so
// this proves the same guarantee WORK-002's race test proves for a single
// Route call: exactly one writer's Create may win the identity for a given
// requirement, and it is the database's own primary key -- not luck -- that
// makes it so.
func TestTodo_APPROVAL_007_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "approval007-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}

	scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
	if err != nil {
		t.Fatalf("NewPromotionScenario: %v", err)
	}
	id := deterministicWorkItemID("race")
	binding := approvalBinding(tenant, instance, "sha256:"+repeatHex("e"))

	const writers = 5
	results := make([]error, writers)
	conns := make([]*pgxadapter.Conn, writers)
	for i := range writers {
		conns[i] = appConn(t, db)
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for i := range writers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i] = inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				in := workitem.MaterializationInput{
					Requirements:     scenario.Requirements,
					Proposal:         binding,
					Resolution:       scenario.Resolution,
					Directory:        scenario.Directory,
					Clock:            scenario.Clock,
					WorkItemID:       id,
					ActorPrincipalID: "system:approval-materializer",
				}
				_, err := workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
				return err
			})
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent materializations committed for the same identity; exactly one may (%v)", winners, writers, results)
	}

	var stored []workitem.WorkItem
	inTenantTx(t, appConn(t, db), tenant, func(tx dbport.Tx) error {
		var err error
		stored, err = store.ListForInstance(ctx, tx, tenant, instance)
		return err
	})
	if len(stored) != len(scenario.Requirements.Requirements) {
		t.Fatalf("%d work items persisted, want exactly %d -- the losers must leave nothing behind",
			len(stored), len(scenario.Requirements.Requirements))
	}
}

// approval007FaultDirectory wraps the real fixture directory and injects a
// failure after a fixed number of Holders lookups, so a materialization can
// be made to fail partway through a multi-requirement set without faking any
// of the resolution logic that ran before the injected failure.
type approval007FaultDirectory struct {
	*humanwork.MemoryDirectory
	failAfter int
	calls     int
}

func (d *approval007FaultDirectory) Holders(t humanwork.Term, at values.Instant) ([]string, error) {
	d.calls++
	if d.calls > d.failAfter {
		return nil, fmt.Errorf("approval007 fault: injected directory failure on holders call %d", d.calls)
	}
	return d.MemoryDirectory.Holders(t, at)
}

// TestTodo_APPROVAL_007_Fault proves that a directory failure partway through
// a multi-requirement set never leaves an earlier requirement's work item
// durably committed on its own: [workitem.MaterializeApprovalRequirements]
// opens no transaction of its own, so the caller's rollback after the
// reported error must remove every write this call already made.
func TestTodo_APPROVAL_007_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "approval007-fault")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	store := workitem.Store{}

	scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
	if err != nil {
		t.Fatalf("NewPromotionScenario: %v", err)
	}
	// The first requirement (current manager) resolves via exactly one
	// Holders call; letting that one through and failing every call after it
	// means the first slot's Create and Route both succeed inside the
	// transaction before the second requirement's resolution blows up.
	faulty := &approval007FaultDirectory{MemoryDirectory: scenario.Directory, failAfter: 1}

	in := workitem.MaterializationInput{
		Requirements:     scenario.Requirements,
		Proposal:         approvalBinding(tenant, instance, "sha256:"+repeatHex("f")),
		Resolution:       scenario.Resolution,
		Directory:        faulty,
		Clock:            scenario.Clock,
		WorkItemID:       deterministicWorkItemID("fault"),
		ActorPrincipalID: "system:approval-materializer",
	}

	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
		return err
	})
	if err == nil {
		t.Fatal("expected the injected directory failure on the second requirement to fail the whole materialization")
	}

	var remaining []workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		remaining, err = store.ListForInstance(ctx, tx, tenant, instance)
		return err
	})
	if len(remaining) != 0 {
		t.Fatalf("%d work items survived a rolled-back materialization, want 0 -- the first requirement's "+
			"already-issued Create and Route must roll back with everything else", len(remaining))
	}
}

// TestTodo_APPROVAL_007_Mutation proves the ClaimedBy threading this file's
// doc comment describes is real: without it, two requirements in the same
// set whose only eligible candidate is the same principal, under
// OneRequirementPerPrincipal, would both resolve to that principal instead of
// the second one excluding them for already filling the first.
func TestTodo_APPROVAL_007_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "approval007-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	store := workitem.Store{}

	scope := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: "org:overlap-test"}
	at := values.NewInstant(fixedInstant)
	source := humanwork.RequirementSource{
		Tier: rules.ApprovalTierStandard, TableID: "test.table", TableVersion: "v1",
		TableDigest: "digest.table.v1", MatchedRowID: "row.standard", GovernancePolicyRef: "policy.test/v1",
	}
	deadline := humanwork.Deadline{DecideBy: at, Expiry: values.NewInstant(fixedInstant.Add(48 * time.Hour))}
	sep := humanwork.SeparationConstraints{OneRequirementPerPrincipal: true, RuleID: "rule.one_per_principal/test"}
	esc := humanwork.EscalationPolicy{OnDeadline: humanwork.EscalationNotify, RuleID: "rule.notify/test"}
	invalidators := []humanwork.Invalidator{
		{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "rule.material_change/test"},
	}

	compileOverlap := func(id string, stage uint32) humanwork.ApprovalRequirement {
		req, err := humanwork.Compile(humanwork.RequirementSpec{
			RequirementID:  id,
			Revision:       1,
			Stage:          stage,
			Candidates:     humanwork.Role("role.overlap.test", scope),
			AuthorityFloor: []string{"role.overlap.test"},
			Quorum:         humanwork.Quorum{MinApprovals: 1},
			Deadline:       deadline,
			Escalation:     esc,
			Separation:     sep,
			Invalidators:   invalidators,
			Source:         source,
		})
		if err != nil {
			t.Fatalf("Compile %s: %v", id, err)
		}
		return req
	}

	set := humanwork.RequirementSet{
		Requirements: []humanwork.ApprovalRequirement{
			compileOverlap("req.overlap.a/v1", 1),
			compileOverlap("req.overlap.b/v1", 2),
		},
		Tier: rules.ApprovalTierStandard, DerivedAt: at, Source: source,
	}

	dir := humanwork.NewMemoryDirectory("directory.overlap-test/1")
	dir.WithHolders(humanwork.Term{Kind: humanwork.ExprRole, Role: "role.overlap.test", Scope: scope}, "principal:overlap")
	dir.WithPrincipal(humanwork.PrincipalFacts{
		PrincipalID: "principal:overlap", Active: true, Available: true,
		Roles: []string{"role.overlap.test"}, OrganizationScopeID: "org:overlap-test",
		IdentityAssuranceRef: "assurance.test/1",
	})
	clock := func() values.Instant { return at }

	in := workitem.MaterializationInput{
		Requirements: set,
		Proposal: workitem.ApprovalProposalBinding{
			TenantID: tenant, WorkflowInstanceID: instance, NodeID: "approval_node",
			CorrelationID: "corr-overlap", ProposalRef: "sha256:" + repeatHex("1"),
			SubjectRefs: []string{"worker:overlap-test"}, OrganizationScopeID: "org:overlap-test",
			PolicyRouteRef: "route.overlap-test/v1", CreatedAt: fixedInstant,
		},
		Resolution: humanwork.ResolutionInput{
			RequesterPrincipalID: "principal:requester-overlap",
			SubjectPrincipalIDs:  []string{"worker:overlap-test"},
			EffectiveAt:          at,
			ClaimedBy:            map[string]string{},
		},
		Directory:        dir,
		Clock:            clock,
		WorkItemID:       deterministicWorkItemID("mutation-overlap"),
		ActorPrincipalID: "system:approval-materializer",
	}

	var items []workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		items, err = workitem.MaterializeApprovalRequirements(ctx, tx, store, in)
		return err
	})
	if len(items) != 2 {
		t.Fatalf("materialized %d items, want 2", len(items))
	}

	first, second := items[0], items[1]
	if first.Status != workitem.StatusAssigned || first.OwnerRef != "principal:overlap" {
		t.Fatalf("first slot = %s/%s, want ASSIGNED/principal:overlap", first.Status, first.OwnerRef)
	}
	if second.Status != workitem.StatusEscalated || second.OwnerKind != workitem.OwnerPolicyRoute {
		t.Fatalf("second slot = %s/%s, want ESCALATED/POLICY_ROUTE -- the principal filling the first "+
			"slot must be excluded from the second under one-requirement-per-principal, not routed to "+
			"them again", second.Status, second.OwnerKind)
	}
	foundDualRole := false
	for _, e := range second.Assignment.Resolution.Excluded {
		if e.PrincipalID == "principal:overlap" && e.RuleID == humanwork.RuleDualRole {
			foundDualRole = true
		}
	}
	if !foundDualRole {
		t.Fatalf("second slot's exclusions do not cite RuleDualRole against the principal already filling "+
			"the first slot: %+v", second.Assignment.Resolution.Excluded)
	}
}
