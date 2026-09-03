package workitem_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture stamps. This package never reads a
// wall clock, so a test that wants a time has to say which one.
var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// insertInstance seeds the one row work_item's foreign key needs: a workflow
// instance. Its own columns are irrelevant to this package's tests, so the
// values are fixed placeholders rather than a compiled plan.
func insertInstance(t *testing.T, db *pgtest.DB, tenant, instanceID uuid.UUID) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.promotion', 1,
			'`+zeroDigest+`', 'SIMULATE', 'RUNNING', 'sha256:input',
			ARRAY['approval_node'], $3, $4)`,
		tenant, instanceID, "corr-"+instanceID.String(), fixedInstant)
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, the only way a test observes the row
// level security policies migration 00017 declares.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTx runs fn inside its own transaction on conn and commits it.
func inTx(t *testing.T, conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// inTxErr is inTx for a call whose own error the test wants to inspect. A
// failing fn rolls the transaction back, so a refused write leaves nothing
// behind.
func inTxErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// inTenantTx is inTx with the tenant scope set as its first statement.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func timePtr(t time.Time) *time.Time { return &t }

// newTaskInput builds a well-formed NewWorkItemInput for a plain TASK work
// item awaiting an instance's approval_node, ready for [workitem.NewWorkItem].
func newTaskInput(tenant, instance uuid.UUID) workitem.NewWorkItemInput {
	return workitem.NewWorkItemInput{
		TenantID:            tenant,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.promotion.review/v1",
		CorrelationID:       "corr-" + instance.String(),
		WorkflowInstanceID:  instance,
		NodeID:              "approval_node",
		SubjectRefs:         []string{"worker:jane"},
		PolicyRouteRef:      "route.promotion.current_manager/v1",
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: "org:acme-eu:engineering",
		DeadlineAt:          fixedInstant.Add(48 * time.Hour),
		CreatedAt:           fixedInstant,
	}
}

// newApprovalInput builds a well-formed NewWorkItemInput for an APPROVAL work
// item deciding requirementRef.
func newApprovalInput(tenant, instance uuid.UUID, requirementRef string) workitem.NewWorkItemInput {
	in := newTaskInput(tenant, instance)
	in.Visibility = workitem.VisibilityCandidateSet
	return in
}

func meta(reason string) workitem.TransitionMeta {
	return workitem.TransitionMeta{
		ActorPrincipalID: "principal:test-actor",
		Reason:           reason,
		At:               fixedInstant,
	}
}

// The WORK-001/003 lifecycle tests need an [humanwork.Resolution] to route
// against but are not themselves testing resolution, so these three build one
// directly rather than deriving it through the full humanwork resolver. The
// WORK-002 tests below use the real resolver instead, through
// humanwork.PromotionDirectory and humanwork.Resolve.

func singleCandidateResolution(principal string) humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID:     "req.test/v1",
		Outcome:           humanwork.OutcomeResolved,
		Candidates:        []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "term.test"}},
		ResolvedAt:        values.NewInstant(fixedInstant),
		EffectiveAt:       values.NewInstant(fixedInstant),
		DirectoryVersion:  "directory.test/1",
		ExpressionDigest:  "sha256:" + repeatHex("1"),
		RequirementDigest: "sha256:" + repeatHex("2"),
		QuorumRequired:    1,
	}
}

func multiCandidateResolution(principals ...string) humanwork.Resolution {
	sorted := append([]string(nil), principals...)
	sort.Strings(sorted)
	candidates := make([]humanwork.Candidate, len(sorted))
	for i, p := range sorted {
		candidates[i] = humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect, TermRef: "term.test"}
	}
	return humanwork.Resolution{
		RequirementID:     "req.test/v1",
		Outcome:           humanwork.OutcomeResolved,
		Candidates:        candidates,
		ResolvedAt:        values.NewInstant(fixedInstant),
		EffectiveAt:       values.NewInstant(fixedInstant),
		DirectoryVersion:  "directory.test/1",
		ExpressionDigest:  "sha256:" + repeatHex("3"),
		RequirementDigest: "sha256:" + repeatHex("4"),
		QuorumRequired:    1,
	}
}

func noAuthorizedApproverResolution() humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID: "req.test/v1",
		Outcome:       humanwork.OutcomeNoAuthorizedApprover,
		Excluded: []humanwork.Exclusion{
			{PrincipalID: "principal:manager-1", RuleID: humanwork.RulePrincipalInactive, Reason: "principal is not an active actor"},
		},
		ResolvedAt:        values.NewInstant(fixedInstant),
		EffectiveAt:       values.NewInstant(fixedInstant),
		DirectoryVersion:  "directory.test/1",
		ExpressionDigest:  "sha256:" + repeatHex("5"),
		RequirementDigest: "sha256:" + repeatHex("6"),
		QuorumRequired:    1,
	}
}

func repeatHex(digit string) string {
	out := ""
	for range 64 {
		out += digit
	}
	return out
}
