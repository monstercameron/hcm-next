package rules

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// dec builds a test decimal at the fixture rounding mode, failing the test on
// a bad literal.
func dec(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("dec(%q,%d): %v", text, scale, err)
	}
	return d
}

// raiseApprovalTable is the worked example from
// planning/specs/human-work-forms-and-rules.md's Decision Tables section,
// built directly against this engine: US raises over 10% and DE raises over
// 8% require finance (DE also requires HR); everything else is manager-only.
func raiseApprovalTable(t *testing.T) Table {
	t.Helper()
	tbl := Table{
		ID:      "hcmnext.engines.rules.test.raise_approval",
		Version: "1",
		Inputs: []Column{
			{Name: "increase_percent", Kind: KindDecimal},
			{Name: "country", Kind: KindString},
		},
		Outputs:   []Column{{Name: "tier", Kind: KindString}},
		HitPolicy: HitPolicyFirst,
		Rows: []Row{
			{
				ID:         "us-over-10",
				Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "10.0000", 4))), Equal(StringValue("US"))},
				Outputs:    []Value{StringValue("FINANCE_REQUIRED")},
			},
			{
				ID:         "de-over-8",
				Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "8.0000", 4))), Equal(StringValue("DE"))},
				Outputs:    []Value{StringValue("FINANCE_AND_HR_REQUIRED")},
			},
			{
				ID:         "otherwise",
				Conditions: []Condition{Any(), Any()},
				Outputs:    []Value{StringValue("MANAGER_ONLY")},
			},
		},
	}
	if err := tbl.Validate(); err != nil {
		t.Fatalf("raiseApprovalTable: %v", err)
	}
	return tbl
}

func raiseInputs(t *testing.T, pct, country string) map[string]Value {
	t.Helper()
	return map[string]Value{
		"increase_percent": DecimalValue(dec(t, pct, 4)),
		"country":          StringValue(country),
	}
}

