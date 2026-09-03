package importing_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/dataops/importing"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func testRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	return reg
}

func validMappingSpec() importing.MappingSpecInput {
	return importing.MappingSpecInput{
		Version: "mapping.acme.workers/v1",
		Fields: []importing.FieldMapping{
			{
				SourceColumn: "title_raw",
				Target:       model.PropertyRef("job.title"),
				Transform:    importing.TransformSpec{Kind: importing.TransformTrim},
				IsIdentity:   false,
			},
			{
				SourceColumn: "amount_raw",
				Target:       model.PropertyRef("compensation_component.amount"),
				Transform:    importing.TransformSpec{Kind: importing.TransformMoneyParse, Currency: "USD"},
			},
			{
				SourceColumn: "expiry_raw",
				Target:       model.PropertyRef("budget_reservation.expiry"),
				Transform:    importing.TransformSpec{Kind: importing.TransformDateParse, Layout: "2006-01-02"},
			},
			{
				SourceColumn: "region_raw",
				Target:       model.PropertyRef("position.pay_band_ref"),
				Transform: importing.TransformSpec{
					Kind:             importing.TransformLookup,
					Crosswalk:        map[string]string{"east": "BAND_E", "west": "BAND_W"},
					CrosswalkVersion: "crosswalk.region_band/v1",
				},
			},
		},
	}
}

func TestTodo_DATAOPS_003(t *testing.T) {
	reg := testRegistry(t)

	t.Run("compiles a valid mapping with reads, writes and a stable digest", func(t *testing.T) {
		spec := validMappingSpec()
		m1, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if len(m1.Reads) != 4 || len(m1.Writes) != 4 {
			t.Fatalf("Reads/Writes = %d/%d, want 4/4", len(m1.Reads), len(m1.Writes))
		}
		for i := 1; i < len(m1.Reads); i++ {
			if m1.Reads[i-1] >= m1.Reads[i] {
				t.Fatalf("Reads not sorted: %v", m1.Reads)
			}
		}
		m2, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile (rerun): %v", err)
		}
		if m1.Digest != m2.Digest {
			t.Fatalf("compiling the same spec twice produced different digests: %s vs %s", m1.Digest, m2.Digest)
		}
	})

	t.Run("field order in the spec does not change the digest", func(t *testing.T) {
		spec := validMappingSpec()
		m1, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		reversed := validMappingSpec()
		for i, j := 0, len(reversed.Fields)-1; i < j; i, j = i+1, j-1 {
			reversed.Fields[i], reversed.Fields[j] = reversed.Fields[j], reversed.Fields[i]
		}
		m2, err := importing.Compile(reg, reversed)
		if err != nil {
			t.Fatalf("Compile (reversed): %v", err)
		}
		if m1.Digest != m2.Digest {
			t.Fatal("reordering the input fields changed the compiled digest")
		}
	})

	t.Run("apply transforms a row into canonical field values", func(t *testing.T) {
		spec := validMappingSpec()
		m, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		header := []string{"title_raw", "amount_raw", "expiry_raw", "region_raw"}
		records := [][]string{{"  Staff Engineer  ", "USD 1,234.56", "2024-01-15", "east"}}
		b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		row, _ := b.Row(0)
		mapped, err := m.Apply(header, row)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		want := map[model.PropertyRef]string{
			model.PropertyRef("job.title"):                     "Staff Engineer",
			model.PropertyRef("compensation_component.amount"): "1234.56",
			model.PropertyRef("budget_reservation.expiry"):     "2024-01-15T00:00:00Z",
			model.PropertyRef("position.pay_band_ref"):         "BAND_E",
		}
		for _, mv := range mapped {
			if !mv.OK {
				t.Fatalf("field %s: transform failed with %s", mv.Target, mv.ErrorCode)
			}
			if mv.Value != want[mv.Target] {
				t.Fatalf("field %s: value = %q, want %q", mv.Target, mv.Value, want[mv.Target])
			}
		}
	})

	t.Run("apply reports an unresolved lookup rather than failing the whole row", func(t *testing.T) {
		spec := validMappingSpec()
		m, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		header := []string{"title_raw", "amount_raw", "expiry_raw", "region_raw"}
		records := [][]string{{"Engineer", "USD 1.00", "2024-01-15", "north"}}
		b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header, records)
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		row, _ := b.Row(0)
		mapped, err := m.Apply(header, row)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		found := false
		for _, mv := range mapped {
			if mv.Target == model.PropertyRef("position.pay_band_ref") {
				found = true
				if mv.OK {
					t.Fatal("expected the lookup to fail for an unmapped region")
				}
				if mv.ErrorCode != importing.RuleLookupUnresolved {
					t.Fatalf("ErrorCode = %q, want %q", mv.ErrorCode, importing.RuleLookupUnresolved)
				}
			}
		}
		if !found {
			t.Fatal("pay_band_ref field missing from Apply output")
		}
	})

	t.Run("apply on a batch missing a mapped column is a structural error", func(t *testing.T) {
		spec := validMappingSpec()
		m, err := importing.Compile(reg, spec)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		header := []string{"title_raw"} // missing the other three mapped columns.
		b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header, [][]string{{"x"}})
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		row, _ := b.Row(0)
		if _, err := m.Apply(header, row); !errors.Is(err, importing.ErrSourceColumnNotInBatch) {
			t.Fatalf("Apply error = %v, want ErrSourceColumnNotInBatch", err)
		}
	})

	t.Run("RED: a mapping that should not compile never returns a usable profile", func(t *testing.T) {
		cases := []struct {
			name    string
			mutate  func(s *importing.MappingSpecInput)
			wantErr error
		}{
			{
				name: "unresolved target property",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[0].Target = model.PropertyRef("not_a_real.property")
				},
				wantErr: importing.ErrUnresolvedProperty,
			},
			{
				name: "unsupported target go type",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[0].Target = model.PropertyRef("worker.status") // lifecycle.StateID
				},
				wantErr: importing.ErrUnsupportedTargetType,
			},
			{
				name: "transform output does not match target type",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[0].Transform = importing.TransformSpec{Kind: importing.TransformMoneyParse, Currency: "USD"}
					// job.title is a string property; MoneyParse emits ClassDecimal.
				},
				wantErr: importing.ErrTransformTypeMismatch,
			},
			{
				name: "duplicate source column",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[1].SourceColumn = s.Fields[0].SourceColumn
				},
				wantErr: importing.ErrDuplicateSourceColumn,
			},
			{
				name: "duplicate target",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[1].Target = s.Fields[0].Target
					s.Fields[1].Transform = s.Fields[0].Transform
				},
				wantErr: importing.ErrDuplicateTarget,
			},
			{
				name: "lookup with no pinned crosswalk version",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[3].Transform.CrosswalkVersion = ""
				},
				wantErr: importing.ErrCrosswalkUnpinned,
			},
			{
				name: "lookup with an empty crosswalk",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[3].Transform.Crosswalk = nil
				},
				wantErr: importing.ErrCrosswalkEmpty,
			},
			{
				name: "date layout outside the closed set",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[2].Transform.Layout = "Jan 2, 2006"
				},
				wantErr: importing.ErrDateLayoutNotAllowed,
			},
			{
				name: "malformed currency code",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[1].Transform.Currency = "usd"
				},
				wantErr: importing.ErrCurrencyMalformed,
			},
			{
				name: "constant transform with no value",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[0].Transform = importing.TransformSpec{Kind: importing.TransformConstant}
				},
				wantErr: importing.ErrConstantEmpty,
			},
			{
				name: "unknown transform kind",
				mutate: func(s *importing.MappingSpecInput) {
					s.Fields[0].Transform = importing.TransformSpec{Kind: importing.TransformKind(200)}
				},
				wantErr: importing.ErrUnknownTransform,
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				spec := validMappingSpec()
				c.mutate(&spec)
				_, err := importing.Compile(reg, spec)
				if err == nil {
					t.Fatal("expected a compile error, got nil")
				}
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("error = %v, want it to wrap %v", err, c.wantErr)
				}
			})
		}
	})
}

