package attest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/timeauth"
)

// ResponseStatus is the deliberately non-coercive outcome of an attestation.
type ResponseStatus string

const (
	ResponseAccepted ResponseStatus = "ACCEPTED"
	ResponseRefused  ResponseStatus = "REFUSED"
	ResponseUnknown  ResponseStatus = "UNKNOWN"
	StatusAccepted                  = ResponseAccepted
	StatusRefused                   = ResponseRefused
	StatusUnknown                   = ResponseUnknown
	// Descriptive aliases keep callers readable without creating a second enum.
	Accepted = ResponseAccepted
	Refused  = ResponseRefused
	Unknown  = ResponseUnknown
)

// AssertionKind distinguishes a response from a later correction or
// revocation. A correction never edits the assertion it names.
type AssertionKind string

const (
	AssertionResponse   AssertionKind = "RESPONSE"
	AssertionCorrection AssertionKind = "CORRECTION"
	AssertionRevocation AssertionKind = "REVOCATION"
	KindResponse                      = AssertionResponse
	KindCorrection                    = AssertionCorrection
	KindRevocation                    = AssertionRevocation
)

var (
	ErrInvalidResponse       = errors.New("attest: invalid response")
	ErrUntrustedTime         = errors.New("attest: trusted time is required")
	ErrIdempotencyConflict   = errors.New("attest: idempotency key conflicts with an existing response")
	ErrResponseNotFound      = errors.New("attest: response not found")
	ErrResponseAlreadyExists = errors.New("attest: response already exists; append a correction or revocation")
	ErrRequiredAttestation   = errors.New("attest: required attestation is not satisfied")
	ErrEvidencePackage       = errors.New("attest: evidence package is invalid")
	ErrConformance           = errors.New("attest: acknowledgement conformance rejected")
)

// TrustedTime is the only time receipt RecordResponse accepts. The source and
// evidence are retained beside the instant so a replay cannot turn a caller's
// wall-clock value into platform authority.
type TrustedTime struct {
	At          time.Time
	Source      string
	EvidenceID  string
	Health      string
	Uncertainty time.Duration
}

func (t TrustedTime) Validate() error {
	if t.At.IsZero() || strings.TrimSpace(t.Source) == "" || strings.TrimSpace(t.EvidenceID) == "" || strings.TrimSpace(t.Health) == "" {
		return ErrUntrustedTime
	}
	if t.Uncertainty < 0 {
		return fmt.Errorf("%w: uncertainty is negative", ErrUntrustedTime)
	}
	return nil
}

// TrustedClock supplies one already-monitored trusted-time receipt. It is
// called at most once for a new idempotency key.
type TrustedClock interface {
	TrustedNow() (TrustedTime, error)
}

// TrustedClockFunc adapts a function to TrustedClock.
type TrustedClockFunc func() (TrustedTime, error)

func (f TrustedClockFunc) TrustedNow() (TrustedTime, error) { return f() }

// TimeAuthClock adapts the TIME-001 monitor to this package without exposing
// the monitor or its mutable sampling state in a response.
type TimeAuthClock struct{ Monitor *timeauth.Monitor }

func (c TimeAuthClock) TrustedNow() (TrustedTime, error) {
	if c.Monitor == nil {
		return TrustedTime{}, ErrUntrustedTime
	}
	instant, err := c.Monitor.RequireTrusted()
	if err != nil {
		return TrustedTime{}, fmt.Errorf("%w: %v", ErrUntrustedTime, err)
	}
	return TrustedTime{At: instant.At.Time(), Source: instant.Evidence.Source, EvidenceID: evidenceID(instant.Evidence), Health: instant.Evidence.Health.String(), Uncertainty: instant.Evidence.Uncertainty}, nil
}

func evidenceID(e timeauth.Evidence) string {
	sum := sha256.Sum256([]byte(e.Source + "\x00" + e.ObservedAt.String() + "\x00" + e.Health.String()))
	return "ev:time:" + hex.EncodeToString(sum[:])[:32]
}

// ResponseRequest is caller input. RecordedAt, revision and digest are
// intentionally absent; the recorder and persistence adapter allocate them.
type ResponseRequest struct {
	Tenant              values.TenantId
	ResponseID          string
	StatementID         string
	StatementVersion    uint64
	StatementDigest     string
	BindingDigest       string
	Status              ResponseStatus
	Kind                AssertionKind
	Reason              string
	EvidenceReceipt     string
	IdempotencyKey      string
	CorrectsResponseID  string
	Authority           string
	AffectedObligations []string
	TransactionID       string
}

