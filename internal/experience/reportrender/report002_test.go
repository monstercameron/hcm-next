package reportrender

import (
	"bytes"
	"sync"
	"testing"
)

func reportFixture() ExecutionRequest {
	d, _ := Publish(ReportDefinition{ID: "headcount", Version: 1, Fields: []string{"name", "department"}, Population: "tenant/workers", Formats: []string{"json", "csv", "html"}})
	return ExecutionRequest{Definition: d, Authorization: Authorization{Principal: Principal{ID: "analyst-1", Tenant: "tenant", Purpose: "workforce-planning"}, Scope: Scope{Population: "tenant/workers", Fields: []string{"name", "department"}}, Allow: func(Principal, Scope) bool { return true }}, Sources: []Source{{Name: "workforce", Watermark: Watermark{Source: "workforce", Value: "ledger-42"}, Complete: true, Rows: []Row{{"name": "Ada", "department": "Engineering"}, {"name": "Lin", "department": "People"}}}}}
}

func TestReportExecutionBindsAuthorizationPopulationWatermarkAndExactOutput(t *testing.T) {
	r, err := Execute(reportFixture())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Complete || r.PrincipalID != "analyst-1" || r.Population != "tenant/workers" || r.WatermarkText != "workforce=ledger-42" {
		t.Fatalf("unexpected result: %+v", r)
	}
	a, _ := r.Render("json")
	b, _ := r.Export("json")
	if !bytes.Equal(a.Bytes, b.Bytes) || a.Digest != b.Digest {
		t.Fatal("view and export diverged")
	}
	if !bytes.Contains(a.Bytes, []byte("ledger-42")) {
		t.Fatal("watermark missing from JSON")
	}
}

func TestTodo_REPORT_002_Property(t *testing.T) {
	a, _ := Execute(reportFixture())
	b, _ := Execute(reportFixture())
	if a.EvidenceDigest != b.EvidenceDigest || a.RowDigest != b.RowDigest {
		t.Fatal("execution is not deterministic")
	}
}
func TestTodo_REPORT_002_Golden(t *testing.T) {
	r, _ := Execute(reportFixture())
	got, _ := r.Render("csv")
	want := "# watermark=workforce=ledger-42\ndepartment,name\nEngineering,Ada\nPeople,Lin\n"
	if string(got.Bytes) != want {
		t.Fatalf("csv golden mismatch: %q", got.Bytes)
	}
}
func TestTodo_REPORT_002_Race(t *testing.T) {
	req := reportFixture()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := Execute(req)
			if err != nil || r.RowDigest == "" {
				t.Errorf("concurrent execution failed: %v", err)
			}
		}()
	}
	wg.Wait()
}
func TestTodo_REPORT_002_Fault(t *testing.T) {
	req := reportFixture()
	req.Sources[0].Complete = false
	r, err := Execute(req)
	if err != nil || r.Status != Partial {
		t.Fatalf("want partial, got %s/%v", r.Status, err)
	}
	req.Sources[0].Watermark.Value = ""
	r, err = Execute(req)
	if err != nil || r.Status != Unknown {
		t.Fatalf("want unknown, got %s/%v", r.Status, err)
	}
}
func TestTodo_REPORT_002_Security(t *testing.T) {
	req := reportFixture()
	req.Authorization.Allow = nil
	if _, err := Execute(req); err != ErrUnauthorized {
		t.Fatalf("want fail closed, got %v", err)
	}
	req = reportFixture()
	req.Authorization.Scope.Population = "tenant/all"
	if _, err := Execute(req); err != ErrUnauthorized {
		t.Fatalf("want population denial, got %v", err)
	}
}
func TestTodo_REPORT_002_Conformance(t *testing.T) {
	r, _ := Execute(reportFixture())
	for _, f := range []string{"json", "csv", "html"} {
		out, err := r.Render(f)
		if err != nil || out.Watermark != "workforce=ledger-42" || !bytes.Contains(out.Bytes, []byte("ledger-42")) {
			t.Fatalf("%s watermark mismatch", f)
		}
	}
}
func TestTodo_REPORT_002_Mutation(t *testing.T) {
	req := reportFixture()
	original := req.Sources[0].Rows[0]["name"]
	r, _ := Execute(req)
	r.Rows[0]["name"] = "mutated"
	if req.Sources[0].Rows[0]["name"] != original {
		t.Fatal("result mutation leaked into request")
	}
	if r.RowDigest == "" {
		t.Fatal("missing digest")
	}
}
