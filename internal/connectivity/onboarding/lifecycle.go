package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// StagingState describes the controlled lifecycle of an onboarding payload.
type StagingState string

const (
	Staged      StagingState = "STAGED"
	Exported    StagingState = "EXPORTED"
	Quarantined StagingState = "QUARANTINED"
	Rejected    StagingState = "REJECTED"
	Destroyed   StagingState = "DESTROYED"
)

// DLPStatus is intentionally closed: no payload may be exported while it is
// uninspected or refused.
type DLPStatus string

const (
	DLPUninspected DLPStatus = "UNINSPECTED"
	DLPCleared     DLPStatus = "CLEARED"
	DLPRefused     DLPStatus = "REFUSED"
)

var (
	ErrLifecycleInvalid   = errors.New("onboarding: invalid staging request")
	ErrLifecycleDenied    = errors.New("onboarding: staging lifecycle denied")
	ErrLifecycleNotFound  = errors.New("onboarding: staging record not found")
	ErrLifecycleConflict  = errors.New("onboarding: staging revision conflict")
	ErrLifecycleHeld      = errors.New("onboarding: staging record is on legal hold")
	ErrLifecycleRetention = errors.New("onboarding: retention has not elapsed")
)

// LineageAnchor is the minimal verifiable lineage retained after payload
// destruction. It contains references and offsets, never source bytes.
type LineageAnchor struct {
	ManifestDigest string
	SourceSnapshot string
	SourceRow      string
	SourceOffset   int64
	TransformRef   string
}

func (a LineageAnchor) validate() bool {
	return validLineageDigest(a.ManifestDigest) && strings.TrimSpace(a.SourceSnapshot) != "" && strings.TrimSpace(a.SourceRow) != "" && a.SourceOffset >= 0 && strings.TrimSpace(a.TransformRef) != ""
}

func (a LineageAnchor) digest() string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", a.ManifestDigest, a.SourceSnapshot, a.SourceRow, a.SourceOffset, a.TransformRef)
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// StagingRequest is a tenant-scoped payload admission request. DLP review is
// a separate operation so a caller cannot claim that an upload was scanned.
type StagingRequest struct {
	TenantID        string
	ArtifactID      string
	Payload         []byte
	Purpose         string
	RetentionUntil  time.Time
	RetentionPolicy string
	Lineage         LineageAnchor
	CreatedAt       time.Time
}

// DLPReview records the redaction-safe inspection decision needed before
// export. Evidence is an id/digest, not matched content.
type DLPReview struct {
	Status     DLPStatus
	EvidenceID string
}

// StagingRecord is a snapshot. Payload is present only until Destroy; all
// identity and lineage fields survive destruction.
type StagingRecord struct {
	TenantID          string
	ArtifactID        string
	Revision          uint64
	State             StagingState
	DLP               DLPStatus
	DLPEvidenceID     string
	Held              bool
	Purpose           string
	RetentionUntil    time.Time
	RetentionPolicy   string
	Payload           []byte
	PayloadDigest     string
	Lineage           LineageAnchor
	LineageDigest     string
	CreatedAt         time.Time
	DestroyedAt       time.Time
	DestructionDigest string
}

// ExportReceipt is the evidence emitted when a staged payload passes DLP and
// hold checks. It never includes payload bytes.
type ExportReceipt struct {
	TenantID      string
	ArtifactID    string
	Revision      uint64
	Purpose       string
	PayloadDigest string
	LineageDigest string
	EvidenceID    string
}

// DestroyRequest makes the two legitimate destruction authorities explicit:
// retention expiry or tenant cleanup. Neither can override a hold.
type DestroyRequest struct {
	TenantID      string
	ArtifactID    string
	Revision      uint64
	At            time.Time
	TenantCleanup bool
}

type stagingEntry struct{ record StagingRecord }

