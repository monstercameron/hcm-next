// Package plan prepares the immutable, storage-neutral transaction plan used
// by the Promotion execution boundary. Preparation reads current ledger heads
// through a narrow port; it never appends, reserves, commits, or calls an
// external system.
package plan

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/governance/decision"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const contractVersion = 1

// Version reports the prepared-plan contract version.
func Version() int { return contractVersion }

var (
	ErrInvalidRequest       = errors.New("transaction plan: invalid prepare request")
	ErrInvalidPlan          = errors.New("transaction plan: invalid plan")
	ErrStaleHead            = errors.New("transaction plan: stale head")
	ErrHeadNotFound         = errors.New("transaction plan: stream head not found")
	ErrPlanExpired          = errors.New("transaction plan: plan expired")
	ErrGovernanceRefused    = errors.New("transaction plan: governance decision refuses preparation")
	ErrGovernanceChanged    = errors.New("transaction plan: governance decision digest is not self-consistent")
	ErrMissingPlannedEvent  = errors.New("transaction plan: planned write has no event")
	ErrMissingOutboxEffect  = errors.New("transaction plan: declared effect has no outbox record")
	ErrInvalidTenantBinding = errors.New("transaction plan: tenant binding is invalid")
)

// StaleHeadError is the typed STALE_HEAD refusal. StreamKey is always the
// exact stream whose expected head was not observed, so a caller can replan
// without parsing an error string.
type StaleHeadError struct {
	StreamKey string
	Expected  int64
	Actual    int64
}

func (e StaleHeadError) Code() string { return "STALE_HEAD" }

func (e StaleHeadError) Error() string {
	return fmt.Sprintf("%s: stream %s expected head %d but observed %d", e.Code(), e.StreamKey, e.Expected, e.Actual)
}

func (e StaleHeadError) Unwrap() error { return ErrStaleHead }

// Head is the read-only result returned by a StreamHeadReader.
type Head struct {
	Tenant          values.TenantId
	StreamKey       string
	Sequence        int64
	Digest          string
	DigestAlgorithm string
}

// StreamHead is a compatibility alias for callers that prefer the explicit
// name used by the ledger schema.
type StreamHead = Head

// StreamHeadReader is the preparation port. It is intentionally independent
// of pgx and of any concrete database handle.
type StreamHeadReader interface {
	CurrentHead(context.Context, values.TenantId, string) (Head, error)
}

// HeadReader is the shorter spelling used by the preparation API.
type HeadReader = StreamHeadReader

// LedgerHeadReader adapts the authoritative stream_head table exposed by
// internal/data/ledger to the preparation port. It accepts a dbport.Querier,
// so a pgtest connection and a production adapter use the same path.
type LedgerHeadReader struct {
	querier datalogger.Querier
}

// NewLedgerHeadReader builds a read-only stream-head adapter.
func NewLedgerHeadReader(q datalogger.Querier) LedgerHeadReader {
	return LedgerHeadReader{querier: q}
}

// CurrentHead reads the registered head for one tenant and stream. A missing
// stream is an error: an unregistered stream cannot later accept a ledger
// append, so treating it as an empty stream would prepare a false plan.
func (r LedgerHeadReader) CurrentHead(ctx context.Context, tenant values.TenantId, streamKey string) (Head, error) {
	if r.querier == nil {
		return Head{}, fmt.Errorf("%w: head reader has no query capability", ErrInvalidRequest)
	}
	if err := tenant.Validate(); err != nil {
		return Head{}, fmt.Errorf("%w: %v", ErrInvalidTenantBinding, err)
	}
	tenantID, err := uuid.Parse(string(tenant))
	if err != nil {
		return Head{}, fmt.Errorf("%w: tenant %q is not the ledger UUID representation: %v", ErrInvalidTenantBinding, tenant, err)
	}
	if strings.TrimSpace(streamKey) == "" {
		return Head{}, fmt.Errorf("%w: stream key is empty", ErrInvalidRequest)
	}
	var (
		sequence  int64
		digest    *string
		algorithm *string
	)
	err = r.querier.QueryRow(ctx, `
		SELECT head_sequence, head_digest, head_digest_algorithm
		FROM stream_head
		WHERE tenant_id = $1 AND stream_key = $2`, tenantID, streamKey).Scan(&sequence, &digest, &algorithm)
	if errors.Is(err, dbport.ErrNoRows) {
		return Head{}, fmt.Errorf("%w: tenant %s stream %s", ErrHeadNotFound, tenant, streamKey)
	}
	if err != nil {
		return Head{}, fmt.Errorf("read stream head %s: %w", streamKey, err)
	}
	got := Head{Tenant: tenant, StreamKey: streamKey, Sequence: sequence}
	if digest != nil {
		got.Digest = *digest
	}
	if algorithm != nil {
		got.DigestAlgorithm = *algorithm
	}
	return got, nil
}