// Response is an immutable response receipt. A refused or unknown status is a
// first-class result and is never interpreted as agreement.
type Response struct {
	Tenant              values.TenantId
	ResponseID          string
	Revision            uint64
	StatementID         string
	StatementVersion    uint64
	StatementDigest     string
	BindingDigest       string
	Status              ResponseStatus
	Kind                AssertionKind
	Reason              string
	EvidenceReceipt     string
	IdempotencyKey      string
	CorrectsResponseID  string
	Authority           string
	AffectedObligations []string
	TransactionID       string
	RecordedAt          TrustedTime
	RequestDigest       string
	Digest              string
	Replayed            bool
}

func (s ResponseStatus) Valid() bool {
	return s == ResponseAccepted || s == ResponseRefused || s == ResponseUnknown
}

func (k AssertionKind) Valid() bool {
	return k == AssertionResponse || k == AssertionCorrection || k == AssertionRevocation
}

func validateRequest(r ResponseRequest) error {
	for name, value := range map[string]string{
		"tenant": r.Tenant.String(), "response_id": r.ResponseID, "statement_id": r.StatementID,
		"statement_digest": r.StatementDigest, "binding_digest": r.BindingDigest,
		"evidence_receipt": r.EvidenceReceipt, "idempotency_key": r.IdempotencyKey,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required", ErrInvalidResponse, name)
		}
	}
	if r.StatementVersion == 0 {
		return fmt.Errorf("%w: statement_version is required", ErrInvalidResponse)
	}
	if !r.Status.Valid() {
		return fmt.Errorf("%w: status is not declared", ErrInvalidResponse)
	}
	if r.Kind == "" {
		r.Kind = AssertionResponse
	}
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: assertion kind is not declared", ErrInvalidResponse)
	}
	if r.Status == ResponseAccepted && r.Kind == AssertionResponse && strings.TrimSpace(r.Reason) != "" {
		return fmt.Errorf("%w: accepted response cannot carry a refusal reason", ErrInvalidResponse)
	}
	if r.Status != ResponseAccepted && strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%w: refused and unknown responses require a reason", ErrInvalidResponse)
	}
	if r.Kind == AssertionResponse && r.CorrectsResponseID != "" {
		return fmt.Errorf("%w: a response cannot name a correction target", ErrInvalidResponse)
	}
	if r.Kind != AssertionResponse {
		if strings.TrimSpace(r.CorrectsResponseID) == "" || strings.TrimSpace(r.Authority) == "" {
			return fmt.Errorf("%w: correction and revocation require target and authority", ErrInvalidResponse)
		}
		if strings.TrimSpace(r.Reason) == "" {
			return fmt.Errorf("%w: correction and revocation require a reason", ErrInvalidResponse)
		}
	}
	seen := make(map[string]struct{}, len(r.AffectedObligations))
	for _, obligation := range r.AffectedObligations {
		if strings.TrimSpace(obligation) == "" || strings.TrimSpace(obligation) != obligation {
			return fmt.Errorf("%w: affected obligation is empty or padded", ErrInvalidResponse)
		}
		if _, ok := seen[obligation]; ok {
			return fmt.Errorf("%w: affected obligations contain a duplicate", ErrInvalidResponse)
		}
		seen[obligation] = struct{}{}
	}
	return nil
}

func requestDigest(r ResponseRequest) string {
	obligations := append([]string(nil), r.AffectedObligations...)
	sort.Strings(obligations)
	parts := []string{r.Tenant.String(), r.ResponseID, r.StatementID, fmt.Sprint(r.StatementVersion), r.StatementDigest, r.BindingDigest, string(r.Status), string(r.Kind), r.Reason, r.EvidenceReceipt, r.IdempotencyKey, r.CorrectsResponseID, r.Authority, strings.Join(obligations, "\x00"), r.TransactionID}
	return digest("attest:response-request:v1", parts...)
}

func responseDigest(r Response) string {
	obligations := append([]string(nil), r.AffectedObligations...)
	sort.Strings(obligations)
	return digest("attest:response:v1", r.Tenant.String(), r.ResponseID, fmt.Sprint(r.Revision), r.StatementID, fmt.Sprint(r.StatementVersion), r.StatementDigest, r.BindingDigest, string(r.Status), string(r.Kind), r.Reason, r.EvidenceReceipt, r.IdempotencyKey, r.CorrectsResponseID, r.Authority, strings.Join(obligations, "\x00"), r.TransactionID, r.RecordedAt.At.UTC().Format(time.RFC3339Nano), r.RecordedAt.Source, r.RecordedAt.EvidenceID, r.RecordedAt.Health, r.RequestDigest)
}

