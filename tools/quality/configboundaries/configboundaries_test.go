package configboundaries_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/configboundaries"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func has(fs []configboundaries.Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestConfigurationOwnershipRejectsActivationAndSourceCycles(t *testing.T) {
	root := fixture(t, map[string]string{
		"definitions/customer/source.go":   "package source\nimport _ \"example/internal/configuration\"\n",
		"internal/configuration/engine.go": "package configuration\nimport _ \"example/internal/domains/leave\"\nfunc Apply() {}\nfunc ExecuteChange() {}\n",
		"internal/rollout/plan.go":         "package rollout\nfunc RewriteBundle() {}\nfunc Run() { RewriteBundle() }\n",
		"internal/customer/config.go":      "package customer\nfunc Activate() {}\nfunc Publish() {}\nfunc Apply() { Activate(); Publish() }\n",
	})
	findings := configboundaries.Check(root)
	for _, code := range []string{configboundaries.DefinitionsRuntimeImport, configboundaries.ConfigurationDomainImport, configboundaries.ConfigurationExecutesDomain, configboundaries.RolloutRewritesBundle, configboundaries.CustomerConfigBypassesPublication} {
		if !has(findings, code) {
			t.Fatalf("missing %s in %#v", code, findings)
		}
	}
}

func TestConfigurationBoundariesAllowSemanticAndImmutablePaths(t *testing.T) {
	root := fixture(t, map[string]string{
		"definitions/forms/source.yaml":      "name: leave\n",
		"internal/configuration/registry.go": "package configuration\ntype Snapshot struct{}\nfunc Select() {}\n",
		"internal/rollout/plan.go":           "package rollout\nfunc TargetVersion() {}\n",
		"internal/customer/view.go":          "package customer\nfunc ReadConfig() {}\n",
	})
	if findings := configboundaries.Check(root); len(findings) != 0 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestConfigurationBoundaryCheckIsDeterministic(t *testing.T) {
	root := fixture(t, map[string]string{"internal/configuration/a.go": "package configuration\nimport _ \"example/internal/data/store\"\n"})
	a, b := configboundaries.Check(root), configboundaries.Check(root)
	if len(a) == 0 || len(a) != len(b) || a[0].String() != b[0].String() {
		t.Fatalf("non-deterministic findings: %#v %#v", a, b)
	}
}

func adversaryFixture() map[string]string {
	return map[string]string{
		"definitions/customer/source.go":   "package source\nimport _ \"example/internal/configuration\"\n",
		"internal/configuration/engine.go": "package configuration\nimport _ \"example/internal/domains/leave\"\nfunc Apply() {}\nfunc ExecuteChange() {}\n",
		"internal/rollout/plan.go":         "package rollout\nfunc RewriteBundle() {}\nfunc Run() { RewriteBundle() }\n",
		"internal/customer/config.go":      "package customer\nfunc Activate() {}\nfunc Publish() {}\nfunc Apply() { Activate(); Publish() }\n",
	}
}

func TestTodo_ARCH_GO_024_Golden(t *testing.T) {
	root := fixture(t, adversaryFixture())
	findings := configboundaries.Check(root)
	var lines []string
	for _, f := range findings {
		lines = append(lines, f.String())
	}
	got, err := json.MarshalIndent(lines, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_ARCH_GO_024_Conformance(t *testing.T) {
	root := fixture(t, map[string]string{
		"definitions/forms/source.yaml":      "name: leave\n",
		"internal/configuration/registry.go": "package configuration\ntype Snapshot struct{}\nfunc Select() {}\nfunc Activate() {}\n",
		"internal/configuration/approve.go":  "package configuration\nfunc Approve() {}\nfunc Rollback() {}\n",
		"internal/rollout/plan.go":           "package rollout\nfunc TargetVersion() {}\n",
		"internal/customer/view.go":          "package customer\nfunc ReadConfig() {}\n",
		"internal/domains/leave/service.go":  "package leave\nfunc ApproveLeave() {}\n",
		"internal/store/postgres/records.go": "package postgres\nfunc SaveRecord() {}\n",
	})
	if findings := configboundaries.Check(root); len(findings) != 0 {
		t.Fatalf("semantic and immutable paths flagged: %#v", findings)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found")
		}
		dir = parent
	}
	if findings := configboundaries.Check(filepath.Join(dir, "internal", "configuration")); len(findings) != 0 {
		t.Fatalf("owned configuration contracts violate their own boundary: %#v", findings)
	}
}

func TestTodo_ARCH_GO_024_Mutation(t *testing.T) {
	singles := []struct {
		name string
		file string
		body string
		code string
	}{
		{"definitions runtime import", "definitions/a/source.go", "package source\nimport _ \"example/internal/configuration\"\n", configboundaries.DefinitionsRuntimeImport},
		{"configuration domain import", "internal/configuration/a.go", "package configuration\nimport _ \"example/internal/domains/leave\"\n", configboundaries.ConfigurationDomainImport},
		{"configuration executes domain", "internal/configuration/b.go", "package configuration\nfunc ExecuteChange() {}\n", configboundaries.ConfigurationExecutesDomain},
		{"rollout rewrites bundle", "internal/rollout/a.go", "package rollout\nfunc Run() { RewriteBundle() }\n", configboundaries.RolloutRewritesBundle},
		{"customer bypasses publication", "internal/customer/a.go", "package customer\nfunc Run() { Activate() }\n", configboundaries.CustomerConfigBypassesPublication},
	}
	for _, tc := range singles {
		t.Run("recall_"+tc.name, func(t *testing.T) {
			root := fixture(t, map[string]string{tc.file: tc.body})
			if findings := configboundaries.Check(root); !has(findings, tc.code) {
				t.Fatalf("single adversary missed, findings=%#v", findings)
			}
		})
	}
	renamed := adversaryFixture()
	renamed["internal/rollout/plan.go"] = "package rollout\nfunc TargetVersion() {}\nfunc Run() { TargetVersion() }\n"
	t.Run("dropout_rollout_rewrite_removed", func(t *testing.T) {
		findings := configboundaries.Check(fixture(t, renamed))
		if has(findings, configboundaries.RolloutRewritesBundle) {
			t.Fatalf("renamed rollout still flagged: %#v", findings)
		}
		for _, code := range []string{configboundaries.DefinitionsRuntimeImport, configboundaries.ConfigurationDomainImport, configboundaries.ConfigurationExecutesDomain, configboundaries.CustomerConfigBypassesPublication} {
			if !has(findings, code) {
				t.Fatalf("unrelated detector %s stopped firing: %#v", code, findings)
			}
		}
	})
}