// PlannedEvent is one event the commit coordinator will append. Sequence is
// assigned during preparation as the next sequence after the prepared head.
// SchemaRef and Digest are both required because a plan must pin the exact
// event contract and payload identity without carrying payload bytes.
type PlannedEvent struct {
	StreamKey string
	Sequence  int64
	EventType string
	SchemaRef string
	Digest    string
}

// Event is a concise compatibility alias.
type Event = PlannedEvent

// OutboxEffect is one durable post-commit effect declaration. The destination
// is not called by this package; only its exact schema and payload digest are
// recorded in the prepared plan.
type OutboxEffect struct {
	EffectID       string
	DestinationRef string
	SchemaRef      string
	PayloadDigest  string
	IdempotencyKey string
}

// Effect is a concise compatibility alias.
type Effect = OutboxEffect

// PrepareRequest supplies proposal material and the already-computed
// governance decision. The proposal revision itself remains immutable; this
// request only supplies the event/effect projections that domain capabilities
// produced from its planned writes and effects.
type PrepareRequest struct {
	Proposal intent.ProposalRevision

	// GovernanceDecision is preferred. GovernanceDecisionDigest is accepted for
	// composition roots that persist only the decision's digest, but when both
	// are supplied they must agree and the decision must reproduce its digest.
	GovernanceDecision       decision.Decision
	GovernanceDecisionDigest string

	Events         []PlannedEvent
	OutboxEffects  []OutboxEffect
	IdempotencyKey string
	Now            values.Instant
	ExpiresAt      values.Instant
	PlanID         string
}

// Input and PlanInput are compatibility aliases for request-oriented callers.
type Input = PrepareRequest
type PlanInput = PrepareRequest

// StreamPlan captures the exact head each touched stream must still have at
// commit time. ExpectedSequence is the observed current head, not the event
// sequence to be appended.
type StreamPlan struct {
	StreamKey        string
	ExpectedSequence int64
	HeadDigest       string
	DigestAlgorithm  string
}

// Stream is a concise compatibility alias.
type Stream = StreamPlan

// TransactionPlan is an immutable prepared transaction value. Its exported
// slices are defensive copies made by Prepare; VerifyDigest detects any later
// attempted mutation before a consumer can use the plan.
type TransactionPlan struct {
	PlanID                   string
	Tenant                   values.TenantId
	ProposalRevisionID       string
	ProposalDigest           string
	IdempotencyKey           string
	GovernanceDecisionDigest string
	ExpiresAt                values.Instant

	Writes        []intent.PlannedWrite
	Streams       []StreamPlan
	Events        []PlannedEvent
	OutboxEffects []OutboxEffect

	Digest string
}

