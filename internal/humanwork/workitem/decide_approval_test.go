package workitem_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// newApprovalInput builds a well-formed NewWorkItemInput for an APPROVAL work
// item bound to proposalRef, ready for [workitem.NewWorkItem]. It mirrors
// newTaskInput (fixtures_test.go) exactly except for the fields WORK-001's
// own CHECK constraints require an approval to carry.
func newApprovalInput(tenant, instance uuid.UUID, proposalRef string) workitem.NewWorkItemInput {
	in := newTaskInput(tenant, instance)
	in.Kind = workitem.KindApproval
	in.ApprovalRequirementRef = "req.promotion.manager-approval/v1"
	in.ProposalRef = proposalRef
	return in
}

// approvalItem creates, routes, claims and starts one APPROVAL work item
// bound to proposalRef for "principal:decider", leaving it IN_PROGRESS --
// [workitem.PermittedActions]' own precondition for ActionDecideApproval --
// and ready for [workitem.Store.DecideApproval].
func approvalItem(t *testing.T, proposalRef string) (workitem.WorkItem, *pgxFixture) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "ep-work-003-approval")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	item, err := workitem.NewWorkItem(newApprovalInput(tenant, instance, proposalRef))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	store := workitem.Store{}
	resolution := singleCandidateResolution("principal:decider")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(context.Background(), tx, item, meta("created"))
		if err != nil {
			return err
		}
		item, err = store.Route(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, Trigger: workitem.TriggerInitialRouting}, meta("routed"))
		if err != nil {
			return err
		}
		item, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "principal:decider", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
			Meta: meta("claimed"),
		})
		if err != nil {
			return err
		}
		item, err = store.Start(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("started"))
		return err
	})
	if item.Status != workitem.StatusInProgress {
		t.Fatalf("fixture item status = %s, want IN_PROGRESS", item.Status)
	}
	return item, &pgxFixture{db: db, conn: conn, tenant: tenant}
}

func decideOn(conn *pgxadapter.Conn, tenant uuid.UUID, item workitem.WorkItem, proposalRef string,
	decision workitem.ApprovalDecision, authority workitem.AuthorityRecheckPort, sessions workitem.SessionRevocationPort,
) (workitem.WorkItem, error) {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return workitem.WorkItem{}, err
	}
	decided, err := (workitem.Store{}).DecideApproval(ctx, tx, workitem.DecideApprovalInput{
		TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
		ProposalRevisionRef: proposalRef, Decision: decision, ReasonRef: "reason.sufficient-evidence/v1",
		DecidingPrincipal: "principal:decider", SessionRef: "session:test/v1", Session: sessions, Authority: authority,
		Now: fixedInstant, Meta: meta("decided"),
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return workitem.WorkItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return workitem.WorkItem{}, err
	}
	return decided, nil
}

func decide(t *testing.T, f *pgxFixture, item workitem.WorkItem, proposalRef string,
	decision workitem.ApprovalDecision, authority workitem.AuthorityRecheckPort, sessions workitem.SessionRevocationPort,
) (workitem.WorkItem, error) {
	t.Helper()
	return decideOn(f.conn, f.tenant, item, proposalRef, decision, authority, sessions)
}

// TestTodo_EP_WORK_003 is the domain-level primary: a current-authority
// approval decision bound to the item's exact proposal revision completes the
// work item exactly once and records nothing beyond the one COMPLETED
// transition every other completion already produces.
func TestTodo_EP_WORK_003(t *testing.T) {
	proposal := "sha256:" + repeatHex("3")
	item, f := approvalItem(t, proposal)
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{}

	decided, err := decide(t, f, item, proposal, workitem.ApprovalDecisionApprove, authority, sessions)
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	if decided.Status != workitem.StatusCompleted || decided.CompletedBy != "principal:decider" {
		t.Fatalf("decided item = %+v", decided)
	}
	wantDigest := workitem.DecisionDigest(item.WorkItemID, proposal, workitem.ApprovalDecisionApprove, "reason.sufficient-evidence/v1")
	if decided.CompletedOutputDigest != wantDigest {
		t.Fatalf("completed output digest = %q, want %q", decided.CompletedOutputDigest, wantDigest)
	}
	if authority.callCount() != 1 || sessions.callCount() != 1 {
		t.Fatalf("authority calls=%d session calls=%d, want 1 and 1", authority.callCount(), sessions.callCount())
	}

	result := workitem.NewDecisionResult(decided, proposal, workitem.ApprovalDecisionApprove, "reason.sufficient-evidence/v1")
	if result.DecidingPrincipal != "principal:decider" || result.DecidedAt.IsZero() || result.Decision != workitem.ApprovalDecisionApprove {
		t.Fatalf("decision result = %+v", result)
	}

	trail, err := loadTransitions(t, f.conn, f.tenant, item.WorkItemID)
	if err != nil {
		t.Fatalf("LoadTransitions: %v", err)
	}
	completedRows := 0
	for _, tr := range trail {
		if tr.ToStatus == workitem.StatusCompleted {
			completedRows++
		}
	}
	if completedRows != 1 {
		t.Fatalf("%d COMPLETED transition rows recorded, want exactly 1", completedRows)
	}
}

