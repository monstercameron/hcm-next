// Package docextract extracts bounded observations from an in-memory document.
// It has no parser or OCR dependency and never treats extracted content as an
// executable instruction.
package docextract

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const schemaVersion = 1

// Extraction states distinguish a complete observation from a bounded or
// parser/OCR failure.
type State string

const (
	Complete State = "COMPLETE"
	Partial  State = "PARTIAL"
	Failed   State = "FAILED"
)

// SpanKind identifies the observation surface.
type SpanKind string

const (
	KindText       SpanKind = "TEXT"
	KindStructured SpanKind = "STRUCTURED"
	KindOCR        SpanKind = "OCR"
	KindMetadata   SpanKind = "METADATA"
)

// Errors returned by extraction. LimitError and LineageError unwrap to their
// sentinels so callers can branch without matching prose.
var (
	ErrInvalidDocument = errors.New("docextract: invalid document")
	ErrLimitExceeded   = errors.New("docextract: extraction limit exceeded")
	ErrLineage         = errors.New("docextract: invalid source lineage")
	ErrOCR             = errors.New("docextract: OCR failed")
)

// ErrLimit is a compatibility alias with the short name used by sibling
// document extraction code.
var ErrLimit = ErrLimitExceeded

// LimitError identifies the declared resource that refused extraction.
type LimitError struct {
	Resource string
	Limit    uint64
	Actual   uint64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("docextract: %s limit %d exceeded by %d", e.Resource, e.Limit, e.Actual)
}

func (e *LimitError) Unwrap() error { return ErrLimitExceeded }

// LineageError identifies a malformed span or source digest.
type LineageError struct{ Detail string }

func (e *LineageError) Error() string { return "docextract: " + e.Detail }
func (e *LineageError) Unwrap() error { return ErrLineage }

// Limits are explicit resource declarations. MaxBytes and MaxPages are
// required; the other bounds are optional refinements.
type Limits struct {
	MaxBytes       uint64
	MaxPages       uint32
	MaxOutputBytes uint64
	MaxSpans       uint32
}

func (l Limits) Validate() error {
	if l.MaxBytes == 0 || l.MaxPages == 0 {
		return fmt.Errorf("%w: MaxBytes and MaxPages are required", ErrInvalidDocument)
	}
	return nil
}

// Page is one in-memory document page. Number is one-based. Blocks are the
// minimal structured form; their offsets are byte offsets into Text.
type Page struct {
	Number uint32
	Text   string
	Blocks []Block
}

// Block is a small structured observation inside a page.
type Block struct {
	Kind   string
	Text   string
	Offset uint64
}

// Document is the source object. Bytes are the artifact bytes whose digest
// binds every extracted span. When Bytes is empty, deterministic bytes are
// derived from pages for a wholly in-memory fixture.
type Document struct {
	ArtifactDigest string
	Bytes          []byte
	Pages          []Page
	Metadata       map[string]string
}

// Lineage binds an extracted span to the source artifact, page, and byte
// offset. Page zero denotes artifact-level metadata.
type Lineage struct {
	ArtifactDigest string
	Page           uint32
	Offset         uint64
	Length         uint64
}

// Span is one extracted observation. Text is retained for plain and
// structured output; Lineage is mandatory for every span.
type Span struct {
	Path    string
	Kind    SpanKind
	Text    string
	Lineage Lineage
}

// Structured is the minimal structured extraction surface.
type Structured struct {
	Paragraphs []Span
}

// OCRRequest is the bounded, page-scoped input supplied to an OCR port.
type OCRRequest struct {
	ArtifactDigest string
	Page           Page
	Limits         Limits
}

// OCRPort is deliberately narrow. This package does not ship or invoke a
// real OCR implementation.
type OCRPort interface {
	Recognize(context.Context, OCRRequest) (string, error)
}

// OCR is an alias for callers that use the shorter port name.
type OCR = OCRPort

// MemoryOCR is a deterministic in-memory OCR fake keyed by one-based page.
type MemoryOCR struct {
	Pages map[uint32]string
}

// InMemoryOCR is the descriptive alias for MemoryOCR.
type InMemoryOCR = MemoryOCR

func (m MemoryOCR) Recognize(ctx context.Context, request OCRRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return m.Pages[request.Page.Number], nil
}

// Request binds a document, declared limits, optional OCR port and version
// pins used in the extraction digest.
type Request struct {
	Document       Document
	Limits         Limits
	OCR            OCRPort
	ParserVersion  string
	ProfileVersion string
}

// Result is a reproducible extraction observation.
type Result struct {
	State          State
	ArtifactDigest string
	Text           string
	Structured     Structured
	Metadata       map[string]string
	Spans          []Span
	Digest         string
	PagesExamined  uint32
	BytesExamined  uint64
	Error          string
}