// Prepare creates a plan against the heads observed through heads. Every
// planned write must be backed by a source baseline and one planned event;
// every declared proposal effect must be represented by one outbox effect.
// No write is issued by this function.
func Prepare(ctx context.Context, heads HeadReader, req PrepareRequest) (TransactionPlan, error) {
	if heads == nil {
		return TransactionPlan{}, fmt.Errorf("%w: head reader is required", ErrInvalidRequest)
	}
	if err := validateRequest(req); err != nil {
		return TransactionPlan{}, err
	}
	governanceDigest, err := governanceDigest(req)
	if err != nil {
		return TransactionPlan{}, err
	}
	if req.GovernanceDecision.State != "" && req.GovernanceDecision.State != decision.Allow {
		return TransactionPlan{}, fmt.Errorf("%w: decision state is %s", ErrGovernanceRefused, req.GovernanceDecision.State)
	}

	baselines, streams, err := normalizedBaselines(req.Proposal)
	if err != nil {
		return TransactionPlan{}, err
	}
	headByStream := make(map[string]Head, len(streams))
	for _, streamKey := range streams {
		head, readErr := heads.CurrentHead(ctx, req.Proposal.Tenant, streamKey)
		if readErr != nil {
			return TransactionPlan{}, readErr
		}
		if head.StreamKey != "" && head.StreamKey != streamKey {
			return TransactionPlan{}, fmt.Errorf("%w: reader returned head for %q while reading %q", ErrInvalidPlan, head.StreamKey, streamKey)
		}
		if head.Sequence < 0 {
			return TransactionPlan{}, fmt.Errorf("%w: stream %s has negative head %d", ErrInvalidPlan, streamKey, head.Sequence)
		}
		if want := baselines[streamKey]; want != head.Sequence {
			return TransactionPlan{}, StaleHeadError{StreamKey: streamKey, Expected: want, Actual: head.Sequence}
		}
		headByStream[streamKey] = head
		baselines[streamKey] = head.Sequence
	}

	events, err := normalizeEvents(req.Events, req.Proposal.Writes, baselines)
	if err != nil {
		return TransactionPlan{}, err
	}
	effects, err := normalizeEffects(req.OutboxEffects, req.Proposal.Effects)
	if err != nil {
		return TransactionPlan{}, err
	}
	streamPlans := make([]StreamPlan, 0, len(streams))
	for _, streamKey := range streams {
		head := headByStream[streamKey]
		streamPlans = append(streamPlans, StreamPlan{StreamKey: streamKey, ExpectedSequence: head.Sequence, HeadDigest: head.Digest, DigestAlgorithm: head.DigestAlgorithm})
	}

	planID := req.PlanID
	if planID == "" {
		planID = derivedPlanID(req.Proposal.Tenant, req.Proposal.ProposalRevisionID, req.IdempotencyKey)
	}
	plan := TransactionPlan{
		PlanID:                   planID,
		Tenant:                   req.Proposal.Tenant,
		ProposalRevisionID:       req.Proposal.ProposalRevisionID,
		ProposalDigest:           req.Proposal.MaterialDigest.Digest,
		IdempotencyKey:           req.IdempotencyKey,
		GovernanceDecisionDigest: governanceDigest,
		ExpiresAt:                req.ExpiresAt,
		Writes:                   cloneWrites(req.Proposal.Writes),
		Streams:                  streamPlans,
		Events:                   cloneEvents(events),
		OutboxEffects:            cloneEffects(effects),
	}
	plan.Digest = plan.computeDigest()
	return plan, nil
}

// PrepareTransactionPlan is the descriptive spelling of Prepare.
func PrepareTransactionPlan(ctx context.Context, heads HeadReader, req PrepareRequest) (TransactionPlan, error) {
	return Prepare(ctx, heads, req)
}

// Preparer binds a head reader for callers that prefer an object seam.
type Preparer struct{ Heads HeadReader }

func (p Preparer) Prepare(ctx context.Context, req PrepareRequest) (TransactionPlan, error) {
	return Prepare(ctx, p.Heads, req)
}

// Verify checks that the plan is self-consistent and that every prepared head
// is still current. It does not check the clock; use VerifyAt when expiry must
// be enforced as well.
func (p TransactionPlan) Verify(ctx context.Context, heads HeadReader) error {
	if err := p.VerifyDigest(); err != nil {
		return err
	}
	if heads == nil {
		return fmt.Errorf("%w: head reader is required", ErrInvalidRequest)
	}
	for _, stream := range p.Streams {
		head, err := heads.CurrentHead(ctx, p.Tenant, stream.StreamKey)
		if err != nil {
			return err
		}
		if head.Sequence != stream.ExpectedSequence {
			return StaleHeadError{StreamKey: stream.StreamKey, Expected: stream.ExpectedSequence, Actual: head.Sequence}
		}
	}
	return nil
}

// VerifyAt additionally enforces the bounded commit expiry at now.
func (p TransactionPlan) VerifyAt(ctx context.Context, heads HeadReader, now values.Instant) error {
	if err := now.Validate(); err != nil {
		return fmt.Errorf("%w: verify time: %v", ErrInvalidRequest, err)
	}
	if now.Compare(p.ExpiresAt) >= 0 {
		return fmt.Errorf("%w: plan %s expired at %s", ErrPlanExpired, p.PlanID, p.ExpiresAt)
	}
	return p.Verify(ctx, heads)
}

// VerifyDigest recomputes the canonical plan digest.
func (p TransactionPlan) VerifyDigest() error {
	got := p.computeDigest()
	if got != p.Digest {
		return fmt.Errorf("%w: plan %s records %q but content hashes to %q", ErrInvalidPlan, p.PlanID, p.Digest, got)
	}
	return nil
}

