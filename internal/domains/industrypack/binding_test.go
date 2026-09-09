package industrypack

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bindingDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("ParseLocalDate(%q): %v", text, err)
	}
	return date
}

func bindingManifest(t *testing.T, id string) IndustryPack {
	t.Helper()
	pack, err := NewIndustryPack(IndustryPack{
		PackID: id, Version: 1, Industry: IndustryHealthcare,
		Owner: "hcmnext", Scope: "healthcare-us", Support: "maintained",
		Compatibility: []CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}},
	})
	if err != nil {
		t.Fatalf("NewIndustryPack(%s): %v", id, err)
	}
	return pack
}

func fixtureSpec(t *testing.T) BindingSpec {
	t.Helper()
	start := bindingDate(t, "2026-01-01")
	end := bindingDate(t, "2027-01-01")
	refdataV1 := ContentRef{Kind: ContentReferenceData, Namespace: "healthcare", ID: "job-family", Version: "1"}
	refdataV2 := ContentRef{Kind: ContentReferenceData, Namespace: "healthcare", ID: "job-family", Version: "2"}
	rule := ContentRef{Kind: ContentRule, Namespace: "healthcare", ID: "promotion-approval", Version: "2026.1"}
	formula := ContentRef{Kind: ContentFormula, Namespace: "healthcare", ID: "annualize", Version: "1"}
	base := Pack{
		Manifest:        bindingManifest(t, "healthcare-base"),
		References:      []ContentRef{rule, formula},
		OverridableRefs: []ContentRef{refdataV1},
		Precedence:      []Precedence{{HigherPack: "healthcare-overlay", LowerPack: "healthcare-base"}},
		Contents: []Content{
			{Ref: refdataV1, Effective: ClosedWindow(start, end), Digest: "sha256:job-v1", Overridable: true},
			{Ref: rule, Effective: OpenWindow(start), Digest: "sha256:rule-v1"},
			{Ref: formula, Effective: OpenWindow(start), Digest: "sha256:formula-v1", Dependencies: []ContentRef{refdataV2}},
		},
	}
	overlay := Pack{
		Manifest:   bindingManifest(t, "healthcare-overlay"),
		References: []ContentRef{refdataV2},
		Contents: []Content{
			{Ref: refdataV2, Effective: ClosedWindow(start, end), Digest: "sha256:job-v2", OverrideOf: &refdataV1},
		},
	}
	return BindingSpec{Packs: []Pack{base, overlay}}
}

// TestTodo_PACK_002 proves exact namespaced/versioned resolution, effective
// windows, a declared override and recursive dependency closure over two packs.
func TestTodo_PACK_002(t *testing.T) {
	binding, err := Bind(fixtureSpec(t))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if len(binding.Contents) != 3 {
		t.Fatalf("bound content count = %d, want 3", len(binding.Contents))
	}
	if binding.CanonicalDigest == "" {
		t.Fatal("binding digest is empty")
	}
	for _, item := range binding.Contents {
		if item.Ref.Namespace == "healthcare" && item.Ref.ID == "job-family" && item.Ref.Version != "2" {
			t.Fatalf("base override remained bound: %+v", item.Ref)
		}
	}
}

func TestTodo_PACK_002_Property(t *testing.T) {
	left, err := Bind(fixtureSpec(t))
	if err != nil {
		t.Fatalf("left Bind: %v", err)
	}
	spec := fixtureSpec(t)
	spec.Packs[0], spec.Packs[1] = spec.Packs[1], spec.Packs[0]
	right, err := Bind(spec)
	if err != nil {
		t.Fatalf("right Bind: %v", err)
	}
	if left.CanonicalDigest != right.CanonicalDigest {
		t.Fatalf("pack order changed digest: %s vs %s", left.CanonicalDigest, right.CanonicalDigest)
	}
	for _, item := range left.Contents {
		if item.Ref.ID != "job-family" {
			continue
		}
		if !item.Effective.Contains(bindingDate(t, "2026-06-01")) {
			t.Fatal("effective window did not contain an in-range date")
		}
		if item.Effective.Contains(bindingDate(t, "2027-01-01")) {
			t.Fatal("effective window included its exclusive end")
		}
		return
	}
	t.Fatal("job-family content was not bound")
}

func TestTodo_PACK_002_Golden(t *testing.T) {
	binding, err := Bind(fixtureSpec(t))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	const want = "sha256:501df21b35e5a2f21077b94fe6c6c4647a3a9c2f3cbe437ccfa5dc230dc218d4"
	if binding.CanonicalDigest != want {
		t.Fatalf("golden digest = %s, want %s", binding.CanonicalDigest, want)
	}
	if got, err := binding.Digest(); err != nil || got != binding.CanonicalDigest {
		t.Fatalf("Digest() = %q, %v; stored = %q", got, err, binding.CanonicalDigest)
	}
}

func TestTodo_PACK_002_Fault(t *testing.T) {
	spec := fixtureSpec(t)
	spec.Packs[1].References[0].Version = "9"
	_, err := Bind(spec)
	if !errors.Is(err, ErrUnresolvedVersion) || !strings.Contains(err.Error(), "healthcare/job-family@9") {
		t.Fatalf("unresolved version = %v, want typed refusal naming ref", err)
	}

	spec = fixtureSpec(t)
	spec.Packs[1].Contents[0].OverrideOf = &ContentRef{Kind: ContentReferenceData, Namespace: "healthcare", ID: "other", Version: "1"}
	_, err = Bind(spec)
	if !errors.Is(err, ErrIllegalOverride) {
		t.Fatalf("illegal override = %v, want ErrIllegalOverride", err)
	}
}

func TestTodo_PACK_002_Security(t *testing.T) {
	spec := fixtureSpec(t)
	spec.Packs[0].Precedence = nil
	_, err := Bind(spec)
	if !errors.Is(err, ErrAmbiguousPrecedence) || !strings.Contains(err.Error(), "healthcare/job-family") {
		t.Fatalf("ambiguous precedence = %v, want typed refusal naming id", err)
	}

	binding, err := Bind(fixtureSpec(t))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	explanation, err := binding.Explain()
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if explanation.ContentCount != 3 || explanation.Digest == "" {
		t.Fatalf("explanation = %+v", explanation)
	}
	if strings.Contains(bindingExplainText(explanation), "formula-v1") {
		t.Fatal("explanation disclosed content body digest")
	}
}

func TestTodo_PACK_002_Mutation(t *testing.T) {
	spec := fixtureSpec(t)
	spec.Packs[0].Contents[2].Dependencies = []ContentRef{{Kind: ContentFormula, Namespace: "healthcare", ID: "missing", Version: "1"}}
	_, err := Bind(spec)
	if !errors.Is(err, ErrUnresolvedID) || !strings.Contains(err.Error(), "healthcare/missing@1") {
		t.Fatalf("missing dependency = %v, want typed refusal naming ref", err)
	}
}

func bindingExplainText(explanation BindingExplanation) string {
	return explanation.Digest
}
