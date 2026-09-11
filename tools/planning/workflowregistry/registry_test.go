package workflowregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
)

func TestWorkflowResearchRegistryMatchesCorpus(t *testing.T) {
	first, err := Scan(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Scan(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("registry digest not deterministic: %s vs %s", first.Digest, second.Digest)
	}
	if len(first.Artifacts) != 6 {
		t.Fatalf("artifacts = %d, want 6 fixture files", len(first.Artifacts))
	}
	want := map[string]string{
		"people/promotion.md":      "DUPLICATE_WORKFLOW_ID",
		"people/promotion-copy.md": "DUPLICATE_WORKFLOW_ID",
		"lifecycle/rushed.md":      "STATE_PROMOTION_WITHOUT_EVIDENCE",
		"samples/sketch.md":        "ORPHAN_WORKFLOW_DOCUMENT",
		"README.md":                "DANGLING_INDEX_LINK",
	}
	for path, code := range want {
		if !hasFinding(first.Findings, path, code) {
			t.Errorf("missing %s for %s: %+v", code, path, first.Findings)
		}
	}
	if hasFinding(first.Findings, "people/promotion.md", "MISSING_IDENTITY_FIELD") {
		t.Errorf("complete fixture document flagged: %+v", first.Findings)
	}
}

func TestTodo_WF_DISC_001_Property(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		code  string
	}{
		{"no kernel family", "kernel_family", "", "MISSING_IDENTITY_FIELD"},
		{"no state", "state", "", "MISSING_IDENTITY_FIELD"},
		{"no owner", "owner_domain", "", "MISSING_IDENTITY_FIELD"},
		{"unknown state", "state", "SHIPPED", "UNKNOWN_STATE"},
		{"unresolved intent", "target_intent", "hcmnext.people.imaginary", "UNRESOLVED_INTENT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			copyFixture(t, filepath.Join("testdata", "solo"), root)
			rewriteField(t, filepath.Join(root, "people", "promotion.md"), tc.key, tc.value)
			registry, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			registry.ResolveAcceptedIntents([]string{"hcmnext.people.promote_worker"})
			if !hasFinding(registry.Findings, "people/promotion.md", tc.code) {
				t.Errorf("mutation %s accepted: %+v", tc.name, registry.Findings)
			}
		})
	}

	t.Run("added file changes digest", func(t *testing.T) {
		root := t.TempDir()
		copyFixture(t, filepath.Join("testdata", "solo"), root)
		if err := os.WriteFile(filepath.Join(root, "extra.md"), []byte("# Extra\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		grown, err := Scan(root)
		if err != nil {
			t.Fatal(err)
		}
		solo, err := Scan(filepath.Join("testdata", "solo"))
		if err != nil {
			t.Fatal(err)
		}
		if grown.Digest == solo.Digest {
			t.Fatal("added artifact did not change the registry digest")
		}
	})
}

func TestTodo_WF_DISC_001_Golden(t *testing.T) {
	registry, err := Scan(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "fixture", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_001_Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..", "planning", "workflows")
	registry, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "catalog.yaml") {
			onDisk++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Artifacts) != onDisk {
		t.Fatalf("registry covers %d artifacts, %d files on disk", len(registry.Artifacts), onDisk)
	}
	again, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Digest != again.Digest {
		t.Fatal("live registry digest not stable across scans")
	}
	workflows := 0
	for _, artifact := range registry.Artifacts {
		if artifact.Kind == KindWorkflow {
			workflows++
		}
	}
	if workflows < 12 {
		t.Fatalf("workflow documents = %d, want at least the 12 known explorations", workflows)
	}
	for _, code := range []string{"DUPLICATE_WORKFLOW_ID", "STATE_PROMOTION_WITHOUT_EVIDENCE", "DANGLING_INDEX_LINK"} {
		for _, finding := range registry.Findings {
			if finding.Code == code {
				t.Fatalf("live corpus has %s: %+v", code, finding)
			}
		}
	}
	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var accepted []string
	for _, descriptor := range descriptors {
		accepted = append(accepted, descriptor.IntentTypeID)
	}
	registry.ResolveAcceptedIntents(accepted)
	if !hasFinding(registry.Findings, "", "UNRESOLVED_INTENT") {
		t.Fatal("live corpus reports no unresolved target intent")
	}
	for _, finding := range registry.Findings {
		if finding.Code == "UNRESOLVED_INTENT" && strings.Contains(finding.Detail, "hcmnext.people.change_manager") {
			t.Fatalf("accepted intent flagged unresolved: %+v", finding)
		}
	}
}

func TestTodo_WF_DISC_001_Mutation(t *testing.T) {
	root := t.TempDir()
	copyFixture(t, filepath.Join("testdata", "solo"), root)

	duplicate := "# Second\n\n```text\nworkflow_id: people.promotion/v1\ntarget_intent: hcmnext.people.promote_worker\nkernel_family: ChangeRequest\nstate: REFERENCE + EXPLORED\nowner_domain: people\n```\n"
	if err := os.WriteFile(filepath.Join(root, "people", "second.md"), []byte(duplicate), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(registry.Findings, "people/second.md", "DUPLICATE_WORKFLOW_ID") {
		t.Fatalf("duplicate workflow id accepted: %+v", registry.Findings)
	}

	promoted := "# Promotion\n\n```text\nworkflow_id: people.promotion/v1\ntarget_intent: hcmnext.people.promote_worker\nkernel_family: ChangeRequest\nstate: IMPLEMENTED\nowner_domain: people\n```\n"
	if err := os.WriteFile(filepath.Join(root, "people", "promotion.md"), []byte(promoted), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "people", "second.md")); err != nil {
		t.Fatal(err)
	}
	registry, err = Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(registry.Findings, "people/promotion.md", "STATE_PROMOTION_WITHOUT_EVIDENCE") {
		t.Fatalf("unevidenced IMPLEMENTED accepted: %+v", registry.Findings)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Index\n\n[Lost](people/lost.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "people", "promotion.md"), filepath.Join(root, "people", "renamed.md")); err != nil {
		t.Fatal(err)
	}
	registry, err = Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(registry.Findings, "README.md", "DANGLING_INDEX_LINK") {
		t.Fatalf("renamed workflow left no dangling index finding: %+v", registry.Findings)
	}
}

func hasFinding(findings []Finding, path, code string) bool {
	for _, finding := range findings {
		if (path == "" || finding.Path == path) && finding.Code == code {
			return true
		}
	}
	return false
}

func copyFixture(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func rewriteField(t *testing.T, path, key, value string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	replaced := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key+":") {
			if value != "" {
				out = append(out, key+": "+value)
			}
			replaced = true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		t.Fatalf("field %s not found in %s", key, path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}
