package reporting

import (
	"errors"
	"sync"
	"testing"
)

func reportFixture() *Registry {
	r := NewRegistry()
	if err := r.RegisterSource(Source{ID: "people", Version: "1", Classification: "internal", Owner: "people"}); err != nil {
		panic(err)
	}
	if err := r.RegisterField(Field{ID: "worker.id", Type: "string", SourceID: "people"}); err != nil {
		panic(err)
	}
	if err := r.RegisterField(Field{ID: "worker.department", Type: "string", SourceID: "people"}); err != nil {
		panic(err)
	}
	if err := r.RegisterMetric(Metric{ID: "headcount", Type: "integer", SourceID: "people", Expression: "count(worker.id)"}); err != nil {
		panic(err)
	}
	if err := r.RegisterPopulation(Population{ID: "active-workers", SourceID: "people", Predicate: "status = active"}); err != nil {
		panic(err)
	}
	return r
}

func reportDefinition() Definition {
	return Definition{ID: "headcount", Version: "1", Owner: "analytics", SourceIDs: []string{"people"}, Fields: []string{"worker.id", "worker.department"}, Metrics: []string{"headcount"}, Population: "active-workers", Parameters: []Parameter{{ID: "department", Type: "string", Default: "engineering", Scope: "department"}}, Filters: []Filter{{Field: "worker.department", Operator: "equals", Parameter: "department"}}, Sorts: []Sort{{Field: "worker.department"}}, Aggregations: []Aggregation{{Metric: "headcount", Function: "count"}}, Disclosure: DisclosurePolicy{Classification: "internal", SuppressSmallGroups: true, MinimumGroupSize: 5}, RenderFormats: []string{"table"}, ExportFormats: []string{"csv"}}
}

func TestReportDefinitionRejectsUnknownFieldsMetricsParametersAndUnsafeDefaults(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Definition)
		want   error
	}{
		{"unknown field", func(d *Definition) { d.Fields = []string{"missing"} }, ErrUnknownReference},
		{"unknown metric", func(d *Definition) { d.Metrics = []string{"missing"} }, ErrUnknownReference},
		{"unknown population", func(d *Definition) { d.Population = "missing" }, ErrUnknownReference},
		{"unknown parameter", func(d *Definition) { d.Filters[0].Parameter = "missing" }, ErrUnknownReference},
		{"broad default", func(d *Definition) { d.Parameters[0].Default = "*" }, ErrUnsafeDefault},
		{"all scope", func(d *Definition) { d.Parameters[0].Scope = "all" }, ErrUnsafeDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := reportDefinition()
			tc.mutate(&d)
			if _, err := reportFixture().Publish(d); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_REPORT_001_Property(t *testing.T) {
	r := reportFixture()
	d, err := r.Publish(reportDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if d.CompatibilityDigest == "" || d.CompatibilityDigest != d.Digest() {
		t.Fatalf("digest not content identity: %+v", d)
	}
	for i := 0; i < 20; i++ {
		got, ok := r.Report(d.ID, d.Version)
		if !ok || got.CompatibilityDigest != d.CompatibilityDigest {
			t.Fatal("published definition was not stable")
		}
	}
}

func TestTodo_REPORT_001_Golden(t *testing.T) {
	r := reportFixture()
	d, err := r.Publish(reportDefinition())
	if err != nil {
		t.Fatal(err)
	}
	// The exact value is intentionally derived from the canonical content;
	// republishing the same logical definition is the golden invariant.
	d2, err := r.Publish(reportDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if d2.CompatibilityDigest != d.CompatibilityDigest {
		t.Fatalf("digest changed: %s != %s", d2.CompatibilityDigest, d.CompatibilityDigest)
	}
}

func TestTodo_REPORT_001_Race(t *testing.T) {
	r := reportFixture()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = r.Publish(reportDefinition()); _, _ = r.Report("headcount", "1") }()
	}
	wg.Wait()
	if got := len(r.Versions("headcount")); got != 1 {
		t.Fatalf("versions=%d, want 1", got)
	}
}

func TestTodo_REPORT_001_Fault(t *testing.T) {
	r := reportFixture()
	d := reportDefinition()
	d.Disclosure = DisclosurePolicy{SuppressSmallGroups: true, MinimumGroupSize: 0}
	if _, err := r.Publish(d); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("error=%v", err)
	}
	d = reportDefinition()
	d.Filters[0].Field = "worker.id" // known field, but intentionally no issue
	if _, err := r.Publish(d); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_REPORT_001_Security(t *testing.T) {
	r := reportFixture()
	d := reportDefinition()
	d.SourceIDs = []string{"people"}
	d.Disclosure.Classification = "restricted"
	published, err := r.Publish(d)
	if err != nil {
		t.Fatal(err)
	}
	if published.Disclosure.Classification != "restricted" {
		t.Fatal("classification was not retained")
	}
	bad := reportDefinition()
	bad.SourceIDs = []string{"unknown-sensitive-source"}
	if _, err := r.Publish(bad); !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("error=%v", err)
	}
}

func TestTodo_REPORT_001_Conformance(t *testing.T) {
	r := reportFixture()
	d, err := r.Publish(reportDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.SourceIDs) == 0 || len(d.RenderFormats) == 0 || len(d.ExportFormats) == 0 || d.Owner == "" {
		t.Fatal("definition lacks required conformance fields")
	}
	dash, err := r.PublishDashboard(Dashboard{ID: "workforce", Version: "1", Owner: "analytics", Panels: []Panel{{ID: "headcount-panel", ReportID: d.ID, ReportDigest: d.CompatibilityDigest}}, RenderFormats: []string{"grid"}})
	if err != nil {
		t.Fatal(err)
	}
	if dash.CompatibilityDigest == "" {
		t.Fatal("dashboard digest missing")
	}
}

func TestTodo_REPORT_001_Mutation(t *testing.T) {
	r := reportFixture()
	d, err := r.Publish(reportDefinition())
	if err != nil {
		t.Fatal(err)
	}
	d.Fields[0] = "changed"
	d.Parameters[0].Default = "changed"
	d.RenderFormats[0] = "changed"
	got, ok := r.Report("headcount", "1")
	if !ok {
		t.Fatal("missing report")
	}
	if got.Fields[0] == "changed" || got.Parameters[0].Default == "changed" || got.RenderFormats[0] == "changed" {
		t.Fatal("registry exposed mutable state")
	}
	if _, err := r.Publish(Definition{ID: "headcount", Version: "1", Owner: "other", SourceIDs: []string{"people"}, Fields: []string{"worker.id"}, Population: "active-workers"}); !errors.Is(err, ErrImmutable) {
		t.Fatalf("error=%v, want immutable: %v", err, ErrImmutable)
	}
}
