package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/monstercameron/hcm-next"

type acceptanceItem struct {
	ID     string
	Source string
	Text   string
	Status string
	Gap    string
	Tests  []string
}

type digestItem struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	TextSHA256 string   `json:"text_sha256"`
	Status     string   `json:"status"`
	Gap        string   `json:"gap"`
	Tests      []string `json:"tests"`
}

// TestPromotionAcceptanceContractIsMachineChecked verifies that the checked-in
// promotion matrix still describes the two normative source sections, and that
// every claimed proof resolves to a real repository test function.
func TestPromotionAcceptanceContractIsMachineChecked(t *testing.T) {
	root := repositoryRoot(t)
	matrix, err := parseMatrix(filepath.Join(root, "test", "acceptance", "testdata", "promotion_acceptance.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	want := append(
		extractBullets(t, filepath.Join(root, "planning", "specs", "workflow-runtime.md"), "Phase 1 Acceptance Contract", "phase1"),
		extractBullets(t, filepath.Join(root, "planning", "reference-workflows", "promote-into-management.md"), "Required conformance scenarios", "scenario")...,
	)
	if len(matrix) != len(want) {
		t.Fatalf("acceptance matrix has %d items, want %d source bullets", len(matrix), len(want))
	}
	for i, item := range matrix {
		if item.ID != want[i].ID {
			t.Errorf("item %d id = %q, want %q", i+1, item.ID, want[i].ID)
		}
		gotTextDigest := digestText(item.Text)
		if gotTextDigest != want[i].TextSHA256 {
			t.Errorf("%s text digest = %s, want fixture source digest %s", item.ID, gotTextDigest, want[i].TextSHA256)
		}
		if item.Source != want[i].Source {
			t.Errorf("%s source = %q, want %q", item.ID, item.Source, want[i].Source)
		}
	}

	existing, err := scanTestFunctions(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range matrix {
		if item.Status != "PROVEN" && item.Status != "PARTIAL" && item.Status != "MISSING" {
			t.Errorf("%s has invalid status %q", item.ID, item.Status)
		}
		if item.Status == "PROVEN" && len(item.Tests) == 0 {
			t.Errorf("%s is PROVEN without a proving test", item.ID)
		}
		if item.Status != "PROVEN" && strings.TrimSpace(item.Gap) == "" {
			t.Errorf("%s is %s without a one-sentence gap note", item.ID, item.Status)
		}
		for _, ref := range item.Tests {
			if !existing[ref] {
				t.Errorf("%s names missing test %q", item.ID, ref)
			}
		}
	}

	for _, item := range matrix {
		if item.Status == "MISSING" || item.Status == "PARTIAL" {
			fmt.Printf("PROMOTION ACCEPTANCE %s %s: %s\n", item.Status, item.ID, item.Gap)
			t.Logf("%s %s: %s", item.Status, item.ID, item.Gap)
		}
	}

	gotGolden := matrixDigest(matrix)
	wantGolden, err := os.ReadFile(filepath.Join(root, "test", "acceptance", "testdata", "promotion_acceptance.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(wantGolden)) != gotGolden {
		t.Errorf("acceptance matrix digest = %s, want golden %s", gotGolden, strings.TrimSpace(string(wantGolden)))
	}
}

type sourceBullet struct {
	ID         string
	Source     string
	TextSHA256 string
}

func extractBullets(t *testing.T, path, heading, source string) []sourceBullet {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if line == "## "+heading {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("heading %q not found in %s", heading, path)
	}

	var bullets []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "- ") {
			bullets = append(bullets, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			continue
		}
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
			if dot := strings.Index(line, ". "); dot > 0 {
				allDigits := true
				for _, r := range line[:dot] {
					if r < '0' || r > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					bullets = append(bullets, strings.TrimSpace(line[dot+2:]))
				}
			}
		}
	}

	result := make([]sourceBullet, 0, len(bullets))
	for i, bullet := range bullets {
		id := fmt.Sprintf("%s-%02d", source, i+1)
		result = append(result, sourceBullet{ID: id, Source: source, TextSHA256: digestText(bullet)})
	}
	return result
}

func scanTestFunctions(root string) (map[string]bool, error) {
	existing := make(map[string]bool)
	for _, relRoot := range []string{"test", "internal/workflow", "internal/domains/promotion", "internal/platform/execution", "internal/intent/app", "internal/resource", "internal/transaction", "internal/data/ledger", "internal/humanwork"} {
		base := filepath.Join(root, filepath.FromSlash(relRoot))
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := modulePath + "/" + filepath.ToSlash(relDir)
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil {
					continue
				}
				name := function.Name.Name
				if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Benchmark") {
					existing[pkg+"::"+name] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return existing, nil
}

func parseMatrix(path string) ([]acceptanceItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read matrix: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var items []acceptanceItem
	current := -1
	for lineNumber, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") || line == "version: 1" || line == "items:" {
			continue
		}
		if strings.HasPrefix(line, "  - id: ") {
			value, err := parseScalar(strings.TrimPrefix(line, "  - id: "))
			if err != nil {
				return nil, fmt.Errorf("matrix line %d: %w", lineNumber+1, err)
			}
			items = append(items, acceptanceItem{ID: value})
			current++
			continue
		}
		if current < 0 {
			return nil, fmt.Errorf("matrix line %d appears before first item: %s", lineNumber+1, line)
		}
		if strings.HasPrefix(line, "      - ") {
			value, err := parseScalar(strings.TrimPrefix(line, "      - "))
			if err != nil {
				return nil, fmt.Errorf("matrix line %d: %w", lineNumber+1, err)
			}
			items[current].Tests = append(items[current].Tests, value)
			continue
		}
		field, raw, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return nil, fmt.Errorf("matrix line %d is not a field: %s", lineNumber+1, line)
		}
		if field == "tests" && strings.TrimSpace(raw) == "[]" {
			continue
		}
		value, err := parseScalar(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("matrix line %d: %w", lineNumber+1, err)
		}
		switch field {
		case "source":
			items[current].Source = value
		case "text":
			items[current].Text = value
		case "status":
			items[current].Status = value
		case "gap":
			items[current].Gap = value
		case "tests":
			if value != "" {
				return nil, fmt.Errorf("matrix line %d: tests must be [] or a list", lineNumber+1)
			}
		default:
			return nil, fmt.Errorf("matrix line %d has unknown field %q", lineNumber+1, field)
		}
	}
	return items, nil
}

func parseScalar(value string) (string, error) {
	if value == "" || value == "[]" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted scalar %q: %w", value, err)
		}
		return parsed, nil
	}
	return value, nil
}

func matrixDigest(items []acceptanceItem) string {
	digests := make([]digestItem, 0, len(items))
	for _, item := range items {
		digests = append(digests, digestItem{
			ID: item.ID, Source: item.Source, TextSHA256: digestText(item.Text),
			Status: item.Status, Gap: item.Gap, Tests: item.Tests,
		})
	}
	encoded, err := json.Marshal(digests)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func digestText(text string) string {
	digest := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
