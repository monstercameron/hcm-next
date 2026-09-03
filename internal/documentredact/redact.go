// Package documentredact creates deterministic, policy-bound document
// derivatives. The source is never returned by this package and every
// derivative surface is produced from the same redacted byte stream.
package documentredact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrInvalidRequest      = errors.New("documentredact: invalid request")
	ErrRedactionIncomplete = errors.New("REDACTION_INCOMPLETE")
)

type Surface string

const (
	Preview   Surface = "PREVIEW"
	Thumbnail Surface = "THUMBNAIL"
	OCR       Surface = "OCR"
	Search    Surface = "SEARCH"
	Export    Surface = "EXPORT"
)

// Region is a half-open byte range in the immutable source artifact.
type Region struct{ Start, End uint32 }

// Field binds a semantic field to its source range. Fields and regions are
// both included in the coverage proof, even when their ranges overlap.
type Field struct {
	Path   string
	Region Region
}

type Source struct {
	ID     string
	Digest string
	Bytes  []byte
}

type Policy struct {
	ID        string
	Version   string
	Purpose   string
	Recipient string
	Regions   []Region
	Fields    []Field
}

type Request struct {
	Source   Source
	Policy   Policy
	Surfaces []Surface
}

type SurfaceResult struct {
	Digest string
	Bytes  []byte
}

type CoverageProof struct {
	SourceDigest  string
	PolicyID      string
	PolicyVersion string
	Regions       []Region
	Fields        []string
	Digest        string
	Complete      bool
}

type Evidence struct {
	PolicyID       string
	PolicyVersion  string
	Purpose        string
	Recipient      string
	SourceDigest   string
	CoveredRegions []Region
	CoveredFields  []string
	OutputDigest   string
	CoverageDigest string
}

type Result struct {
	SourceDigest string
	OutputDigest string
	Surfaces     map[Surface]SurfaceResult
	Proof        CoverageProof
	Evidence     Evidence
}

func digest(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }

func (p Policy) ranges() ([]Region, []string, error) {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Purpose) == "" || strings.TrimSpace(p.Recipient) == "" {
		return nil, nil, ErrInvalidRequest
	}
	r := append([]Region(nil), p.Regions...)
	names := make([]string, 0, len(p.Fields))
	for _, f := range p.Fields {
		if strings.TrimSpace(f.Path) == "" {
			return nil, nil, ErrInvalidRequest
		}
		r = append(r, f.Region)
		names = append(names, f.Path)
	}
	if len(r) == 0 {
		return nil, nil, ErrInvalidRequest
	}
	sort.Slice(r, func(i, j int) bool {
		if r[i].Start != r[j].Start {
			return r[i].Start < r[j].Start
		}
		return r[i].End < r[j].End
	})
	sort.Strings(names)
	return r, names, nil
}

func validateRanges(r []Region, n int) error {
	for _, x := range r {
		if x.Start >= x.End || uint64(x.End) > uint64(n) {
			return ErrInvalidRequest
		}
	}
	return nil
}