// TestTodo_RULE_002 proves the core decision-table contract: exact,
// deterministic results under each declared hit policy, matched row ids and
// an explanation trace, and a digest that cites the exact table version an
// evaluation ran against.
func TestTodo_RULE_002(t *testing.T) {
	t.Run("FIRST returns the first matching row and stops", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		cases := []struct {
			name    string
			pct     string
			country string
			wantRow string
			wantOut string
		}{
			{"US over threshold", "15.0000", "US", "us-over-10", "FINANCE_REQUIRED"},
			{"US at threshold does not escalate", "10.0000", "US", "otherwise", "MANAGER_ONLY"},
			{"DE over threshold", "9.0000", "DE", "de-over-8", "FINANCE_AND_HR_REQUIRED"},
			{"DE at threshold does not escalate", "8.0000", "DE", "otherwise", "MANAGER_ONLY"},
			{"unrelated country falls to otherwise", "50.0000", "FR", "otherwise", "MANAGER_ONLY"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				result, err := Evaluate(tbl, raiseInputs(t, tc.pct, tc.country))
				if err != nil {
					t.Fatalf("Evaluate: %v", err)
				}
				if result.Status != StatusMatched {
					t.Fatalf("status = %s, want MATCHED", result.Status)
				}
				if len(result.MatchedRowIDs) != 1 || result.MatchedRowIDs[0] != tc.wantRow {
					t.Fatalf("matched rows = %v, want [%s]", result.MatchedRowIDs, tc.wantRow)
				}
				if len(result.Matches) != 1 || result.Matches[0].Outputs[0].String() != tc.wantOut {
					t.Fatalf("output = %v, want %s", result.Matches, tc.wantOut)
				}
				if result.TableID != tbl.ID || result.TableVersion != tbl.Version {
					t.Fatalf("result cites %s@%s, want %s@%s", result.TableID, result.TableVersion, tbl.ID, tbl.Version)
				}
				wantDigest, err := tbl.Digest()
				if err != nil {
					t.Fatalf("table digest: %v", err)
				}
				if result.TableDigest != wantDigest {
					t.Fatalf("result digest = %s, want %s", result.TableDigest, wantDigest)
				}
				// The trace must explain the winning row column by column, and
				// stop there: FIRST does not evaluate rows after its winner.
				if len(result.Trace) == 0 {
					t.Fatal("no explanation trace was produced")
				}
				last := result.Trace[len(result.Trace)-1]
				if last.RowID != tc.wantRow || !last.Matched {
					t.Fatalf("trace does not end at the winning row: last entry = %+v", last)
				}
				if len(last.Columns) != len(tbl.Inputs) {
					t.Fatalf("trace has %d column entries, want %d", len(last.Columns), len(tbl.Inputs))
				}
			})
		}
	})

	t.Run("UNIQUE reports CONFLICT on overlap and UNKNOWN on a gap", func(t *testing.T) {
		overlapping := Table{
			ID:        "hcmnext.engines.rules.test.unique_overlap",
			Version:   "1",
			Inputs:    []Column{{Name: "score", Kind: KindDecimal}},
			Outputs:   []Column{{Name: "bucket", Kind: KindString}},
			HitPolicy: HitPolicyUnique,
			Rows: []Row{
				{ID: "low", Conditions: []Condition{LessThan(DecimalValue(dec(t, "50.00", 2)))}, Outputs: []Value{StringValue("LOW")}},
				{ID: "mid", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "10.00", 2)))}, Outputs: []Value{StringValue("MID")}},
			},
		}
		if err := overlapping.Validate(); err != nil {
			t.Fatalf("table: %v", err)
		}

		// 30 satisfies both "< 50" and "> 10": a genuine overlap.
		result, err := Evaluate(overlapping, map[string]Value{"score": DecimalValue(dec(t, "30.00", 2))})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != StatusConflict {
			t.Fatalf("status = %s, want CONFLICT", result.Status)
		}
		if len(result.MatchedRowIDs) != 2 {
			t.Fatalf("matched rows = %v, want both overlapping rows cited", result.MatchedRowIDs)
		}

		// -5 satisfies only "< 50": a clean unique win.
		low, err := Evaluate(overlapping, map[string]Value{"score": DecimalValue(dec(t, "-5.00", 2))})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if low.Status != StatusMatched || len(low.MatchedRowIDs) != 1 || low.MatchedRowIDs[0] != "low" {
			t.Fatalf("Evaluate = %+v, want a clean unique match on 'low'", low)
		}

		gapped := Table{
			ID:        "hcmnext.engines.rules.test.unique_gap",
			Version:   "1",
			Inputs:    []Column{{Name: "score", Kind: KindDecimal}},
			Outputs:   []Column{{Name: "bucket", Kind: KindString}},
			HitPolicy: HitPolicyUnique,
			Rows: []Row{
				{ID: "low", Conditions: []Condition{LessThan(DecimalValue(dec(t, "10.00", 2)))}, Outputs: []Value{StringValue("LOW")}},
				{ID: "high", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "50.00", 2)))}, Outputs: []Value{StringValue("HIGH")}},
			},
		}
		if err := gapped.Validate(); err != nil {
			t.Fatalf("table: %v", err)
		}
		// 30 satisfies neither "< 10" nor "> 50": a genuine gap.
		gap, err := Evaluate(gapped, map[string]Value{"score": DecimalValue(dec(t, "30.00", 2))})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if gap.Status != StatusUnknown {
			t.Fatalf("status = %s, want UNKNOWN for an uncovered gap", gap.Status)
		}
		if len(gap.MatchedRowIDs) != 0 {
			t.Fatalf("matched rows = %v, want none for a gap", gap.MatchedRowIDs)
		}
	})

	t.Run("COLLECT gathers every matching row's outputs in declared order", func(t *testing.T) {
		tbl := Table{
			ID:        "hcmnext.engines.rules.test.collect_flags",
			Version:   "1",
			Inputs:    []Column{{Name: "amount", Kind: KindDecimal}},
			Outputs:   []Column{{Name: "flag", Kind: KindString}},
			HitPolicy: HitPolicyCollect,
			Rows: []Row{
				{ID: "over-10", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "10.00", 2)))}, Outputs: []Value{StringValue("OVER_10")}},
				{ID: "over-20", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "20.00", 2)))}, Outputs: []Value{StringValue("OVER_20")}},
				{ID: "over-30", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "30.00", 2)))}, Outputs: []Value{StringValue("OVER_30")}},
			},
		}
		if err := tbl.Validate(); err != nil {
			t.Fatalf("table: %v", err)
		}
		result, err := Evaluate(tbl, map[string]Value{"amount": DecimalValue(dec(t, "25.00", 2))})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != StatusMatched {
			t.Fatalf("status = %s, want MATCHED", result.Status)
		}
		wantRows := []string{"over-10", "over-20"}
		if len(result.MatchedRowIDs) != len(wantRows) {
			t.Fatalf("matched rows = %v, want %v", result.MatchedRowIDs, wantRows)
		}
		for i, id := range wantRows {
			if result.MatchedRowIDs[i] != id {
				t.Fatalf("matched rows = %v, want %v in order", result.MatchedRowIDs, wantRows)
			}
		}
		// COLLECT always evaluates every row, unlike FIRST, so the trace
		// covers the whole table including the non-matching "over-30" row.
		if len(result.Trace) != len(tbl.Rows) {
			t.Fatalf("trace has %d entries, want one per row (%d)", len(result.Trace), len(tbl.Rows))
		}
	})

	t.Run("BETWEEN and IN operators", func(t *testing.T) {
		tbl := Table{
			ID:      "hcmnext.engines.rules.test.between_in",
			Version: "1",
			Inputs: []Column{
				{Name: "age", Kind: KindInt},
				{Name: "code", Kind: KindString},
			},
			Outputs:   []Column{{Name: "bucket", Kind: KindString}},
			HitPolicy: HitPolicyFirst,
			Rows: []Row{
				{
					ID:         "working-age-known-code",
					Conditions: []Condition{Between(IntValue(18), IntValue(65)), In(StringValue("A"), StringValue("B"))},
					Outputs:    []Value{StringValue("MATCH")},
				},
				{ID: "otherwise", Conditions: []Condition{Any(), Any()}, Outputs: []Value{StringValue("NO_MATCH")}},
			},
		}
		if err := tbl.Validate(); err != nil {
			t.Fatalf("table: %v", err)
		}
		cases := []struct {
			age  int64
			code string
			want string
		}{
			{18, "A", "MATCH"}, // low inclusive boundary
			{65, "B", "MATCH"}, // high inclusive boundary
			{17, "A", "NO_MATCH"},
			{66, "A", "NO_MATCH"},
			{30, "C", "NO_MATCH"}, // not in the IN set
		}
		for _, tc := range cases {
			result, err := Evaluate(tbl, map[string]Value{"age": IntValue(tc.age), "code": StringValue(tc.code)})
			if err != nil {
				t.Fatalf("Evaluate(%d,%s): %v", tc.age, tc.code, err)
			}
			if len(result.Matches) != 1 || result.Matches[0].Outputs[0].String() != tc.want {
				t.Fatalf("Evaluate(%d,%s) = %v, want %s", tc.age, tc.code, result.Matches, tc.want)
			}
		}
	})
}

