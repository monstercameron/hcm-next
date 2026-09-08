package merit

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const ChangeBasePayIntentType = "hcmnext.rewards.change_base_pay/v1"

var (
	ErrInvalidCompensationIntent = errors.New("merit: invalid compensation child intent")
	ErrIntentConflict            = errors.New("merit: compensation child intent conflicts with existing intent")
)

// CompensationChangeIntent is the bounded child of a finalized merit cycle.
// Money is carried at the cycle's declared scale; no float or string arithmetic
// is involved in constructing the target pay.
type CompensationChangeIntent struct {
	IntentID        string
	IntentType      string
	IntentVersion   uint64
	CycleID         string
	CycleRevision   uint64
	ParticipantID   string
	CurrentBasePay  values.Money
	TargetBasePay   values.Money
	EffectiveAt     values.Instant
	Reason          string
	SourceDigest    string
	CanonicalDigest string
}

func (i CompensationChangeIntent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" || i.IntentType != ChangeBasePayIntentType || i.IntentVersion != 1 ||
		strings.TrimSpace(i.CycleID) == "" || i.CycleRevision == 0 || strings.TrimSpace(i.ParticipantID) == "" ||
		strings.TrimSpace(i.SourceDigest) == "" || strings.TrimSpace(i.Reason) == "" {
		return ErrInvalidCompensationIntent
	}
	if err := i.CurrentBasePay.Validate(); err != nil {
		return fmt.Errorf("%w: current pay: %v", ErrInvalidCompensationIntent, err)
	}
	if err := i.TargetBasePay.Validate(); err != nil {
		return fmt.Errorf("%w: target pay: %v", ErrInvalidCompensationIntent, err)
	}
	if i.CurrentBasePay.Currency() != i.TargetBasePay.Currency() || i.TargetBasePay.Amount().Cmp(i.CurrentBasePay.Amount()) < 0 {
		return fmt.Errorf("%w: target pay must be non-decreasing in the same currency", ErrInvalidCompensationIntent)
	}
	if err := i.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidCompensationIntent, err)
	}
	if i.CanonicalDigest == "" || i.CanonicalDigest != i.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidCompensationIntent)
	}
	return nil
}

func (i CompensationChangeIntent) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.merit.CompensationChangeIntent", schemaVersion).
		String("intent_id", i.IntentID).String("intent_type", i.IntentType).Int("intent_version", int64(i.IntentVersion)).
		String("cycle_id", i.CycleID).Int("cycle_revision", int64(i.CycleRevision)).String("participant_id", i.ParticipantID).
		Value("current_base_pay", i.CurrentBasePay).Value("target_base_pay", i.TargetBasePay).
		Value("effective_at", i.EffectiveAt).String("reason", i.Reason).String("source_digest", i.SourceDigest).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (i CompensationChangeIntent) computedDigest() string { return canonicalbytes.Digest(i.body()) }

func newCompensationChangeIntent(c MeritCycle, r MeritRecommendation) (CompensationChangeIntent, error) {
	amount, err := values.NewMoneyFromDecimal(r.Amount, c.Currency)
	if err != nil {
		return CompensationChangeIntent{}, err
	}
	target, err := r.BasePay.Add(amount)
	if err != nil {
		return CompensationChangeIntent{}, err
	}
	idBody, err := canonicalbytes.New("hcmnext.domains.merit.CompensationChangeIntentID", schemaVersion).
		String("intent_type", ChangeBasePayIntentType).Int("intent_version", 1).String("cycle_id", c.CycleID).
		String("participant_id", r.ParticipantID).Value("current_base_pay", r.BasePay).Value("target_base_pay", target).
		Value("effective_at", c.EffectiveAt).String("reason", "MERIT_CYCLE").String("source_digest", c.CanonicalDigest).Bytes()
	if err != nil {
		return CompensationChangeIntent{}, err
	}
	id := canonicalbytes.Digest(idBody)
	i := CompensationChangeIntent{IntentID: id, IntentType: ChangeBasePayIntentType, IntentVersion: 1, CycleID: c.CycleID, CycleRevision: c.Revision, ParticipantID: r.ParticipantID, CurrentBasePay: r.BasePay, TargetBasePay: target, EffectiveAt: c.EffectiveAt, Reason: "MERIT_CYCLE", SourceDigest: c.CanonicalDigest}
	i.CanonicalDigest = i.computedDigest()
	return i, i.Validate()
}

