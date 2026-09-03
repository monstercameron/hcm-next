package balance

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func validDefinition() AccumulatorDefinition {
	return AccumulatorDefinition{
		ID: "pto", Version: "1.0.0", Name: "Paid time off",
		Unit: "hour", Currency: "USD", Subject: "worker", Period: PeriodCalendarYear,
		Dimensions: []BalanceDimension{{Name: "worker_id", ValueType: "entity_id", Required: true}, {Name: "program", ValueType: "reference", Required: true}},
		EntryTypes: []string{"GRANT", "USAGE", "CORRECTION"},
		Authority:  Authority{SourceID: "rewards.balance", Version: "2026.1"},
		Floor:      PolicyRule{Kind: RuleNone, Version: "1"}, Cap: PolicyRule{Kind: RuleConfigured, Version: "1", Value: "40"},
		Expiry: PolicyRule{Kind: RuleConfigured, Version: "1", Value: "end_of_year"}, Rollover: PolicyRule{Kind: RuleNone, Version: "1"}, Correction: PolicyRule{Kind: RuleConfigured, Version: "1", Value: "append_only"},
	}
}

func TestPublishAccumulatorDefinition(t *testing.T) {
	d := validDefinition()
	published, err := Publish(d)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Definition.ID != "pto" {
		t.Fatalf("published definition = %+v", published.Definition)
	}
	d.Dimensions[0].Name = "mutated"
	d.EntryTypes[0] = "MUTATED"
	if published.Definition.Dimensions[0].Name == "mutated" || published.Definition.EntryTypes[0] == "MUTATED" {
		t.Fatal("publication aliases caller-owned slices")
	}
}

func TestPublishRejectsMissingRequiredContract(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AccumulatorDefinition)
		field  string
	}{
		{"unit", func(d *AccumulatorDefinition) { d.Unit = "" }, "unit"},
		{"currency", func(d *AccumulatorDefinition) { d.Currency = "" }, "currency"},
		{"subject", func(d *AccumulatorDefinition) { d.Subject = "" }, "subject"},
		{"period", func(d *AccumulatorDefinition) { d.Period = PeriodUnspecified }, "period"},
		{"dimensions", func(d *AccumulatorDefinition) { d.Dimensions = nil }, "dimensions"},
		{"entry types", func(d *AccumulatorDefinition) { d.EntryTypes = nil }, "entry_types"},
		{"authority", func(d *AccumulatorDefinition) { d.Authority.SourceID = "" }, "authority.source_id"},
		{"floor rule", func(d *AccumulatorDefinition) { d.Floor = PolicyRule{} }, "floor.kind"},
		{"correction rule", func(d *AccumulatorDefinition) { d.Correction = PolicyRule{} }, "correction.kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDefinition()
			tc.mutate(&d)
			_, err := Publish(d)
			if !errors.Is(err, ErrPublicationRejected) || !errors.Is(err, ErrDefinitionInvalid) {
				t.Fatalf("error = %v, want both publication and definition errors", err)
			}
			var detail *DefinitionError
			if !errors.As(err, &detail) || detail.Field != tc.field {
				t.Fatalf("detail = %#v, want field %q", detail, tc.field)
			}
		})
	}
}

func TestValidateRejectsDuplicateDimensionsAndEntryTypes(t *testing.T) {
	d := validDefinition()
	d.Dimensions[1].Name = "WORKER_ID"
	if err := d.Validate(); err == nil {
		t.Fatal("duplicate dimensions accepted")
	}
	d = validDefinition()
	d.EntryTypes = append(d.EntryTypes, "grant")
	if err := d.Validate(); err == nil {
		t.Fatal("duplicate entry type accepted")
	}
}

// TestTodo_BAL_001 is the registry's primary acceptance test. Every required
// contract member is exercised through the public Publish boundary so a
// structurally incomplete definition can never be mistaken for a release.
func TestTodo_BAL_001(t *testing.T) {
	checks := []struct {
		name   string
		mutate func(*AccumulatorDefinition)
		field  string
	}{
		{"id", func(d *AccumulatorDefinition) { d.ID = " " }, "id"},
		{"version", func(d *AccumulatorDefinition) { d.Version = "" }, "version"},
		{"name", func(d *AccumulatorDefinition) { d.Name = "\t" }, "name"},
		{"unit", func(d *AccumulatorDefinition) { d.Unit = "" }, "unit"},
		{"currency", func(d *AccumulatorDefinition) { d.Currency = " " }, "currency"},
		{"subject", func(d *AccumulatorDefinition) { d.Subject = "" }, "subject"},
		{"period", func(d *AccumulatorDefinition) { d.Period = PeriodUnspecified }, "period"},
		{"dimensions", func(d *AccumulatorDefinition) { d.Dimensions = nil }, "dimensions"},
		{"entry_types", func(d *AccumulatorDefinition) { d.EntryTypes = nil }, "entry_types"},
		{"authority_source", func(d *AccumulatorDefinition) { d.Authority.SourceID = "" }, "authority.source_id"},
		{"authority_version", func(d *AccumulatorDefinition) { d.Authority.Version = "" }, "authority.version"},
		{"floor", func(d *AccumulatorDefinition) { d.Floor = PolicyRule{} }, "floor.kind"},
		{"cap", func(d *AccumulatorDefinition) { d.Cap = PolicyRule{} }, "cap.kind"},
		{"expiry", func(d *AccumulatorDefinition) { d.Expiry = PolicyRule{} }, "expiry.kind"},
		{"rollover", func(d *AccumulatorDefinition) { d.Rollover = PolicyRule{} }, "rollover.kind"},
		{"correction", func(d *AccumulatorDefinition) { d.Correction = PolicyRule{} }, "correction.kind"},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			d := validDefinition()
			tc.mutate(&d)
			_, err := Publish(d)
			assertRejectedField(t, err, tc.field, d.Version)
		})
	}
}

