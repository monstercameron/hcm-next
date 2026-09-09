package importing_test

import (
	"strconv"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
)

func stageFixtureBatch(t *testing.T) importing.Batch {
	t.Helper()
	source := csvSource(t, "s3://imports/acme/2026-01-01/workers.csv")
	retrievedAt := fixedInstant(t)
	header := []string{"employee_id", "full_name", "hire_date", "salary", "is_active", "notes"}
	records := [][]string{
		{"E001", "Jane Doe", "2024-01-15", "USD 120,000.00", "true", "first note"},
		{"E002", "John Roe", "2024-02-01", "USD 95,500.50", "false", ""},
		{"E003", "Amy Poe", "2024-03-10", "USD 105,000.00", "true", "third note"},
	}
	b, err := importing.StageBatch(source, retrievedAt, header, records)
	if err != nil {
		t.Fatalf("StageBatch: %v", err)
	}
	return b
}

func TestTodo_DATAOPS_002(t *testing.T) {
	b := stageFixtureBatch(t)

	t.Run("infers a stable type per column", func(t *testing.T) {
		p, err := importing.ProfileBatch(b)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		if got, want := p.RowCount, 3; got != want {
			t.Fatalf("RowCount = %d, want %d", got, want)
		}
		if p.BatchDigest != b.Digest() {
			t.Fatal("profile does not bind to the batch's digest")
		}

		cases := []struct {
			column string
			kind   importing.ColumnKind
			format string
		}{
			{"employee_id", importing.KindText, ""},
			{"full_name", importing.KindText, ""},
			{"hire_date", importing.KindDate, "2006-01-02"},
			{"salary", importing.KindMoney, "MONEY:USD"},
			{"is_active", importing.KindBoolean, ""},
			{"notes", importing.KindText, ""},
		}
		for _, c := range cases {
			col, ok := p.Column(c.column)
			if !ok {
				t.Fatalf("column %q missing from profile", c.column)
			}
			if col.Kind != c.kind {
				t.Fatalf("column %q kind = %s, want %s", c.column, col.Kind, c.kind)
			}
			if c.format != "" && col.Format != c.format {
				t.Fatalf("column %q format = %q, want %q", c.column, col.Format, c.format)
			}
		}
	})

	t.Run("identifier detection preserves zero-padded numeric ids", func(t *testing.T) {
		source := csvSource(t, "s3://x")
		header := []string{"badge_id"}
		records := [][]string{{"00042"}, {"00099"}, {"00007"}}
		b2, err := importing.StageBatch(source, fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		p, err := importing.ProfileBatch(b2)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		col, _ := p.Column("badge_id")
		if col.Kind != importing.KindIdentifier {
			t.Fatalf("kind = %s, want IDENTIFIER (a numeric parse would have stripped the leading zeros)", col.Kind)
		}
	})

	t.Run("null and blank counts, and exact min/max", func(t *testing.T) {
		source := csvSource(t, "s3://x")
		header := []string{"amount"}
		records := [][]string{{"10.5"}, {""}, {"-3.25"}, {"100.00"}, {"  "}}
		b2, err := importing.StageBatch(source, fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		p, err := importing.ProfileBatch(b2)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		col, _ := p.Column("amount")
		if col.Kind != importing.KindDecimal {
			t.Fatalf("kind = %s, want DECIMAL", col.Kind)
		}
		if col.NullCount != 2 {
			t.Fatalf("NullCount = %d, want 2", col.NullCount)
		}
		if col.ValueCount != 3 {
			t.Fatalf("ValueCount = %d, want 3", col.ValueCount)
		}
		if col.Min != "-3.25" || col.Max != "100.00" {
			t.Fatalf("min/max = %q/%q, want -3.25/100.00", col.Min, col.Max)
		}
	})

	t.Run("distinct count is bounded on high-cardinality columns", func(t *testing.T) {
		source := csvSource(t, "s3://x")
		header := []string{"note"}
		var records [][]string
		for i := 0; i < importing.MaxDistinctTracked+50; i++ {
			records = append(records, []string{"n" + strconv.Itoa(i)})
		}
		b2, err := importing.StageBatch(source, fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		p, err := importing.ProfileBatch(b2)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		col, _ := p.Column("note")
		if !col.DistinctBounded {
			t.Fatal("expected DistinctBounded to be true past the tracking cap")
		}
		if col.DistinctCount != importing.MaxDistinctTracked {
			t.Fatalf("DistinctCount = %d, want the cap %d", col.DistinctCount, importing.MaxDistinctTracked)
		}
		if col.ValueCount != importing.MaxDistinctTracked+50 {
			t.Fatalf("ValueCount = %d, want exact count regardless of the distinct cap", col.ValueCount)
		}
	})

	t.Run("mixed currency never classifies as money", func(t *testing.T) {
		source := csvSource(t, "s3://x")
		header := []string{"amount"}
		records := [][]string{{"USD 10.00"}, {"EUR 20.00"}}
		b2, err := importing.StageBatch(source, fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		p, err := importing.ProfileBatch(b2)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		col, _ := p.Column("amount")
		if col.Kind != importing.KindText {
			t.Fatalf("kind = %s, want TEXT for disagreeing currencies", col.Kind)
		}
	})
}

func TestTodo_DATAOPS_002_Golden(t *testing.T) {
	b := stageFixtureBatch(t)
	p, err := importing.ProfileBatch(b)
	if err != nil {
		t.Fatalf("ProfileBatch: %v", err)
	}
	golden(t, "dataops_002_profile_digest.golden", []byte(p.Digest))

	p2, err := importing.ProfileBatch(b)
	if err != nil {
		t.Fatalf("ProfileBatch (rerun): %v", err)
	}
	if p.Digest != p2.Digest {
		t.Fatalf("re-profiling the same batch produced a different digest: %s vs %s", p.Digest, p2.Digest)
	}
}

func FuzzTodo_DATAOPS_002(f *testing.F) {
	f.Add("10", "USD 10.00", "true", "2024-01-01")
	f.Add("", "not money", "maybe", "not a date")
	f.Add("007", "$5.00", "TRUE", "2024/01/01")
	f.Add("-12.50", "USD -5.00 USD", "false", "01/02/2024")

	f.Fuzz(func(t *testing.T, a, moneyLike, boolLike, dateLike string) {
		source := csvSource(t, "s3://fuzz")
		header := []string{"a", "b", "c", "d"}
		records := [][]string{{a, moneyLike, boolLike, dateLike}}
		b, err := importing.StageBatch(source, fixedInstant(t), header, records)
		if err != nil {
			return
		}
		p1, err := importing.ProfileBatch(b)
		if err != nil {
			t.Fatalf("ProfileBatch: %v", err)
		}
		p2, err := importing.ProfileBatch(b)
		if err != nil {
			t.Fatalf("ProfileBatch (rerun): %v", err)
		}
		if p1.Digest != p2.Digest {
			t.Fatalf("profiling the same batch twice produced different digests: %s vs %s", p1.Digest, p2.Digest)
		}
		for _, col := range p1.Columns {
			if col.NullCount+col.ValueCount != p1.RowCount {
				t.Fatalf("column %q: NullCount(%d)+ValueCount(%d) != RowCount(%d)",
					col.Name, col.NullCount, col.ValueCount, p1.RowCount)
			}
			if !col.Kind.Valid() {
				t.Fatalf("column %q has invalid kind %d", col.Name, col.Kind)
			}
		}
	})
}
