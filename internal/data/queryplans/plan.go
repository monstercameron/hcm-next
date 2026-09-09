package queryplans

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PlanNode is one node of a PostgreSQL EXPLAIN (FORMAT JSON) plan tree, kept
// to exactly the fields this package reasons about. PostgreSQL's own JSON
// carries many more (Startup Cost, Total Cost, Plan Rows, ...); this package
// deliberately does not decode them; a plan's cost estimate is not evidence,
// its access path is, and the golden in testdata/plan_shapes.golden.json
// would be unusable as a checked-in artifact if it moved every time a row
// estimate changed by one.
type PlanNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name,omitempty"`
	IndexName    string     `json:"Index Name,omitempty"`
	Alias        string     `json:"Alias,omitempty"`
	Plans        []PlanNode `json:"Plans,omitempty"`
}

// explainRow is the shape of one element of the top-level JSON array EXPLAIN
// (FORMAT JSON) returns.
type explainRow struct {
	Plan PlanNode `json:"Plan"`
}

// ParseExplainJSON decodes the text EXPLAIN (FORMAT JSON) produced and
// returns its root plan node.
func ParseExplainJSON(raw string) (PlanNode, error) {
	var rows []explainRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return PlanNode{}, fmt.Errorf("queryplans: parse EXPLAIN JSON: %w", err)
	}
	if len(rows) == 0 {
		return PlanNode{}, fmt.Errorf("queryplans: EXPLAIN (FORMAT JSON) returned no plan")
	}
	return rows[0].Plan, nil
}

// Explain runs "EXPLAIN (FORMAT JSON) "+sqlText with args over q and returns
// the parsed plan. It never executes the statement's side effects (no
// ANALYZE keyword): a read is explained without being run twice, and a
// write's plan can be inspected without applying it.
func Explain(ctx context.Context, q dbport.Querier, sqlText string, args []any) (PlanNode, error) {
	row := q.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sqlText, args...)
	var raw string
	if err := row.Scan(&raw); err != nil {
		return PlanNode{}, fmt.Errorf("queryplans: run EXPLAIN: %w", err)
	}
	return ParseExplainJSON(raw)
}

// Walk visits n and every descendant, depth-first, root first.
func (n PlanNode) Walk(fn func(PlanNode)) {
	fn(n)
	for _, child := range n.Plans {
		child.Walk(fn)
	}
}

// belongsToTable reports whether relation is table itself or one of its hash
// partitions (ledger_event's own naming convention: ledger_event_p0 ..
// ledger_event_p3). A partitioned table's plan names the child relation the
// planner actually touched, never the parent, so a check that only compared
// equality would never see a partitioned table's own sequential scan.
func belongsToTable(relation, table string) bool {
	if relation == table {
		return true
	}
	return strings.HasPrefix(relation, table+"_p")
}

// HasSeqScanOn reports whether the plan contains a Seq Scan node against
// table or one of its partitions.
func (n PlanNode) HasSeqScanOn(table string) bool {
	found := false
	n.Walk(func(p PlanNode) {
		if p.NodeType == "Seq Scan" && belongsToTable(p.RelationName, table) {
			found = true
		}
	})
	return found
}

// UsesIndexContaining reports whether the plan contains an index-based scan
// node (Index Scan, Index Only Scan or Bitmap Index Scan) whose index name
// contains at least one of substrings. Substring matching, rather than exact
// equality, is what lets one expectation cover every hash partition of a
// partitioned table's own per-partition index names.
func (n PlanNode) UsesIndexContaining(substrings ...string) bool {
	found := false
	n.Walk(func(p PlanNode) {
		if p.IndexName == "" || !strings.Contains(p.NodeType, "Index") {
			return
		}
		for _, s := range substrings {
			if s != "" && strings.Contains(p.IndexName, s) {
				found = true
				return
			}
		}
	})
	return found
}

// Shape is a plan node stripped to exactly the two properties this package
// checks: what kind of node it is, and which relation or index it names.
// It is the type the golden compares, on purpose excluding every cost,
// row-estimate or timing field EXPLAIN also reports, none of which is a
// property of the schema this todo proves.
type Shape struct {
	NodeType     string  `json:"node"`
	RelationName string  `json:"relation,omitempty"`
	IndexName    string  `json:"index,omitempty"`
	Children     []Shape `json:"children,omitempty"`
}

// Shape projects a PlanNode onto its [Shape].
func (n PlanNode) Shape() Shape {
	s := Shape{NodeType: n.NodeType, RelationName: n.RelationName, IndexName: n.IndexName}
	for _, child := range n.Plans {
		s.Children = append(s.Children, child.Shape())
	}
	return s
}

// partitionSuffix matches migrations/00005_ledger.sql's own hash-partition
// naming convention (ledger_event_p0 .. ledger_event_p3) on a relation or
// index name.
var partitionSuffix = regexp.MustCompile(`_p[0-9]+`)

// Normalized returns a copy of s with two run-to-run, non-schema sources of
// noise collapsed out:
//
//  1. A hash-partition suffix (ledger_event_p2, ledger_event_p2_pkey, ...) is
//     rewritten to "_pN". Which partition a test's own randomly generated
//     tenant UUID hashes to changes every run; the literal partition it
//     lands on is not a property of the plan shape this package checks.
//
//  2. A "Bitmap Heap Scan" node whose only child is a "Bitmap Index Scan" is
//     collapsed into one "Index Scan" node carrying the heap scan's own
//     relation and the bitmap scan's own index. PostgreSQL chooses between
//     walking an index directly and building a bitmap from it on a cost
//     estimate that can tip either way between two runs seeded with the same
//     shape of data; both answered the query through the same index, and
//     which of the two scan STRATEGIES it picked is not the property
//     [Entry.ExpectedIndexSubstrings] or this golden exists to pin.
//
//  3. "Incremental Sort" is renamed to "Sort", for the identical reason: it
//     is PostgreSQL exploiting an input already ordered by a leading index
//     column to sort only the tie-break columns, chosen over a plain Sort on
//     the same kind of cost tie-break, over the same input, to the same
//     output order.
func (s Shape) Normalized() Shape {
	var children []Shape
	for _, child := range s.Children {
		children = append(children, child.Normalized())
	}

	nodeType := s.NodeType
	if nodeType == "Bitmap Index Scan" {
		nodeType = "Index Scan"
	}
	// Incremental Sort (PostgreSQL 13+) exploits an input already sorted by
	// a leading index column to sort only the remaining tie-break columns;
	// PostgreSQL toggles between it and a plain Sort on the same cost-tie
	// basis as the bitmap-vs-plain scan choice above, over the identical
	// input and producing the identical output order.
	if nodeType == "Incremental Sort" {
		nodeType = "Sort"
	}
	relation := partitionSuffix.ReplaceAllString(s.RelationName, "_pN")
	index := partitionSuffix.ReplaceAllString(s.IndexName, "_pN")

	if nodeType == "Bitmap Heap Scan" && len(children) == 1 &&
		children[0].NodeType == "Index Scan" && children[0].RelationName == "" {
		return Shape{NodeType: "Index Scan", RelationName: relation, IndexName: children[0].IndexName}
	}
	return Shape{NodeType: nodeType, RelationName: relation, IndexName: index, Children: children}
}
