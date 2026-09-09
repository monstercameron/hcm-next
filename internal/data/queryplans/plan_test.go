package queryplans_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/queryplans"
)

const seqScanPlanJSON = `[
  {
    "Plan": {
      "Node Type": "Seq Scan",
      "Relation Name": "journey_worker",
      "Alias": "journey_worker"
    }
  }
]`

const indexScanPlanJSON = `[
  {
    "Plan": {
      "Node Type": "Index Scan",
      "Relation Name": "journey_worker",
      "Index Name": "journey_worker_recorded"
    }
  }
]`

const partitionedAppendPlanJSON = `[
  {
    "Plan": {
      "Node Type": "Append",
      "Plans": [
        {
          "Node Type": "Index Scan",
          "Relation Name": "ledger_event_p0",
          "Index Name": "ledger_event_p0_pkey"
        },
        {
          "Node Type": "Seq Scan",
          "Relation Name": "ledger_event_p1"
        }
      ]
    }
  }
]`

// TestTodo_DB_020_ParseExplainJSON_RootPlan proves the root plan node decodes
// with its own node type intact.
func TestTodo_DB_020_ParseExplainJSON_RootPlan(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(seqScanPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	if plan.NodeType != "Seq Scan" {
		t.Errorf("NodeType = %q, want Seq Scan", plan.NodeType)
	}
	if plan.RelationName != "journey_worker" {
		t.Errorf("RelationName = %q, want journey_worker", plan.RelationName)
	}
}

func TestParseExplainJSON_EmptyArrayIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := queryplans.ParseExplainJSON(`[]`); err == nil {
		t.Fatal("ParseExplainJSON(`[]`) = nil error, want one")
	}
}

func TestParseExplainJSON_MalformedTextIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := queryplans.ParseExplainJSON(`not json`); err == nil {
		t.Fatal("ParseExplainJSON on malformed text = nil error, want one")
	}
}