// ValidateResponse checks a response after a persistence adapter has assigned
// its revision and trusted-time receipt.
func ValidateResponse(r Response) error {
	if err := validateRequest(ResponseRequest{Tenant: r.Tenant, ResponseID: r.ResponseID, StatementID: r.StatementID, StatementVersion: r.StatementVersion, StatementDigest: r.StatementDigest, BindingDigest: r.BindingDigest, Status: r.Status, Kind: r.Kind, Reason: r.Reason, EvidenceReceipt: r.EvidenceReceipt, IdempotencyKey: r.IdempotencyKey, CorrectsResponseID: r.CorrectsResponseID, Authority: r.Authority, AffectedObligations: r.AffectedObligations, TransactionID: r.TransactionID}); err != nil {
		return err
	}
	if r.Revision == 0 || r.RequestDigest == "" {
		return fmt.Errorf("%w: revision and request digest are required", ErrInvalidResponse)
	}
	return r.RecordedAt.Validate()
}

// ValidateResponseDraft validates the caller fields before a store assigns a
// revision and digest.
func ValidateResponseDraft(r Response) error {
	return validateRequest(ResponseRequest{Tenant: r.Tenant, ResponseID: r.ResponseID, StatementID: r.StatementID, StatementVersion: r.StatementVersion, StatementDigest: r.StatementDigest, BindingDigest: r.BindingDigest, Status: r.Status, Kind: r.Kind, Reason: r.Reason, EvidenceReceipt: r.EvidenceReceipt, IdempotencyKey: r.IdempotencyKey, CorrectsResponseID: r.CorrectsResponseID, Authority: r.Authority, AffectedObligations: r.AffectedObligations, TransactionID: r.TransactionID})
}

// DigestResponse returns the canonical digest an append-only adapter should
// store after assigning Revision.
func DigestResponse(r Response) string { return responseDigest(r) }

