package object

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// MetadataVersion identifies the immutable metadata contract.
	MetadataVersion = 1
	// MetadataHardLimit is the object-plane backstop for a single ingest.
	MetadataHardLimit int64 = 64 << 20
)

var (
	ErrInvalidMetadata   = errors.New("object: invalid artifact metadata")
	ErrMetadataTooLarge  = errors.New("object: artifact exceeds metadata size limit")
	ErrMediaTypeMismatch = errors.New("object: declared media type does not match content")
	ErrMetadataConflict  = errors.New("object: caller metadata cannot override server metadata")
)

// MetadataPolicy is server-owned ingest configuration. Classification and
// retention are deliberately policy values, not fields in IngestRequest.
type MetadataPolicy struct {
	MaxBytes          int64
	AllowedMediaTypes []string
	Classification    string
	RetentionPolicy   string
	RequireSourceRef  bool
}

// IngestRequest contains untrusted upload claims and bytes. UserMetadata is
// accepted only so callers cannot accidentally mistake it for authoritative
// metadata; it is never copied into SafeMetadata.
type IngestRequest struct {
	SuggestedFilename string
	DeclaredMediaType string
	Content           []byte
	SourceRef         string
	ParentArtifactID  string
	UserMetadata      map[string]string
}

// LineageRef is an immutable reference to the source and optional parent
// artifact. It contains no provider locator or user-controlled path.
type LineageRef struct {
	SourceRef        string
	ParentArtifactID string
}

// SafeMetadata is the complete server-derived metadata frozen at ingest.
type SafeMetadata struct {
	ArtifactID      string
	Filename        string
	MediaType       string
	Size            int64
	Digest          string
	Classification  string
	RetentionPolicy string
	Lineage         LineageRef
}

// Validate checks the immutable result and ensures the output remains safe
// even when it came from a persistence adapter rather than Normalize.
func (m SafeMetadata) Validate() error {
	if !validDigest(m.Digest) || m.ArtifactID != m.Digest {
		return fmt.Errorf("%w: artifact id and digest must be the same sha256 reference", ErrInvalidMetadata)
	}
	if m.Size <= 0 || m.Size > MetadataHardLimit {
		return fmt.Errorf("%w: size is outside the bounded range", ErrInvalidMetadata)
	}
	if !safeFilename(m.Filename) || strings.TrimSpace(m.MediaType) != m.MediaType || m.MediaType == "" {
		return fmt.Errorf("%w: filename or media type is unsafe", ErrInvalidMetadata)
	}
	if strings.TrimSpace(m.Classification) == "" || strings.TrimSpace(m.RetentionPolicy) == "" {
		return fmt.Errorf("%w: classification and retention policy are required", ErrInvalidMetadata)
	}
	if strings.TrimSpace(m.Lineage.SourceRef) == "" {
		return fmt.Errorf("%w: lineage source is required", ErrInvalidMetadata)
	}
	if m.Lineage.ParentArtifactID != "" && !validDigest(m.Lineage.ParentArtifactID) {
		return fmt.Errorf("%w: parent artifact reference is invalid", ErrInvalidMetadata)
	}
	return nil
}

// Normalize derives bounded, immutable safe metadata from the bytes and
// server policy. Caller filename, classification, retention and arbitrary
// metadata never become authoritative output.
func Normalize(req IngestRequest, policy MetadataPolicy) (SafeMetadata, error) {
	if err := validatePolicy(policy); err != nil {
		return SafeMetadata{}, err
	}
	if len(req.Content) == 0 {
		return SafeMetadata{}, fmt.Errorf("%w: content is required", ErrInvalidMetadata)
	}
	size := int64(len(req.Content))
	if size > policy.MaxBytes || size > MetadataHardLimit {
		return SafeMetadata{}, fmt.Errorf("%w: size=%d limit=%d", ErrMetadataTooLarge, size, minInt64(policy.MaxBytes, MetadataHardLimit))
	}
	if err := validateHint(req); err != nil {
		return SafeMetadata{}, err
	}
	if policy.RequireSourceRef && strings.TrimSpace(req.SourceRef) == "" {
		return SafeMetadata{}, fmt.Errorf("%w: source reference is required", ErrInvalidMetadata)
	}
	if strings.TrimSpace(req.SourceRef) == "" {
		return SafeMetadata{}, fmt.Errorf("%w: source reference is required", ErrInvalidMetadata)
	}
	if req.ParentArtifactID != "" && !validDigest(req.ParentArtifactID) {
		return SafeMetadata{}, fmt.Errorf("%w: parent artifact reference is invalid", ErrInvalidMetadata)
	}

	mediaType := deriveMediaType(req.Content)
	if !allowedMediaType(policy.AllowedMediaTypes, mediaType) {
		return SafeMetadata{}, fmt.Errorf("%w: server-derived type %q is not allowed", ErrInvalidMetadata, mediaType)
	}
	if req.DeclaredMediaType != "" {
		declared, err := canonicalMediaType(req.DeclaredMediaType)
		if err != nil || !sameMediaFamily(declared, mediaType) {
			return SafeMetadata{}, fmt.Errorf("%w: declared=%q detected=%q", ErrMediaTypeMismatch, req.DeclaredMediaType, mediaType)
		}
	}
	digest := contentDigest(req.Content)
	metadata := SafeMetadata{
		ArtifactID:      digest,
		Filename:        derivedFilename(digest, mediaType),
		MediaType:       mediaType,
		Size:            size,
		Digest:          digest,
		Classification:  policy.Classification,
		RetentionPolicy: policy.RetentionPolicy,
		Lineage:         LineageRef{SourceRef: req.SourceRef, ParentArtifactID: req.ParentArtifactID},
	}
	if err := metadata.Validate(); err != nil {
		return SafeMetadata{}, err
	}
	return metadata, nil
}