func assertRejectedField(t *testing.T, err error, field, version string) {
	t.Helper()
	if !errors.Is(err, ErrPublicationRejected) || !errors.Is(err, ErrDefinitionInvalid) {
		t.Fatalf("error = %v, want publication and definition rejection", err)
	}
	var detail *DefinitionError
	if !errors.As(err, &detail) || detail.Field != field {
		t.Fatalf("detail = %#v, want field %q", detail, field)
	}
	if want := "BAL_001_REJECTED definition=" + version; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want marker %q", err, want)
	}
}

// TestTodo_BAL_001_Property proves publication does not alias caller-owned
// contract slices, which would let a released definition change after review.
func TestTodo_BAL_001_Property(t *testing.T) {
	d := validDefinition()
	p, err := Publish(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Dimensions[0].Name = "changed"
	d.EntryTypes[0] = "changed"
	if p.Definition.Dimensions[0].Name == "changed" || p.Definition.EntryTypes[0] == "changed" {
		t.Fatal("publication aliases mutable caller slices")
	}
}

// TestTodo_BAL_001_Golden keeps the rejection marker stable for downstream
// conformance tooling without depending on the caller's payload values.
func TestTodo_BAL_001_Golden(t *testing.T) {
	d := validDefinition()
	d.Correction = PolicyRule{Kind: RuleConfigured, Version: "1"}
	_, err := Publish(d)
	assertRejectedField(t, err, "correction.value", d.Version)
	if got := err.Error(); !strings.Contains(got, "correction.value: is required for a configured rule") {
		t.Fatalf("error = %q, missing exact reason", got)
	}
}

// TestTodo_BAL_001_Conformance covers explicit rule states and the two
// relational uniqueness rules that define a balance identity.
func TestTodo_BAL_001_Conformance(t *testing.T) {
	for _, kind := range []RuleKind{RuleNone, RuleConfigured} {
		d := validDefinition()
		d.Floor = PolicyRule{Kind: kind, Version: "v1"}
		if kind == RuleConfigured {
			d.Floor.Value = "0"
		}
		if err := d.Validate(); err != nil {
			t.Fatalf("rule kind %q rejected: %v", kind, err)
		}
	}
	for name, mutate := range map[string]func(*AccumulatorDefinition){
		"dimension_case": func(d *AccumulatorDefinition) { d.Dimensions[1].Name = "WORKER_ID" },
		"entry_case":     func(d *AccumulatorDefinition) { d.EntryTypes[1] = "grant" },
	} {
		t.Run(name, func(t *testing.T) {
			d := validDefinition()
			mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("duplicate accepted")
			}
		})
	}
}

// TestTodo_BAL_001_Mutation is intentionally table-driven: each required
// policy rule must reject an omitted kind, not silently become unrestricted.
func TestTodo_BAL_001_Mutation(t *testing.T) {
	for _, field := range []string{"floor", "cap", "expiry", "rollover", "correction"} {
		t.Run(field, func(t *testing.T) {
			d := validDefinition()
			rule := func(*AccumulatorDefinition) {}
			switch field {
			case "floor":
				rule = func(d *AccumulatorDefinition) { d.Floor = PolicyRule{} }
			case "cap":
				rule = func(d *AccumulatorDefinition) { d.Cap = PolicyRule{} }
			case "expiry":
				rule = func(d *AccumulatorDefinition) { d.Expiry = PolicyRule{} }
			case "rollover":
				rule = func(d *AccumulatorDefinition) { d.Rollover = PolicyRule{} }
			case "correction":
				rule = func(d *AccumulatorDefinition) { d.Correction = PolicyRule{} }
			}
			rule(&d)
			if _, err := Publish(d); err == nil {
				t.Fatal("omitted rule accepted")
			}
		})
	}
}

// TestTodo_BAL_001_Race checks that read-only validation/publication is safe
// when several callers inspect the same immutable input concurrently.
func TestTodo_BAL_001_Race(t *testing.T) {
	d := validDefinition()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := Publish(d); err != nil {
				t.Errorf("publish %d: %v", i, err)
			}
			if err := d.Validate(); err != nil {
				t.Errorf("validate %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestTodo_BAL_001_ValidationReasons(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AccumulatorDefinition)
		field  string
	}{
		{"configured_value", func(d *AccumulatorDefinition) { d.Cap.Value = "" }, "cap.value"},
		{"none_value", func(d *AccumulatorDefinition) { d.Floor.Value = "0" }, "floor.value"},
		{"bad_kind", func(d *AccumulatorDefinition) { d.Expiry.Kind = RuleKind("UNKNOWN") }, "expiry.kind"},
		{"dimension_type", func(d *AccumulatorDefinition) { d.Dimensions[0].ValueType = " " }, "dimensions[0].value_type"},
		{"entry_blank", func(d *AccumulatorDefinition) { d.EntryTypes[0] = " " }, "entry_types[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDefinition()
			tc.mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid definition accepted")
			} else if !strings.Contains(err.Error(), fmt.Sprintf("%s:", tc.field)) {
				t.Fatalf("error = %v, want field %s", err, tc.field)
			}
		})
	}
}