// StagingStore is the pure lifecycle state machine used by staging/export
// adapters. Every mutation is tenant and revision fenced.
type StagingStore struct {
	mu      sync.Mutex
	records map[string]*stagingEntry
	now     func() time.Time
}

// NewStagingStore creates an empty lifecycle store.
func NewStagingStore(now func() time.Time) *StagingStore {
	return &StagingStore{records: make(map[string]*stagingEntry), now: now}
}

func (s *StagingStore) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func stagingKey(tenant, artifact string) string { return tenant + "\x00" + artifact }

// Stage admits a payload into the DLP-pending staging state and computes both
// payload and minimal-lineage digests before retaining it.
func (s *StagingStore) Stage(ctx context.Context, req StagingRequest) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	if s == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ArtifactID) == "" || len(req.Payload) == 0 || strings.TrimSpace(req.Purpose) == "" || req.RetentionUntil.IsZero() || strings.TrimSpace(req.RetentionPolicy) == "" || !req.Lineage.validate() {
		return StagingRecord{}, ErrLifecycleInvalid
	}
	created := req.CreatedAt
	if created.IsZero() {
		created = s.clock()
	}
	record := StagingRecord{TenantID: req.TenantID, ArtifactID: req.ArtifactID, Revision: 1, State: Staged, DLP: DLPUninspected, Purpose: req.Purpose, RetentionUntil: req.RetentionUntil.UTC(), RetentionPolicy: req.RetentionPolicy, Payload: append([]byte(nil), req.Payload...), PayloadDigest: lifecycleDigest(req.Payload), Lineage: req.Lineage, LineageDigest: req.Lineage.digest(), CreatedAt: created.UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[stagingKey(req.TenantID, req.ArtifactID)]; exists {
		return StagingRecord{}, ErrLifecycleConflict
	}
	s.records[stagingKey(req.TenantID, req.ArtifactID)] = &stagingEntry{record: record}
	return cloneStaging(record), nil
}

// ReviewDLP records an inspection verdict; it does not itself export bytes.
func (s *StagingStore) ReviewDLP(ctx context.Context, tenant, artifact string, revision uint64, review DLPReview) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	if s == nil || review.Status != DLPCleared && review.Status != DLPRefused || strings.TrimSpace(review.EvidenceID) == "" {
		return StagingRecord{}, ErrLifecycleInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(tenant, artifact, revision)
	if err != nil {
		return StagingRecord{}, err
	}
	if entry.record.State == Destroyed {
		return StagingRecord{}, ErrLifecycleDenied
	}
	entry.record.DLP = review.Status
	entry.record.DLPEvidenceID = review.EvidenceID
	entry.record.Revision++
	if review.Status == DLPRefused {
		entry.record.State = Rejected
	}
	return cloneStaging(entry.record), nil
}

// Quarantine prevents export while preserving the payload for a later
// governed retention/cleanup decision.
func (s *StagingStore) Quarantine(ctx context.Context, tenant, artifact string, revision uint64) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(tenant, artifact, revision)
	if err != nil {
		return StagingRecord{}, err
	}
	if entry.record.State == Destroyed {
		return StagingRecord{}, ErrLifecycleDenied
	}
	entry.record.State = Quarantined
	entry.record.Revision++
	return cloneStaging(entry.record), nil
}

// SetHold updates the current legal-hold intersection. A held record cannot
// be exported or destroyed, even when retention has elapsed.
func (s *StagingStore) SetHold(ctx context.Context, tenant, artifact string, revision uint64, held bool) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(tenant, artifact, revision)
	if err != nil {
		return StagingRecord{}, err
	}
	if entry.record.State == Destroyed {
		return StagingRecord{}, ErrLifecycleDenied
	}
	entry.record.Held = held
	entry.record.Revision++
	return cloneStaging(entry.record), nil
}

