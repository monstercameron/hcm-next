package telemetry

import (
	"errors"
	"testing"
)

func TestTelemetrySchemaLinterRejectsUnknownDynamicOrBreakingDefinitions(t *testing.T) {
	allow, err := DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	gate := NewSchemaGate(allow, MetricCatalog())
	findings := gate.Lint([]SemanticDefinition{
		{File: "telemetry.go", Symbol: "dynamic", Kind: SemanticSpan, Name: "hcmnext.span.{tenant}", Version: 1, Owner: "owner", Description: "bad"},
		{File: "telemetry.go", Symbol: "unknown", Kind: SemanticAttribute, Name: "password", Signal: SignalSpan, Version: 1, Owner: "owner", Description: "bad"},
		{File: "telemetry.go", Symbol: "rename", Kind: SemanticEvent, Name: "workflow.done", PreviousName: "workflow.complete", Version: 1, Owner: "owner", Description: "bad"},
	})
	if len(findings) != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	if err := gate.Refuse([]SemanticDefinition{{File: "telemetry.go", Symbol: "unknown", Kind: SemanticAttribute, Name: "password", Signal: SignalSpan, Version: 1, Owner: "owner", Description: "bad"}}); err == nil {
		t.Fatal("schema gate accepted unknown attribute")
	} else {
		var typed *FindingsError
		if !errors.As(err, &typed) || typed.Findings[0].Code != FindingUnknownAttribute {
			t.Fatalf("untyped schema rejection: %v", err)
		}
	}
}

func TestTodo_OBS_020_Property(t *testing.T) {
	allow, _ := DefaultAllowlist()
	gate := NewSchemaGate(allow, MetricCatalog())
	ok, finding := gate.Observe(SignalSpan, "outcome", "success")
	if !ok || finding.Code != "" {
		t.Fatalf("registered observation rejected: %v", finding)
	}
}

func TestTodo_OBS_020_Golden(t *testing.T) {
	allow, _ := DefaultAllowlist()
	gate := NewSchemaGate(allow, MetricCatalog())
	_, finding := gate.Observe(SignalSpan, "not_registered", "secret")
	if finding.Code != FindingUnknownAttribute || finding.Semantic != "not_registered" || finding.Detail == "" {
		t.Fatalf("finding = %+v", finding)
	}
}

func TestTodo_OBS_020_Conformance(t *testing.T) {
	allow, _ := DefaultAllowlist()
	gate := NewSchemaGate(allow, MetricCatalog())
	if err := gate.Refuse([]SemanticDefinition{{File: "metric.go", Symbol: "metric", Kind: SemanticMetric, Name: "not.catalogued", Version: 1, Unit: "1", Owner: "owner", Description: "metric"}}); err == nil {
		t.Fatal("unknown metric accepted")
	}
	bounded, err := NewAllowlist(AttributeDefinition{
		Key: "bounded", Class: ClassOperationalPublic,
		Signals: []SignalKind{SignalMetric}, MaxCardinality: 1,
		Description: "test bound",
	})
	if err != nil {
		t.Fatal(err)
	}
	cardinality := NewSchemaGate(bounded, nil)
	if ok, _ := cardinality.Observe(SignalMetric, "bounded", "one"); !ok {
		t.Fatal("first bounded value rejected")
	}
	if ok, finding := cardinality.Observe(SignalMetric, "bounded", "two"); ok || finding.Code != FindingHighCardinality || finding.Limit != 1 {
		t.Fatalf("high-cardinality value accepted: ok=%v finding=%+v", ok, finding)
	}
}

func TestTodo_OBS_020_Mutation(t *testing.T) {
	allow, _ := DefaultAllowlist()
	gate := NewSchemaGate(allow, MetricCatalog())
	for i := 0; i < 2; i++ {
		if ok, _ := gate.Observe(SignalMetric, "cell_id", string(rune('a'+i))); !ok {
			t.Fatalf("bounded cardinality rejected value %d", i)
		}
	}
}
