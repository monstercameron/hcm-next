package docredact

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/docextract"
	"github.com/monstercameron/hcm-next/internal/trust/dlp"
)

func manualExtraction() docextract.Result {
	return docextract.Result{
		State: docextract.Complete, ArtifactDigest: "sha256:artifact", Digest: "sha256:extraction",
		Text: "Alice 123", Metadata: map[string]string{"owner": "Alice"},
		Spans: []docextract.Span{
			{Path: "name", Kind: docextract.KindText, Text: "Alice", Lineage: docextract.Lineage{ArtifactDigest: "sha256:artifact", Page: 1, Offset: 0, Length: 5}},
			{Path: "account", Kind: docextract.KindOCR, Text: "123", Lineage: docextract.Lineage{ArtifactDigest: "sha256:artifact", Page: 1, Offset: 6, Length: 3}},
			{Path: "metadata.owner", Kind: docextract.KindMetadata, Text: "Alice", Lineage: docextract.Lineage{ArtifactDigest: "sha256:artifact", Length: 5}},
		},
		Structured: docextract.Structured{Paragraphs: []docextract.Span{{Path: "paragraph", Kind: docextract.KindStructured, Text: "internal", Lineage: docextract.Lineage{ArtifactDigest: "sha256:artifact", Page: 1, Offset: 10, Length: 8}}}},
	}
}

func manualPolicy() Policy {
	return Policy{ID: "policy", Version: "v1", Audience: "external", Classifications: map[string]dlp.DataClass{"name": dlp.ClassPII, "account": dlp.ClassBank, "metadata.owner": dlp.ClassInternal, "paragraph": dlp.ClassPHI}, Rules: []Rule{{Class: dlp.ClassPII, Style: Mask}, {Class: dlp.ClassBank, Style: Remove}, {Class: dlp.ClassInternal, Style: Tokenize}, {Class: dlp.ClassPHI, Style: Mask}}, TokenKey: []byte("key")}
}

