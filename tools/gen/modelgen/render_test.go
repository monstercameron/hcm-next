package modelgen

import (
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func TestRenderNilModelSet(t *testing.T) {
	if _, err := Render(nil); err == nil {
		t.Fatal("Render(nil) succeeded; want an error")
	}
}

func TestRenderRealCatalogParsesAndIsGofmtClean(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := Render(ms)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	src, ok := files[OutputFile]
	if !ok {
		t.Fatalf("Render did not produce %s; got %v", OutputFile, sortedFileKeys(files))
	}
	if !strings.HasPrefix(string(src), GeneratedHeader) {
		t.Fatalf("generated file does not start with %q", GeneratedHeader)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, OutputFile, src, parser.AllErrors); err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, src)
	}
	again, err := format.Source(src)
	if err != nil {
		t.Fatalf("format.Source on already-rendered output: %v", err)
	}
	if string(again) != string(src) {
		t.Fatalf("rendered output is not gofmt-idempotent")
	}
	for _, want := range []string{
		"type Person struct", "func (v Person) Validate() error",
		"type EntityMeta struct", "type PropertyMeta struct", "type RelationshipMeta struct",
		"func New() *Registry", "func (r *Registry) Digest() string",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated file missing %q", want)
		}
	}
}

func TestRenderKnownAtRecordedAtImportsValues(t *testing.T) {
	reg := syntheticRegistry(t)
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := Render(ms)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	src := string(files[OutputFile])
	if !strings.Contains(src, valuesImportPath) {
		t.Errorf("rendered file using values.KnownAt/RecordedAt fields does not import %q", valuesImportPath)
	}
	if strings.Contains(src, lifecycleImportPath) {
		t.Errorf("rendered file with no lifecycle-typed field imports %q anyway", lifecycleImportPath)
	}
	for _, want := range []string{
		"Instant().Validate()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("rendered file missing known-at/recorded-at validation call %q", want)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms1, err := Build(reg)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	files1, err := Render(ms1)
	if err != nil {
		t.Fatalf("Render 1: %v", err)
	}
	ms2, err := Build(reg)
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}
	files2, err := Render(ms2)
	if err != nil {
		t.Fatalf("Render 2: %v", err)
	}
	if OutputDigest(files1) != OutputDigest(files2) {
		t.Fatal("two renders of the same registry produced different output digests")
	}
}