// Export moves only a DLP-cleared, unheld staged record to EXPORTED and
// returns a receipt binding the exported bytes to their lineage.
func (s *StagingStore) Export(ctx context.Context, tenant, artifact string, revision uint64, now time.Time) (StagingRecord, ExportReceipt, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, ExportReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(tenant, artifact, revision)
	if err != nil {
		return StagingRecord{}, ExportReceipt{}, err
	}
	if entry.record.State != Staged || entry.record.DLP != DLPCleared || entry.record.Held || now.IsZero() {
		return StagingRecord{}, ExportReceipt{}, ErrLifecycleDenied
	}
	entry.record.State = Exported
	entry.record.Revision++
	receipt := ExportReceipt{TenantID: tenant, ArtifactID: artifact, Revision: entry.record.Revision, Purpose: entry.record.Purpose, PayloadDigest: entry.record.PayloadDigest, LineageDigest: entry.record.LineageDigest, EvidenceID: entry.record.DLPEvidenceID}
	return cloneStaging(entry.record), receipt, nil
}

// Destroy removes payload bytes only after retention or tenant cleanup has
// authorized it and no hold remains. The returned record retains enough
// digests and lineage references for offline verification.
func (s *StagingStore) Destroy(ctx context.Context, req DestroyRequest) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	if req.At.IsZero() {
		return StagingRecord{}, ErrLifecycleInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(req.TenantID, req.ArtifactID, req.Revision)
	if err != nil {
		return StagingRecord{}, err
	}
	if entry.record.Held {
		return StagingRecord{}, ErrLifecycleHeld
	}
	if entry.record.State == Destroyed {
		return cloneStaging(entry.record), nil
	}
	if !req.TenantCleanup && req.At.Before(entry.record.RetentionUntil) {
		return StagingRecord{}, ErrLifecycleRetention
	}
	if entry.record.State != Staged && entry.record.State != Exported && entry.record.State != Quarantined && entry.record.State != Rejected {
		return StagingRecord{}, ErrLifecycleDenied
	}
	entry.record.Payload = nil
	entry.record.State = Destroyed
	entry.record.DestroyedAt = req.At.UTC()
	entry.record.Revision++
	entry.record.DestructionDigest = lifecycleDigest([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", entry.record.TenantID, entry.record.ArtifactID, entry.record.Revision, entry.record.PayloadDigest, entry.record.LineageDigest)))
	return cloneStaging(entry.record), nil
}

// Get returns a defensive tenant-scoped snapshot.
func (s *StagingStore) Get(ctx context.Context, tenant, artifact string) (StagingRecord, error) {
	if err := ctx.Err(); err != nil {
		return StagingRecord{}, err
	}
	if s == nil {
		return StagingRecord{}, ErrLifecycleInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.records[stagingKey(tenant, artifact)]
	if !ok {
		return StagingRecord{}, ErrLifecycleNotFound
	}
	return cloneStaging(entry.record), nil
}

func (s *StagingStore) entryLocked(tenant, artifact string, revision uint64) (*stagingEntry, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(artifact) == "" || revision == 0 {
		return nil, ErrLifecycleInvalid
	}
	entry, ok := s.records[stagingKey(tenant, artifact)]
	if !ok {
		return nil, ErrLifecycleNotFound
	}
	if entry.record.Revision != revision || entry.record.TenantID != tenant {
		return nil, ErrLifecycleConflict
	}
	return entry, nil
}

func cloneStaging(r StagingRecord) StagingRecord {
	r.Payload = append([]byte(nil), r.Payload...)
	return r
}

func lifecycleDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns bounded lifecycle facts and digests without payload bytes.
func (r StagingRecord) Explain() string {
	return fmt.Sprintf("onboarding staging tenant=%s artifact=%s revision=%d state=%s dlp=%s held=%t payload_digest=%s lineage_digest=%s", r.TenantID, r.ArtifactID, r.Revision, r.State, r.DLP, r.Held, r.PayloadDigest, r.LineageDigest)
}

// ExplainStaging is the package-level Explain-shaped entry point.
func ExplainStaging(r StagingRecord) string { return r.Explain() }