// TestTodo_EP_WORK_003_Conformance covers the stale task/proposal digest
// clause and the kind guard: a caller-asserted proposal revision that no
// longer matches the item's current one refuses with current-safe data
// (STALE_PROPOSAL, no domain write), and DecideApproval never applies to a
// plain TASK item.
func TestTodo_EP_WORK_003_Conformance(t *testing.T) {
	t.Run("stale proposal revision refuses and mutates nothing", func(t *testing.T) {
		current := "sha256:" + repeatHex("4")
		item, f := approvalItem(t, current)
		authority := &authorityRecheckDouble{allowed: true}
		sessions := &sessionRecheckDouble{}
		stale := "sha256:" + repeatHex("5")
		_, err := decide(t, f, item, stale, workitem.ApprovalDecisionApprove, authority, sessions)
		if workitem.CodeOf(err) != workitem.CodeStaleProposal {
			t.Fatalf("error = %v, code=%q, want %q", err, workitem.CodeOf(err), workitem.CodeStaleProposal)
		}
		if authority.callCount() != 0 || sessions.callCount() != 0 {
			t.Fatalf("a stale-proposal decision reached authority/session checks: authority=%d session=%d",
				authority.callCount(), sessions.callCount())
		}
		assertWorkItemUnchanged(t, f, item)
	})

	t.Run("a task item is never decided as an approval", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := insertTenant(t, db, "ep-work-003-wrong-kind")
		instance := uuid.New()
		insertInstance(t, db, tenant, instance)
		conn := appConn(t, db)
		item, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		store := workitem.Store{}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Create(context.Background(), tx, item, meta("created"))
			if err != nil {
				return err
			}
			item, err = store.Route(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: singleCandidateResolution("principal:decider"), Trigger: workitem.TriggerInitialRouting}, meta("routed"))
			if err != nil {
				return err
			}
			item, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:decider", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("claimed"),
			})
			if err != nil {
				return err
			}
			item, err = store.Start(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("started"))
			return err
		})
		f := &pgxFixture{db: db, conn: conn, tenant: tenant}
		authority := &authorityRecheckDouble{allowed: true}
		sessions := &sessionRecheckDouble{}
		_, err = decide(t, f, item, "sha256:"+repeatHex("6"), workitem.ApprovalDecisionApprove, authority, sessions)
		if workitem.CodeOf(err) != workitem.CodeIllegalTransition {
			t.Fatalf("error = %v, code=%q, want %q", err, workitem.CodeOf(err), workitem.CodeIllegalTransition)
		}
		if authority.callCount() != 0 {
			t.Fatalf("a wrong-kind decision reached the authority check: calls=%d", authority.callCount())
		}
	})
}

// TestTodo_EP_WORK_003_Security proves the current-authority recheck is not
// bypassable for an approval decision: a denied recheck (standing in for a
// changed manager, a revoked delegation, or -- composed by the driver that
// injects [workitem.AuthorityRecheckPort] -- the requester attempting to
// decide their own proposal) refuses CodeAuthorityChanged and leaves the item
// exactly as it was.
func TestTodo_EP_WORK_003_Security(t *testing.T) {
	proposal := "sha256:" + repeatHex("7")
	item, f := approvalItem(t, proposal)
	authority := &authorityRecheckDouble{allowed: false}
	sessions := &sessionRecheckDouble{}
	_, err := decide(t, f, item, proposal, workitem.ApprovalDecisionApprove, authority, sessions)
	if workitem.CodeOf(err) != workitem.CodeAuthorityChanged {
		t.Fatalf("error = %v, code=%q, want %q", err, workitem.CodeOf(err), workitem.CodeAuthorityChanged)
	}
	if authority.callCount() != 1 || sessions.callCount() != 1 {
		t.Fatalf("authority calls=%d session calls=%d, want 1 and 1", authority.callCount(), sessions.callCount())
	}
	assertWorkItemUnchanged(t, f, item)
}

