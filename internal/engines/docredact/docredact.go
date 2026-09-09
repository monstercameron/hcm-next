// Package docredact produces deterministic, policy-bound derivatives of
// extracted document observations. It never changes the source extraction;
// every replacement records the source span and its lineage.
package docredact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/docextract"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

const schemaVersion = 1

// Style is the closed set of transformations a redaction policy may use.
type Style string

const (
	Mask     Style = "MASK"
	Remove   Style = "REMOVE"
	Tokenize Style = "TOKENIZE"

	// StyleMask, StyleRemove and StyleTokenize are descriptive aliases for
	// callers that prefer namespaced constants.
	StyleMask     = Mask
	StyleRemove   = Remove
	StyleTokenize = Tokenize
)

// Rule binds one classified data class to a style for an optional audience.
// An empty Audience is the default rule; a non-empty Audience is considered
// only when it equals Policy.Audience.
type Rule struct {
	Audience string
	Class    dlp.DataClass
	Style    Style
}

// Policy declares the audience-specific redaction contract. Classifications
// maps extraction span paths to the server-resolved data class carried by
// that span. Rules and Classes are equivalent ways to declare styles; Rules
// permit audience-specific entries while Classes is convenient for one
// audience. Audiences is the map form for callers that already have policy
// bundles grouped by audience.
type Policy struct {
	ID              string
	Version         string
	Audience        string
	Rules           []Rule
	Classes         map[dlp.DataClass]Style
	Audiences       map[string]map[dlp.DataClass]Style
	Classifications map[string]dlp.DataClass
	// SpanClasses is a descriptive alias for Classifications. If both are
	// supplied, they must agree for every shared path.
	SpanClasses map[string]dlp.DataClass
	// TokenKey is required by TOKENIZE and is never copied into a result or
	// digest in raw form.
	TokenKey []byte
}

var (
	ErrInvalidRequest      = errors.New("docredact: invalid request")
	ErrUnknownDataClass    = errors.New("docredact: unknown data class")
	ErrInvalidStyle        = errors.New("docredact: invalid redaction style")
	ErrTokenKeyRequired    = errors.New("docredact: tokenize requires a key")
	ErrLineage             = errors.New("docredact: invalid source lineage")
	ErrRedactionIncomplete = errors.New("REDACTION_INCOMPLETE")
)

// Redaction records one replacement and the exact source span it came from.
// Replacement is safe derivative content: it is either a mask, empty, or a
// keyed token and never contains the original source text.
type Redaction struct {
	SpanPath      string
	Class         dlp.DataClass
	Style         Style
	SourceLineage docextract.Lineage
	Replacement   string
}

// Result is the deterministic derivative. Spans and Structured contain the
// same observations as the extraction with selected text replaced; Metadata
// is copied and redacted by its metadata span path where applicable.
type Result struct {
	ArtifactDigest string
	SourceDigest   string
	PolicyID       string
	PolicyVersion  string
	Audience       string
	Text           string
	Spans          []docextract.Span
	Structured     docextract.Structured
	Metadata       map[string]string
	Redactions     []Redaction
	Digest         string
}

// Derivative is an alias for callers that use the domain term.
type Derivative = Result

// Validate checks the closed policy vocabulary and its audience bindings.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Audience) == "" {
		return fmt.Errorf("%w: policy id, version and audience are required", ErrInvalidRequest)
	}
	classifications, err := mergeClassifications(p)
	if err != nil {
		return err
	}
	for path, class := range classifications {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("%w: empty classified span path", ErrInvalidRequest)
		}
		if !class.Valid() {
			return fmt.Errorf("%w: %q", ErrUnknownDataClass, class)
		}
	}
	styles, err := p.styles()
	if err != nil {
		return err
	}
	needsKey := false
	for _, style := range styles {
		if style == Tokenize {
			needsKey = true
		}
	}
	if needsKey && len(p.TokenKey) == 0 {
		return ErrTokenKeyRequired
	}
	return nil
}

func mergeClassifications(p Policy) (map[string]dlp.DataClass, error) {
	out := make(map[string]dlp.DataClass, len(p.Classifications)+len(p.SpanClasses))
	for path, class := range p.Classifications {
		out[path] = class
	}
	for path, class := range p.SpanClasses {
		if current, ok := out[path]; ok && current != class {
			return nil, fmt.Errorf("%w: classifications disagree for span %q", ErrInvalidRequest, path)
		}
		out[path] = class
	}
	return out, nil
}

