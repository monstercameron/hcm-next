package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// DownloadState is the state a content id must be in before bytes can be
// streamed to a caller.
type DownloadState string

const (
	DownloadReceived    DownloadState = "RECEIVED"
	DownloadScanning    DownloadState = "SCANNING"
	DownloadAvailable   DownloadState = "AVAILABLE"
	DownloadQuarantined DownloadState = "QUARANTINED"
	DownloadDeleted     DownloadState = "DELETED"
)

var (
	ErrDownloadDenied   = errors.New("artifacts: download authorization denied")
	ErrDownloadNotFound = errors.New("artifacts: download artifact not found")
	ErrDownloadRange    = errors.New("artifacts: invalid download range")
	ErrDownloadTampered = errors.New("artifacts: download digest mismatch")
	ErrDownloadInvalid  = errors.New("artifacts: invalid download request")
)

// DownloadRecord is the policy-side representation of an artifact. Bytes are
// held here only for the pure kernel implementation; a production adapter can
// replace the byte slice with an object-store reader without changing Open.
type DownloadRecord struct {
	TenantID        string
	ContentID       string
	Revision        uint64
	Bytes           []byte
	Classification  model.ClassificationLabel
	State           DownloadState
	Held            bool
	AllowedPurposes []string
}

// DownloadGrant is a short-lived, purpose-bound authorization snapshot.
// Open rechecks every field against the current record before returning a
// reader, so a stale grant or guessed locator cannot disclose bytes.
type DownloadGrant struct {
	TenantID               string
	Principal              string
	Purpose                string
	ContentID              string
	Revision               uint64
	AllowedClassifications []model.ClassificationLabel
	ExpiresAt              time.Time
}

// DownloadRequest asks for an inclusive-start/exclusive-end range. A nil End
// streams through EOF.
type DownloadRequest struct {
	Grant DownloadGrant
	Start int64
	End   *int64
}

// DownloadReceipt records the exact bytes granted without retaining payload.
type DownloadReceipt struct {
	TenantID    string
	ContentID   string
	Revision    uint64
	Principal   string
	Purpose     string
	RangeStart  int64
	RangeEnd    int64
	ByteCount   int64
	FullDigest  string
	RangeDigest string
	Digest      string
}

// DownloadCatalog is a concurrency-safe in-memory policy/catalog boundary.
// It is intentionally provider-neutral and is useful as the reference
// behavior for a database/object-store adapter.
type DownloadCatalog struct {
	mu      sync.RWMutex
	records map[string]DownloadRecord
}

// NewDownloadCatalog creates an empty catalog.
func NewDownloadCatalog() *DownloadCatalog {
	return &DownloadCatalog{records: make(map[string]DownloadRecord)}
}

func downloadKey(tenant, contentID string) string { return tenant + "\x00" + contentID }