// CanonicalBytes returns a defensive copy of the digest preimage.
func (p TransactionPlan) CanonicalBytes() []byte {
	e := encoder{}
	e.str("hcmnext.transaction.plan.v1")
	e.str(p.PlanID).str(string(p.Tenant)).str(p.ProposalRevisionID).str(p.ProposalDigest)
	e.str(p.IdempotencyKey).str(p.GovernanceDecisionDigest).instant(p.ExpiresAt)
	e.list(len(p.Writes), func(e *encoder, i int) {
		w := p.Writes[i]
		e.str(w.Subject.Kind).str(w.Subject.SubjectID).str(w.Subject.AuthorityDomain)
		e.raw(w.ResourceKey.Canonical()).str(w.FieldPath).str(w.CurrentCanonicalText).str(w.ProposedCanonicalText)
		e.str(w.SourceAuthorityDecision).raw(w.ExpectedRevision.Canonical())
	})
	e.list(len(p.Streams), func(e *encoder, i int) {
		s := p.Streams[i]
		e.str(s.StreamKey).uvarint(uint64(s.ExpectedSequence)).str(s.HeadDigest).str(s.DigestAlgorithm)
	})
	e.list(len(p.Events), func(e *encoder, i int) {
		x := p.Events[i]
		e.str(x.StreamKey).uvarint(uint64(x.Sequence)).str(x.EventType).str(x.SchemaRef).str(x.Digest)
	})
	e.list(len(p.OutboxEffects), func(e *encoder, i int) {
		x := p.OutboxEffects[i]
		e.str(x.EffectID).str(x.DestinationRef).str(x.SchemaRef).str(x.PayloadDigest).str(x.IdempotencyKey)
	})
	return append([]byte(nil), e.data...)
}

func (p TransactionPlan) computeDigest() string {
	sum := sha256.Sum256(p.CanonicalBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns metadata only; it never includes event payloads or other
// material values that a caller may not be authorized to disclose.
func (p TransactionPlan) Explain() string {
	return fmt.Sprintf("transaction plan id=%s proposal_revision=%s streams=%d events=%d effects=%d expires_at=%s digest=%s", p.PlanID, p.ProposalRevisionID, len(p.Streams), len(p.Events), len(p.OutboxEffects), p.ExpiresAt, p.Digest)
}

// Explain is the package-level Explain-shaped symbol used by policy tooling.
func Explain(p TransactionPlan) string { return p.Explain() }

// VerifyBoundPlan checks the plan digest before handing exactly that digest to
// GOVERN-003's result binding method.
func VerifyBoundPlan(v interface{ VerifyBoundPlan(string) error }, p TransactionPlan) error {
	if v == nil {
		return fmt.Errorf("%w: revalidation result is required", ErrInvalidRequest)
	}
	if err := p.VerifyDigest(); err != nil {
		return err
	}
	return v.VerifyBoundPlan(p.Digest)
}

func validateRequest(req PrepareRequest) error {
	if err := req.Proposal.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: proposal tenant: %v", ErrInvalidRequest, err)
	}
	for _, field := range []struct{ name, value string }{
		{"proposal_revision_id", req.Proposal.ProposalRevisionID},
		{"proposal_digest", req.Proposal.MaterialDigest.Digest},
		{"idempotency_key", req.IdempotencyKey},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, field.name)
		}
	}
	if err := req.Now.Validate(); err != nil {
		return fmt.Errorf("%w: now: %v", ErrInvalidRequest, err)
	}
	if err := req.ExpiresAt.Validate(); err != nil {
		return fmt.Errorf("%w: expires_at: %v", ErrInvalidRequest, err)
	}
	if req.ExpiresAt.Compare(req.Now) <= 0 {
		return fmt.Errorf("%w: expires_at must be after now", ErrInvalidRequest)
	}
	if len(req.Proposal.Writes) == 0 {
		return fmt.Errorf("%w: proposal declares no planned writes", ErrInvalidRequest)
	}
	for i, w := range req.Proposal.Writes {
		if err := w.ResourceKey.Validate(); err != nil {
			return fmt.Errorf("%w: write %d resource: %v", ErrInvalidRequest, i, err)
		}
		if w.ResourceKey.Tenant != req.Proposal.Tenant {
			return fmt.Errorf("%w: write %d belongs to tenant %s, proposal belongs to %s", ErrInvalidTenantBinding, i, w.ResourceKey.Tenant, req.Proposal.Tenant)
		}
		if strings.TrimSpace(w.FieldPath) == "" || strings.TrimSpace(w.SourceAuthorityDecision) == "" || !w.ExpectedRevision.IsSpecified() {
			return fmt.Errorf("%w: write %d lacks field, authority decision, or expected revision", ErrInvalidRequest, i)
		}
	}
	for _, reservation := range req.Proposal.Reservations {
		if !reservation.Expiry.IsSet() || reservation.Expiry.Compare(req.Now) <= 0 {
			return fmt.Errorf("%w: reservation %q is expired at preparation", ErrInvalidRequest, reservation.ReservationID)
		}
	}
	return nil
}

