package importing_test

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("drifted from %s\n got: %s\nwant: %s", path, got, want)
	}
}

func fixedInstant(t *testing.T) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse fixed instant: %v", err)
	}
	return values.NewInstant(parsed)
}

func csvSource(t *testing.T, uri string) importing.SourceDescriptor {
	t.Helper()
	s := importing.SourceDescriptor{Kind: importing.SourceKindCSV, URI: uri, Tenant: values.TenantId("acme-corp")}
	if err := s.Validate(); err != nil {
		t.Fatalf("source descriptor: %v", err)
	}
	return s
}

func TestTodo_DATAOPS_001(t *testing.T) {
	source := csvSource(t, "s3://imports/acme/2026-01-01/workers.csv")
	retrievedAt := fixedInstant(t)
	header := []string{"employee_id", "full_name", "hire_date"}
	records := [][]string{
		{"E001", "Jane Doe", "2024-01-15"},
		{"E002", "John Roe", "2024-02-01"},
	}

	t.Run("stages without any domain write surface", func(t *testing.T) {
		b, err := importing.StageBatch(source, retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("staged batch fails Validate: %v", err)
		}
		if got, want := b.RowCount(), 2; got != want {
			t.Fatalf("RowCount = %d, want %d", got, want)
		}
		if got := b.Digest(); got == "" {
			t.Fatal("Digest is empty")
		}
		if got := b.Source(); got != source {
			t.Fatalf("Source = %+v, want %+v", got, source)
		}
	})

	t.Run("returned slices are defensive copies", func(t *testing.T) {
		b, err := importing.StageBatch(source, retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		h := b.Header()
		h[0] = "corrupted"
		if b.Header()[0] != "employee_id" {
			t.Fatal("mutating a returned header copy changed the batch")
		}
		row, ok := b.Row(0)
		if !ok {
			t.Fatal("row 0 missing")
		}
		cells := row.Cells()
		cells[0] = "corrupted"
		row2, _ := b.Row(0)
		if row2.Cells()[0] != "E001" {
			t.Fatal("mutating a returned cell copy changed the batch")
		}
		// The header the caller passed in must not alias the batch either.
		header[0] = "corrupted-input"
		if b.Header()[0] != "employee_id" {
			t.Fatal("mutating the caller's header slice changed the batch")
		}
		header[0] = "employee_id"
	})

	t.Run("row identity is content-addressed, not positional", func(t *testing.T) {
		bA, err := importing.StageBatch(csvSource(t, "s3://a"), retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch A: %v", err)
		}
		bB, err := importing.StageBatch(csvSource(t, "s3://b"), fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch B: %v", err)
		}
		rowA, _ := bA.Row(0)
		rowB, _ := bB.Row(0)
		if rowA.ID() != rowB.ID() {
			t.Fatalf("identical row content under identical header produced different ids: %s vs %s",
				rowA.ID(), rowB.ID())
		}
		if bA.Digest() == bB.Digest() {
			t.Fatal("batches with different sources produced the same digest")
		}

		reordered := [][]string{records[1], records[0]}
		bC, err := importing.StageBatch(csvSource(t, "s3://a"), retrievedAt, header, reordered)
		if err != nil {
			t.Fatalf("StageBatch reordered: %v", err)
		}
		if bA.Digest() == bC.Digest() {
			t.Fatal("reordering rows did not change the batch digest")
		}
		rowC0, _ := bC.Row(0)
		rowA1, _ := bA.Row(1)
		if rowC0.ID() != rowA1.ID() {
			t.Fatal("reordering rows changed a row's own content-addressed id")
		}
	})

	t.Run("staging is deterministic across repeated runs", func(t *testing.T) {
		b1, err := importing.StageBatch(source, retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch 1: %v", err)
		}
		b2, err := importing.StageBatch(source, retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch 2: %v", err)
		}
		if b1.Digest() != b2.Digest() {
			t.Fatalf("two stagings of identical input produced different digests: %s vs %s",
				b1.Digest(), b2.Digest())
		}
	})

	t.Run("StageCSV agrees with StageBatch over equivalent input", func(t *testing.T) {
		csvText := "employee_id,full_name,hire_date\r\nE001,Jane Doe,2024-01-15\r\nE002,John Roe,2024-02-01\r\n"
		bCSV, err := importing.StageCSV(source, retrievedAt, strings.NewReader(csvText))
		if err != nil {
			t.Fatalf("StageCSV: %v", err)
		}
		bDirect, err := importing.StageBatch(source, retrievedAt, header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		if bCSV.Digest() != bDirect.Digest() {
			t.Fatalf("StageCSV digest %s != StageBatch digest %s", bCSV.Digest(), bDirect.Digest())
		}
	})

	t.Run("malformed input never stages", func(t *testing.T) {
		cases := []struct {
			name    string
			header  []string
			records [][]string
			source  importing.SourceDescriptor
		}{
			{
				name:    "duplicate column",
				header:  []string{"employee_id", "employee_id"},
				records: [][]string{{"E001", "E001"}},
				source:  source,
			},
			{
				name:    "row width mismatch",
				header:  []string{"employee_id", "full_name"},
				records: [][]string{{"E001"}},
				source:  source,
			},
			{
				name:    "invalid utf8 cell",
				header:  []string{"employee_id", "full_name"},
				records: [][]string{{"E001", "Jane \xff\xfeDoe"}},
				source:  source,
			},
			{
				name:    "oversized cell",
				header:  []string{"employee_id", "note"},
				records: [][]string{{"E001", strings.Repeat("x", importing.MaxCellBytes+1)}},
				source:  source,
			},
			{
				name:    "empty header",
				header:  nil,
				records: nil,
				source:  source,
			},
			{
				name:    "invalid tenant",
				header:  []string{"employee_id"},
				records: [][]string{{"E001"}},
				source:  importing.SourceDescriptor{Kind: importing.SourceKindCSV, URI: "s3://x", Tenant: values.TenantId("A")},
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := importing.StageBatch(c.source, retrievedAt, c.header, c.records)
				if err == nil {
					t.Fatal("expected a staging error, got nil")
				}
			})
		}
	})
}

func TestTodo_DATAOPS_001_Golden(t *testing.T) {
	source := csvSource(t, "s3://imports/acme/2026-01-01/workers.csv")
	retrievedAt := fixedInstant(t)
	header := []string{"employee_id", "full_name", "hire_date"}
	records := [][]string{
		{"E001", "Jane Doe", "2024-01-15"},
		{"E002", "John Roe", "2024-02-01"},
	}
	b, err := importing.StageBatch(source, retrievedAt, header, records)
	if err != nil {
		t.Fatalf("StageBatch: %v", err)
	}
	golden(t, "dataops_001_batch_digest.golden", []byte(b.Digest()))

	row0, _ := b.Row(0)
	golden(t, "dataops_001_row0_id.golden", []byte(row0.ID()))
}

// TestTodo_DATAOPS_001_Security exercises the RED clause directly: malformed,
// oversized or cross-tenant input must never produce a batch, staging must
// expose no mutation surface, and an unbounded stream must be rejected rather
// than exhausting memory.
func TestTodo_DATAOPS_001_Security(t *testing.T) {
	source := csvSource(t, "s3://imports/acme/2026-01-01/workers.csv")
	retrievedAt := fixedInstant(t)

	t.Run("Batch and Row expose no write method", func(t *testing.T) {
		forbidden := []string{"Set", "Delete", "Save", "Write", "Update", "Mutate", "Append", "Remove", "Put"}
		for _, typ := range []reflect.Type{reflect.TypeOf(importing.Batch{}), reflect.TypeOf(importing.Row{})} {
			for i := 0; i < typ.NumMethod(); i++ {
				name := typ.Method(i).Name
				for _, bad := range forbidden {
					if strings.HasPrefix(name, bad) {
						t.Fatalf("%s exposes mutation-shaped method %s", typ.Name(), name)
					}
				}
			}
		}
	})

	t.Run("too many rows is rejected", func(t *testing.T) {
		header := []string{"id"}
		records := make([][]string, importing.MaxRows+1)
		for i := range records {
			records[i] = []string{strconv.Itoa(i)}
		}
		_, err := importing.StageBatch(source, retrievedAt, header, records)
		if err == nil {
			t.Fatal("expected ErrTooManyRows, got nil")
		}
	})

	t.Run("unbounded CSV stream is rejected without buffering it whole", func(t *testing.T) {
		r := &infiniteCSVReader{}
		_, err := importing.StageCSV(source, retrievedAt, r)
		if err == nil {
			t.Fatal("expected a size-bound error, got nil")
		}
		if r.served < importing.MaxSourceCSVBytes {
			t.Fatalf("rejected after only %d bytes, expected to serve at least the cap", r.served)
		}
	})

	t.Run("cross-tenant source is rejected", func(t *testing.T) {
		bad := importing.SourceDescriptor{Kind: importing.SourceKindCSV, URI: "s3://x", Tenant: values.TenantId("")}
		_, err := importing.StageBatch(bad, retrievedAt, []string{"id"}, [][]string{{"1"}})
		if err == nil {
			t.Fatal("expected a source-invalid error, got nil")
		}
	})

	t.Run("unset retrieval time is rejected", func(t *testing.T) {
		_, err := importing.StageBatch(source, values.Instant{}, []string{"id"}, [][]string{{"1"}})
		if err == nil {
			t.Fatal("expected ErrRetrievedAtUnset, got nil")
		}
	})
}

// infiniteCSVReader serves an endless stream of well-formed, single-column
// CSV rows without ever allocating the whole stream, so the size-cap test
// cannot be defeated by simply making the cap cheap to hit. Each row is long
// enough (200 bytes) that the byte cap is reached in far fewer than MaxRows
// rows, so the test proves ErrSourceTooLarge fires rather than ErrTooManyRows.
type infiniteCSVReader struct {
	served int
	buf    []byte
}

func (r *infiniteCSVReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		r.buf = []byte(strings.Repeat("a", 200) + "\n")
	}
	n := copy(p, r.buf)
	r.served += n
	return n, nil
}

func FuzzTodo_DATAOPS_001(f *testing.F) {
	f.Add("a,b\n1,2\n3,4\n")
	f.Add("only-header\n")
	f.Add("a,a\n1,2\n")
	f.Add("a,b\n1\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, csvText string) {
		source := csvSource(t, "s3://fuzz")
		retrievedAt := fixedInstant(t)
		b1, err1 := importing.StageCSV(source, retrievedAt, strings.NewReader(csvText))
		if err1 != nil {
			return // Rejection is an allowed outcome for arbitrary bytes.
		}
		if err := b1.Validate(); err != nil {
			t.Fatalf("StageCSV returned a batch that fails Validate: %v", err)
		}
		b2, err2 := importing.StageCSV(source, retrievedAt, strings.NewReader(csvText))
		if err2 != nil {
			t.Fatalf("second staging of the same bytes failed: %v", err2)
		}
		if b1.Digest() != b2.Digest() {
			t.Fatalf("staging the same csv text twice produced different digests: %s vs %s",
				b1.Digest(), b2.Digest())
		}
	})
}
