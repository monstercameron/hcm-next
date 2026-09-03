// Package documentextract provides a bounded, offline document observation
// contract. Extracted values are observations: they are never instructions or
// authoritative business facts.
package documentextract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type State string

const (
	Complete State = "COMPLETE"
	Partial  State = "PARTIAL"
	Failed   State = "FAILED"
)

type Kind string

const (
	Text     Kind = "TEXT"
	OCR      Kind = "OCR"
	Metadata Kind = "METADATA"
)

var (
	ErrInvalidRequest = errors.New("documentextract: invalid request")
	ErrLimit          = errors.New("documentextract: resource limit exceeded")
	ErrLineage        = errors.New("documentextract: invalid source lineage")
)

// Limits are applied before and during extraction. Zero means unlimited except
// MaxBytes, which must be set by a production caller.
type Limits struct {
	MaxBytes, MaxOutputBytes, MaxFields, MaxMetadataBytes uint64
	MaxPages, MaxSpanBytes                                uint32
}

type Source struct {
	ID, Digest, MediaType, Filename, Classification, Taint string
	Bytes                                                  []byte
}

type Request struct {
	Source                        Source
	ParserVersion, ProfileVersion string
	Limits                        Limits
	EnableOCR                     bool
	Metadata                      map[string]string
}

type Span struct {
	Page, Start, End uint32 // page is one-based; offsets are byte offsets.
}

type Field struct {
	Path, Raw, Normalized string
	Kind                  Kind
	Span                  Span
	Confidence            float32
	Classification, Taint string
}

type Result struct {
	State                         State
	SourceID, SourceDigest        string
	DerivativeDigest              string
	ParserVersion, ProfileVersion string
	Fields                        []Field
	BytesExamined, BytesProduced  uint64
	Error                         string
}

// OCRProvider is deliberately narrow: implementations may be backed by a
// local model, but this package performs no network or process execution.
type OCRProvider interface {
	Extract(context.Context, []byte, Limits) ([]Field, error)
}

// Parser extracts an existing text layer. Implementations must return byte
// spans into the supplied source and must not mutate it.
type Parser interface {
	Extract(context.Context, []byte, Limits) ([]Field, error)
}

// PlainTextParser is a deterministic parser for text/plain, CSV, and JSON
// bytes. It is useful as the safe baseline and test double for richer parsers.
type PlainTextParser struct{}

func (PlainTextParser) Extract(ctx context.Context, b []byte, l Limits) ([]Field, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.MaxOutputBytes > 0 && uint64(len(b)) > l.MaxOutputBytes {
		return nil, ErrLimit
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return nil, fmt.Errorf("%w: binary text", ErrInvalidRequest)
	}
	lines := bytes.Split(b, []byte{'\n'})
	out := make([]Field, 0, len(lines))
	start := 0
	for i, line := range lines {
		if l.MaxFields > 0 && uint64(len(out)) >= l.MaxFields {
			return out, ErrLimit
		}
		end := start + len(line)
		value := strings.TrimSpace(string(line))
		if value != "" {
			out = append(out, Field{Path: fmt.Sprintf("text.%d", i+1), Raw: value, Normalized: value, Kind: Text, Span: Span{Page: 1, Start: uint32(start), End: uint32(end)}, Confidence: 1})
		}
		start = end + 1
	}
	return out, nil
}

