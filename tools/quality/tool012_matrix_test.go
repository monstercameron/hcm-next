package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_TOOL_012_Golden pins the TOOL-012 matrix and its two fixture
// halves. The primary test exercises the detector; this test makes sure a
// future edit cannot silently remove either the deliberate race (RED) or the
// synchronized control (GREEN).
func TestTodo_TOOL_012_Golden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "definitions", "planning", "todo-registry.json"))
	if err != nil {
		t.Fatalf("read TODO registry: %v", err)
	}
	var entries []struct {
		ID         string            `json:"id"`
		TestMatrix map[string]string `json:"test_matrix"`
		Red        string            `json:"red"`
		Green      string            `json:"green"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse TODO registry: %v", err)
	}
	var entry *struct {
		ID         string            `json:"id"`
		TestMatrix map[string]string `json:"test_matrix"`
		Red        string            `json:"red"`
		Green      string            `json:"green"`
	}
	for i := range entries {
		if entries[i].ID == "TOOL-012" {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("TOOL-012 is missing from the TODO registry")
	}
	want := map[string]string{
		"PRIMARY":     "TestTodo_TOOL_012",
		"GOLDEN":      "TestTodo_TOOL_012_Golden",
		"INTEGRATION": "TestTodo_TOOL_012_Integration",
		"RACE":        "TestTodo_TOOL_012_Race",
	}
	for kind, name := range want {
		if entry.TestMatrix[kind] != name {
			t.Errorf("matrix[%q] = %q, want %q", kind, entry.TestMatrix[kind], name)
		}
	}
	if !strings.Contains(entry.Red, "shared-state race") || !strings.Contains(entry.Green, "-race") {
		t.Fatalf("registry red/green contract lost: red=%q green=%q", entry.Red, entry.Green)
	}

	fixture, err := os.ReadFile(filepath.Join(root, "tools", "quality", "testdata", "racefixture", "racefixture.go"))
	if err != nil {
		t.Fatalf("read race fixture: %v", err)
	}
	text := string(fixture)
	if !strings.Contains(text, "RacyCounter") || !strings.Contains(text, "SynchronizedCounter") || !strings.Contains(text, "sync.Mutex") {
		t.Fatal("race fixture no longer contains both racy and synchronized implementations")
	}
}

// TestTodo_TOOL_012_Integration pins the CI integration point. The local
// windows/arm64 toolchain cannot execute -race; Linux CI is the authoritative
// environment for the full root-module race sweep.
func TestTodo_TOOL_012_Integration(t *testing.T) {
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "tests.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	text := string(workflow)
	if !strings.Contains(text, "runs-on: ubuntu-latest") {
		t.Fatal("TOOL-012 race job is not pinned to a Linux runner")
	}
	if !strings.Contains(text, "go test -race -count=1 ./...") {
		t.Fatal("go-core CI job does not run the full root-module race sweep")
	}
}

// TestTodo_TOOL_012_Race runs the detector RED/GREEN proof. The primary test
// probes the active toolchain and skips explicitly when that platform does
// not support -race (for example windows/arm64); it therefore never reports
// unsupported local execution as race coverage.
func TestTodo_TOOL_012_Race(t *testing.T) {
	TestTodo_TOOL_012(t)
}