// CompensationChangeIntents projects one deterministic child per finalized
// recommendation. Calling it again yields byte-identical children.
func (c MeritCycle) CompensationChangeIntents() ([]CompensationChangeIntent, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.State != CycleFinalized {
		return nil, ErrCycleTransition
	}
	out := make([]CompensationChangeIntent, 0, len(c.Recommendations))
	for _, r := range c.Recommendations {
		if r.State == RecommendationRejected {
			continue
		}
		if r.State != RecommendationFinalized {
			return nil, ErrCycleTransition
		}
		if r.EffectRevision != c.Revision {
			continue
		}
		i, err := newCompensationChangeIntent(c, r)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ParticipantID < out[b].ParticipantID })
	return out, nil
}

// Correct creates an append-only successor from a finalized cycle. The
// correction must be finalized again before child intents can be projected.
func (c MeritCycle) Correct(participantID string, adjustment CalibrationAdjustment) (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if c.State != CycleFinalized {
		return MeritCycle{}, ErrCorrectionAfterFinalization
	}
	idx := -1
	for j := range c.Recommendations {
		if c.Recommendations[j].ParticipantID == participantID {
			idx = j
			break
		}
	}
	if idx < 0 {
		return MeritCycle{}, fmt.Errorf("%w: participant has no recommendation", ErrInvalidAdjustment)
	}
	normalized, err := NewCalibrationAdjustment(adjustment)
	if err != nil {
		return MeritCycle{}, err
	}
	if normalized.ParticipantID != participantID || !normalized.From.Equal(c.Recommendations[idx].Amount) {
		return MeritCycle{}, ErrInvalidAdjustment
	}
	member, _ := c.member(participantID)
	if normalized.AdjusterID == member.ManagerID {
		return MeritCycle{}, ErrCalibrationSeparation
	}
	rec := c.Recommendations[idx]
	rec.Adjustments = append(append([]CalibrationAdjustment(nil), rec.Adjustments...), normalized)
	rec.Amount, rec.State, rec.CanonicalDigest = normalized.To, RecommendationAdjusted, ""
	rec.ApprovedBy, rec.ApprovalReceiptID, rec.ApprovedDigest = "", "", ""
	rec.approvalVerified = false
	rec.RejectedBy, rec.RejectionReceiptID, rec.RejectedDigest = "", "", ""
	rec.rejectionVerified = false
	rec.verifiedDecisionDigest = ""
	rec.EffectRevision = 0
	rec, err = NewMeritRecommendation(rec)
	if err != nil {
		return MeritCycle{}, err
	}
	next := c
	next.Revision, next.ParentRevision, next.ParentDigest = c.Revision+1, c.Revision, c.CanonicalDigest
	next.State, next.CanonicalDigest = CycleCalibrating, ""
	next.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	for j := range next.Recommendations {
		if j == idx || next.Recommendations[j].State == RecommendationRejected {
			continue
		}
		next.Recommendations[j].State = RecommendationApproved
		next.Recommendations[j].CanonicalDigest = ""
		next.Recommendations[j], err = NewMeritRecommendation(next.Recommendations[j])
		if err != nil {
			return MeritCycle{}, err
		}
	}
	next.Recommendations[idx] = rec
	return NewMeritCycle(next)
}

// MemoryCompensationIntentStore makes child emission idempotent by intent ID.
type MemoryCompensationIntentStore struct {
	mu    sync.Mutex
	items map[string]CompensationChangeIntent
}

func NewMemoryCompensationIntentStore() *MemoryCompensationIntentStore {
	return &MemoryCompensationIntentStore{items: make(map[string]CompensationChangeIntent)}
}
func (s *MemoryCompensationIntentStore) Emit(c MeritCycle) ([]CompensationChangeIntent, error) {
	children, err := c.CompensationChangeIntents()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, child := range children {
		if old, ok := s.items[child.IntentID]; ok && old.CanonicalDigest != child.CanonicalDigest {
			return nil, ErrIntentConflict
		}
	}
	fresh := make([]CompensationChangeIntent, 0, len(children))
	for _, child := range children {
		if _, ok := s.items[child.IntentID]; ok {
			continue
		}
		s.items[child.IntentID] = child
		fresh = append(fresh, child)
	}
	return fresh, nil
}
