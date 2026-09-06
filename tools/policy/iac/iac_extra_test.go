package iac

import (
	"strings"
	"testing"
	"time"
)

func TestIAC_MetadataAndDigest(t *testing.T) {
	if Version() != 1 || !strings.Contains(Explain(), "IAC-003") {
		t.Fatalf("policy metadata = version %d, explain %q", Version(), Explain())
	}
	first := Digest([]byte("plan"))
	if !strings.HasPrefix(first, "sha256:") || first != Digest([]byte("plan")) || first == Digest([]byte("other")) {
		t.Fatalf("Digest is not stable/content-addressed: %q", first)
	}
	if (Report{}).OK() != true || (Report{Findings: []Finding{{Code: "BAD"}}}).OK() {
		t.Fatal("Report.OK does not reflect findings")
	}
}

func TestIAC_ResourceFindingBranches(t *testing.T) {
	base := validResource()
	cases := []struct {
		name   string
		mutate func(*Resource)
		code   string
	}{
		{"missing id", func(r *Resource) { r.ID = "" }, "MISSING_ID"},
		{"public database", func(r *Resource) { r.Kind, r.Public = " DATABASE ", true }, "PUBLIC_DATABASE"},
		{"wildcard star", func(r *Resource) { r.Egress = []string{"*"} }, "WILDCARD_EGRESS"},
		{"wildcard ipv4", func(r *Resource) { r.Egress = []string{"0.0.0.0/0"} }, "WILDCARD_EGRESS"},
		{"wildcard ipv6", func(r *Resource) { r.Egress = []string{"::/0"} }, "WILDCARD_EGRESS"},
		{"wildcard any", func(r *Resource) { r.Egress = []string{"ANY"} }, "WILDCARD_EGRESS"},
		{"wildcard internet", func(r *Resource) { r.Egress = []string{"internet"} }, "WILDCARD_EGRESS"},
		{"mutable image", func(r *Resource) { r.ImageDigest = "latest" }, "MUTABLE_IMAGE"},
		{"short digest", func(r *Resource) { r.ImageDigest = "sha256:" + strings.Repeat("a", 63) }, "MUTABLE_IMAGE"},
		{"unverified image", func(r *Resource) { r.ImageSignatureVerified = false }, "UNVERIFIED_IMAGE"},
		{"missing encryption", func(r *Resource) { r.Encryption = false }, "MISSING_ENCRYPTION"},
		{"missing backup", func(r *Resource) { r.Backup = false }, "MISSING_BACKUP"},
		{"missing tags", func(r *Resource) { r.Tags = map[string]string{"owner": "team", "environment": "test"} }, "MISSING_TAGS"},
		{"non-workload does not need image", func(r *Resource) { r.Kind = "service"; r.ImageDigest = ""; r.ImageSignatureVerified = false }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource := base
			tc.mutate(&resource)
			findings := resourceFindings(resource)
			if tc.code == "" {
				if len(findings) != 0 {
					t.Fatalf("resourceFindings = %+v, want clean", findings)
				}
				return
			}
			for _, finding := range findings {
				if finding.Code == tc.code {
					return
				}
			}
			t.Fatalf("resourceFindings = %+v, missing %s", findings, tc.code)
		})
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"*", true}, {"  Any ", true}, {"internet", true}, {"10.0.0.0/8", false}, {"", false},
	} {
		if got := isWildcard(tc.value); got != tc.want {
			t.Errorf("isWildcard(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestIAC_ValidateExceptionAndPlanBranches(t *testing.T) {
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	resource := validResource()
	changed := Input{Resources: []Resource{resource}, ExpectedPlan: []byte("expected"), ActualPlan: []byte("actual"), EvaluatedAt: at, Changes: []PlanChange{{ResourceID: resource.ID, Action: " destroy "}, {ResourceID: resource.ID, Action: "REPLACE"}, {ResourceID: resource.ID, Action: "update"}}}
	report := Validate(changed)
	if !hasFinding(report, "PLAN_DIFF") || !hasFinding(report, "DESTRUCTIVE_CHANGE") || report.ExpectedDigest == "" || report.ActualDigest == "" {
		t.Fatalf("plan/change report = %+v", report)
	}
	invalid := Exception{ID: "invalid", ResourceID: resource.ID, Code: "DESTRUCTIVE_CHANGE"}
	duplicate := Exception{ID: "duplicate", ResourceID: resource.ID, Code: "DESTRUCTIVE_CHANGE", Owner: "owner", Rationale: "why", ExpiresAt: at.Add(time.Hour)}
	duplicateTwo := duplicate
	duplicateTwo.ID = "duplicate-two"
	expired := Exception{ID: "expired", ResourceID: resource.ID, Code: "MISSING_BACKUP", Owner: "owner", Rationale: "why", ExpiresAt: at}
	report = Validate(Input{Resources: []Resource{{ID: "other", Kind: "service", Encryption: true, Backup: false, Tags: map[string]string{"owner": "o", "environment": "e", "data_classification": "c"}}}, Exceptions: []Exception{invalid, duplicate, duplicateTwo, expired}, EvaluatedAt: at})
	for _, code := range []string{"INVALID_EXCEPTION", "DUPLICATE_EXCEPTION", "EXPIRED_EXCEPTION", "MISSING_BACKUP"} {
		if !hasFinding(report, code) {
			t.Errorf("report missing %s: %+v", code, report.Findings)
		}
	}
	validException := Exception{ID: "approved", ResourceID: resource.ID, Code: "DESTRUCTIVE_CHANGE", Owner: "owner", Rationale: "reviewed", ExpiresAt: at.Add(time.Hour)}
	recovered := Validate(Input{Resources: []Resource{resource}, Changes: []PlanChange{{ResourceID: resource.ID, Action: "replace"}}, Exceptions: []Exception{validException}, EvaluatedAt: at})
	if !recovered.OK() || len(recovered.Applied) != 1 || recovered.Applied[0] != "approved" {
		t.Fatalf("valid exception did not recover admission: %+v", recovered)
	}
	if err := Check(Input{Resources: []Resource{resource}, Changes: []PlanChange{{ResourceID: resource.ID, Action: "destroy"}}}); err == nil || !strings.Contains(err.Error(), "DESTRUCTIVE_CHANGE") {
		t.Fatalf("Check error = %v", err)
	}
	if err := Check(Input{Resources: []Resource{resource}}); err != nil {
		t.Fatalf("Check rejected valid input: %v", err)
	}
}