func (p Policy) styles() (map[dlp.DataClass]Style, error) {
	styles := make(map[dlp.DataClass]Style)
	for class, style := range p.Classes {
		if err := validateStyle(style); err != nil {
			return nil, err
		}
		if !class.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrUnknownDataClass, class)
		}
		styles[class] = style
	}
	for audience, audienceStyles := range p.Audiences {
		for class, style := range audienceStyles {
			if err := validateStyle(style); err != nil {
				return nil, err
			}
			if !class.Valid() {
				return nil, fmt.Errorf("%w: %q", ErrUnknownDataClass, class)
			}
			if audience == p.Audience {
				styles[class] = style
			}
		}
	}
	for _, rule := range p.Rules {
		if !rule.Class.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrUnknownDataClass, rule.Class)
		}
		if rule.Audience != "" && rule.Audience != p.Audience {
			continue
		}
		if err := validateStyle(rule.Style); err != nil {
			return nil, err
		}
		styles[rule.Class] = rule.Style
	}
	return styles, nil
}

func validateStyle(style Style) error {
	switch style {
	case Mask, Remove, Tokenize:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidStyle, style)
	}
}

// Generate creates a derivative from extraction and policy. The operation is
// pure: it copies all returned slices and maps, and its digest includes the
// source extraction digest, policy version, audience, classifications, and
// resulting observations.
func Generate(extraction docextract.Result, policy Policy) (Result, error) {
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}
	if extraction.ArtifactDigest == "" || extraction.Digest == "" {
		return Result{}, fmt.Errorf("%w: extraction lacks artifact and result digests", ErrInvalidRequest)
	}
	if extraction.State == docextract.Failed {
		return Result{}, fmt.Errorf("%w: failed extraction cannot produce a derivative", ErrInvalidRequest)
	}
	if err := validateExtractionLineage(extraction); err != nil {
		return Result{}, err
	}
	styles, err := policy.styles()
	if err != nil {
		return Result{}, err
	}
	classifications, err := mergeClassifications(policy)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		ArtifactDigest: extraction.ArtifactDigest,
		SourceDigest:   extraction.Digest,
		PolicyID:       policy.ID,
		PolicyVersion:  policy.Version,
		Audience:       policy.Audience,
		Text:           extraction.Text,
		Spans:          cloneSpans(extraction.Spans),
		Structured:     docextract.Structured{Paragraphs: cloneSpans(extraction.Structured.Paragraphs)},
		Metadata:       cloneMetadata(extraction.Metadata),
	}

	for i := range result.Spans {
		if err := redactSpan(&result.Spans[i], classifications, styles, policy.TokenKey, &result); err != nil {
			return Result{}, err
		}
	}
	for i := range result.Structured.Paragraphs {
		if err := redactSpan(&result.Structured.Paragraphs[i], classifications, styles, policy.TokenKey, &result); err != nil {
			return Result{}, err
		}
	}
	result.Text, err = redactText(extraction.Text, extraction.Spans, classifications, styles, policy.TokenKey, &result)
	if err != nil {
		return Result{}, err
	}
	for _, redaction := range result.Redactions {
		if strings.HasPrefix(redaction.SpanPath, "metadata.") {
			if _, ok := result.Metadata[strings.TrimPrefix(redaction.SpanPath, "metadata.")]; ok {
				result.Metadata[strings.TrimPrefix(redaction.SpanPath, "metadata.")] = redaction.Replacement
			}
		}
	}
	result.Digest = digestResult(result, policy, classifications, styles)
	return result, nil
}

// Redact is a concise alias for Generate.
func Redact(extraction docextract.Result, policy Policy) (Result, error) {
	return Generate(extraction, policy)
}

func validateExtractionLineage(extraction docextract.Result) error {
	for _, span := range append(cloneSpans(extraction.Spans), extraction.Structured.Paragraphs...) {
		if span.Path == "" || span.Lineage.ArtifactDigest != extraction.ArtifactDigest {
			return fmt.Errorf("%w: span %q is not bound to the extraction artifact", ErrLineage, span.Path)
		}
		if span.Text == "" {
			return fmt.Errorf("%w: span %q has no source text", ErrLineage, span.Path)
		}
		if span.Lineage.Length != uint64(len(span.Text)) {
			return fmt.Errorf("%w: span %q length does not match source text", ErrLineage, span.Path)
		}
	}
	return nil
}

func redactSpan(span *docextract.Span, classifications map[string]dlp.DataClass, styles map[dlp.DataClass]Style, key []byte, result *Result) error {
	class, classified := classifications[span.Path]
	if !classified {
		return nil
	}
	style, selected := styles[class]
	if !selected {
		return nil
	}
	replacement := replacementFor(style, class, span.Text, key)
	span.Text = replacement
	result.Redactions = append(result.Redactions, Redaction{
		SpanPath: span.Path, Class: class, Style: style,
		SourceLineage: span.Lineage, Replacement: replacement,
	})
	return nil
}