// Extract performs one bounded extraction observation. Parser and OCR errors
// produce PARTIAL when some fields were recovered, and FAILED otherwise.
func Extract(ctx context.Context, r Request, parser Parser, ocr OCRProvider) (Result, error) {
	res := Result{State: Failed, SourceID: r.Source.ID, SourceDigest: r.Source.Digest, ParserVersion: r.ParserVersion, ProfileVersion: r.ProfileVersion}
	if err := validateRequest(r); err != nil {
		res.Error = err.Error()
		return res, err
	}
	res.BytesExamined = uint64(len(r.Source.Bytes))
	var fields []Field
	var firstErr error
	if parser != nil {
		fields, firstErr = parser.Extract(ctx, r.Source.Bytes, r.Limits)
	} else if isText(r.Source.MediaType) {
		fields, firstErr = (PlainTextParser{}).Extract(ctx, r.Source.Bytes, r.Limits)
	}
	if firstErr != nil && !errors.Is(firstErr, ErrLimit) {
		res.Error = firstErr.Error()
	}
	if r.EnableOCR && ocr != nil {
		o, err := ocr.Extract(ctx, r.Source.Bytes, r.Limits)
		fields = append(fields, o...)
		if err != nil && firstErr == nil {
			firstErr = err
			res.Error = err.Error()
		}
	}
	for k, v := range r.Metadata {
		if r.Limits.MaxFields > 0 && uint64(len(fields)) >= r.Limits.MaxFields {
			firstErr = ErrLimit
			break
		}
		fields = append(fields, Field{Path: "metadata." + k, Raw: v, Normalized: v, Kind: Metadata, Span: Span{Page: 1}, Confidence: 1})
	}
	for i := range fields {
		fields[i].Classification = r.Source.Classification
		fields[i].Taint = r.Source.Taint
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	res.Fields = fields
	res.BytesProduced = uint64(len(canonicalFields(fields)))
	res.DerivativeDigest = digest(canonicalFields(fields))
	if err := ValidateLineage(r, res); err != nil {
		res.State = Failed
		res.Error = err.Error()
		return res, err
	}
	if firstErr != nil {
		res.State = Partial
		if len(fields) == 0 {
			res.State = Failed
		}
		return res, nil
	}
	res.State = Complete
	return res, nil
}

func ValidateLineage(r Request, res Result) error {
	if res.SourceID != r.Source.ID || res.SourceDigest != r.Source.Digest || digest(r.Source.Bytes) != r.Source.Digest {
		return ErrLineage
	}
	if res.ParserVersion == "" || res.ProfileVersion == "" || res.DerivativeDigest == "" {
		return ErrLineage
	}
	for _, f := range res.Fields {
		if f.Classification != r.Source.Classification || f.Taint != r.Source.Taint {
			return ErrLineage
		}
		if f.Span.End < f.Span.Start || (f.Span.End-f.Span.Start) > r.Limits.MaxSpanBytes && r.Limits.MaxSpanBytes > 0 {
			return ErrLineage
		}
		if f.Span.Page == 0 || uint64(f.Span.End) > uint64(len(r.Source.Bytes)) {
			return ErrLineage
		}
	}
	return nil
}

func validateRequest(r Request) error {
	if strings.TrimSpace(r.Source.ID) == "" || len(r.Source.Bytes) == 0 || r.Source.Digest == "" || r.ParserVersion == "" || r.ProfileVersion == "" {
		return ErrInvalidRequest
	}
	if r.Limits.MaxBytes > 0 && uint64(len(r.Source.Bytes)) > r.Limits.MaxBytes {
		return ErrLimit
	}
	if digest(r.Source.Bytes) != r.Source.Digest {
		return ErrLineage
	}
	for k, v := range r.Metadata {
		if strings.TrimSpace(k) == "" || (r.Limits.MaxMetadataBytes > 0 && uint64(len(k)+len(v)) > r.Limits.MaxMetadataBytes) {
			return ErrLimit
		}
	}
	return nil
}

func isText(m string) bool {
	return strings.HasPrefix(strings.ToLower(m), "text/") || strings.EqualFold(m, "application/json") || strings.EqualFold(m, "text/csv") || filepath.Ext(m) == ".txt"
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func canonicalFields(f []Field) []byte {
	var b strings.Builder
	for _, x := range f {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%d:%d:%d\x00%s\x00%s\n", x.Path, x.Raw, x.Normalized, x.Span.Page, x.Span.Start, x.Span.End, x.Classification, x.Taint)
	}
	return []byte(b.String())
}
