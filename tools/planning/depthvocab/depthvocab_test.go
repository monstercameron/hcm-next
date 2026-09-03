package depthvocab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDepthVocabularyRejectsSilentPromotion is the primary red/green test
// for GOV-005.
func TestDepthVocabularyRejectsSilentPromotion(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"coverage word DEFINED", "- **Depth:** DEFINED"},
		{"coverage word PARTIAL", "- **Implementation Depth:** PARTIAL"},
		{"alternate wording contract only", "- **Depth:** contract only"},
		{"architecture prose as staffing authority", "- **Staffing:** long-term architecture"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := CheckDepthDeclarations(tc.line)
			if len(violations) != 1 {
				t.Fatalf("expected exactly one violation, got %d: %v", len(violations), violations)
			}
		})
	}

	t.Run("every canonical value maps cleanly with zero violations", func(t *testing.T) {
		for _, d := range CanonicalDepths {
			line := "- **Depth:** " + d
			if v := CheckDepthDeclarations(line); len(v) != 0 {
				t.Errorf("canonical value %q was rejected: %v", d, v)
			}
		}
	})

	t.Run("lines without a depth marker are ignored", func(t *testing.T) {
		text := "This paragraph mentions DEFINED and PARTIAL in prose, not as a declaration."
		if v := CheckDepthDeclarations(text); len(v) != 0 {
			t.Errorf("expected zero violations for non-declaration prose, got %v", v)
		}
	})

	t.Run("unrecognized value still rejected generically", func(t *testing.T) {
		v := CheckDepthDeclarations("- **Depth:** SOMEDAY MAYBE")
		if len(v) != 1 {
			t.Fatalf("expected one violation, got %v", v)
		}
		if !strings.Contains(v[0].Issue, "not one of the four canonical") {
			t.Errorf("expected generic rejection issue, got %q", v[0].Issue)
		}
	})
}

// TestTodo_GOV_005_Golden pins the exact violation format.
func TestTodo_GOV_005_Golden(t *testing.T) {
	v := CheckDepthDeclarations("- **Depth:** DEFINED")
	if len(v) != 1 {
		t.Fatalf("expected one violation, got %v", v)
	}
	const want = `line 1: "DEFINED": silent promotion: an artifact-maturity/coverage word or legacy alternate wording used as a depth declaration`
	if got := v[0].String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}

// TestTodo_GOV_005_Conformance runs the checker against the real planning
// corpus. No document currently declares a Depth/Implementation Depth/
// Staffing marker line, so this must stay clean; a future document that
// introduces one must use only the four canonical values.
func TestTodo_GOV_005_Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..", "planning")
	var total int

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		violations := CheckDepthDeclarations(string(content))
		total += len(violations)
		for _, v := range violations {
			t.Errorf("%s: %s", path, v)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk planning/: %v", err)
	}
	if total != 0 {
		t.Fatalf("found %d depth-vocabulary violations in the real planning corpus", total)
	}
}