// TestTodo_EP_WORK_003_Fault proves an expired/revoked session refuses a
// decision before authority is even consulted, exactly as
// [workitem.CompleteWithAuthorityRecheck] already guarantees for a plain
// completion.
func TestTodo_EP_WORK_003_Fault(t *testing.T) {
	proposal := "sha256:" + repeatHex("8")
	item, f := approvalItem(t, proposal)
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{err: sessionRevokedError{}}
	_, err := decide(t, f, item, proposal, workitem.ApprovalDecisionApprove, authority, sessions)
	if workitem.CodeOf(err) != workitem.CodeSessionRevoked {
		t.Fatalf("error = %v, code=%q, want %q", err, workitem.CodeOf(err), workitem.CodeSessionRevoked)
	}
	if authority.callCount() != 0 {
		t.Fatalf("a revoked session still reached the authority check: calls=%d", authority.callCount())
	}
	assertWorkItemUnchanged(t, f, item)
}

// TestTodo_EP_WORK_003_Mutation proves the digest formula, not just a
// generic hash: changing any one of proposal revision, decision or reason
// changes the digest, so a replay under the same idempotency key but a
// mutated decision is guaranteed to produce a different
// CompletedOutputDigest for internal/transport/endpoint's payload-conflict
// check to catch -- the digest side of "a duplicate/mutated decision must
// never complete".
func TestTodo_EP_WORK_003_Mutation(t *testing.T) {
	workItemID := uuid.New()
	base := workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.a/v1")
	cases := map[string]string{
		"changed proposal": workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("2"), workitem.ApprovalDecisionApprove, "reason.a/v1"),
		"changed decision": workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionReject, "reason.a/v1"),
		"changed reason":   workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.b/v1"),
		"changed item":     workitem.DecisionDigest(uuid.New(), "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.a/v1"),
	}
	for name, got := range cases {
		if got == base {
			t.Fatalf("%s: digest unchanged (%q); a mutated decision must never share the original's digest", name, got)
		}
	}
	// Identical inputs are stable: replaying the exact same decision must
	// reproduce the exact same digest, so an honest exact replay is
	// recognized as a replay rather than a fresh write.
	again := workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.a/v1")
	if again != base {
		t.Fatalf("identical inputs produced different digests: %q vs %q", again, base)
	}
}

// TestTodo_EP_WORK_003_Golden pins the exact bytes of a decision digest and
// the exact shape of the returned [workitem.DecisionResult] for fixed input,
// guarding against an accidental change to either formula.
func TestTodo_EP_WORK_003_Golden(t *testing.T) {
	workItemID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	digest := workitem.DecisionDigest(workItemID, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.golden/v1")
	// DecisionDigest's exact bytes for this fixed input, transcribed once so
	// a change to the formula (schema tag, field order, separator) is caught
	// even though it would still produce a well-formed sha256:<hex> string.
	const wantDigest = "sha256:8118d8cbd60ce7d1ee610d45d41079fa2337ff175862572d9d8424dfcc7dcfd6"
	if digest != wantDigest {
		t.Fatalf("decision digest = %q, want %q", digest, wantDigest)
	}

	completedAt := fixedInstant
	item := workitem.WorkItem{
		WorkItemID: workItemID, Status: workitem.StatusCompleted,
		CompletedBy: "principal:decider", CompletedAt: &completedAt, CompletedOutputDigest: digest,
	}
	result := workitem.NewDecisionResult(item, "sha256:"+repeatHex("1"), workitem.ApprovalDecisionApprove, "reason.golden/v1")
	golden := result.WorkItemID.String() + "|" + result.ProposalRevisionRef + "|" + string(result.Decision) + "|" +
		result.ReasonRef + "|" + result.DecidingPrincipal + "|" + result.DecidedAt.Format(time.RFC3339)
	wantGolden := workItemID.String() + "|sha256:" + repeatHex("1") + "|APPROVE|reason.golden/v1|principal:decider|" + fixedInstant.Format(time.RFC3339)
	if golden != wantGolden {
		t.Fatalf("decision result golden = %q, want %q", golden, wantGolden)
	}
}

// TestTodo_EP_WORK_003_Property sweeps every declared [workitem.ApprovalDecision]
// against a fixed work item and proposal revision: every declared decision
// digest is well-formed and unique to that decision, and the zero value
// ([workitem.ApprovalDecisionUnspecified]) is never a decision
// [workitem.Store.DecideApproval] accepts -- a Go zero value must never mean
// "decided".
func TestTodo_EP_WORK_003_Property(t *testing.T) {
	proposal := "sha256:" + repeatHex("2")
	item, f := approvalItem(t, proposal)
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{}

	if workitem.ApprovalDecisionUnspecified.Valid() {
		t.Fatal("the zero-value decision validates; a Go zero value must never mean decided")
	}
	_, err := decide(t, f, item, proposal, workitem.ApprovalDecisionUnspecified, authority, sessions)
	if workitem.CodeOf(err) != workitem.CodeInvalidRecord {
		t.Fatalf("unspecified decision error = %v, code=%q, want %q", err, workitem.CodeOf(err), workitem.CodeInvalidRecord)
	}
	if authority.callCount() != 0 {
		t.Fatalf("an unspecified decision reached the authority check: calls=%d", authority.callCount())
	}

	seen := map[string]workitem.ApprovalDecision{}
	for _, d := range []workitem.ApprovalDecision{
		workitem.ApprovalDecisionApprove, workitem.ApprovalDecisionReject,
		workitem.ApprovalDecisionRequestMoreInformation, workitem.ApprovalDecisionAbstain,
	} {
		if !d.Valid() {
			t.Fatalf("declared decision %q does not validate", d)
		}
		digest := workitem.DecisionDigest(item.WorkItemID, proposal, d, "reason.sweep/v1")
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			t.Fatalf("decision %q digest = %q, not well-formed", d, digest)
		}
		if prior, dup := seen[digest]; dup {
			t.Fatalf("decisions %q and %q share a digest", d, prior)
		}
		seen[digest] = d
	}
}

