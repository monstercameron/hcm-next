package controlcrosswalk

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/planning/todoregistry"
)

func TestTodo_GOV_030(t *testing.T) {
	revision := realRevision(t)
	if len(revision.Controls) != 51 {
		t.Fatalf("control count = %d, want 51 matrix rows", len(revision.Controls))
	}
	if findControl(revision, "E-05").Status != Implemented {
		t.Fatalf("E-05 status = %q, want %q", findControl(revision, "E-05").Status, Implemented)
	}
	if findControl(revision, "E-16").Status != Missing {
		t.Fatalf("E-16 status = %q, want %q", findControl(revision, "E-16").Status, Missing)
	}
	if err := Verify(revision); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_GOV_030_Golden(t *testing.T) {
	revision := realRevision(t)
	data, err := json.MarshalIndent(struct {
		SchemaVersion int    `json:"schema_version"`
		Version       string `json:"version"`
		Controls      int    `json:"controls"`
		Digest        string `json:"digest"`
	}{revision.SchemaVersion, revision.Version, len(revision.Controls), revision.Digest}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "real-registry.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(data, '\n'), want) {
		t.Fatalf("real-registry golden mismatch:\n got %s\nwant %s", data, want)
	}
}

func TestTodo_GOV_030_Security(t *testing.T) {
	definition, todos := realInputs(t)
	for index := range definition.Controls {
		if definition.Controls[index].ID == "E-16" {
			definition.Controls[index].DeclaredStatus = string(Implemented)
		}
	}
	_, err := Regenerate(definition, todos, nil)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Regenerate error = %v, want typed validation error", err)
	}
	found := false
	for _, finding := range validation.Findings {
		if finding.Code == "STATUS_NOT_DERIVED" && strings.Contains(finding.Field, ".status") {
			found = true
		}
	}
	if !found {
		t.Fatalf("status refusal did not name the status field: %+v", validation.Findings)
	}
	revision := realRevision(t)
	explanation := revision.Explain()
	for _, raw := range []string{"E-16", "GOV-030", "internal/"} {
		if strings.Contains(explanation, raw) {
			t.Fatalf("Explain leaked raw identifier %q: %s", raw, explanation)
		}
	}
}

func TestTodo_GOV_030_Integration(t *testing.T) {
	definition, todos := realInputs(t)
	revision, err := NewCompiler(definition, todos).Regenerate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Controls) != len(definition.Controls) {
		t.Fatalf("compiled controls = %d, seed controls = %d", len(revision.Controls), len(definition.Controls))
	}
}

func TestTodo_GOV_030_Conformance(t *testing.T) {
	revision := realRevision(t)
	for _, control := range revision.Controls {
		frameworks := []string{control.Frameworks.NIST80053, control.Frameworks.CSF20, control.Frameworks.ISO27001, control.Frameworks.SOC2, control.Frameworks.CIS, control.Frameworks.ASVS}
		for _, value := range frameworks {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("control %s has an empty framework reference", control.ID)
			}
		}
		if control.OwnerPackage == "" || len(control.TodoIDs) == 0 || len(control.Evidence) == 0 || control.Digest == "" {
			t.Fatalf("incomplete resolved control: %+v", control)
		}
		if control.Status != Implemented && control.Status != Partial && control.Status != Missing {
			t.Fatalf("invalid status %q", control.Status)
		}
		for _, evidence := range control.Evidence {
			if evidence.TestName == "" {
				t.Fatalf("evidence %s has no registry-resolved test", control.ID)
			}
		}
	}
}

func TestTodo_GOV_030_Mutation(t *testing.T) {
	definition, todos := realInputs(t)
	before, err := Regenerate(definition, todos, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutated := append([]todoregistry.Todo(nil), todos...)
	for index := range mutated {
		if mutated[index].ID == "GOV-030" {
			mutated[index].Done = true
		}
	}
	after, err := Regenerate(definition, mutated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest == after.Digest {
		t.Fatal("revision digest did not change when cited todo status changed")
	}
	if findControl(after, "E-16").Status != Implemented {
		t.Fatalf("mutated E-16 status = %q, want %q", findControl(after, "E-16").Status, Implemented)
	}
}

func realRevision(t *testing.T) Revision {
	t.Helper()
	definition, todos := realInputs(t)
	revision, err := Regenerate(definition, todos, nil)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func realInputs(t *testing.T) (Definition, []todoregistry.Todo) {
	t.Helper()
	definition, err := Load(filepath.Join("testdata", "security-control-crosswalk.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..")
	todos, err := LoadRegistry(filepath.Join(root, "definitions", "planning", "todo-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	return definition, todos
}

func findControl(revision Revision, id string) Control {
	for _, control := range revision.Controls {
		if control.ID == id {
			return control
		}
	}
	return Control{}
}