// TestTodo_RULE_002_Property asserts invariants that must hold for any table
// built through this engine, not just specific pinned numbers.
func TestTodo_RULE_002_Property(t *testing.T) {
	t.Run("evaluation is a pure function of table and inputs", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		inputs := raiseInputs(t, "12.3400", "US")
		first, err := Evaluate(tbl, inputs)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		second, err := Evaluate(tbl, inputs)
		if err != nil {
			t.Fatalf("Evaluate (repeat): %v", err)
		}
		if first.TableDigest != second.TableDigest || len(first.MatchedRowIDs) != len(second.MatchedRowIDs) ||
			first.MatchedRowIDs[0] != second.MatchedRowIDs[0] || first.Status != second.Status {
			t.Fatalf("two evaluations of identical inputs disagreed: %+v vs %+v", first, second)
		}
	})

	t.Run("row order is the priority under FIRST", func(t *testing.T) {
		// Two rows that both match "5": reordering them changes the winner,
		// which is the entire meaning of a declared row order.
		rowA := Row{ID: "a", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "0.00", 2)))}, Outputs: []Value{StringValue("A")}}
		rowB := Row{ID: "b", Conditions: []Condition{LessThan(DecimalValue(dec(t, "10.00", 2)))}, Outputs: []Value{StringValue("B")}}

		aFirst := Table{ID: "t", Version: "1", Inputs: []Column{{Name: "x", Kind: KindDecimal}}, Outputs: []Column{{Name: "y", Kind: KindString}}, HitPolicy: HitPolicyFirst, Rows: []Row{rowA, rowB}}
		bFirst := Table{ID: "t", Version: "1", Inputs: []Column{{Name: "x", Kind: KindDecimal}}, Outputs: []Column{{Name: "y", Kind: KindString}}, HitPolicy: HitPolicyFirst, Rows: []Row{rowB, rowA}}

		in := map[string]Value{"x": DecimalValue(dec(t, "5.00", 2))}
		res1, err := Evaluate(aFirst, in)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		res2, err := Evaluate(bFirst, in)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if res1.MatchedRowIDs[0] != "a" {
			t.Fatalf("expected row 'a' first, got %v", res1.MatchedRowIDs)
		}
		if res2.MatchedRowIDs[0] != "b" {
			t.Fatalf("expected row 'b' first when reordered, got %v", res2.MatchedRowIDs)
		}
		if aFirst.Canonical() == nil || bFirst.Canonical() == nil {
			t.Fatal("both orderings must be valid, encodable tables")
		}
		if string(aFirst.Canonical()) == string(bFirst.Canonical()) {
			t.Fatal("reordering rows must change the table's canonical bytes")
		}
	})

	t.Run("decimal comparisons are exact regardless of the input's declared scale", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		// 10.00 at scale 2 and 10.0000 at scale 4 are the same number: neither
		// is "> 10", so both fall to otherwise.
		for _, scale := range []int32{0, 2, 4, 6} {
			result, err := Evaluate(tbl, map[string]Value{
				"increase_percent": DecimalValue(dec(t, "10", scale)),
				"country":          StringValue("US"),
			})
			if err != nil {
				t.Fatalf("Evaluate at scale %d: %v", scale, err)
			}
			if result.MatchedRowIDs[0] != "otherwise" {
				t.Fatalf("scale %d: matched %v, want otherwise for a value exactly at the threshold", scale, result.MatchedRowIDs)
			}
		}
	})

	t.Run("Any matches every value of its kind", func(t *testing.T) {
		samples := []Value{
			DecimalValue(dec(t, "-999.99", 2)),
			DecimalValue(dec(t, "0.00", 2)),
			DecimalValue(dec(t, "999999.99", 2)),
			StringValue(""),
			StringValue("anything"),
			BoolValue(true),
			BoolValue(false),
			IntValue(-1),
			IntValue(1 << 40),
		}
		for _, v := range samples {
			ok, err := Any().Match(v)
			if err != nil {
				t.Fatalf("Any().Match(%s): %v", v, err)
			}
			if !ok {
				t.Fatalf("Any().Match(%s) = false, want true", v)
			}
		}
	})
}

