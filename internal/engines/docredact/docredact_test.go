package docredact

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/docextract"
	"github.com/monstercameron/hcm-next/internal/trust/dlp"
)

func fixtureExtraction(t *testing.T) docextract.Result {
	t.Helper()
	result, err := docextract.Extract(context.Background(), docextract.Request{
		Document: docextract.Document{
			Bytes: []byte("name: Alice\nssn: 123-45-6789\nnotes: internal\n"),
			Pages: []docextract.Page{{Number: 1, Text: "name: Alice\nssn: 123-45-6789\nnotes: internal\n"}},
		},
		Limits:         docextract.Limits{MaxBytes: 1000, MaxPages: 1},
		ParserVersion:  "plain-v1",
		ProfileVersion: "fixture-v1",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return result
}

func fixturePolicy() Policy {
	return Policy{
		ID:       "policy.document.external",
		Version:  "2026-09-05.1",
		Audience: "external-reviewer",
		Classifications: map[string]dlp.DataClass{
			"page.1.line.1": dlp.ClassPII,
			"page.1.line.2": dlp.ClassBank,
			"page.1.line.3": dlp.ClassInternal,
		},
		Rules: []Rule{
			{Class: dlp.ClassPII, Style: Mask},
			{Class: dlp.ClassBank, Style: Remove},
			{Audience: "external-reviewer", Class: dlp.ClassInternal, Style: Tokenize},
		},
		TokenKey: []byte("fixture-token-key"),
	}
}

// TestTodo_DOC_REDACT_001 proves the derivative is policy-bound, transforms
// every selected span, and retains source lineage for each replacement.
func TestTodo_DOC_REDACT_001(t *testing.T) {
	extraction := fixtureExtraction(t)
	policy := fixturePolicy()
	result, err := Generate(extraction, policy)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := result.Verify(extraction, policy); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(result.Redactions) != 3 {
		t.Fatalf("redactions = %d, want 3", len(result.Redactions))
	}
	for _, redaction := range result.Redactions {
		if redaction.SourceLineage.ArtifactDigest != extraction.ArtifactDigest || redaction.Replacement == "" && redaction.Style != Remove {
			t.Fatalf("redaction lost lineage or replacement: %+v", redaction)
		}
	}
	if strings.Contains(result.Text, "Alice") || strings.Contains(result.Text, "123-45-6789") || strings.Contains(result.Text, "internal") {
		t.Fatalf("redacted text leaked source content: %q", result.Text)
	}
	if extraction.Text == result.Text {
		t.Fatal("redaction did not produce a derivative")
	}
}

func TestTodo_DOC_REDACT_001_Golden(t *testing.T) {
	extraction := fixtureExtraction(t)
	result, err := Generate(extraction, fixturePolicy())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	const wantText = "███████████\n\ntok_999005a3a34e1135a8f9293a\n"
	const wantDigest = "sha256:27463bdc2efb45888905b5b785d2a3a6ebbfad7505ce03621ed73ea29a3169fe"
	if result.Text != wantText {
		t.Fatalf("derivative text = %q, want %q", result.Text, wantText)
	}
	if result.Digest != wantDigest {
		t.Fatalf("derivative digest = %q, want golden %q", result.Digest, wantDigest)
	}
}

func TestTodo_DOC_REDACT_001_Integration(t *testing.T) {
	extraction := fixtureExtraction(t)
	policy := fixturePolicy()
	first, err := Redact(extraction, policy)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	second, err := Generate(extraction, policy)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if first.Digest != second.Digest || first.Text != second.Text {
		t.Fatalf("alias and primary APIs diverged: %+v vs %+v", first, second)
	}
	changed := policy
	changed.Version = "2026-09-05.2"
	third, err := Generate(extraction, changed)
	if err != nil {
		t.Fatalf("Generate changed policy: %v", err)
	}
	if third.Digest == first.Digest {
		t.Fatal("policy version did not bind the derivative digest")
	}
}

func TestUnknownDataClassIsRefused(t *testing.T) {
	policy := fixturePolicy()
	policy.Classifications["page.1.line.1"] = dlp.DataClass("TENANT_SECRET")
	if _, err := Generate(fixtureExtraction(t), policy); !errors.Is(err, ErrUnknownDataClass) {
		t.Fatalf("error = %v, want ErrUnknownDataClass", err)
	}
}

func TestDerivativeMutationIsRefused(t *testing.T) {
	extraction := fixtureExtraction(t)
	policy := fixturePolicy()
	result, err := Generate(extraction, policy)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	result.Spans[0].Text = "Alice"
	if !errors.Is(result.Verify(extraction, policy), ErrRedactionIncomplete) {
		t.Fatal("mutated derivative was accepted")
	}
}