// Extract performs one bounded extraction. A source byte/page refusal returns
// a typed error and a non-COMPLETE result; recovered pages are retained as
// PARTIAL evidence.
func Extract(ctx context.Context, request Request) (Result, error) {
	result := Result{State: Failed, Metadata: map[string]string{}}
	if err := request.Limits.Validate(); err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result, err
	}
	raw := sourceBytes(request.Document)
	result.BytesExamined = uint64(len(raw))
	if len(raw) == 0 {
		err := fmt.Errorf("%w: document has no bytes or pages", ErrInvalidDocument)
		result.Error = err.Error()
		return result, err
	}
	digest := canonicalbytes.Digest(raw)
	if request.Document.ArtifactDigest != "" && !sameDigest(request.Document.ArtifactDigest, digest) {
		err := &LineageError{Detail: "artifact digest does not match in-memory bytes"}
		result.Error = err.Error()
		return result, err
	}
	result.ArtifactDigest = digest
	if uint64(len(raw)) > request.Limits.MaxBytes {
		err := &LimitError{Resource: "bytes", Limit: request.Limits.MaxBytes, Actual: uint64(len(raw))}
		result.Error = err.Error()
		return result, err
	}
	pages := normalizedPages(request.Document, raw)
	seenPages := make(map[uint32]struct{}, len(pages))
	for _, page := range pages {
		if page.Number == 0 {
			err := fmt.Errorf("%w: page number is zero", ErrInvalidDocument)
			result.Error = err.Error()
			return result, err
		}
		if _, exists := seenPages[page.Number]; exists {
			err := fmt.Errorf("%w: duplicate page %d", ErrInvalidDocument, page.Number)
			result.Error = err.Error()
			return result, err
		}
		seenPages[page.Number] = struct{}{}
	}
	if uint32(len(pages)) > request.Limits.MaxPages {
		err := &LimitError{Resource: "pages", Limit: uint64(request.Limits.MaxPages), Actual: uint64(len(pages))}
		result.Error = err.Error()
		return result, err
	}
	for key, value := range request.Document.Metadata {
		result.Metadata[key] = value
	}

	var firstErr error
	var textParts []string
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			firstErr = err
			break
		}
		result.PagesExamined++
		pageText := page.Text
		if pageText == "" && request.OCR != nil {
			ocrText, err := request.OCR.Recognize(ctx, OCRRequest{ArtifactDigest: digest, Page: page, Limits: request.Limits})
			if err != nil {
				firstErr = fmt.Errorf("%w: page %d: %v", ErrOCR, page.Number, err)
			} else if ocrText != "" {
				pageText = ocrText
				result.Spans = append(result.Spans, Span{Path: fmt.Sprintf("page.%d.ocr", page.Number), Kind: KindOCR, Text: ocrText, Lineage: Lineage{ArtifactDigest: digest, Page: page.Number, Length: uint64(len(ocrText))}})
			}
		}
		if pageText != "" {
			textParts = append(textParts, pageText)
			result.Spans = append(result.Spans, textSpans(digest, page, pageText)...)
			paragraphs := structuredSpans(digest, page, pageText)
			result.Structured.Paragraphs = append(result.Structured.Paragraphs, paragraphs...)
		}
		if len(page.Blocks) > 0 {
			for blockIndex, block := range page.Blocks {
				result.Structured.Paragraphs = append(result.Structured.Paragraphs, Span{Path: fmt.Sprintf("page.%d.block.%d", page.Number, blockIndex+1), Kind: KindStructured, Text: block.Text, Lineage: Lineage{ArtifactDigest: digest, Page: page.Number, Offset: block.Offset, Length: uint64(len(block.Text))}})
			}
		}
		if request.Limits.MaxSpans > 0 && uint32(len(result.Spans)+len(result.Structured.Paragraphs)) > request.Limits.MaxSpans {
			firstErr = &LimitError{Resource: "spans", Limit: uint64(request.Limits.MaxSpans), Actual: uint64(len(result.Spans) + len(result.Structured.Paragraphs))}
			break
		}
	}
	result.Text = strings.Join(textParts, "\n")
	result.Spans = append(result.Spans, metadataSpans(digest, request.Document.Metadata)...)
	sort.SliceStable(result.Spans, func(i, j int) bool { return spanLess(result.Spans[i], result.Spans[j]) })
	sort.SliceStable(result.Structured.Paragraphs, func(i, j int) bool { return spanLess(result.Structured.Paragraphs[i], result.Structured.Paragraphs[j]) })
	if request.Limits.MaxOutputBytes > 0 && uint64(len(result.Text)) > request.Limits.MaxOutputBytes {
		firstErr = &LimitError{Resource: "output bytes", Limit: request.Limits.MaxOutputBytes, Actual: uint64(len(result.Text))}
	}
	result.Digest = resultDigest(request, result)
	if firstErr != nil {
		result.Error = firstErr.Error()
		if len(result.Spans) == 0 {
			result.State = Failed
		} else {
			result.State = Partial
		}
		result.Digest = resultDigest(request, result)
		return result, firstErr
	}
	result.State = Complete
	result.Digest = resultDigest(request, result)
	return result, nil
}

// ExtractDocument is a compact helper for callers that do not need version
// pins in the request.
func ExtractDocument(ctx context.Context, document Document, limits Limits, ocr OCRPort) (Result, error) {
	return Extract(ctx, Request{Document: document, Limits: limits, OCR: ocr})
}

