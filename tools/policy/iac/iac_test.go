package iac

import (
	"strings"
	"testing"
	"time"
)

func validResource() Resource {
	return Resource{ID: "cell-a", Kind: "workload", Egress: []string{"api.example.test/443"}, Image: "hcm/app", ImageDigest: "sha256:" + strings.Repeat("a", 64), ImageSignatureVerified: true, Encryption: true, Backup: true, Tags: map[string]string{"owner": "platform", "environment": "test", "data_classification": "internal"}}
}

func TestTodo_IAC_003(t *testing.T) {
	in := Input{Resources: []Resource{validResource()}, ExpectedPlan: []byte(`{"resources":["cell-a"]}`), ActualPlan: []byte(`{"resources":["cell-a"]}`)}
	if report := Validate(in); !report.OK() {
		t.Fatalf("valid input rejected: %+v", report.Findings)
	}
	bad := validResource()
	bad.Kind, bad.Public = "database", true
	bad.Egress = []string{"*"}
	bad.Encryption, bad.Backup, bad.Tags = false, false, map[string]string{}
	workload := validResource()
	workload.ID, workload.ImageDigest, workload.ImageSignatureVerified = "workload-a", "latest", false
	report := Validate(Input{Resources: []Resource{bad, workload}, Changes: []PlanChange{{ResourceID: bad.ID, Action: "destroy"}}})
	for _, code := range []string{"PUBLIC_DATABASE", "WILDCARD_EGRESS", "MUTABLE_IMAGE", "UNVERIFIED_IMAGE", "MISSING_ENCRYPTION", "MISSING_BACKUP", "MISSING_TAGS", "DESTRUCTIVE_CHANGE"} {
		if !hasFinding(report, code) {
			t.Errorf("missing finding %s in %+v", code, report.Findings)
		}
	}
}

func TestTodo_IAC_003_Integration(t *testing.T) {
	in := Input{Resources: []Resource{validResource()}, ExpectedPlan: []byte("reviewed"), ActualPlan: []byte("changed")}
	if err := Check(in); err == nil || !strings.Contains(err.Error(), "PLAN_DIFF") {
		t.Fatalf("plan drift error = %v", err)
	}
}

func TestTodo_IAC_003_Recovery(t *testing.T) {
	r := validResource()
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	report := Validate(Input{Resources: []Resource{r}, Changes: []PlanChange{{ResourceID: r.ID, Action: "replace"}}, Exceptions: []Exception{{ID: "exc-1", ResourceID: r.ID, Code: "DESTRUCTIVE_CHANGE", Owner: "human", Rationale: "recovery rehearsal", ExpiresAt: at.Add(time.Hour)}}, EvaluatedAt: at})
	if !report.OK() {
		t.Fatalf("reviewed exception did not recover admission: %+v", report.Findings)
	}
}

func hasFinding(report Report, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
