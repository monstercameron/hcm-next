package rolloutplan

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTodo_ROLLOUT_002_Golden(t *testing.T) {
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/cohorts_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
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

func TestTodo_ROLLOUT_002_Security(t *testing.T) {
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := RenderOwner(set, "canary")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"sub:1", "sub:2"} {
		if !strings.Contains(owner, member) {
			t.Fatalf("owner render lost member %q:\n%s", member, owner)
		}
	}
	observer, err := RenderObserver(set, "canary")
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"sub:1", "sub:2", "sub:3", "sub:4", "members:", "excluded:"} {
		if strings.Contains(observer, leaked) {
			t.Fatalf("observer render leaks %q:\n%s", leaked, observer)
		}
	}
	if !strings.Contains(observer, set.Cohorts[0].Digest) {
		t.Fatalf("observer render lost the digest:\n%s", observer)
	}
	t.Run("unknown stage renders nothing", func(t *testing.T) {
		if _, err := RenderObserver(set, "void"); !HasCohortCode(err, UnknownStage) {
			t.Fatalf("unknown stage rendered: %v", err)
		}
	})
	t.Run("tenant isolation in resolution", func(t *testing.T) {
		req := cohortRequest()
		req.Targets = []StageTarget{{Stage: "canary", Tenants: []string{"tenant-2"}, Orgs: []string{"org:acme"}, Residencies: []string{"EU", "US"}}}
		isolated, err := FreezeCohorts(req)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range isolated.Cohorts[0].Members {
			if member == "sub:1" || member == "sub:2" {
				t.Fatalf("tenant-1 member leaked into tenant-2 cohort: %+v", isolated.Cohorts[0].Members)
			}
		}
	})
}