// TestTodo_DB_020_HasSeqScanOn_DetectsRootSeqScan proves the RED case: a bare
// sequential scan on the table under proof is detected.
func TestTodo_DB_020_HasSeqScanOn_DetectsRootSeqScan(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(seqScanPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	if !plan.HasSeqScanOn("journey_worker") {
		t.Error("HasSeqScanOn(journey_worker) = false, want true")
	}
	if plan.HasSeqScanOn("job_partition") {
		t.Error("HasSeqScanOn(job_partition) = true on an unrelated table, want false")
	}
}

// TestHasSeqScanOn_IndexScanIsNotASeqScan proves the GREEN case: an index
// scan node never counts as a sequential scan, even on the same table.
func TestHasSeqScanOn_IndexScanIsNotASeqScan(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(indexScanPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	if plan.HasSeqScanOn("journey_worker") {
		t.Error("HasSeqScanOn(journey_worker) = true for an Index Scan plan, want false")
	}
}

// TestUsesIndexContaining_MatchesBySubstring proves the substring contract
// UsesIndexContaining documents: a caller names a fragment of an index name
// (e.g. the shared prefix of every hash partition's own index), not the
// exact per-partition name.
func TestUsesIndexContaining_MatchesBySubstring(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(indexScanPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	if !plan.UsesIndexContaining("journey_worker_recorded") {
		t.Error("UsesIndexContaining(exact name) = false, want true")
	}
	if !plan.UsesIndexContaining("nonexistent", "recorded") {
		t.Error("UsesIndexContaining(one matching substring among several) = false, want true")
	}
	if plan.UsesIndexContaining("no_such_index") {
		t.Error("UsesIndexContaining(unrelated substring) = true, want false")
	}
}

// TestPartitionedPlan_SeqScanOnOnePartitionIsDetected proves belongsToTable's
// partition-prefix rule: ledger_event_p1 counts as ledger_event for the
// purpose of "did any partition get sequentially scanned", because a request
// scoped to one tenant lands on exactly one hash partition and that
// partition's own relation name is what the plan reports, never the parent's.
func TestPartitionedPlan_SeqScanOnOnePartitionIsDetected(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(partitionedAppendPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	if !plan.HasSeqScanOn("ledger_event") {
		t.Error("HasSeqScanOn(ledger_event) = false with a Seq Scan on ledger_event_p1, want true")
	}
	if !plan.UsesIndexContaining("_pkey") {
		t.Error("UsesIndexContaining(_pkey) = false with an Index Scan using ledger_event_p0_pkey, want true")
	}
}

// TestShape_DropsEverythingButNodeRelationAndIndex proves the golden's own
// contract: Shape never carries a cost, row estimate or timing field, so a
// checked-in golden built from it never moves when only an estimate does.
func TestShape_DropsEverythingButNodeRelationAndIndex(t *testing.T) {
	t.Parallel()
	plan, err := queryplans.ParseExplainJSON(partitionedAppendPlanJSON)
	if err != nil {
		t.Fatalf("ParseExplainJSON: %v", err)
	}
	shape := plan.Shape()
	if shape.NodeType != "Append" {
		t.Fatalf("root Shape.NodeType = %q, want Append", shape.NodeType)
	}
	if len(shape.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(shape.Children))
	}
	if shape.Children[0].IndexName != "ledger_event_p0_pkey" {
		t.Errorf("Children[0].IndexName = %q, want ledger_event_p0_pkey", shape.Children[0].IndexName)
	}
	if shape.Children[1].NodeType != "Seq Scan" || shape.Children[1].RelationName != "ledger_event_p1" {
		t.Errorf("Children[1] = %+v, want the Seq Scan on ledger_event_p1", shape.Children[1])
	}
}

// fakeRow implements dbport.Row over one fixed string, so Explain can be
// unit-tested without a database.
type fakeRow struct {
	value string
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	ptr, ok := dest[0].(*string)
	if !ok {
		return errors.New("fakeRow: dest[0] is not *string")
	}
	*ptr = r.value
	return nil
}

// fakeQuerier records the statement it was asked to run and answers with a
// fixed row, so Explain's own SQL-text prefixing can be proven without a
// database.
type fakeQuerier struct {
	gotSQL  string
	gotArgs []any
	row     fakeRow
}

func (f *fakeQuerier) Query(ctx context.Context, sqlText string, args ...any) (dbport.Rows, error) {
	return nil, errors.New("fakeQuerier: Query is not implemented")
}

func (f *fakeQuerier) QueryRow(ctx context.Context, sqlText string, args ...any) dbport.Row {
	f.gotSQL = sqlText
	f.gotArgs = args
	return f.row
}

func TestExplain_PrefixesTheStatementAndForwardsArgs(t *testing.T) {
	t.Parallel()
	q := &fakeQuerier{row: fakeRow{value: indexScanPlanJSON}}
	plan, err := queryplans.Explain(context.Background(), q, "SELECT 1 FROM journey_worker WHERE tenant_id = $1", []any{"tenant-a"})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan.NodeType != "Index Scan" {
		t.Errorf("Explain returned NodeType = %q, want Index Scan (the fake row's own plan)", plan.NodeType)
	}
	const want = "EXPLAIN (FORMAT JSON) SELECT 1 FROM journey_worker WHERE tenant_id = $1"
	if q.gotSQL != want {
		t.Errorf("statement sent = %q, want %q", q.gotSQL, want)
	}
	if len(q.gotArgs) != 1 || q.gotArgs[0] != "tenant-a" {
		t.Errorf("args forwarded = %v, want [tenant-a]", q.gotArgs)
	}
}

// TestShape_NormalizedCollapsesPartitionSuffixes proves the golden-stability
// contract Normalized documents: two shapes that differ only in which
// hash partition a randomly generated tenant landed on compare equal after
// normalization.
func TestShape_NormalizedCollapsesPartitionSuffixes(t *testing.T) {
	t.Parallel()
	a := queryplans.Shape{NodeType: "Index Scan", RelationName: "ledger_event_p0", IndexName: "ledger_event_p0_pkey"}
	b := queryplans.Shape{NodeType: "Index Scan", RelationName: "ledger_event_p3", IndexName: "ledger_event_p3_pkey"}
	na, nb := a.Normalized(), b.Normalized()
	if !reflect.DeepEqual(na, nb) {
		t.Fatalf("Normalized() did not collapse partition suffixes: %+v vs %+v", na, nb)
	}
	if na.RelationName != "ledger_event_pN" || na.IndexName != "ledger_event_pN_pkey" {
		t.Errorf("Normalized() = %+v, want relation/index rewritten to the _pN placeholder", na)
	}
}

// TestShape_NormalizedCollapsesBitmapScanIntoIndexScan proves the golden-
// stability contract for the other source of run-to-run noise Normalized
// documents: a Bitmap Heap Scan built from one Bitmap Index Scan compares
// equal, after normalization, to a plain Index Scan through the same index.
func TestShape_NormalizedCollapsesBitmapScanIntoIndexScan(t *testing.T) {
	t.Parallel()
	bitmap := queryplans.Shape{
		NodeType:     "Bitmap Heap Scan",
		RelationName: "journey_worker",
		Children: []queryplans.Shape{
			{NodeType: "Bitmap Index Scan", IndexName: "journey_worker_recorded"},
		},
	}
	plain := queryplans.Shape{
		NodeType:     "Index Scan",
		RelationName: "journey_worker",
		IndexName:    "journey_worker_recorded",
	}
	if !reflect.DeepEqual(bitmap.Normalized(), plain.Normalized()) {
		t.Fatalf("Normalized() did not collapse the bitmap scan pair: %+v vs %+v",
			bitmap.Normalized(), plain.Normalized())
	}
}

// TestShape_NormalizedRenamesIncrementalSort proves the third golden-
// stability rule: Incremental Sort and Sort compare equal after
// normalization when they wrap the same child.
func TestShape_NormalizedRenamesIncrementalSort(t *testing.T) {
	t.Parallel()
	child := queryplans.Shape{NodeType: "Index Scan", RelationName: "journey_worker", IndexName: "journey_worker_recorded"}
	incremental := queryplans.Shape{NodeType: "Incremental Sort", Children: []queryplans.Shape{child}}
	plain := queryplans.Shape{NodeType: "Sort", Children: []queryplans.Shape{child}}
	if !reflect.DeepEqual(incremental.Normalized(), plain.Normalized()) {
		t.Fatalf("Normalized() did not equate Incremental Sort and Sort: %+v vs %+v",
			incremental.Normalized(), plain.Normalized())
	}
}

func TestExplain_ScanFailureIsReported(t *testing.T) {
	t.Parallel()
	q := &fakeQuerier{row: fakeRow{err: errors.New("boom")}}
	if _, err := queryplans.Explain(context.Background(), q, "SELECT 1", nil); err == nil {
		t.Fatal("Explain with a failing Scan = nil error, want one")
	}
}