func TestPolicyValidate_BindingsAndClosedVocabularies(t *testing.T) {
	base := manualPolicy()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*Policy)
		want error
	}{
		{"missing id", func(p *Policy) { p.ID = "" }, ErrInvalidRequest},
		{"missing version", func(p *Policy) { p.Version = "" }, ErrInvalidRequest},
		{"missing audience", func(p *Policy) { p.Audience = "" }, ErrInvalidRequest},
		{"classification conflict", func(p *Policy) { p.SpanClasses = map[string]dlp.DataClass{"name": dlp.ClassBank} }, ErrInvalidRequest},
		{"empty path", func(p *Policy) { p.Classifications = map[string]dlp.DataClass{" ": dlp.ClassPII} }, ErrInvalidRequest},
		{"unknown class", func(p *Policy) { p.Classifications = map[string]dlp.DataClass{"name": "SECRET"} }, ErrUnknownDataClass},
		{"invalid class style", func(p *Policy) { p.Classes = map[dlp.DataClass]Style{dlp.ClassPII: "BOGUS"}; p.Rules = nil }, ErrInvalidStyle},
		{"unknown class style map", func(p *Policy) { p.Classes = map[dlp.DataClass]Style{"SECRET": Mask}; p.Rules = nil }, ErrUnknownDataClass},
		{"audience invalid style", func(p *Policy) {
			p.Audiences = map[string]map[dlp.DataClass]Style{"other": {dlp.ClassPII: "BOGUS"}}
			p.Rules = nil
		}, ErrInvalidStyle},
		{"audience unknown class", func(p *Policy) {
			p.Audiences = map[string]map[dlp.DataClass]Style{"other": {"SECRET": Mask}}
			p.Rules = nil
		}, ErrUnknownDataClass},
		{"rule unknown class", func(p *Policy) { p.Rules = []Rule{{Class: "SECRET", Style: Mask}} }, ErrUnknownDataClass},
		{"token key", func(p *Policy) { p.TokenKey = nil }, ErrTokenKeyRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.Classifications = cloneClassifications(base.Classifications)
			bad.Rules = append([]Rule(nil), base.Rules...)
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
	if err := (Policy{ID: "p", Version: "v", Audience: "a", Rules: []Rule{{Audience: "other", Class: dlp.ClassPII, Style: Mask}}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func cloneClassifications(in map[string]dlp.DataClass) map[string]dlp.DataClass {
	out := make(map[string]dlp.DataClass, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestGenerate_AllStylesStructuredMetadataAndLineage(t *testing.T) {
	extraction := manualExtraction()
	policy := manualPolicy()
	result, err := Generate(extraction, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Verify(extraction, policy); err != nil {
		t.Fatal(err)
	}
	if len(result.Redactions) != 4 || result.Spans[0].Text == "Alice" || result.Spans[1].Text != "" || result.Metadata["owner"] == "Alice" || result.Structured.Paragraphs[0].Text == "internal" {
		t.Fatalf("unexpected derivative = %+v", result)
	}
	if extraction.Spans[0].Text != "Alice" || extraction.Metadata["owner"] != "Alice" {
		t.Fatal("Generate mutated extraction input")
	}
	if got, err := Redact(extraction, policy); err != nil || got.Digest != result.Digest {
		t.Fatalf("Redact alias = %+v, %v", got, err)
	}
	if replacementFor(Mask, dlp.ClassPII, "abc", nil) == "abc" || replacementFor(Remove, dlp.ClassPII, "abc", nil) != "" || replacementFor("OTHER", dlp.ClassPII, "abc", nil) != "abc" {
		t.Fatal("replacement styles incorrect")
	}
}

func TestGenerate_RejectsFailedAndMalformedLineage(t *testing.T) {
	policy := manualPolicy()
	cases := []struct {
		name string
		edit func(*docextract.Result)
		want error
	}{
		{"missing artifact", func(r *docextract.Result) { r.ArtifactDigest = "" }, ErrInvalidRequest},
		{"missing result digest", func(r *docextract.Result) { r.Digest = "" }, ErrInvalidRequest},
		{"failed", func(r *docextract.Result) { r.State = docextract.Failed }, ErrInvalidRequest},
		{"empty path", func(r *docextract.Result) { r.Spans[0].Path = "" }, ErrLineage},
		{"wrong artifact", func(r *docextract.Result) { r.Spans[0].Lineage.ArtifactDigest = "other" }, ErrLineage},
		{"empty text", func(r *docextract.Result) { r.Spans[0].Text = "" }, ErrLineage},
		{"wrong length", func(r *docextract.Result) { r.Spans[0].Lineage.Length = 99 }, ErrLineage},
		{"classified text absent", func(r *docextract.Result) { r.Text = "different" }, ErrLineage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := manualExtraction()
			tc.edit(&bad)
			if _, err := Generate(bad, policy); !errors.Is(err, tc.want) {
				t.Fatalf("Generate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestResultVerifyRejectsTamperingAndRecomputeErrors(t *testing.T) {
	extraction := manualExtraction()
	policy := manualPolicy()
	result, err := Generate(extraction, policy)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Result)
	}{
		{"digest", func(r *Result) { r.Digest = "changed" }},
		{"text", func(r *Result) { r.Text = "Alice" }},
		{"artifact", func(r *Result) { r.ArtifactDigest = "changed" }},
		{"source", func(r *Result) { r.SourceDigest = "changed" }},
		{"policy id", func(r *Result) { r.PolicyID = "changed" }},
		{"version", func(r *Result) { r.PolicyVersion = "changed" }},
		{"audience", func(r *Result) { r.Audience = "changed" }},
		{"span", func(r *Result) { r.Spans[0].Text = "Alice" }},
		{"structured", func(r *Result) { r.Structured.Paragraphs[0].Text = "internal" }},
		{"metadata", func(r *Result) { r.Metadata["owner"] = "Alice" }},
		{"redaction", func(r *Result) { r.Redactions[0].Replacement = "Alice" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := result
			mutated.Spans = append([]docextract.Span(nil), result.Spans...)
			mutated.Structured.Paragraphs = append([]docextract.Span(nil), result.Structured.Paragraphs...)
			mutated.Metadata = cloneStringMap(result.Metadata)
			mutated.Redactions = append([]Redaction(nil), result.Redactions...)
			tc.mutate(&mutated)
			if !errors.Is(mutated.Verify(extraction, policy), ErrRedactionIncomplete) {
				t.Fatal("tampered derivative accepted")
			}
		})
	}
	badExtraction := extraction
	badExtraction.State = docextract.Failed
	if !errors.Is(result.Verify(badExtraction, policy), ErrRedactionIncomplete) {
		t.Fatal("Verify accepted an extraction that cannot be regenerated")
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