// ValidateLineage checks that every span remains bound to the result artifact
// and to a valid page/offset range.
func ValidateLineage(document Document, result Result) error {
	digest := result.ArtifactDigest
	pages := normalizedPages(document, sourceBytes(document))
	for _, span := range append(append([]Span(nil), result.Spans...), result.Structured.Paragraphs...) {
		if span.Lineage.ArtifactDigest != digest || span.Lineage.Page == 0 && span.Kind != KindMetadata {
			return &LineageError{Detail: "span is not bound to the result artifact"}
		}
		if span.Kind == KindMetadata {
			continue
		}
		var page Page
		found := false
		for _, candidate := range pages {
			if candidate.Number == span.Lineage.Page {
				page, found = candidate, true
				break
			}
		}
		if !found || (page.Text != "" && span.Lineage.Offset+span.Lineage.Length > uint64(len(page.Text))) {
			return &LineageError{Detail: fmt.Sprintf("span %q is outside page %d", span.Path, span.Lineage.Page)}
		}
	}
	return nil
}

func sourceBytes(document Document) []byte {
	if len(document.Bytes) > 0 {
		return append([]byte(nil), document.Bytes...)
	}
	var parts []string
	for _, page := range document.Pages {
		text := page.Text
		if text == "" {
			var blocks []string
			for _, block := range page.Blocks {
				blocks = append(blocks, block.Text)
			}
			text = strings.Join(blocks, "\n")
		}
		parts = append(parts, text)
	}
	return []byte(strings.Join(parts, "\f"))
}

func normalizedPages(document Document, raw []byte) []Page {
	if len(document.Pages) > 0 {
		pages := append([]Page(nil), document.Pages...)
		sort.SliceStable(pages, func(i, j int) bool { return pages[i].Number < pages[j].Number })
		return pages
	}
	return []Page{{Number: 1, Text: string(raw)}}
}

func textSpans(digest string, page Page, text string) []Span {
	var spans []Span
	start := 0
	line := 1
	for start <= len(text) {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}
		value := strings.TrimSpace(text[start:end])
		if value != "" {
			left := start + len(text[start:end]) - len(strings.TrimLeft(text[start:end], " \t"))
			spans = append(spans, Span{Path: fmt.Sprintf("page.%d.line.%d", page.Number, line), Kind: KindText, Text: value, Lineage: Lineage{ArtifactDigest: digest, Page: page.Number, Offset: uint64(left), Length: uint64(len(value))}})
		}
		line++
		if end == len(text) {
			break
		}
		start = end + 1
	}
	return spans
}

func structuredSpans(digest string, page Page, text string) []Span {
	lines := textSpans(digest, page, text)
	for i := range lines {
		lines[i].Kind = KindStructured
		lines[i].Path = strings.Replace(lines[i].Path, ".line.", ".paragraph.", 1)
	}
	return lines
}

func metadataSpans(digest string, metadata map[string]string) []Span {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	spans := make([]Span, 0, len(keys))
	for _, key := range keys {
		spans = append(spans, Span{Path: "metadata." + key, Kind: KindMetadata, Text: metadata[key], Lineage: Lineage{ArtifactDigest: digest, Page: 1, Length: uint64(len(metadata[key]))}})
	}
	return spans
}

func spanLess(a, b Span) bool {
	if a.Lineage.Page != b.Lineage.Page {
		return a.Lineage.Page < b.Lineage.Page
	}
	if a.Lineage.Offset != b.Lineage.Offset {
		return a.Lineage.Offset < b.Lineage.Offset
	}
	return a.Path < b.Path
}

func sameDigest(got, want string) bool {
	if got == want {
		return true
	}
	return strings.TrimPrefix(got, "sha256:") == strings.TrimPrefix(want, "sha256:")
}

func resultDigest(request Request, result Result) string {
	w := canonicalbytes.New("hcmnext.engines.docextract.Result", schemaVersion).
		String("artifact_digest", result.ArtifactDigest).
		String("state", string(result.State)).
		String("parser_version", request.ParserVersion).
		String("profile_version", request.ProfileVersion).
		String("text", result.Text).
		Int("max_bytes", int64(request.Limits.MaxBytes)).
		Int("max_pages", int64(request.Limits.MaxPages)).
		Int("max_output_bytes", int64(request.Limits.MaxOutputBytes)).
		Int("max_spans", int64(request.Limits.MaxSpans))
	w.Count("spans", len(result.Spans))
	for _, span := range result.Spans {
		w.String("path", span.Path).String("kind", string(span.Kind)).String("text", span.Text).String("artifact", span.Lineage.ArtifactDigest).Int("page", int64(span.Lineage.Page)).Int("offset", int64(span.Lineage.Offset)).Int("length", int64(span.Lineage.Length))
	}
	w.Count("structured", len(result.Structured.Paragraphs))
	for _, span := range result.Structured.Paragraphs {
		w.String("path", span.Path).String("text", span.Text).Int("page", int64(span.Lineage.Page)).Int("offset", int64(span.Lineage.Offset)).Int("length", int64(span.Lineage.Length))
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
