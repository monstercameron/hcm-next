package approval_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// goldenJSON compares v against testdata/name, or rewrites it under -update.
func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

// shiftedInstant is one hour after the fixture clock, for mutation cases that
// need a different but still valid decision time.
func shiftedInstant() values.Instant {
	return values.NewInstant(humanwork.ScenarioAt().Time().Add(time.Hour))
}

func mustFixture(t *testing.T) *approval.Fixture {
	t.Helper()
	f, err := approval.NewPromotionFixture()
	if err != nil {
		t.Fatalf("promotion fixture: %v", err)
	}
	return f
}