// Register adds an immutable record. Re-registering the same tenant/content
// id is allowed only when all bytes and policy identity are identical.
func (c *DownloadCatalog) Register(record DownloadRecord) error {
	if c == nil || strings.TrimSpace(record.TenantID) == "" || !ValidContentID(record.ContentID) || len(record.Bytes) == 0 || record.Revision == 0 || !record.Classification.Valid() || record.State == "" {
		return ErrDownloadInvalid
	}
	if record.State != DownloadReceived && record.State != DownloadScanning && record.State != DownloadAvailable && record.State != DownloadQuarantined && record.State != DownloadDeleted {
		return ErrDownloadInvalid
	}
	if MultipartDigest(record.Bytes) != record.ContentID {
		return ErrDownloadTampered
	}
	record.Bytes = append([]byte(nil), record.Bytes...)
	record.AllowedPurposes = append([]string(nil), record.AllowedPurposes...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.records[downloadKey(record.TenantID, record.ContentID)]; ok {
		if old.Revision != record.Revision || old.Classification != record.Classification || old.State != record.State || old.Held != record.Held || !bytes.Equal(old.Bytes, record.Bytes) || !sameStrings(old.AllowedPurposes, record.AllowedPurposes) {
			return ErrDownloadTampered
		}
		return nil
	}
	c.records[downloadKey(record.TenantID, record.ContentID)] = record
	return nil
}

// SetState changes only the policy state and revision fence. Bytes and
// identity remain immutable; deletion is represented as a state, not a
// silent missing row.
func (c *DownloadCatalog) SetState(tenant, contentID string, revision uint64, state DownloadState, held bool) error {
	if c == nil || strings.TrimSpace(tenant) == "" || !ValidContentID(contentID) || revision == 0 {
		return ErrDownloadInvalid
	}
	if state != DownloadReceived && state != DownloadScanning && state != DownloadAvailable && state != DownloadQuarantined && state != DownloadDeleted {
		return ErrDownloadInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.records[downloadKey(tenant, contentID)]
	if !ok {
		return ErrDownloadNotFound
	}
	if record.Revision != revision {
		return ErrDownloadDenied
	}
	record.Revision++
	record.State = state
	record.Held = held
	c.records[downloadKey(tenant, contentID)] = record
	return nil
}

// Open rechecks tenant, purpose, class, revision, lifecycle state and hold
// before returning a streaming reader and a byte/range/digest receipt.
func (c *DownloadCatalog) Open(ctx context.Context, req DownloadRequest, now time.Time) (io.ReadCloser, DownloadReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, DownloadReceipt{}, err
	}
	if c == nil || strings.TrimSpace(req.Grant.TenantID) == "" || strings.TrimSpace(req.Grant.Principal) == "" || strings.TrimSpace(req.Grant.Purpose) == "" || req.Grant.ContentID == "" {
		return nil, DownloadReceipt{}, ErrDownloadInvalid
	}
	if req.Grant.ExpiresAt.IsZero() || !now.Before(req.Grant.ExpiresAt) {
		return nil, DownloadReceipt{}, ErrDownloadDenied
	}
	c.mu.RLock()
	record, ok := c.records[downloadKey(req.Grant.TenantID, req.Grant.ContentID)]
	c.mu.RUnlock()
	if !ok {
		return nil, DownloadReceipt{}, ErrDownloadNotFound
	}
	if req.Grant.ContentID != record.ContentID || req.Grant.Revision != record.Revision || record.State != DownloadAvailable || record.Held || !purposeAllowed(req.Grant.Purpose, record.AllowedPurposes) || !classificationAllowed(req.Grant.AllowedClassifications, record.Classification) {
		return nil, DownloadReceipt{}, ErrDownloadDenied
	}
	if MultipartDigest(record.Bytes) != record.ContentID {
		return nil, DownloadReceipt{}, ErrDownloadTampered
	}
	end := int64(len(record.Bytes))
	if req.End != nil {
		end = *req.End
	}
	if req.Start < 0 || end < req.Start || end > int64(len(record.Bytes)) {
		return nil, DownloadReceipt{}, ErrDownloadRange
	}
	rangeBytes := record.Bytes[req.Start:end]
	receipt := DownloadReceipt{TenantID: record.TenantID, ContentID: record.ContentID, Revision: record.Revision, Principal: req.Grant.Principal, Purpose: req.Grant.Purpose, RangeStart: req.Start, RangeEnd: end, ByteCount: int64(len(rangeBytes)), FullDigest: record.ContentID, RangeDigest: MultipartDigest(rangeBytes)}
	receipt.Digest = digestDownloadReceipt(receipt)
	return io.NopCloser(bytes.NewReader(append([]byte(nil), rangeBytes...))), receipt, nil
}

func purposeAllowed(purpose string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	for _, candidate := range allowed {
		if candidate == purpose {
			return true
		}
	}
	return false
}

func classificationAllowed(allowed []model.ClassificationLabel, classification model.ClassificationLabel) bool {
	for _, candidate := range allowed {
		if candidate == classification {
			return true
		}
	}
	return false
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

func digestDownloadReceipt(r DownloadReceipt) string {
	canonical := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%d\x00%d\x00%d\x00%s\x00%s", r.TenantID, r.ContentID, r.Revision, r.Principal, r.Purpose, r.RangeStart, r.RangeEnd, r.ByteCount, r.FullDigest, r.RangeDigest)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// MultipartDigest computes the plain sha256 hex representation used by the
// data artifact content_id column.
func MultipartDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Explain returns a payload-free receipt summary.
func (r DownloadReceipt) Explain() string {
	return fmt.Sprintf("artifact download tenant=%s content_id=%s revision=%d range=[%d,%d) bytes=%d digest=%s", r.TenantID, r.ContentID, r.Revision, r.RangeStart, r.RangeEnd, r.ByteCount, r.RangeDigest)
}

// ExplainDownload is the package-level Explain-shaped entry point.
func ExplainDownload(r DownloadReceipt) string { return r.Explain() }