func TestTodo_DATAOPS_003_Golden(t *testing.T) {
	reg := testRegistry(t)
	m, err := importing.Compile(reg, validMappingSpec())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	golden(t, "dataops_003_mapping_digest.golden", []byte(m.Digest))
}

func FuzzTodo_DATAOPS_003(f *testing.F) {
	f.Add("title_raw", "hello world")
	f.Add("", "")
	f.Add("weird col!!", "value with \x00 null")

	f.Fuzz(func(t *testing.T, sourceColumn, constant string) {
		reg := testRegistry(t)
		spec := importing.MappingSpecInput{
			Version: "fuzz/v1",
			Fields: []importing.FieldMapping{
				{
					SourceColumn: sourceColumn,
					Target:       model.PropertyRef("job.title"),
					Transform:    importing.TransformSpec{Kind: importing.TransformConstant, Constant: constant},
				},
			},
		}
		m1, err1 := importing.Compile(reg, spec)
		if err1 != nil {
			return // Rejection is an allowed outcome for arbitrary input.
		}
		m2, err2 := importing.Compile(reg, spec)
		if err2 != nil {
			t.Fatalf("second compile of the same spec failed: %v", err2)
		}
		if m1.Digest != m2.Digest {
			t.Fatalf("compiling the same spec twice produced different digests: %s vs %s", m1.Digest, m2.Digest)
		}
	})
}