func governanceDigest(req PrepareRequest) (string, error) {
	got := strings.TrimSpace(req.GovernanceDecisionDigest)
	if req.GovernanceDecision.Digest != "" {
		if want := req.GovernanceDecision.CanonicalDigest(); want != req.GovernanceDecision.Digest {
			return "", fmt.Errorf("%w: recorded %q recomputes to %q", ErrGovernanceChanged, req.GovernanceDecision.Digest, want)
		}
		if got != "" && got != req.GovernanceDecision.Digest {
			return "", fmt.Errorf("%w: supplied %q and decision %q differ", ErrGovernanceChanged, got, req.GovernanceDecision.Digest)
		}
		if req.GovernanceDecision.ProposalRevisionDigest != req.Proposal.MaterialDigest.Digest {
			return "", fmt.Errorf("%w: decision is for proposal digest %q, plan proposal is %q", ErrGovernanceChanged, req.GovernanceDecision.ProposalRevisionDigest, req.Proposal.MaterialDigest.Digest)
		}
		return req.GovernanceDecision.Digest, nil
	}
	if got == "" {
		return "", fmt.Errorf("%w: governance decision digest is required", ErrInvalidRequest)
	}
	return got, nil
}

func normalizedBaselines(rev intent.ProposalRevision) (map[string]int64, []string, error) {
	baselines := make(map[string]int64)
	for _, b := range rev.SourceBaselines {
		if b.StreamID == "" {
			return nil, nil, fmt.Errorf("%w: source baseline has no stream", ErrInvalidRequest)
		}
		sequence, ok := b.ExpectedRevision.Sequence()
		if !ok {
			return nil, nil, fmt.Errorf("%w: source baseline %s is not a sequence revision", ErrInvalidRequest, b.StreamID)
		}
		if previous, exists := baselines[b.StreamID]; exists && previous != int64(sequence) {
			return nil, nil, fmt.Errorf("%w: stream %s has conflicting source baselines", ErrInvalidRequest, b.StreamID)
		}
		baselines[b.StreamID] = int64(sequence)
	}
	for _, w := range rev.Writes {
		stream := w.ExpectedRevision.Stream()
		sequence, ok := w.ExpectedRevision.Sequence()
		if !ok || stream == "" {
			return nil, nil, fmt.Errorf("%w: write on %s is not pinned to a sequence revision", ErrInvalidRequest, w.FieldPath)
		}
		if baseline, exists := baselines[stream]; exists && baseline != int64(sequence) {
			return nil, nil, fmt.Errorf("%w: write baseline for %s disagrees with source baseline", ErrInvalidRequest, stream)
		}
		baselines[stream] = int64(sequence)
	}
	streams := make([]string, 0, len(baselines))
	for stream := range baselines {
		streams = append(streams, stream)
	}
	sort.Strings(streams)
	return baselines, streams, nil
}