func redactText(source string, sourceSpans []docextract.Span, classifications map[string]dlp.DataClass, styles map[dlp.DataClass]Style, key []byte, result *Result) (string, error) {
	targets := make([]docextract.Span, 0)
	for _, span := range sourceSpans {
		if span.Kind != docextract.KindText && span.Kind != docextract.KindOCR {
			continue
		}
		class, ok := classifications[span.Path]
		if !ok {
			continue
		}
		if _, ok := styles[class]; ok {
			targets = append(targets, span)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Lineage.Page != targets[j].Lineage.Page {
			return targets[i].Lineage.Page < targets[j].Lineage.Page
		}
		if targets[i].Lineage.Offset != targets[j].Lineage.Offset {
			return targets[i].Lineage.Offset < targets[j].Lineage.Offset
		}
		return targets[i].Path < targets[j].Path
	})
	text := source
	searchFrom := 0
	for _, span := range targets {
		start := strings.Index(text[searchFrom:], span.Text)
		if start < 0 {
			return "", fmt.Errorf("%w: classified text span %q is absent from extraction text", ErrLineage, span.Path)
		}
		start += searchFrom
		class := classifications[span.Path]
		style := styles[class]
		replacement := replacementFor(style, class, span.Text, key)
		text = text[:start] + replacement + text[start+len(span.Text):]
		searchFrom = start + len(replacement)
	}
	return text, nil
}

func replacementFor(style Style, class dlp.DataClass, source string, key []byte) string {
	switch style {
	case Mask:
		return strings.Repeat("█", utf8.RuneCountInString(source))
	case Remove:
		return ""
	case Tokenize:
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(string(class)))
		mac.Write([]byte{0})
		mac.Write([]byte(source))
		return "tok_" + hex.EncodeToString(mac.Sum(nil)[:12])
	default:
		return source
	}
}

func cloneSpans(in []docextract.Span) []docextract.Span {
	return append([]docextract.Span(nil), in...)
}

func cloneMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func digestResult(result Result, policy Policy, classifications map[string]dlp.DataClass, styles map[dlp.DataClass]Style) string {
	w := canonicalbytes.New("hcmnext.engines.docredact.Result", schemaVersion).
		String("artifact_digest", result.ArtifactDigest).
		String("source_digest", result.SourceDigest).
		String("policy_id", policy.ID).
		String("policy_version", policy.Version).
		String("audience", policy.Audience).
		String("token_key_digest", canonicalbytes.Digest(policy.TokenKey))

	paths := make([]string, 0, len(classifications))
	for path := range classifications {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		w.String("classification_path", path).String("classification", string(classifications[path]))
	}
	classes := make([]string, 0, len(styles))
	for class := range styles {
		classes = append(classes, string(class))
	}
	sort.Strings(classes)
	for _, class := range classes {
		w.String("style_class", class).String("style", string(styles[dlp.DataClass(class)]))
	}
	w.String("text", result.Text)
	w.Count("spans", len(result.Spans))
	for _, span := range result.Spans {
		w.String("span_path", span.Path).String("span_kind", string(span.Kind)).String("span_text", span.Text).
			String("span_artifact", span.Lineage.ArtifactDigest).Int("span_page", int64(span.Lineage.Page)).
			Int("span_offset", int64(span.Lineage.Offset)).Int("span_length", int64(span.Lineage.Length))
	}
	w.Count("structured", len(result.Structured.Paragraphs))
	for _, span := range result.Structured.Paragraphs {
		w.String("structured_path", span.Path).String("structured_text", span.Text).
			Int("structured_page", int64(span.Lineage.Page)).Int("structured_offset", int64(span.Lineage.Offset)).
			Int("structured_length", int64(span.Lineage.Length))
	}
	keys := make([]string, 0, len(result.Metadata))
	for key := range result.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		w.String("metadata_key", key).String("metadata_value", result.Metadata[key])
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Verify recomputes the derivative and refuses any changed text, lineage,
// redaction record, or digest with REDACTION_INCOMPLETE.
func (r Result) Verify(extraction docextract.Result, policy Policy) error {
	expected, err := Generate(extraction, policy)
	if err != nil {
		return fmt.Errorf("%w: cannot recompute derivative: %v", ErrRedactionIncomplete, err)
	}
	if r.Digest != expected.Digest || r.Text != expected.Text || r.ArtifactDigest != expected.ArtifactDigest ||
		r.SourceDigest != expected.SourceDigest || r.PolicyID != expected.PolicyID || r.PolicyVersion != expected.PolicyVersion ||
		r.Audience != expected.Audience || !sameSpans(r.Spans, expected.Spans) ||
		!sameSpans(r.Structured.Paragraphs, expected.Structured.Paragraphs) || !sameMetadata(r.Metadata, expected.Metadata) ||
		!sameRedactions(r.Redactions, expected.Redactions) {
		return fmt.Errorf("%w: derivative does not match the source and policy", ErrRedactionIncomplete)
	}
	return nil
}

func sameSpans(a, b []docextract.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameMetadata(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func sameRedactions(a, b []Redaction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
