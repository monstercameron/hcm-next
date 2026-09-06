package demand

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func demandInstant(at time.Time) values.Instant { return values.NewInstant(at.UTC()) }

func demandWindow(t *testing.T, start, end time.Time) values.EffectiveInterval {
	t.Helper()
	window, err := values.NewInstantInterval(demandInstant(start), demandInstant(end))
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func demandRule(t *testing.T) BucketRule {
	t.Helper()
	return BucketRule{
		ID: "hourly", Version: "bucket/v1", Origin: demandInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Width: time.Hour, ResultScale: 2, Rounding: values.RoundingHalfEven,
	}
}

func demandSignal(t *testing.T, id, source, sourceRef, quantity, unit, confidence string, start, end time.Time) DemandSignal {
	t.Helper()
	amount, err := values.NewQuantity(quantity, unit, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	factor, err := values.NewDecimal(confidence, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return DemandSignal{
		SignalID: id, Work: demandWindow(t, start, end), OrgUnit: "org:clinical", Role: "role:nurse",
		Quantity: amount, Unit: unit, Priority: 1, Source: source, SourceRef: sourceRef,
		Confidence: factor, Scenario: "base", Version: "signals/v1",
	}
}

func TestTodo_DEMAND_002(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	a := demandSignal(t, "signal-a", "forecast", "source-a", "2.00", "FTE", "0.50", start, end)
	b := demandSignal(t, "signal-b", "survey", "source-b", "4.00", "FTE", "0.75", start, end)
	duplicate := a
	duplicate.SignalID = "signal-z-duplicate"
	result, err := AggregateSignals([]DemandSignal{b, duplicate, a}, demandRule(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Aggregates) != 1 {
		t.Fatalf("aggregates = %d, want one bucket", len(result.Aggregates))
	}
	item := result.Aggregates[0]
	if item.WeightedQuantity.String() != "4.00 FTE" || len(item.Sources) != 2 || item.FormulaVersion != AggregationFormulaVersion || item.RuleVersion != "bucket/v1" {
		t.Fatalf("aggregate = %+v", item)
	}
	if result.CanonicalDigest == "" {
		t.Fatal("aggregation did not receive a digest")
	}
	explanation, err := result.Explain()
	if err != nil || explanation.FormulaVersion != AggregationFormulaVersion || len(explanation.Buckets) != 1 || explanation.Buckets[0].Total != "4.00 FTE" {
		t.Fatalf("Explain = %+v, %v", explanation, err)
	}
}

func TestTodo_DEMAND_002_Property(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	a := demandSignal(t, "signal-a", "forecast", "source-a", "2.00", "FTE", "0.50", start, end)
	b := demandSignal(t, "signal-b", "survey", "source-b", "4.00", "FTE", "0.75", start, end)
	forward, err := Aggregate(AggregationRequest{Signals: []DemandSignal{a, b}, BucketRule: demandRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := Aggregate(AggregationRequest{Signals: []DemandSignal{b, a}, BucketRule: demandRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	if forward.CanonicalDigest != reverse.CanonicalDigest || string(forward.Canonical()) != string(reverse.Canonical()) {
		t.Fatal("input order changed aggregate digest")
	}
	if err := forward.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_DEMAND_002_Fault(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	rule := demandRule(t)
	first := demandSignal(t, "signal-a", "forecast", "source-a", "2.00", "FTE", "0.50", start, start.Add(30*time.Minute))
	second := demandSignal(t, "signal-b", "forecast", "source-a", "2.00", "FTE", "0.50", start.Add(15*time.Minute), start.Add(45*time.Minute))
	if _, err := AggregateSignals([]DemandSignal{first, second}, rule); !errors.Is(err, ErrOverlappingSourceWindows) {
		t.Fatalf("overlapping source windows error = %v", err)
	}

	incompatible := demandSignal(t, "signal-c", "survey", "source-c", "2.00", "HOURS", "0.50", start, start.Add(30*time.Minute))
	if _, err := AggregateSignals([]DemandSignal{first, incompatible}, rule); !errors.Is(err, ErrAggregationUnitMismatch) {
		t.Fatalf("incompatible units error = %v", err)
	}

	spanning := demandSignal(t, "signal-d", "forecast", "source-d", "2.00", "FTE", "0.50", start.Add(45*time.Minute), start.Add(75*time.Minute))
	if _, err := AggregateSignals([]DemandSignal{spanning}, rule); !errors.Is(err, ErrWindowSpansBuckets) {
		t.Fatalf("spanning window error = %v", err)
	}

	missingSource := first
	missingSource.SourceRef = ""
	if _, err := AggregateSignals([]DemandSignal{missingSource}, rule); !errors.Is(err, ErrSourceRefRequired) {
		t.Fatalf("missing source ref error = %v", err)
	}

	aggregation, err := AggregateSignals([]DemandSignal{first}, rule)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(aggregation.Aggregates[0].Bucket.Key, "source-a") {
		t.Fatal("source identity must remain a partition, not a bucket key")
	}
}
