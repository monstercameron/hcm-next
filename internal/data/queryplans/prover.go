package queryplans

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Proof is one catalogue entry's result: the exact statement the real store
// method sent, the plan PostgreSQL chose for it, and the two properties
// DB-020 asks every critical query to hold.
type Proof struct {
	Entry Entry
	Call  Call
	Plan  PlanNode

	// SeqScanOnTable is true if the plan sequentially scans Entry.Table (or,
	// for a hash-partitioned table, one of its partitions).
	SeqScanOnTable bool
	// UsesExpectedIndex is true if the plan uses an index whose name contains
	// one of Entry.ExpectedIndexSubstrings.
	UsesExpectedIndex bool
}

// OK reports whether the proof holds: no sequential scan on the table under
// proof, and the declared index is what the plan actually used.
func (p Proof) OK() bool {
	return !p.SeqScanOnTable && p.UsesExpectedIndex
}

// Prove seeds e's table to e.RowThreshold under tenant on seedConn, then
// drives e's real store call through appConn -- which must already be
// scoped to the hcmnext_app role and to tenant's app.tenant_id, the same way
// a production request-scoped connection reaches these tables -- and
// explains the exact statement that call sent.
//
// seedConn and appConn are deliberately two different capabilities, not one
// connection asked to play both parts: seeding needs to write past
// migrations/00001_platform_control.sql's forbid_mutation triggers and
// past whatever INSERT/UPDATE grant hcmnext_app does or does not hold on a
// given table, so it runs as the migration-owning role; the plan this
// package reports has to be the one hcmnext_app actually gets, under row
// level security, or it proves nothing about the path a real request takes.
func Prove(ctx context.Context, seedConn, appConn dbport.Conn, tenant uuid.UUID, e Entry) (Proof, error) {
	threshold := e.RowThreshold
	if threshold <= 0 {
		threshold = DefaultRowThreshold
	}
	run, err := e.Prepare(ctx, seedConn, tenant, threshold)
	if err != nil {
		return Proof{}, fmt.Errorf("queryplans: prepare %s: %w", e.Name, err)
	}

	spy := NewSpy(appConn)
	if err := run(ctx, spy); err != nil {
		return Proof{}, fmt.Errorf("queryplans: run %s: %w", e.Name, err)
	}
	call, ok := spy.CallAgainst(e.Table)
	if !ok {
		return Proof{}, fmt.Errorf("queryplans: %s issued no statement naming %s (observed %d statement(s))",
			e.Name, e.Table, len(spy.Calls))
	}

	plan, err := Explain(ctx, appConn, call.SQL, call.Args)
	if err != nil {
		return Proof{}, fmt.Errorf("queryplans: explain %s: %w", e.Name, err)
	}

	return Proof{
		Entry:             e,
		Call:              call,
		Plan:              plan,
		SeqScanOnTable:    plan.HasSeqScanOn(e.Table),
		UsesExpectedIndex: plan.UsesIndexContaining(e.ExpectedIndexSubstrings...),
	}, nil
}
