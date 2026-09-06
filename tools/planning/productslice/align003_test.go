package productslice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func identifierVocabularyFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "identifier-vocabulary.yaml")
}

func loadIdentifierFixture(t *testing.T) IdentifierVocabulary {
	t.Helper()
	v, err := LoadIdentifierVocabularyYAML(identifierVocabularyFixturePath(t))
	if err != nil {
		t.Fatalf("LoadIdentifierVocabularyYAML: %v", err)
	}
	return v
}

// TestTodo_ALIGN_003 proves the versioned identifier rows have one explicit
// authority, canonical and digest rules, and a closed mint/carry boundary.
func TestTodo_ALIGN_003(t *testing.T) {
	v := loadIdentifierFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("identifier vocabulary Validate: %v", err)
	}
	if err := v.VerifyDigest(); err != nil {
		t.Fatalf("identifier vocabulary VerifyDigest: %v", err)
	}
	if len(v.Identifiers) < 8 {
		t.Fatalf("identifier vocabulary has %d rows, want the product-slice join points", len(v.Identifiers))
	}
	if got := v.Explain(); strings.Contains(got, "promotion") || strings.Contains(got, "worker") {
		t.Fatalf("Explain exposed identifier values: %q", got)
	}
}

func TestTodo_ALIGN_003_Property(t *testing.T) {
	v := loadIdentifierFixture(t)
	for _, row := range v.Identifiers {
		if row.OwningLayer == "" {
			t.Fatalf("row %q has no owner", row.Kind)
		}
		if _, err := v.Resolve(row.Kind); err != nil {
			t.Fatalf("Resolve(%q): %v", row.Kind, err)
		}
	}
}

func TestTodo_ALIGN_003_Golden(t *testing.T) {
	v := loadIdentifierFixture(t)
	want, err := os.ReadFile(filepath.Join("testdata", "identifier-vocabulary.golden.json"))
	if err != nil {
		t.Fatalf("read identifier vocabulary golden: %v", err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("parse identifier vocabulary golden: %v", err)
	}
	const wantDigest = "sha256:ad898c8f2a32e7921097f1832663d06c880e1f6c7caa16d0bb038ea0bd0b1ebc"
	if golden.Digest != wantDigest {
		t.Fatalf("golden digest=%q want=%q", golden.Digest, wantDigest)
	}
	if got := v.DigestValue(); got != wantDigest || v.Digest != wantDigest {
		t.Fatalf("identifier vocabulary digest=%q stored=%q want=%q", got, v.Digest, wantDigest)
	}
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	registry, err := LoadRegistryYAML(filepath.Join(root, "definitions", "planning", "product-slices.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistryYAML: %v", err)
	}
	const wantProductSliceDigest = "sha256:0f60ebfbbd1aaa05df18d53ad3c31b714e4367ef81f153d299f3755b838e3101"
	if registry.Digest != wantProductSliceDigest {
		t.Fatalf("real product-slice digest=%q want=%q", registry.Digest, wantProductSliceDigest)
	}
	wantKinds := []string{"business_intent_ref", "capability_id", "feature_id", "package_path", "page_ref", "slice_id", "todo_id", "widget_ref"}
	gotKinds := ProductSliceIdentifierKinds(registry)
	if strings.Join(gotKinds, "\n") != strings.Join(wantKinds, "\n") {
		t.Fatalf("real product-slice identifier kinds=%v want=%v", gotKinds, wantKinds)
	}
}

func TestTodo_ALIGN_003_Security(t *testing.T) {
	base := IdentifierDefinition{Kind: "worker_id", OwningLayer: "business", CanonicalForm: "opaque entity id", DigestRule: "sha256 canonical", MintedBy: []string{"business"}, CarriedBy: []string{"presentation", "persistence"}}
	cases := []struct {
		name string
		rows []IdentifierDefinition
		want error
	}{
		{name: "ownerless", rows: []IdentifierDefinition{{Kind: "worker_id", CanonicalForm: "opaque", DigestRule: "sha256", MintedBy: []string{"business"}}}, want: ErrIdentifierOwnerless},
		{name: "doubly owned", rows: []IdentifierDefinition{base, {Kind: "worker_id", OwningLayer: "persistence", CanonicalForm: base.CanonicalForm, DigestRule: base.DigestRule, MintedBy: []string{"persistence"}, CarriedBy: []string{"presentation"}}}, want: ErrIdentifierDoublyOwned},
		{name: "inconsistent", rows: []IdentifierDefinition{{Kind: "worker_id", OwningLayer: "business", CanonicalForm: base.CanonicalForm, DigestRule: base.DigestRule, MintedBy: []string{"presentation"}, CarriedBy: []string{"business"}}}, want: ErrIdentifierInconsistent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewIdentifierVocabulary(tc.rows...)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate error=%v, want errors.Is(..., %v)", err, tc.want)
			}
			var refusalErr *IdentifierRefusal
			if !errors.As(err, &refusalErr) || refusalErr.Kind != "worker_id" || refusalErr.Layer == "" {
				t.Fatalf("refusal=%#v, want typed kind/layer refusal", err)
			}
		})
	}
}

func TestTodo_ALIGN_003_Conformance(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	registry, err := LoadRegistryYAML(filepath.Join(root, "definitions", "planning", "product-slices.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistryYAML: %v", err)
	}
	v := loadIdentifierFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("identifier vocabulary Validate: %v", err)
	}
	for _, kind := range ProductSliceIdentifierKinds(registry) {
		if _, err := v.Resolve(kind); err != nil {
			t.Errorf("product-slice identifier kind %q: %v", kind, err)
		}
	}
}