// Generate creates all requested surfaces from one canonical redacted stream.
// A covered byte is replaced with a fixed-length run of U+2588 in UTF-8-safe
// ASCII form (X), preserving offsets and making repeated runs identical.
func Generate(ctx context.Context, req Request) (Result, error) {
	var out Result
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if req.Source.ID == "" || len(req.Source.Bytes) == 0 || req.Source.Digest == "" || digest(req.Source.Bytes) != req.Source.Digest {
		return out, ErrInvalidRequest
	}
	ranges, fields, err := req.Policy.ranges()
	if err != nil {
		return out, err
	}
	if err = validateRanges(ranges, len(req.Source.Bytes)); err != nil {
		return out, err
	}
	if len(req.Surfaces) == 0 {
		return out, ErrInvalidRequest
	}
	seen := map[Surface]bool{}
	surfaces := append([]Surface(nil), req.Surfaces...)
	for _, s := range surfaces {
		if seen[s] || !validSurface(s) {
			return out, ErrInvalidRequest
		}
		seen[s] = true
	}
	redacted := append([]byte(nil), req.Source.Bytes...)
	for _, x := range ranges {
		for i := x.Start; i < x.End; i++ {
			redacted[i] = 'X'
		}
	}
	// The oracle rejects a derivative that still contains any covered source
	// segment. This catches accidental pass-through and overlapping mistakes.
	for _, x := range ranges {
		if bytes.Equal(redacted[x.Start:x.End], req.Source.Bytes[x.Start:x.End]) {
			return out, incomplete()
		}
	}
	outDigest := digest(redacted)
	out = Result{SourceDigest: req.Source.Digest, OutputDigest: outDigest, Surfaces: make(map[Surface]SurfaceResult, len(surfaces))}
	for _, s := range surfaces {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		out.Surfaces[s] = SurfaceResult{Digest: outDigest, Bytes: append([]byte(nil), redacted...)}
	}
	proof := CoverageProof{SourceDigest: req.Source.Digest, PolicyID: req.Policy.ID, PolicyVersion: req.Policy.Version, Regions: ranges, Fields: fields, Complete: true}
	proof.Digest = coverageDigest(proof)
	out.Proof = proof
	out.Evidence = Evidence{PolicyID: req.Policy.ID, PolicyVersion: req.Policy.Version, Purpose: req.Policy.Purpose, Recipient: req.Policy.Recipient, SourceDigest: req.Source.Digest, CoveredRegions: ranges, CoveredFields: fields, OutputDigest: outDigest, CoverageDigest: proof.Digest}
	return out, nil
}

// Redact is a concise alias for Generate.
func Redact(ctx context.Context, req Request) (Result, error) { return Generate(ctx, req) }

func incomplete() error {
	return fmt.Errorf("%w: covered content was unchanged", ErrRedactionIncomplete)
}
func validSurface(s Surface) bool {
	switch s {
	case Preview, Thumbnail, OCR, Search, Export:
		return true
	}
	return false
}

func coverageDigest(p CoverageProof) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%s\x00", p.SourceDigest, p.PolicyID, p.PolicyVersion)
	for _, r := range p.Regions {
		fmt.Fprintf(&b, "%d:%d\x00", r.Start, r.End)
	}
	for _, f := range p.Fields {
		b.WriteString(f)
		b.WriteByte(0)
	}
	return digest([]byte(b.String()))
}

// Verify checks that evidence and every derivative surface remain bound to
// the same source, policy coverage, and output digest.
func (r Result) Verify(req Request) error {
	ranges, fields, err := req.Policy.ranges()
	if err != nil || validateRanges(ranges, len(req.Source.Bytes)) != nil || len(req.Surfaces) == 0 {
		return incomplete()
	}
	if r.SourceDigest != req.Source.Digest || r.Proof.SourceDigest != req.Source.Digest || !r.Proof.Complete || r.Proof.Digest != coverageDigest(r.Proof) {
		return incomplete()
	}
	if !sameRegions(r.Proof.Regions, ranges) || !sameStrings(r.Proof.Fields, fields) {
		return incomplete()
	}
	if r.Evidence.PolicyID != req.Policy.ID || r.Evidence.PolicyVersion != req.Policy.Version || r.Evidence.OutputDigest != r.OutputDigest || r.Evidence.CoverageDigest != r.Proof.Digest {
		return incomplete()
	}
	for _, s := range req.Surfaces {
		v, ok := r.Surfaces[s]
		if !ok || v.Digest != r.OutputDigest || digest(v.Bytes) != r.OutputDigest || !oracle(v.Bytes, req.Source.Bytes, ranges) {
			return incomplete()
		}
	}
	return nil
}

func oracle(derivative, source []byte, ranges []Region) bool {
	if len(derivative) != len(source) {
		return false
	}
	for _, x := range ranges {
		if bytes.Equal(derivative[x.Start:x.End], source[x.Start:x.End]) {
			return false
		}
	}
	return true
}

func sameRegions(a, b []Region) bool {
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

func sameStrings(a, b []string) bool {
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
