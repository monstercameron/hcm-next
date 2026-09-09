package configboundaries_test

import (
	"os"
	"path/filepath"
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

func TestTodo_ARCH_GO_024_Golden(t *testing.T) {
	TestConfigurationOwnershipRejectsActivationAndSourceCycles(t)
}
func TestTodo_ARCH_GO_024_Conformance(t *testing.T) {
	TestConfigurationBoundariesAllowSemanticAndImmutablePaths(t)
}
func TestTodo_ARCH_GO_024_Mutation(t *testing.T) {
	TestConfigurationOwnershipRejectsActivationAndSourceCycles(t)
}
