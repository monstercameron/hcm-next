package deferredimports

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhaseOneImportsRejectDeferredSubsystems is the primary red/green
// test for GOV-009.
func TestPhaseOneImportsRejectDeferredSubsystems(t *testing.T) {
	t.Run("Kafka import is rejected", func(t *testing.T) {
		dir := copyFixture(t, "forbidden")
		violations, err := ScanDir(dir)
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		if len(violations) != 1 || violations[0].Subsystem != "Kafka" {
			t.Fatalf("expected one Kafka violation, got %v", violations)
		}
	})

	t.Run("ClickHouse, OpenSearch and vector fragments are individually recognized", func(t *testing.T) {
		cases := map[string]string{
			"github.com/ClickHouse/clickhouse-go/v2":        "ClickHouse",
			"github.com/opensearch-project/opensearch-go":   "OpenSearch",
			"github.com/milvus-io/milvus-sdk-go":            "Vector infrastructure",
			"hcm-next/internal/billing/full":                "Full billing",
			"hcm-next/internal/domains/payroll/calculation": "Payroll calculation",
			"hcm-next/internal/omnichannel/inbox":           "Omnichannel",
		}
		for importPath, wantSubsystem := range cases {
			f, ok := MatchForbidden(importPath)
			if !ok || f.Subsystem != wantSubsystem {
				t.Errorf("MatchForbidden(%q) = %v, %v; want subsystem %q", importPath, f, ok, wantSubsystem)
			}
		}
	})

	t.Run("a clean production tree has zero violations", func(t *testing.T) {
		dir := copyFixture(t, "clean")
		violations, err := ScanDir(dir)
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		if len(violations) != 0 {
			t.Errorf("expected zero violations, got %v", violations)
		}
	})

	t.Run("an unrelated import is not flagged", func(t *testing.T) {
		if _, ok := MatchForbidden("github.com/google/uuid"); ok {
			t.Error("github.com/google/uuid should not be flagged as a deferred subsystem")
		}
	})

	t.Run("the real internal/ tree has zero forbidden imports", func(t *testing.T) {
		root := filepath.Join("..", "..", "..", "internal")
		if _, err := os.Stat(root); os.IsNotExist(err) {
			t.Skip("no internal/ directory present")
		}
		violations, err := ScanDir(root)
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		if len(violations) != 0 {
			t.Errorf("found %d forbidden imports in internal/:", len(violations))
			for _, v := range violations {
				t.Errorf("  %s", v)
			}
		}
	})
}

// TestTodo_GOV_009_Property fuzzes MatchForbidden with synthetic import
// paths: any path built by embedding a forbidden fragment must always
// match, and a path built purely from unrelated segments must never match.
func TestTodo_GOV_009_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	segments := []string{"github.com", "example", "internal", "pkg", "v2", "client", "server", "widgets", "foo123", "bar-baz"}

	randomPath := func() string {
		n := 2 + rng.Intn(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = segments[rng.Intn(len(segments))]
		}
		return strings.Join(parts, "/")
	}

	for i := 0; i < 200; i++ {
		base := randomPath()

		// Case 1: embed a forbidden fragment - must always match.
		frag := Forbidden[rng.Intn(len(Forbidden))]
		withForbidden := base + "/" + frag.Fragment + "/" + randomPath()
		if _, ok := MatchForbidden(withForbidden); !ok {
			t.Fatalf("expected %q to match a forbidden fragment (%s)", withForbidden, frag.Fragment)
		}

		// Case 2: no fragment embedded - must never match, unless the
		// random path happened to contain a fragment by chance (checked
		// explicitly rather than assumed away).
		containsAnyFragment := false
		for _, f := range Forbidden {
			if strings.Contains(base, f.Fragment) {
				containsAnyFragment = true
				break
			}
		}
		if _, ok := MatchForbidden(base); ok != containsAnyFragment {
			t.Fatalf("MatchForbidden(%q) = %v, want %v", base, ok, containsAnyFragment)
		}
	}
}

// TestTodo_GOV_009_Golden pins the exact violation message format.
func TestTodo_GOV_009_Golden(t *testing.T) {
	file := filepath.Join("internal", "connectivity", "kafka.go")
	v := Violation{File: file, Import: "github.com/segmentio/kafka-go", Subsystem: "Kafka"}
	want := file + `: imports "github.com/segmentio/kafka-go" (Kafka is a Phase 1 non-goal and must never be a mandatory dependency)`
	if got := v.String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}

// copyFixture copies testdata/<name>/*.go.txt into a fresh temp directory
// as *.go files, so ScanDir can parse them without them being compiled as
// part of this module.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	srcDir := filepath.Join("testdata", name)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}

	dstDir := t.TempDir()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		dstName := strings.TrimSuffix(e.Name(), ".txt")
		if err := os.WriteFile(filepath.Join(dstDir, dstName), content, 0o644); err != nil {
			t.Fatalf("write %s: %v", dstName, err)
		}
	}
	return dstDir
}