// TestTodo_EP_WORK_003_Integration exercises the full lifecycle through real
// Postgres -- Create, Route, Claim, Start, DecideApproval -- and proves EP-
// WORK-003's sharpest clause end to end: a domain-mutation counter wrapping
// the same [dbport.Tx] the decision runs in stays at zero for every table
// except work_item and work_item_transition, and even against those two
// tables performs exactly the one UPDATE and one INSERT any other completion
// already performs. Approving therefore never IS the business change; it
// only records the decision and its signal (the appended COMPLETED
// transition), which is what a workflow reads later to run the actual
// change.
func TestTodo_EP_WORK_003_Integration(t *testing.T) {
	proposal := "sha256:" + repeatHex("9")
	item, f := approvalItem(t, proposal)
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{}

	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	counting := &countingExecutor{Tx: tx}
	decided, err := (workitem.Store{}).DecideApproval(ctx, counting, workitem.DecideApprovalInput{
		TenantID: f.tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
		ProposalRevisionRef: proposal, Decision: workitem.ApprovalDecisionApprove, ReasonRef: "reason.integration/v1",
		DecidingPrincipal: "principal:decider", SessionRef: "session:test/v1", Session: sessions, Authority: authority,
		Now: fixedInstant, Meta: meta("decided"),
	})
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if decided.Status != workitem.StatusCompleted {
		t.Fatalf("decided status = %s, want COMPLETED", decided.Status)
	}

	// The zero-domain-mutation proof: every mutating statement this call
	// issued touched only work_item or work_item_transition, and touched
	// work_item exactly once (the version-guarded UPDATE) and
	// work_item_transition exactly once (the append-only INSERT). Any write
	// to a table this test does not name -- a ledger, a budget reservation,
	// an org-chart row, anything belonging to the business change a decision
	// authorizes -- would fail this assertion.
	for table, count := range counting.writes {
		if table != "work_item" && table != "work_item_transition" {
			t.Fatalf("DecideApproval wrote to unexpected table %q (%d statements); the business change must never run synchronously here", table, count)
		}
	}
	if counting.writes["work_item"] != 1 {
		t.Fatalf("work_item writes = %d, want exactly 1", counting.writes["work_item"])
	}
	if counting.writes["work_item_transition"] != 1 {
		t.Fatalf("work_item_transition writes = %d, want exactly 1", counting.writes["work_item_transition"])
	}

	trail, err := loadTransitions(t, f.conn, f.tenant, item.WorkItemID)
	if err != nil {
		t.Fatalf("LoadTransitions: %v", err)
	}
	completedRows := 0
	for _, tr := range trail {
		if tr.ToStatus == workitem.StatusCompleted {
			completedRows++
		}
	}
	if completedRows != 1 {
		t.Fatalf("%d COMPLETED transition rows, want exactly 1", completedRows)
	}
}
