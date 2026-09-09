package depedge_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func loadPolicy(t *testing.T) *depedge.Policy {
	t.Helper()
	root := repopath.RootDir()
	manifestPath := filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml")
	p, err := depedge.Load(manifestPath)
	if err != nil {
		t.Fatalf("loading package-dependency-policy manifest: %v", err)
	}
	return p
}

// TestPackageDependencyPolicyRejectsReverseEdges is the ARCH-GO-002 primary
// test: a table of synthetic importer/imported edges, each expecting either
// no violation or a violation naming the exact rule broken.
func TestPackageDependencyPolicyRejectsReverseEdges(t *testing.T) {
	p := loadPolicy(t)
	mod := p.Module

	cases := []struct {
		name     string
		importer string
		imported string
		wantRule string // "" means no violation expected
	}{
		{
			name:     "kernel importing engines",
			importer: mod + "/internal/kernel/values",
			imported: mod + "/internal/engines/transform",
			wantRule: depedge.RuleKernelUpward,
		},
		{
			name:     "kernel importing domains",
			importer: mod + "/internal/kernel/canonical",
			imported: mod + "/internal/domains/people",
			wantRule: depedge.RuleKernelUpward,
		},
		{
			name:     "kernel importing capability",
			importer: mod + "/internal/kernel/values",
			imported: mod + "/internal/capability/discovery",
			wantRule: depedge.RuleKernelUpward,
		},
		{
			name:     "kernel importing a port/adapter",
			importer: mod + "/internal/kernel/values",
			imported: mod + "/internal/data/postgres",
			wantRule: depedge.RuleKernelUpward,
		},
		{
			name:     "engine importing a domain implementation",
			importer: mod + "/internal/engines/eligibility",
			imported: mod + "/internal/domains/people/aggregate",
			wantRule: depedge.RuleEngineImportsDomain,
		},
		{
			name:     "workflow importing domain persistence",
			importer: mod + "/internal/workflow/step",
			imported: mod + "/internal/domains/people/store",
			wantRule: depedge.RuleWorkflowDomainPersist,
		},
		{
			name:     "workflow importing domain persistence (repository variant)",
			importer: mod + "/internal/workflow/runtime",
			imported: mod + "/internal/domains/compensation/repository",
			wantRule: depedge.RuleWorkflowDomainPersist,
		},
		{
			name:     "transport importing the data store",
			importer: mod + "/internal/transport/grpc",
			imported: mod + "/internal/data/repository",
			wantRule: depedge.RuleTransportImportsStore,
		},
		{
			name:     "transport importing the ledger store",
			importer: mod + "/internal/transport/grpc",
			imported: mod + "/internal/ledger",
			wantRule: depedge.RuleTransportImportsStore,
		},
		{
			name:     "domain importing a concrete data adapter",
			importer: mod + "/internal/domains/people",
			imported: mod + "/internal/data/postgres",
			wantRule: depedge.RuleBusinessConcreteAdapter,
		},
		{
			name:     "capability importing a concrete connectivity adapter",
			importer: mod + "/internal/capability/handler",
			imported: mod + "/internal/connectivity/http/adapters",
			wantRule: depedge.RuleBusinessConcreteAdapter,
		},
		{
			name:     "workflow importing a concrete transaction adapter",
			importer: mod + "/internal/workflow/step",
			imported: mod + "/internal/transaction/adapters",
			wantRule: depedge.RuleBusinessConcreteAdapter,
		},

		// Allowed edges: downward layer dependencies and port (not adapter)
		// use are not violations.
		{
			name:     "domain importing kernel is downward and allowed",
			importer: mod + "/internal/domains/people",
			imported: mod + "/internal/kernel/values",
			wantRule: "",
		},
		{
			name:     "capability importing engines is downward and allowed",
			importer: mod + "/internal/capability/handler",
			imported: mod + "/internal/engines/transform",
			wantRule: "",
		},
		{
			name:     "transport importing capability is downward and allowed",
			importer: mod + "/internal/transport/grpc",
			imported: mod + "/internal/capability/gateway",
			wantRule: "",
		},
		{
			name:     "domain importing the transaction port itself is allowed",
			importer: mod + "/internal/domains/people",
			imported: mod + "/internal/transaction",
			wantRule: "",
		},
		{
			name:     "workflow importing the ledger port itself is allowed",
			importer: mod + "/internal/workflow/step",
			imported: mod + "/internal/ledger",
			wantRule: "",
		},
		{
			name:     "engine importing kernel is downward and allowed",
			importer: mod + "/internal/engines/transform",
			imported: mod + "/internal/kernel/canonical",
			wantRule: "",
		},
		{
			name:     "unrelated packages outside the module are ignored",
			importer: mod + "/internal/kernel/values",
			imported: "google.golang.org/protobuf/proto",
			wantRule: "",
		},
		{
			name:     "self-import is never a violation",
			importer: mod + "/internal/kernel/values",
			imported: mod + "/internal/kernel/values",
			wantRule: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violation := p.CheckEdge(tc.importer, tc.imported)

			if tc.wantRule == "" {
				if violation != nil {
					t.Fatalf("CheckEdge(%q, %q) = %+v, want no violation", tc.importer, tc.imported, violation)
				}
				return
			}

			if violation == nil {
				t.Fatalf("CheckEdge(%q, %q) = nil, want rule %q", tc.importer, tc.imported, tc.wantRule)
			}
			if violation.Importer != tc.importer {
				t.Errorf("violation.Importer = %q, want %q", violation.Importer, tc.importer)
			}
			if violation.Imported != tc.imported {
				t.Errorf("violation.Imported = %q, want %q", violation.Imported, tc.imported)
			}
			if violation.Rule != tc.wantRule {
				t.Errorf("violation.Rule = %q, want %q", violation.Rule, tc.wantRule)
			}
		})
	}
}