// NormalizeMetadata is a descriptive alias used by ingest adapters.
func NormalizeMetadata(req IngestRequest, policy MetadataPolicy) (SafeMetadata, error) {
	return Normalize(req, policy)
}

func validatePolicy(policy MetadataPolicy) error {
	if policy.MaxBytes <= 0 || policy.MaxBytes > MetadataHardLimit {
		return fmt.Errorf("%w: policy size must be between 1 and %d", ErrInvalidMetadata, MetadataHardLimit)
	}
	if strings.TrimSpace(policy.Classification) == "" || strings.TrimSpace(policy.RetentionPolicy) == "" {
		return fmt.Errorf("%w: policy classification and retention are required", ErrInvalidMetadata)
	}
	if len(policy.AllowedMediaTypes) == 0 {
		return fmt.Errorf("%w: policy media allowlist is required", ErrInvalidMetadata)
	}
	seen := make(map[string]struct{}, len(policy.AllowedMediaTypes))
	for _, raw := range policy.AllowedMediaTypes {
		canonical, err := canonicalMediaType(raw)
		if err != nil {
			return fmt.Errorf("%w: policy media type %q is invalid", ErrInvalidMetadata, raw)
		}
		if _, ok := seen[canonical]; ok {
			return fmt.Errorf("%w: policy media allowlist contains duplicates", ErrInvalidMetadata)
		}
		seen[canonical] = struct{}{}
	}
	return nil
}

func validateHint(req IngestRequest) error {
	if req.SuggestedFilename != "" {
		if !utf8.ValidString(req.SuggestedFilename) || strings.TrimSpace(req.SuggestedFilename) != req.SuggestedFilename || !safeFilename(req.SuggestedFilename) {
			return fmt.Errorf("%w: filename contains path, control or Unicode boundary abuse", ErrInvalidMetadata)
		}
	}
	for key, value := range req.UserMetadata {
		if !utf8.ValidString(key) || !utf8.ValidString(value) || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "./\\") {
			return fmt.Errorf("%w: user metadata key is unsafe", ErrMetadataConflict)
		}
	}
	return nil
}

func canonicalMediaType(raw string) (string, error) {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil || parsed == "" || strings.ContainsAny(parsed, "\r\n") {
		return "", errOrInvalid(err)
	}
	return strings.ToLower(parsed), nil
}

func deriveMediaType(content []byte) string {
	detected := strings.ToLower(strings.Split(http.DetectContentType(content), ";")[0])
	switch detected {
	case "application/pdf", "image/png", "image/jpeg", "application/zip", "text/plain":
		return detected
	default:
		return "application/octet-stream"
	}
}

func allowedMediaType(allowed []string, detected string) bool {
	for _, raw := range allowed {
		canonical, err := canonicalMediaType(raw)
		if err == nil && sameMediaFamily(canonical, detected) {
			return true
		}
	}
	return false
}

func sameMediaFamily(a, b string) bool {
	if a == b {
		return true
	}
	if b == "text/plain" && (a == "text/csv" || a == "application/json" || a == "application/xml" || a == "text/plain") {
		return true
	}
	if b == "application/zip" && strings.HasSuffix(a, "+zip") {
		return true
	}
	return false
}

func derivedFilename(digest, mediaType string) string {
	extension := "bin"
	switch mediaType {
	case "application/pdf":
		extension = "pdf"
	case "image/png":
		extension = "png"
	case "image/jpeg":
		extension = "jpg"
	case "application/zip":
		extension = "zip"
	case "text/plain":
		extension = "txt"
	}
	return "artifact-" + strings.TrimPrefix(digest, "sha256:")[:16] + "." + extension
}

func safeFilename(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || strings.HasPrefix(name, "~") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return utf8.ValidString(name)
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func errOrInvalid(err error) error {
	if err != nil {
		return err
	}
	return ErrInvalidMetadata
}

// Explain returns metadata categories and sizes but never raw names,
// source references or content.
func (m SafeMetadata) Explain() string {
	return fmt.Sprintf("artifact metadata type=%s size=%d classification=%s retention=%s digest=%s", m.MediaType, m.Size, m.Classification, m.RetentionPolicy, m.Digest)
}

// ExplainMetadata is the package-level explanation entry point.
func ExplainMetadata(m SafeMetadata) string { return m.Explain() }

// SortedAllowedMediaTypes is useful to policy adapters when rendering a
// deterministic policy receipt.
func SortedAllowedMediaTypes(policy MetadataPolicy) []string {
	out := append([]string(nil), policy.AllowedMediaTypes...)
	sort.Strings(out)
	return out
}
