package integrationboundaries_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/integrationboundaries"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hasCode(fs []integrationboundaries.Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestIntegrationPackagesRejectDuplicateTransformAndProviderLeakage(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/integration/mapping/mapping.go": `package mapping
type Op string
func Execute() {}
`,
		"internal/domains/payroll/pay.go": `package payroll
import "example/internal/connectivity/provider/workday"
var _ = workday.Client{}
`,
		"internal/integration/observe/observe.go": `package observe
import "example/internal/connectivity/provider/workday"
type Observation struct { Response workday.Response }
`,
		"internal/integration/reconcile/reconcile.go": `package reconcile
import "example/internal/connectivity/provider/workday"
func Repair(c workday.Client) { c.Update() }
`,
	})
	findings := integrationboundaries.Check(root)
	for _, code := range []string{"duplicate-transform", "provider-leak", "provider-type-leak", "reconcile-provider-mutation"} {
		if !hasCode(findings, code) {
			t.Errorf("expected %s finding, got %#v", code, findings)
		}
	}
}

func TestIntegrationBoundariesAcceptDelegationAndNormalizedContracts(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/integration/mapping/mapping.go": `package mapping
import "example/internal/engines/transformation"
var _ transformation.Operation
`,
		"internal/integration/observe/observe.go": `package observe
type Response struct { Value string }
type Observation struct { Response Response }
`,
		"internal/integration/reconcile/reconcile.go": `package reconcile
type Candidate struct { ID string }
func Repair(c Candidate) Candidate { return c }
`,
		"internal/domains/payroll/pay_test.go": `package payroll
import "example/internal/connectivity/provider/workday"
var _ workday.Client
`,
	})
	if got := integrationboundaries.Check(root); len(got) != 0 {
		t.Fatalf("valid boundaries rejected: %#v", got)
	}
}

func TestIntegrationBoundariesAreDeterministicAndSkipGenerated(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/integration/mapping/z.go": `package mapping
func Execute() {}
`,
		"internal/integration/mapping/a.go": `package mapping
type IR struct{}
`,
		"internal/integration/gen/mapping/generated.go": `package mapping
func Execute() {}
`,
	})
	a, b := integrationboundaries.Check(root), integrationboundaries.Check(root)
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("unexpected findings: %#v", a)
	}
	if a[0].Path > a[1].Path {
		t.Fatalf("findings not sorted: %#v", a)
	}
	for i := range a {
		if a[i].String() != b[i].String() {
			t.Fatalf("nondeterministic findings")
		}
	}
}
