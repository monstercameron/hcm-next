package balance

import (
	"errors"
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
