// Package drafts owns the persistence contract for resumable form drafts.
//
// Drafts are encrypted, revisioned and scoped to one tenant and principal. A
// draft is never a submission and the draft API has no effect-producing port.
package drafts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Outcome is the result of attempting to resume a draft.
type Outcome string

const (
	Current        Outcome = "CURRENT"
	RebaseRequired Outcome = "REBASE_REQUIRED"
	Expired        Outcome = "EXPIRED"
	Denied         Outcome = "DENIED"
)

var (
	ErrInvalidInput = errors.New("drafts: invalid input")
	ErrConflict     = errors.New("drafts: revision conflict")
	ErrNotFound     = errors.New("drafts: draft not found")
	ErrExpired      = errors.New("drafts: draft expired")
	ErrDenied       = errors.New("drafts: access denied")
	ErrRebase       = errors.New("drafts: rebase required")
	ErrValidation   = errors.New("drafts: submission validation failed")
)

// EffectCounters intentionally exposes no mutation hooks. It is carried by
// submission results so callers can assert that saving/resuming is effect-free.
type EffectCounters struct {
	WorkflowSignals  int
	DomainEvents     int
	OutboxEntries    int
	ProviderRequests int
}

func (e EffectCounters) IsZero() bool { return e == (EffectCounters{}) }

type Draft struct {
	ID, TenantID, PrincipalID string
	FormID, FormVersion       string
	Revision                  uint64
	ExpiresAt                 time.Time
	AnswerDigest              [32]byte
}

type SaveRequest struct {
	ID, TenantID, PrincipalID string
	FormID, FormVersion       string
	ExpectedRevision          uint64
	Answers                   []byte
	ExpiresAt                 time.Time
}

type ResumeRequest struct {
	ID, TenantID, PrincipalID string
	FormID, FormVersion       string
	Revision                  uint64
}

type ResumeResult struct {
	Outcome Outcome
	Draft   Draft
	Answers []byte
}

type ValidateFunc func(formVersion string, answers []byte) error

type Submission struct {
	ID, DraftID, TenantID, PrincipalID string
	FormID, FormVersion                string
	DraftRevision                      uint64
	Answers                            []byte
	SubmittedAt                        time.Time
}

type SubmitResult struct {
	Submission Submission
	Effects    EffectCounters
}

// Repository is the application-facing draft contract. Implementations must
// preserve tenant/principal fencing, revision CAS and the effect-free draft
// path; Submit is the only operation that can produce an immutable submission.
type Repository interface {
	Save(SaveRequest) (Draft, error)
	Resume(ResumeRequest) (ResumeResult, error)
	Submit(ResumeRequest, ValidateFunc) (SubmitResult, error)
}

type Config struct {
	Key []byte
	Now func() time.Time
}

type Store struct {
	mu      sync.RWMutex
	aead    cipher.AEAD
	now     func() time.Time
	records map[string]record
	seq     uint64
}

type record struct {
	draft  Draft
	sealed []byte
}