// TestTodo_RULE_002_Golden pins the canonical byte encoding of representative
// values and a table, plus a fixed set of evaluation vectors, against
// testdata/rule_002_golden.json. A change to the wire format or to the
// engine's evaluation order fails this test even when every other assertion
// still happens to pass.
func TestTodo_RULE_002_Golden(t *testing.T) {
	raw, err := os.ReadFile("testdata/rule_002_golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Values []struct {
			Name         string `json:"name"`
			CanonicalHex string `json:"canonical_hex"`
		} `json:"values"`
		TableCanonicalHex string `json:"table_canonical_hex"`
		TableDigest       string `json:"table_digest"`
		Evaluations       []struct {
			IncreasePercent string `json:"increase_percent"`
			Country         string `json:"country"`
			Status          string `json:"status"`
			MatchedRow      string `json:"matched_row"`
		} `json:"evaluations"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	values := map[string]Value{
		"decimal 10.0000": DecimalValue(dec(t, "10.0000", 4)),
		"decimal -1.50":   DecimalValue(dec(t, "-1.50", 2)),
		"string US":       StringValue("US"),
		"bool true":       BoolValue(true),
		"bool false":      BoolValue(false),
		"int 42":          IntValue(42),
	}
	for _, tc := range golden.Values {
		v, ok := values[tc.Name]
		if !ok {
			t.Fatalf("golden fixture names unknown value %q", tc.Name)
		}
		if got := hex.EncodeToString(v.Canonical()); got != tc.CanonicalHex {
			t.Errorf("canonical(%s) = %s, want %s", tc.Name, got, tc.CanonicalHex)
		}
	}

	tbl := raiseApprovalTable(t)
	if got := hex.EncodeToString(tbl.Canonical()); got != golden.TableCanonicalHex {
		t.Errorf("table canonical bytes changed:\n got  %s\n want %s", got, golden.TableCanonicalHex)
	}
	digest, err := tbl.Digest()
	if err != nil {
		t.Fatalf("table digest: %v", err)
	}
	if digest != golden.TableDigest {
		t.Errorf("table digest = %s, want %s", digest, golden.TableDigest)
	}

	for _, tc := range golden.Evaluations {
		result, err := Evaluate(tbl, raiseInputs(t, tc.IncreasePercent, tc.Country))
		if err != nil {
			t.Errorf("Evaluate(%s,%s): %v", tc.IncreasePercent, tc.Country, err)
			continue
		}
		if result.Status.String() != tc.Status {
			t.Errorf("Evaluate(%s,%s) status = %s, want %s", tc.IncreasePercent, tc.Country, result.Status, tc.Status)
		}
		if len(result.MatchedRowIDs) == 0 || result.MatchedRowIDs[0] != tc.MatchedRow {
			t.Errorf("Evaluate(%s,%s) matched = %v, want [%s]", tc.IncreasePercent, tc.Country, result.MatchedRowIDs, tc.MatchedRow)
		}
	}
}

// TestTodo_RULE_002_Race proves the engine holds no shared mutable state:
// concurrent evaluations of the same table never interfere, and Table's own
// exported slices are never mutated by Evaluate or Canonical.
func TestTodo_RULE_002_Race(t *testing.T) {
	tbl := raiseApprovalTable(t)
	wantDigest, err := tbl.Digest()
	if err != nil {
		t.Fatalf("table digest: %v", err)
	}

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pct := []string{"15.0000", "10.0000", "9.0000", "8.0000", "3.0000"}[i%5]
			country := []string{"US", "US", "DE", "DE", "FR"}[i%5]
			for range 50 {
				result, err := Evaluate(tbl, raiseInputs(t, pct, country))
				if err != nil {
					errCh <- err
					return
				}
				if result.TableDigest != wantDigest {
					errCh <- errors.New("concurrent evaluation produced a different table digest")
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent evaluation failed: %v", err)
	}

	// The table's own fields must be unchanged after concurrent use.
	if got, err := tbl.Digest(); err != nil || got != wantDigest {
		t.Fatalf("table digest after concurrent use = %s, %v, want %s", got, err, wantDigest)
	}
}

// TestTodo_RULE_002_Fault proves the engine fails closed: a malformed table
// is refused at Validate, and a malformed or incomplete evaluation input is
// refused rather than silently matched against a wildcard row.
func TestTodo_RULE_002_Fault(t *testing.T) {
	t.Run("table without id or version", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Version = ""
		if err := tbl.Validate(); !errors.Is(err, ErrTableIdentity) {
			t.Fatalf("error = %v, want ErrTableIdentity", err)
		}
		if tbl.Canonical() != nil {
			t.Fatal("an invalid table must have no canonical encoding")
		}
	})

	t.Run("table with no declared hit policy", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.HitPolicy = HitPolicyUnspecified
		if err := tbl.Validate(); !errors.Is(err, ErrHitPolicyUnspecified) {
			t.Fatalf("error = %v, want ErrHitPolicyUnspecified", err)
		}
	})

	t.Run("table with no rows", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows = nil
		if err := tbl.Validate(); !errors.Is(err, ErrTableEmpty) {
			t.Fatalf("error = %v, want ErrTableEmpty", err)
		}
	})

	t.Run("duplicate row id", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows[2].ID = tbl.Rows[0].ID
		if err := tbl.Validate(); !errors.Is(err, ErrDuplicateRowID) {
			t.Fatalf("error = %v, want ErrDuplicateRowID", err)
		}
	})

	t.Run("two rows declare identical conditions under FIRST", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		dup := tbl.Rows[0]
		dup.ID = "duplicate"
		tbl.Rows = append(tbl.Rows, dup)
		if err := tbl.Validate(); !errors.Is(err, ErrAmbiguousRows) {
			t.Fatalf("error = %v, want ErrAmbiguousRows", err)
		}
	})

	t.Run("identical conditions are legal under COLLECT", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.HitPolicy = HitPolicyCollect
		dup := tbl.Rows[0]
		dup.ID = "duplicate"
		tbl.Rows = append(tbl.Rows, dup)
		if err := tbl.Validate(); err != nil {
			t.Fatalf("COLLECT must accept overlapping rows: %v", err)
		}
	})

	t.Run("row shaped for the wrong number of columns", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows[0].Conditions = tbl.Rows[0].Conditions[:1]
		if err := tbl.Validate(); !errors.Is(err, ErrRowInvalid) {
			t.Fatalf("error = %v, want ErrRowInvalid", err)
		}
	})

	t.Run("condition kind disagrees with its column", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows[0].Conditions[0] = Equal(StringValue("not-a-decimal"))
		if err := tbl.Validate(); !errors.Is(err, ErrKindMismatch) {
			t.Fatalf("error = %v, want ErrKindMismatch", err)
		}
	})

	t.Run("ordering operator on an unorderable column", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows[0].Conditions[1] = GreaterThan(StringValue("US"))
		if err := tbl.Validate(); !errors.Is(err, ErrOperatorUnsupported) {
			t.Fatalf("error = %v, want ErrOperatorUnsupported", err)
		}
	})

	t.Run("output kind disagrees with its column", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		tbl.Rows[0].Outputs[0] = BoolValue(true)
		if err := tbl.Validate(); !errors.Is(err, ErrKindMismatch) {
			t.Fatalf("error = %v, want ErrKindMismatch", err)
		}
	})

	t.Run("missing declared input is refused, never treated as a wildcard", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		_, err := Evaluate(tbl, map[string]Value{"increase_percent": DecimalValue(dec(t, "50.0000", 4))})
		if !errors.Is(err, ErrMissingInput) {
			t.Fatalf("error = %v, want ErrMissingInput", err)
		}
	})

	t.Run("input names a column the table does not declare", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		in := raiseInputs(t, "5.0000", "US")
		in["unexpected_column"] = StringValue("x")
		_, err := Evaluate(tbl, in)
		if !errors.Is(err, ErrUnknownInput) {
			t.Fatalf("error = %v, want ErrUnknownInput", err)
		}
	})

	t.Run("input kind disagrees with its column", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		_, err := Evaluate(tbl, map[string]Value{
			"increase_percent": StringValue("not a decimal"),
			"country":          StringValue("US"),
		})
		if !errors.Is(err, ErrKindMismatch) {
			t.Fatalf("error = %v, want ErrKindMismatch", err)
		}
	})

	t.Run("an unset decimal input is refused, not silently zero", func(t *testing.T) {
		tbl := raiseApprovalTable(t)
		_, err := Evaluate(tbl, map[string]Value{
			"increase_percent": DecimalValue(values.Decimal{}),
			"country":          StringValue("US"),
		})
		if err == nil {
			t.Fatal("Evaluate accepted an unset decimal input")
		}
	})

	t.Run("a zero Value has no canonical encoding", func(t *testing.T) {
		if (Value{}).Canonical() != nil {
			t.Fatal("the zero Value must have no canonical encoding")
		}
	})
}

// FuzzTodo_RULE_002 drives evaluation with arbitrary percentages and country
// codes. It asserts the properties that must hold for every input: the
// engine never panics, the status is always one of the declared statuses, and
// two evaluations of the same input are identical.
func FuzzTodo_RULE_002(f *testing.F) {
	f.Add(int64(1500000), "US")
	f.Add(int64(1000000), "US")
	f.Add(int64(0), "DE")
	f.Add(int64(-999999999), "")
	f.Add(int64(999999999999), "ZZ")

	f.Fuzz(func(t *testing.T, unscaledPct int64, country string) {
		tbl := raiseApprovalTable(t)
		pct, err := values.NewDecimal(formatCents(unscaledPct), 4, values.RoundingHalfEven)
		if err != nil {
			t.Skip("not a value this fixture needs to construct")
		}
		result, err := Evaluate(tbl, map[string]Value{
			"increase_percent": DecimalValue(pct),
			"country":          StringValue(country),
		})
		if err != nil {
			t.Fatalf("Evaluate failed on a valid table: %v", err)
		}
		if !result.Status.Valid() || result.Status == StatusUnspecified {
			t.Fatalf("status = %v is not a declared status", result.Status)
		}
		if result.Status != StatusMatched {
			t.Fatalf("a FIRST table with a wildcard 'otherwise' row must always match, got %s", result.Status)
		}

		again, err := Evaluate(tbl, map[string]Value{
			"increase_percent": DecimalValue(pct),
			"country":          StringValue(country),
		})
		if err != nil {
			t.Fatalf("Evaluate (repeat) failed: %v", err)
		}
		if again.MatchedRowIDs[0] != result.MatchedRowIDs[0] || again.TableDigest != result.TableDigest {
			t.Fatalf("two evaluations of the same input disagreed: %+v vs %+v", result, again)
		}
	})
}

// formatCents renders an arbitrary int64 as fixed-point text at 4 fractional
// digits, so the fuzzer's raw integers become arbitrary decimal literals
// without ever going through a float.
func formatCents(unscaled int64) string {
	neg := unscaled < 0
	if neg {
		unscaled = -unscaled
	}
	s := itoa(unscaled)
	for len(s) <= 4 {
		s = "0" + s
	}
	whole, frac := s[:len(s)-4], s[len(s)-4:]
	if neg {
		return "-" + whole + "." + frac
	}
	return whole + "." + frac
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// TestVersionIsStable is the ARCH-GO-009 engine package contract test for
// rules' Version(): it reports a fixed, positive contract version with no
// dependency on any Table or Result.
func TestVersionIsStable(t *testing.T) {
	if v := Version(); v != Version() || v <= 0 {
		t.Fatalf("Version() = %d, want a stable positive contract version", v)
	}
}

// TestCompileAcceptsAValidTableAndRejectsAMalformedOne is the table
// validation step ARCH-GO-009 asks rules to expose as Compile, symmetric
// with Evaluate: it accepts exactly the tables Table.Validate accepts, and
// rejects exactly the ones it rejects.
func TestCompileAcceptsAValidTableAndRejectsAMalformedOne(t *testing.T) {
	table := raiseApprovalTable(t)
	compiled, err := Compile(table)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.ID != table.ID || compiled.Version != table.Version {
		t.Fatalf("Compile returned %s@%s, want %s@%s", compiled.ID, compiled.Version, table.ID, table.Version)
	}

	malformed := table
	malformed.Rows = nil
	if _, err := Compile(malformed); !errors.Is(err, ErrTableEmpty) {
		t.Fatalf("Compile error = %v, want ErrTableEmpty", err)
	}
}

// TestResultExplainNamesTheTableTheStatusAndTheWinningRow is the
// ARCH-GO-009 engine package contract test for rules' Explain: it renders a
// narrative that names the table version, the honest status and the
// matched row, for an audit log or review screen.
func TestResultExplainNamesTheTableTheStatusAndTheWinningRow(t *testing.T) {
	table := raiseApprovalTable(t)
	result, err := Evaluate(table, raiseInputs(t, "12.0000", "US"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	explanation := result.Explain()
	for _, want := range []string{table.ID, table.Version, StatusMatched.String(), "us-over-10"} {
		if !strings.Contains(explanation, want) {
			t.Errorf("Explain() = %q, want it to contain %q", explanation, want)
		}
	}
}
