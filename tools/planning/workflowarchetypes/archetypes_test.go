package workflowarchetypes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureCatalogs() []string {
	return []string{
		filepath.Join("people", "catalog.md"),
		filepath.Join("leave", "catalog.md"),
	}
}

func TestWorkflowCatalogRejectsImplicitArchetypeResponsibilities(t *testing.T) {
	first, err := ScanCatalogs(filepath.Join("testdata", "fixture"), fixtureCatalogs())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ScanCatalogs(filepath.Join("testdata", "fixture"), fixtureCatalogs())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("catalog digest not deterministic: %s vs %s", first.Digest, second.Digest)
	}
	if len(first.Rows) != 5 {
		t.Fatalf("rows = %d, want 5 fixture rows", len(first.Rows))
	}
	counts := map[string]int{}
	for _, row := range first.Rows {
		counts[row.Catalog]++
	}
	if counts["people/catalog.md"] != 3 || counts["leave/catalog.md"] != 2 {
		t.Fatalf("per-catalog counts = %v, want people 3 and leave 2", counts)
	}
	want := map[string]string{
		"people/catalog.md|GhostRow":      "UNKNOWN_ARCHETYPE",
		"people/catalog.md|EmptyDeps":     "EMPTY_REQUIRED_CELL",
		"people/catalog.md|ChangeManager": "DUPLICATE_INTENT_PLACEMENT",
		"leave/catalog.md|ChangeManager":  "DUPLICATE_INTENT_PLACEMENT",
	}
	for key, code := range want {
		parts := strings.SplitN(key, "|", 2)
		if !hasFinding(first.Findings, parts[0], parts[1], code) {
			t.Errorf("missing %s for %s: %+v", code, key, first.Findings)
		}
	}
	if hasFinding(first.Findings, "leave/catalog.md", "TakeLeave", "UNKNOWN_ARCHETYPE") {
		t.Errorf("complete fixture row flagged: %+v", first.Findings)
	}
}

func TestTodo_WF_DISC_002_Property(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		intent   string
		archCell string
		delta    string
		code     string
		negative bool
	}{
		{"unknown archetype", "people/catalog.md", "ChangeManager", "A0", "cycle validation", "UNKNOWN_ARCHETYPE", false},
		{"empty delta", "people/catalog.md", "ChangeManager", "A2", "", "EMPTY_REQUIRED_CELL", false},
		{"malformed row", "people/catalog.md", "ChangeManager", "A2", "cycle validation", "MALFORMED_ROW", false},
		{"dual archetype legitimate", "people/catalog.md", "ChangeManager", "A2/A8", "cycle validation", "", true},
		{"direct disposition legitimate", "people/catalog.md", "ChangeManager", "D1", "cycle validation", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			copyFixture(t, filepath.Join("testdata", "solo"), root)
			rewriteRow(t, filepath.Join(root, tc.file), tc.intent, tc.archCell, tc.delta, tc.name == "malformed row")
			report, err := ScanCatalogs(root, fixtureCatalogs())
			if err != nil {
				t.Fatal(err)
			}
			if tc.negative {
				if len(report.Findings) != 0 {
					t.Errorf("legitimate mapping rejected: %+v", report.Findings)
				}
				return
			}
			if !hasFinding(report.Findings, strings.ReplaceAll(tc.file, string(filepath.Separator), "/"), tc.intent, tc.code) {
				t.Errorf("mutation %s accepted: %+v", tc.name, report.Findings)
			}
		})
	}
}

func TestTodo_WF_DISC_002_Golden(t *testing.T) {
	report, err := ScanCatalogs(filepath.Join("testdata", "fixture"), fixtureCatalogs())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(report, "", "  ")
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

func TestTodo_WF_DISC_002_Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..", "planning", "workflows")
	catalogs := []string{
		"people/catalog.md",
		"workforce/catalog.md",
		"rewards/catalog.md",
		"lifecycle/catalog.md",
		"leave/catalog.md",
		"hr-service/catalog.md",
	}
	report, err := ScanCatalogs(root, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int{
		"people/catalog.md":     39,
		"workforce/catalog.md":  28,
		"rewards/catalog.md":    21,
		"lifecycle/catalog.md":  41,
		"leave/catalog.md":      46,
		"hr-service/catalog.md": 23,
	}
	counts := map[string]int{}
	for _, row := range report.Rows {
		counts[row.Catalog]++
	}
	for catalog, want := range wantCounts {
		if counts[catalog] != want {
			t.Errorf("catalog %s rows = %d, want %d", catalog, counts[catalog], want)
		}
	}
	if len(report.Findings) != 0 {
		t.Fatalf("live catalogs have findings: %+v", report.Findings)
	}
	defined, err := LoadArchetypes(filepath.Join(root, "_shared", "workflow-archetypes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(defined) != 21 {
		t.Fatalf("archetype definitions = %d, want A1-A20 plus D1", len(defined))
	}
	for _, row := range report.Rows {
		for _, arch := range row.Archetypes {
			if !defined[arch] {
				t.Fatalf("catalog archetype %s is not defined", arch)
			}
		}
	}
}

func TestTodo_WF_DISC_002_Mutation(t *testing.T) {
	root := t.TempDir()
	copyFixture(t, filepath.Join("testdata", "solo"), root)

	duplicate := "# Extra\n\n| Intent        | Arch. | Primary dependencies/data | Required step delta |\n| ------------- | ----- | ------------------------- | ------------------- |\n| ChangeManager | A2    | Ppl, Org                  | org validation      |\n"
	if err := os.MkdirAll(filepath.Join(root, "extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra", "catalog.md"), []byte(duplicate), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ScanCatalogs(root, append(fixtureCatalogs(), filepath.Join("extra", "catalog.md")))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(report.Findings, "extra/catalog.md", "ChangeManager", "DUPLICATE_INTENT_PLACEMENT") {
		t.Fatalf("duplicate intent placement accepted: %+v", report.Findings)
	}

	unknown := "# Extra\n\n| Intent    | Arch. | Primary dependencies/data | Required step delta |\n| --------- | ----- | ------------------------- | ------------------- |\n| TakeLeave | A77   | Time                      | balance check       |\n"
	if err := os.WriteFile(filepath.Join(root, "extra", "catalog.md"), []byte(unknown), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = ScanCatalogs(root, []string{filepath.Join("extra", "catalog.md")})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(report.Findings, "extra/catalog.md", "TakeLeave", "UNKNOWN_ARCHETYPE") {
		t.Fatalf("unknown archetype accepted: %+v", report.Findings)
	}
}

func hasFinding(findings []Finding, catalog, intent, code string) bool {
	catalog = strings.ReplaceAll(catalog, string(filepath.Separator), "/")
	for _, finding := range findings {
		if finding.Catalog == catalog && finding.Intent == intent && finding.Code == code {
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

func rewriteRow(t *testing.T, path, intent, archCell, delta string, malformed bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	replaced := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| "+intent+" ") || strings.HasPrefix(line, "| "+intent+" |") {
			cells := strings.Split(line, "|")
			// cells[0] empty, cells[1] intent, cells[2] arch, cells[3] deps, cells[4] data, cells[5] delta, cells[6] empty
			if malformed {
				line = "| " + intent + " | " + archCell + " |"
			} else {
				cells[2] = " " + archCell + " "
				if len(cells) > 5 {
					cells[5] = " " + delta + " "
				}
				line = strings.Join(cells, "|")
			}
			replaced = true
		}
		out = append(out, line)
	}
	if !replaced {
		t.Fatalf("row %s not found in %s", intent, path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}