func digest(profile string, parts ...string) string {
	h := sha256.New()
	for _, part := range append([]string{profile}, parts...) {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ResponseStore is the durable append boundary. Implementations must preserve
// every response and make idempotency lookup tenant-scoped and atomic.
type ResponseStore interface {
	AppendResponse(context.Context, Response) (Response, error)
	GetResponse(context.Context, values.TenantId, string, uint64) (Response, error)
	GetResponseByIdempotency(context.Context, values.TenantId, string) (Response, error)
	ListResponseHistory(context.Context, values.TenantId, string) ([]Response, error)
}

// Recorder records one response with one trusted-time read. Exact retries
// return the original immutable receipt without consulting the clock again.
type Recorder struct {
	store ResponseStore
	clock TrustedClock
	mu    sync.Mutex
}

func NewRecorder(store ResponseStore, clock TrustedClock) (*Recorder, error) {
	if store == nil || clock == nil {
		return nil, fmt.Errorf("%w: response store and trusted clock are required", ErrInvalidResponse)
	}
	return &Recorder{store: store, clock: clock}, nil
}

func (r *Recorder) RecordResponse(ctx context.Context, req ResponseRequest) (Response, error) {
	if r == nil || r.store == nil || r.clock == nil {
		return Response{}, fmt.Errorf("%w: recorder is not configured", ErrInvalidResponse)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	if req.Kind == "" {
		req.Kind = AssertionResponse
	}
	if prior, err := r.store.GetResponseByIdempotency(ctx, req.Tenant, req.IdempotencyKey); err == nil {
		if prior.RequestDigest != requestDigest(req) {
			return Response{}, ErrIdempotencyConflict
		}
		prior.Replayed = true
		return prior, nil
	} else if !errors.Is(err, ErrResponseNotFound) {
		return Response{}, err
	}
	if req.Kind == AssertionResponse {
		if history, err := r.store.ListResponseHistory(ctx, req.Tenant, req.ResponseID); err == nil && len(history) > 0 {
			return Response{}, ErrResponseAlreadyExists
		} else if err != nil && !errors.Is(err, ErrResponseNotFound) {
			return Response{}, err
		}
	} else {
		if _, err := r.store.ListResponseHistory(ctx, req.Tenant, req.CorrectsResponseID); err != nil {
			return Response{}, fmt.Errorf("%w: correction target: %v", ErrInvalidResponse, err)
		}
	}
	tm, err := r.clock.TrustedNow()
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrUntrustedTime, err)
	}
	if err := tm.Validate(); err != nil {
		return Response{}, err
	}
	return r.store.AppendResponse(ctx, Response{
		Tenant: req.Tenant, ResponseID: req.ResponseID, StatementID: req.StatementID,
		StatementVersion: req.StatementVersion, StatementDigest: req.StatementDigest,
		BindingDigest: req.BindingDigest, Status: req.Status, Kind: req.Kind,
		Reason: req.Reason, EvidenceReceipt: req.EvidenceReceipt, IdempotencyKey: req.IdempotencyKey,
		CorrectsResponseID: req.CorrectsResponseID, Authority: req.Authority,
		AffectedObligations: append([]string(nil), req.AffectedObligations...), TransactionID: req.TransactionID,
		RecordedAt: tm, RequestDigest: requestDigest(req),
	})
}

// Record is the short spelling used by workflow adapters.
func (r *Recorder) Record(ctx context.Context, req ResponseRequest) (Response, error) {
	return r.RecordResponse(ctx, req)
}

// RecordResponse is the package-level convenience form for one-shot callers.
func RecordResponse(ctx context.Context, store ResponseStore, clock TrustedClock, req ResponseRequest) (Response, error) {
	recorder, err := NewRecorder(store, clock)
	if err != nil {
		return Response{}, err
	}
	return recorder.RecordResponse(ctx, req)
}

// MemoryResponseStore is a concurrency-safe append-only port for pure tests
// and local composition. It deliberately has no persistence authority.
type MemoryResponseStore struct {
	mu    sync.RWMutex
	rows  map[string][]Response
	byKey map[string]Response
}

var _ ResponseStore = (*MemoryResponseStore)(nil)

func NewMemoryResponseStore() *MemoryResponseStore {
	return &MemoryResponseStore{rows: make(map[string][]Response), byKey: make(map[string]Response)}
}

func responseKey(tenant values.TenantId, id string) string { return tenant.String() + "\x00" + id }
func idemKey(tenant values.TenantId, key string) string    { return tenant.String() + "\x00" + key }

func (s *MemoryResponseStore) AppendResponse(ctx context.Context, in Response) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if s == nil {
		return Response{}, ErrInvalidResponse
	}
	if in.Kind == "" {
		in.Kind = AssertionResponse
	}
	if err := ValidateResponseDraft(in); err != nil {
		return Response{}, err
	}
	if !in.Status.Valid() || !in.Kind.Valid() || in.RecordedAt.Validate() != nil || in.RequestDigest == "" {
		return Response{}, ErrInvalidResponse
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.byKey[idemKey(in.Tenant, in.IdempotencyKey)]; ok {
		if old.RequestDigest != in.RequestDigest {
			return Response{}, ErrIdempotencyConflict
		}
		old.Replayed = true
		return old, nil
	}
	key := responseKey(in.Tenant, in.ResponseID)
	in.Revision = uint64(len(s.rows[key]) + 1)
	in.Digest = responseDigest(in)
	in.AffectedObligations = append([]string(nil), in.AffectedObligations...)
	s.rows[key] = append(s.rows[key], in)
	s.byKey[idemKey(in.Tenant, in.IdempotencyKey)] = in
	return in, nil
}

func (s *MemoryResponseStore) GetResponse(ctx context.Context, tenant values.TenantId, id string, revision uint64) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.rows[responseKey(tenant, id)]
	if revision == 0 || revision > uint64(len(rows)) {
		return Response{}, ErrResponseNotFound
	}
	return cloneResponse(rows[revision-1]), nil
}

func (s *MemoryResponseStore) GetResponseByIdempotency(ctx context.Context, tenant values.TenantId, key string) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.byKey[idemKey(tenant, key)]
	if !ok {
		return Response{}, ErrResponseNotFound
	}
	return cloneResponse(row), nil
}

func (s *MemoryResponseStore) ListResponseHistory(ctx context.Context, tenant values.TenantId, id string) ([]Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.rows[responseKey(tenant, id)]
	if len(rows) == 0 {
		return nil, ErrResponseNotFound
	}
	out := make([]Response, len(rows))
	for i := range rows {
		out[i] = cloneResponse(rows[i])
	}
	return out, nil
}

func cloneResponse(in Response) Response {
	in.AffectedObligations = append([]string(nil), in.AffectedObligations...)
	return in
}
