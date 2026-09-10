package engineownership

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	root := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		root = filepath.Dir(root)
	}
	t.Fatal("go.mod not found above the test")
	return ""
}

// redClusters names every GOV-029 RED cluster: each must resolve to an
// owned package or to an explicit conformance-only deferral, never to a
// workflow sample, connector, generic engine or bare fixture.
var redClusters = []string{
	"ats", "workforce-access", "hr-cases", "worker-lifecycle", "fx",
	"localization", "location-jurisdiction", "service-seniority",
	"collective-bargaining", "reporting", "payroll-inputs", "tax-profile",
	"pay-methods", "job-architecture", "identity-work-authorization",
	"employee-relations", "mobility", "safety", "skills-career-succession",
	"advanced-rewards", "assets", "contact-verification",
}

// TestSemanticEngineOwnershipRejectsConformanceOnlyOrGenericOwner is the
// GOV-029 primary test.
func TestSemanticEngineOwnershipRejectsConformanceOnlyOrGenericOwner(t *testing.T) {
	root := repoRoot(t)
	registry := Registry()
	if findings := Verify(root, registry); len(findings) != 0 {
		t.Fatalf("registry findings: %+v", findings)
	}
	if len(registry) != len(redClusters) {
		t.Fatalf("registry holds %d clusters, want all %d RED clusters", len(registry), len(redClusters))
	}
	for _, name := range redClusters {
		cluster, err := Resolve(name)
		if err != nil {
			t.Fatalf("RED cluster %s does not resolve: %v", name, err)
		}
		switch cluster.Status {
		case StatusOwned:
			for _, generic := range []string{"workflow", "connector", "sample", "fixture", "generic", "rules-engine", "program"} {
				if strings.Contains(strings.ToLower(cluster.OwnerPackage), generic) {
					t.Fatalf("cluster %s resolves to generic owner %s", name, cluster.OwnerPackage)
				}
			}
		case StatusConformanceOnly:
			if len(cluster.Capabilities) != 0 {
				t.Fatalf("deferred cluster %s publishes capabilities: %v", name, cluster.Capabilities)
			}
		default:
			t.Fatalf("cluster %s has unknown status %q", name, cluster.Status)
		}
	}
	// The three known deferrals stay explicit and capability-free.
	for _, name := range []string{"ats", "reporting", "localization"} {
		cluster, err := Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if cluster.Status != StatusConformanceOnly || cluster.OwnerPackage != "" {
			t.Fatalf("deferred cluster %s = %+v, want explicit CONFORMANCE_ONLY", name, cluster)
		}
	}
}

func TestTodo_GOV_029_Property(t *testing.T) {
	root := repoRoot(t)
	registry := Registry()
	// The registry is a function of its entries only: same entries, same
	// digest, regardless of declaration order.
	if Digest(registry) != Digest(append([]Cluster(nil), registry...)) {
		t.Fatal("registry digest is not a pure function of its entries")
	}
	reversed := append([]Cluster(nil), registry...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if Digest(registry) != Digest(reversed) {
		t.Fatal("registry digest depends on declaration order")
	}
	// Every owned package resolves inside the module, never outside it.
	for _, cluster := range registry {
		if cluster.Status != StatusOwned {
			continue
		}
		if !strings.HasPrefix(cluster.OwnerPackage, "internal/") && !strings.HasPrefix(cluster.OwnerPackage, "tools/") {
			t.Fatalf("cluster %s owner %s escapes the module", cluster.Name, cluster.OwnerPackage)
		}
		if strings.Contains(cluster.OwnerPackage, "..") {
			t.Fatalf("cluster %s owner %s is not a clean path", cluster.Name, cluster.OwnerPackage)
		}
	}
	if findings := Verify(root, registry); len(findings) != 0 {
		t.Fatalf("registry findings: %+v", findings)
	}
}

func TestTodo_GOV_029_Golden(t *testing.T) {
	got := Digest(Registry()) + "\n"
	path := filepath.Join("testdata", "engineownership.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch: want %s got %s", path, want, got)
	}
}

func TestTodo_GOV_029_Conformance(t *testing.T) {
	root := repoRoot(t)
	// Every contract dimension the GREEN clause requires is stated for
	// every cluster, owned or deferred.
	for _, cluster := range Registry() {
		if len(cluster.Entities) == 0 || len(cluster.IntentSets) == 0 ||
			strings.TrimSpace(cluster.Persistence) == "" || strings.TrimSpace(cluster.Boundary) == "" ||
			strings.TrimSpace(cluster.Correction) == "" || strings.TrimSpace(cluster.PhaseDepth) == "" ||
			strings.TrimSpace(cluster.ClosureTest) == "" {
			t.Fatalf("cluster %s leaves a contract dimension unstated: %+v", cluster.Name, cluster)
		}
		if cluster.Status == StatusOwned && len(cluster.Capabilities) == 0 {
			t.Fatalf("owned cluster %s states no capability surface", cluster.Name)
		}
	}
	if findings := Verify(root, Registry()); len(findings) != 0 {
		t.Fatalf("registry findings: %+v", findings)
	}
}

func TestTodo_GOV_029_Mutation(t *testing.T) {
	root := repoRoot(t)
	at := func(name string) Cluster {
		cluster, err := Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		return cluster
	}
	mutate := func(cluster Cluster, fn func(*Cluster)) Cluster {
		fn(&cluster)
		return cluster
	}
	cases := []struct {
		name    string
		cluster Cluster
		code    string
	}{
		{"unknown cluster resolves nowhere", Cluster{}, UnknownCluster},
		{"owned package outside the tree", mutate(at("fx"), func(c *Cluster) {
			c.OwnerPackage = "internal/domains/fx-missing"
		}), MissingOwnerDir},
		{"deferred with capabilities", mutate(at("ats"), func(c *Cluster) {
			c.Capabilities = []string{"ats.execute"}
		}), DeferredWithCapabilities},
		{"deferred with owner", mutate(at("ats"), func(c *Cluster) {
			c.OwnerPackage = "internal/domains/matching"
		}), DeferredWithOwner},
		{"unstated entities", mutate(at("fx"), func(c *Cluster) { c.Entities = nil }), MissingContract},
		{"unstated closure", mutate(at("fx"), func(c *Cluster) { c.ClosureTest = "" }), MissingContract},
		{"foreign intent set", mutate(at("fx"), func(c *Cluster) {
			c.IntentSets = []string{"BI.SOMETHING"}
		}), InvalidIntentSet},
		{"missing closure test", mutate(at("fx"), func(c *Cluster) {
			c.ClosureTest = "TestTodo_FX_999_Nothing"
		}), MissingClosureTest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cluster.Name == "" {
				if _, err := Resolve("no-such-cluster"); err == nil {
					t.Fatal("unknown cluster resolved")
				} else if regErr, ok := err.(*RegistryError); !ok || regErr.Code != UnknownCluster {
					t.Fatalf("unknown cluster err = %v, want UNKNOWN_CLUSTER", err)
				}
				return
			}
			findings := Verify(root, []Cluster{tc.cluster})
			matched := false
			for _, finding := range findings {
				if finding.Code == tc.code {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("findings %+v miss code %s", findings, tc.code)
			}
		})
	}
	// Duplicates are refused even when each entry is otherwise valid.
	dup := append([]Cluster{at("fx")}, at("fx"))
	matched := false
	for _, finding := range Verify(root, dup) {
		if finding.Code == DuplicateCluster {
			matched = true
		}
	}
	if !matched {
		t.Fatal("duplicate cluster accepted")
	}
}