func normalizeEvents(input []PlannedEvent, writes []intent.PlannedWrite, heads map[string]int64) ([]PlannedEvent, error) {
	if len(input) != len(writes) {
		return nil, fmt.Errorf("%w: got %d event(s) for %d planned write(s)", ErrMissingPlannedEvent, len(input), len(writes))
	}
	writesByStream := make(map[string]int)
	for _, write := range writes {
		writesByStream[write.ExpectedRevision.Stream()]++
	}
	byStream := make(map[string][]PlannedEvent)
	for _, event := range input {
		if event.StreamKey == "" || event.EventType == "" || event.SchemaRef == "" || event.Digest == "" {
			return nil, fmt.Errorf("%w: every event needs stream, type, schema ref, and digest", ErrInvalidRequest)
		}
		if _, ok := heads[event.StreamKey]; !ok {
			return nil, fmt.Errorf("%w: event targets undeclared stream %s", ErrInvalidRequest, event.StreamKey)
		}
		byStream[event.StreamKey] = append(byStream[event.StreamKey], event)
	}
	for stream, count := range writesByStream {
		if len(byStream[stream]) != count {
			return nil, fmt.Errorf("%w: stream %s has %d event(s) for %d planned write(s)", ErrMissingPlannedEvent, stream, len(byStream[stream]), count)
		}
	}
	for stream, events := range byStream {
		sort.Slice(events, func(i, j int) bool {
			return eventKey(events[i]) < eventKey(events[j])
		})
		for i := range events {
			want := heads[stream] + int64(i) + 1
			if events[i].Sequence != 0 && events[i].Sequence != want {
				return nil, StaleHeadError{StreamKey: stream, Expected: events[i].Sequence - int64(i) - 1, Actual: heads[stream]}
			}
			events[i].Sequence = want
		}
		byStream[stream] = events
	}
	streams := make([]string, 0, len(byStream))
	for stream := range byStream {
		streams = append(streams, stream)
	}
	sort.Strings(streams)
	out := make([]PlannedEvent, 0, len(input))
	for _, stream := range streams {
		out = append(out, byStream[stream]...)
	}
	return out, nil
}

func normalizeEffects(input []OutboxEffect, declared []intent.PlannedEffect) ([]OutboxEffect, error) {
	if len(input) != len(declared) {
		return nil, fmt.Errorf("%w: got %d outbox effect(s) for %d declared effect(s)", ErrMissingOutboxEffect, len(input), len(declared))
	}
	want := make(map[string]bool, len(declared))
	for _, effect := range declared {
		if effect.EffectID == "" {
			return nil, fmt.Errorf("%w: declared effect has no id", ErrInvalidRequest)
		}
		want[effect.EffectID] = true
	}
	seen := make(map[string]bool, len(input))
	for i, effect := range input {
		if !want[effect.EffectID] || seen[effect.EffectID] {
			return nil, fmt.Errorf("%w: outbox effect %d does not match exactly one declared effect", ErrMissingOutboxEffect, i)
		}
		if effect.DestinationRef == "" || effect.SchemaRef == "" || effect.PayloadDigest == "" || effect.IdempotencyKey == "" {
			return nil, fmt.Errorf("%w: outbox effect %q is incomplete", ErrInvalidRequest, effect.EffectID)
		}
		seen[effect.EffectID] = true
	}
	out := append([]OutboxEffect(nil), input...)
	sort.Slice(out, func(i, j int) bool { return out[i].EffectID < out[j].EffectID })
	return out, nil
}

func eventKey(e PlannedEvent) string {
	return e.StreamKey + "\x00" + e.EventType + "\x00" + e.SchemaRef + "\x00" + e.Digest
}

func derivedPlanID(tenant values.TenantId, revision, key string) string {
	h := sha256.New()
	writeString(h, string(tenant))
	writeString(h, revision)
	writeString(h, key)
	return "txplan-" + hex.EncodeToString(h.Sum(nil))[:32]
}

func writeString(h interface{ Write([]byte) (int, error) }, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(value))
}

func cloneWrites(in []intent.PlannedWrite) []intent.PlannedWrite {
	out := append([]intent.PlannedWrite(nil), in...)
	for i := range out {
		out[i].ResourceKey.Segments = append([]string(nil), out[i].ResourceKey.Segments...)
	}
	return out
}

func cloneEvents(in []PlannedEvent) []PlannedEvent  { return append([]PlannedEvent(nil), in...) }
func cloneEffects(in []OutboxEffect) []OutboxEffect { return append([]OutboxEffect(nil), in...) }

type encoder struct{ data []byte }

func (e *encoder) uvarint(v uint64) *encoder {
	e.data = binary.AppendUvarint(e.data, v)
	return e
}

func (e *encoder) str(value string) *encoder {
	e.uvarint(uint64(len(value)))
	e.data = append(e.data, value...)
	return e
}

func (e *encoder) raw(value []byte) *encoder {
	e.uvarint(uint64(len(value)))
	e.data = append(e.data, value...)
	return e
}

func (e *encoder) instant(value values.Instant) *encoder {
	if !value.IsSet() {
		return e.uvarint(0)
	}
	seconds, nanos := value.Unix()
	return e.uvarint(1).uvarint(uint64(seconds)).uvarint(uint64(nanos))
}

func (e *encoder) list(length int, write func(*encoder, int)) {
	e.uvarint(uint64(length))
	for i := 0; i < length; i++ {
		sub := encoder{}
		write(&sub, i)
		e.raw(sub.data)
	}
}
