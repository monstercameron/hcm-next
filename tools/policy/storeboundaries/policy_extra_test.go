package storeboundaries

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition"
)

func policyRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func TestPolicy_HelperFormattingAndConversions(t *testing.T) {
	if got := funcSuffix("store.go", ""); got != "/store.go" {
		t.Fatalf("funcSuffix without function = %q", got)
	}
	if got := funcSuffix("store.go", "List"); got != "/store.go:List" {
		t.Fatalf("funcSuffix with function = %q", got)
	}
	tenant := TenantFinding{Package: "pkg", File: "store.go", Func: "List", Verb: VerbSelect, Tables: []string{"worker"}}
	if got := tenantFindingToViolation(tenant); got.Kind != KindTenantScope || got.Package != "pkg" || got.File != "store.go" || got.Func != "List" || got.Message == "" {
		t.Fatalf("tenant conversion lost evidence: %+v", got)
	}
	pool := PoolFinding{Package: "pkg"}
	if got := poolFindingToViolation(pool); got.Kind != KindPoolImport || got.Package != "pkg" || got.Message == "" {
		t.Fatalf("pool conversion lost evidence: %+v", got)
	}
	if got := DefaultPolicy(); got.PolicyDate != PolicyDate || len(got.Exceptions) != 0 {
		t.Fatalf("DefaultPolicy() = %+v", got)
	}
	for _, tc := range []struct {
		name         string
		expiry, date string
		want         bool
	}{
		{"same date", "2026-09-05", "2026-09-05", true},
		{"future", "2026-09-06", "2026-09-05", true},
		{"expired", "2026-09-04", "2026-09-05", false},
		{"malformed expiry", "2026-9-05", "2026-09-05", false},
		{"malformed date", "2026-09-05", "today", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Exception{Expiry: tc.expiry}).Active(tc.date); got != tc.want {
				t.Fatalf("Active(%q) = %v, want %v", tc.date, got, tc.want)
			}
		})
	}
}

func TestPolicy_ExceptionCoverageAndFiltering(t *testing.T) {
	v := Violation{Kind: KindTenantScope, Package: "pkg", File: "store.go", Func: "List", Message: "scope"}
	base := Exception{Kind: KindTenantScope, Package: "pkg", Owner: "owner", Rationale: "reason", FollowupTodo: "TODO", Expiry: "2099-01-01"}
	for _, tc := range []struct {
		name string
		ex   Exception
		want bool
	}{
		{"matching exception", base, true},
		{"missing kind", func() Exception { e := base; e.Kind = ""; return e }(), false},
		{"missing package", func() Exception { e := base; e.Package = ""; return e }(), false},
		{"wrong kind", func() Exception { e := base; e.Kind = KindPoolImport; return e }(), false},
		{"wrong package", func() Exception { e := base; e.Package = "other"; return e }(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ex.Covers(v); got != tc.want {
				t.Fatalf("Covers() = %v, want %v", got, tc.want)
			}
		})
	}
	other := v
	other.Package = "other"
	raw := []Violation{other, v}
	policy := Policy{PolicyDate: "2026-01-01", Exceptions: []Exception{base}}
	remaining := CheckWithPolicy(raw, policy)
	if len(remaining) != 1 || remaining[0].Package != "other" {
		t.Fatalf("CheckWithPolicy remaining = %+v, want only other package", remaining)
	}
	invalid := base
	invalid.Owner = ""
	if got := CheckWithPolicy([]Violation{v}, Policy{Exceptions: []Exception{invalid}}); len(got) != 1 {
		t.Fatalf("incomplete exception waived a finding: %+v", got)
	}
	if got := CheckWithPolicy([]Violation{v}, Policy{PolicyDate: "2026-01-01", Exceptions: []Exception{func() Exception { e := base; e.Expiry = "2020-01-01"; return e }()}}); len(got) != 1 {
		t.Fatalf("expired exception waived a finding: %+v", got)
	}
	if got := CheckWithPolicy([]Violation{v}, Policy{PolicyDate: "2026-01-01", Exceptions: []Exception{func() Exception { e := base; e.ReplacementPlan = "plan"; e.FollowupTodo = ""; return e }()}}); len(got) != 0 {
		t.Fatalf("replacement-plan exception did not waive a finding: %+v", got)
	}
}

func TestPolicy_LoadAndSourceHelpers(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(valid, []byte("exceptions: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPolicy(valid)
	if err != nil || loaded.PolicyDate != PolicyDate {
		t.Fatalf("LoadPolicy default date = %+v, err=%v", loaded, err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("exceptions: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(bad); err == nil || !strings.Contains(err.Error(), "parsing policy") {
		t.Fatalf("malformed policy error = %v", err)
	}
	if _, err := LoadPolicy(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "reading policy") {
		t.Fatalf("missing policy error = %v", err)
	}
	if got := nonTestGoFiles([]string{"a.go", "b_test.go", "c.go"}); len(got) != 2 || got[1] != "c.go" {
		t.Fatalf("nonTestGoFiles = %v", got)
	}
	pkgs, err := goList(policyRepoRoot(t), "./tools/policy/storeboundaries")
	if err != nil || len(pkgs) != 1 || pkgs[0].ImportPath == "" {
		t.Fatalf("goList = %+v, err=%v", pkgs, err)
	}
	sources, err := loadPackageSources(policyRepoRoot(t), "./tools/policy/storeboundaries")
	if err != nil || len(sources) != 1 || len(sources[0].Files) == 0 {
		t.Fatalf("loadPackageSources = %+v, err=%v", sources, err)
	}
}

func TestPolicy_ReportAndRegistryHelpers(t *testing.T) {
	report := Report{
		TenantScope: []TenantFinding{{Package: "z", File: "z.go", Func: "Z", Verb: VerbSelect, Tables: []string{"worker"}}},
		PoolImport:  []PoolFinding{{Package: "a"}},
	}
	violations := report.Violations()
	if len(violations) != 2 || violations[0].Kind != KindPoolImport || violations[1].Kind != KindTenantScope {
		t.Fatalf("Violations() = %+v, want deterministic kind order", violations)
	}
	reg := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{
		{Table: "z", EncryptionClass: storagedisposition.EncryptionFieldLevel},
		{Table: "a", EncryptionClass: storagedisposition.EncryptionFieldLevel},
		{Table: "plain", EncryptionClass: storagedisposition.EncryptionPlatformManaged},
	}}
	if got := fieldLevelTables(reg); strings.Join(got, ",") != "a,z" {
		t.Fatalf("fieldLevelTables = %v", got)
	}
}