func NewStore(cfg Config) (*Store, error) {
	block, err := aes.NewCipher(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("drafts: key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Store{aead: aead, now: now, records: make(map[string]record)}, nil
}

func validateIdentity(tenant, principal, form, version string) error {
	if tenant == "" || principal == "" || form == "" || version == "" {
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Save(req SaveRequest) (Draft, error) {
	if err := validateIdentity(req.TenantID, req.PrincipalID, req.FormID, req.FormVersion); err != nil {
		return Draft{}, err
	}
	if req.ID == "" || len(req.Answers) == 0 || req.ExpiresAt.IsZero() {
		return Draft{}, ErrInvalidInput
	}
	if !req.ExpiresAt.After(s.now()) {
		return Draft{}, ErrExpired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.records[req.ID]; ok {
		if old.draft.TenantID != req.TenantID || old.draft.PrincipalID != req.PrincipalID {
			return Draft{}, ErrDenied
		}
		// A draft is pinned to the form definition it was created from. A
		// caller must rebase rather than silently moving its answers to a
		// different form or version.
		if old.draft.FormID != req.FormID || old.draft.FormVersion != req.FormVersion {
			return Draft{}, ErrRebase
		}
		if old.draft.Revision != req.ExpectedRevision {
			return Draft{}, ErrConflict
		}
	}
	revision := uint64(1)
	if old, ok := s.records[req.ID]; ok {
		revision = old.draft.Revision + 1
	}
	d := Draft{ID: req.ID, TenantID: req.TenantID, PrincipalID: req.PrincipalID, FormID: req.FormID, FormVersion: req.FormVersion, Revision: revision, ExpiresAt: req.ExpiresAt, AnswerDigest: sha256.Sum256(req.Answers)}
	sealed, err := s.seal(req.Answers)
	if err != nil {
		return Draft{}, err
	}
	s.records[req.ID] = record{draft: d, sealed: sealed}
	return d, nil
}

func (s *Store) Resume(req ResumeRequest) (ResumeResult, error) {
	if err := validateIdentity(req.TenantID, req.PrincipalID, req.FormID, req.FormVersion); err != nil || req.ID == "" {
		return ResumeResult{Outcome: Denied}, ErrInvalidInput
	}
	s.mu.RLock()
	rec, ok := s.records[req.ID]
	s.mu.RUnlock()
	if !ok {
		return ResumeResult{Outcome: Denied}, ErrNotFound
	}
	if rec.draft.TenantID != req.TenantID || rec.draft.PrincipalID != req.PrincipalID || rec.draft.FormID != req.FormID {
		return ResumeResult{Outcome: Denied}, ErrDenied
	}
	if !rec.draft.ExpiresAt.After(s.now()) {
		return ResumeResult{Outcome: Expired, Draft: rec.draft}, ErrExpired
	}
	if rec.draft.FormVersion != req.FormVersion || req.Revision != rec.draft.Revision {
		return ResumeResult{Outcome: RebaseRequired, Draft: rec.draft}, ErrRebase
	}
	answers, err := s.open(rec.sealed)
	if err != nil {
		return ResumeResult{Outcome: Denied}, err
	}
	return ResumeResult{Outcome: Current, Draft: rec.draft, Answers: answers}, nil
}

func (s *Store) Submit(req ResumeRequest, validate ValidateFunc) (SubmitResult, error) {
	if validate == nil {
		return SubmitResult{}, ErrInvalidInput
	}
	r, err := s.Resume(req)
	if err != nil {
		return SubmitResult{Effects: EffectCounters{}}, err
	}
	if err := validate(r.Draft.FormVersion, r.Answers); err != nil {
		return SubmitResult{Effects: EffectCounters{}}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	s.mu.Lock()
	// Validation may take time. Re-check the revision before creating the
	// immutable submission so a concurrent save cannot submit stale answers.
	current, ok := s.records[req.ID]
	if !ok {
		s.mu.Unlock()
		return SubmitResult{Effects: EffectCounters{}}, ErrNotFound
	}
	if current.draft.TenantID != req.TenantID || current.draft.PrincipalID != req.PrincipalID || current.draft.FormID != req.FormID {
		s.mu.Unlock()
		return SubmitResult{Effects: EffectCounters{}}, ErrDenied
	}
	if current.draft.Revision != r.Draft.Revision {
		s.mu.Unlock()
		return SubmitResult{Effects: EffectCounters{}}, ErrConflict
	}
	s.seq++
	id := fmt.Sprintf("submission-%d", s.seq)
	s.mu.Unlock()
	return SubmitResult{Submission: Submission{ID: id, DraftID: r.Draft.ID, TenantID: r.Draft.TenantID, PrincipalID: r.Draft.PrincipalID, FormID: r.Draft.FormID, FormVersion: r.Draft.FormVersion, DraftRevision: r.Draft.Revision, Answers: append([]byte(nil), r.Answers...), SubmittedAt: s.now()}, Effects: EffectCounters{}}, nil
}

func (s *Store) seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}
func (s *Store) open(sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrDenied
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

// Encrypted returns a copy of the stored ciphertext for audit/inspection tests.
func (s *Store) Encrypted(id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), rec.sealed...), nil
}
