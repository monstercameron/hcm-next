package legal

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Rule-pack and registry errors. All are matchable with errors.Is.
var (
	ErrRulePackID           = errors.New("legal: rule pack id is required")
	ErrRulePackVersion      = errors.New("legal: rule pack version must be positive")
	ErrRulePackJurisdiction = errors.New("legal: rule pack jurisdiction must resolve to at least country and state")
	ErrRulePackWindow       = errors.New("legal: rule pack effective window is invalid")
	ErrRulePackDuplicate    = errors.New("legal: rule pack id and version is already registered for this jurisdiction")
	ErrRuleCoverageUnknown  = errors.New("legal: RULE_COVERAGE_UNKNOWN")
)

// EffectiveWindow is a half-open [Start, End) calendar-date window: Start is
// inclusive, End is exclusive, and no End means open-ended. It is a
// deliberately small statute-effective-date type: unlike
// values.EffectiveInterval it does not require a governing business
// calendar, because a statute's effective date is not a working-day concept.
type EffectiveWindow struct {
	Start  values.LocalDate
	End    values.LocalDate
	HasEnd bool
}

// NewOpenEffectiveWindow builds an open-ended window starting at start.
func NewOpenEffectiveWindow(start values.LocalDate) (EffectiveWindow, error) {
	if err := start.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	return EffectiveWindow{Start: start}, nil
}

// NewClosedEffectiveWindow builds a closed [start, end) window.
func NewClosedEffectiveWindow(start, end values.LocalDate) (EffectiveWindow, error) {
	if err := start.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if err := end.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if start.Compare(end) >= 0 {
		return EffectiveWindow{}, fmt.Errorf("%w: [%s,%s) is empty or inverted", ErrRulePackWindow, start, end)
	}
	return EffectiveWindow{Start: start, End: end, HasEnd: true}, nil
}

// Validate reports whether the window is well formed.
func (w EffectiveWindow) Validate() error {
	if err := w.Start.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if w.HasEnd {
		if err := w.End.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrRulePackWindow, err)
		}
		if w.Start.Compare(w.End) >= 0 {
			return fmt.Errorf("%w: [%s,%s) is empty or inverted", ErrRulePackWindow, w.Start, w.End)
		}
	}
	return nil
}

// Contains reports whether d falls in the half-open window.
func (w EffectiveWindow) Contains(d values.LocalDate) bool {
	if w.Validate() != nil || d.Validate() != nil {
		return false
	}
	if d.Compare(w.Start) < 0 {
		return false
	}
	if w.HasEnd && d.Compare(w.End) >= 0 {
		return false
	}
	return true
}

// String returns "[start,end)" or "[start,)" for an open end.
func (w EffectiveWindow) String() string {
	end := ""
	if w.HasEnd {
		end = w.End.String()
	}
	return "[" + w.Start.String() + "," + end + ")"
}

// RulePack is one versioned, jurisdiction-scoped, effective-dated bundle of
// typed obligations for a promotion-and-base-pay-change transaction. It is a
// fixture skeleton: [CaliforniaPromotionPack] and [NewYorkPromotionPack] seed
// it from drafted, unreviewed state research, and every rule inside carries
// its own [Citation] back to that research.
type RulePack struct {
	PackID       string
	Version      uint32
	Jurisdiction Jurisdiction
	Window       EffectiveWindow

	Notices                 []NoticeObligation
	FieldRestrictions       []FieldRestriction
	RetentionRules          []RetentionRule
	LeaveInteractions       []LeaveInteraction
	PayFrequencyConstraints []PayFrequencyConstraint
	FinalPayDeadlines       []FinalPayDeadline
	PayTransparencyDuties   []PayTransparencyDuty
	NonCompeteThresholds    []NonCompeteThreshold
	EVerifyChecks           []EVerifyStatusCheck
	MiniWARNTriggers        []MiniWARNTrigger
}

// Validate reports whether the pack and every obligation inside it are well
// formed and cited. It does not check counsel review status beyond requiring
// one be declared; see [ReviewStatus].
func (p RulePack) Validate() error {
	if p.PackID == "" {
		return ErrRulePackID
	}
	if p.Version == 0 {
		return ErrRulePackVersion
	}
	if !p.Jurisdiction.IsStateResolved() {
		return ErrRulePackJurisdiction
	}
	if err := p.Window.Validate(); err != nil {
		return err
	}
	for _, o := range p.Notices {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.FieldRestrictions {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.RetentionRules {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.LeaveInteractions {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.PayFrequencyConstraints {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.FinalPayDeadlines {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.PayTransparencyDuties {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.NonCompeteThresholds {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.EVerifyChecks {
		if err := o.validate(); err != nil {
			return err
		}
	}
	for _, o := range p.MiniWARNTriggers {
		if err := o.validate(); err != nil {
			return err
		}
	}
	return nil
}

// packKey identifies one rule pack's registration slot.
type packKey struct {
	Jurisdiction Jurisdiction
	PackID       string
	Version      uint32
}

// Registry publishes [RulePack] versions and answers, for a jurisdiction and
// effective date, which release governs. Registration is immutable: the same
// (jurisdiction, pack id, version) can never be registered twice, so a
// [LegalContext] that pinned a release keeps resolving to the same content
// forever.
type Registry struct {
	mu    sync.RWMutex
	packs map[packKey]*RulePack
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{packs: map[packKey]*RulePack{}}
}

// Register validates and publishes pack. It copies pack's obligation slices
// so a caller's later mutation of its own pack value can never reach the
// registry.
func (r *Registry) Register(pack RulePack) error {
	if err := pack.Validate(); err != nil {
		return err
	}
	stored := pack
	stored.Notices = append([]NoticeObligation(nil), pack.Notices...)
	stored.FieldRestrictions = append([]FieldRestriction(nil), pack.FieldRestrictions...)
	stored.RetentionRules = append([]RetentionRule(nil), pack.RetentionRules...)
	stored.LeaveInteractions = append([]LeaveInteraction(nil), pack.LeaveInteractions...)
	stored.PayFrequencyConstraints = append([]PayFrequencyConstraint(nil), pack.PayFrequencyConstraints...)
	stored.FinalPayDeadlines = append([]FinalPayDeadline(nil), pack.FinalPayDeadlines...)
	stored.PayTransparencyDuties = append([]PayTransparencyDuty(nil), pack.PayTransparencyDuties...)
	stored.NonCompeteThresholds = append([]NonCompeteThreshold(nil), pack.NonCompeteThresholds...)
	stored.EVerifyChecks = append([]EVerifyStatusCheck(nil), pack.EVerifyChecks...)
	stored.MiniWARNTriggers = append([]MiniWARNTrigger(nil), pack.MiniWARNTriggers...)

	key := packKey{Jurisdiction: pack.Jurisdiction, PackID: pack.PackID, Version: pack.Version}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.packs[key]; exists {
		return fmt.Errorf("%w: %s %s v%d", ErrRulePackDuplicate, pack.Jurisdiction, pack.PackID, pack.Version)
	}
	r.packs[key] = &stored
	return nil
}

// Lookup returns the highest-versioned registered pack whose jurisdiction
// matches j (falling back from an exact country/state/locality match to a
// state-level pack when j names a locality no pack targets specifically) and
// whose effective window contains date. It returns [ErrRuleCoverageUnknown]
// when nothing matches: an unregistered jurisdiction is never treated as
// "no obligations apply".
func (r *Registry) Lookup(j Jurisdiction, date values.LocalDate) (*RulePack, error) {
	if err := date.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuleCoverageUnknown, err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidateKeys := []Jurisdiction{j}
	if j.Locality != "" {
		candidateKeys = append(candidateKeys, Jurisdiction{Country: j.Country, State: j.State})
	}

	var best *RulePack
	for _, key := range candidateKeys {
		for k, pack := range r.packs {
			if k.Jurisdiction != key {
				continue
			}
			if !pack.Window.Contains(date) {
				continue
			}
			if best == nil || pack.Version > best.Version {
				best = pack
			}
		}
		if best != nil {
			break
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no rule pack governs %s as of %s", ErrRuleCoverageUnknown, j, date)
	}
	// Return a defensive copy so a caller can never mutate registry state
	// through the returned pointer.
	out := *best
	return &out, nil
}

// GetExact returns the exact (jurisdiction, pack id, version) release, or
// [ErrRuleCoverageUnknown] if it was never registered or has since been
// removed from this registry instance. [Evaluate] uses this to re-fetch the
// precise release a [LegalContext] pinned, rather than re-resolving "latest".
func (r *Registry) GetExact(release RulePackRelease) (*RulePack, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pack, ok := r.packs[packKey{Jurisdiction: release.Jurisdiction, PackID: release.PackID, Version: release.Version}]
	if !ok {
		return nil, fmt.Errorf("%w: %s %s v%d is not registered", ErrRuleCoverageUnknown, release.Jurisdiction, release.PackID, release.Version)
	}
	out := *pack
	return &out, nil
}

// PromotionProposalSnapshot is the minimal, evaluation-time snapshot of a
// promotion-and-base-pay-change proposal that [Evaluate] needs. It is a
// fixture input shape: the real proposal snapshot type belongs to the
// business-intent kernel (see planning/specs/business-intent-and-change-
// request.md), which this package does not own or import.
type PromotionProposalSnapshot struct {
	WorkerID      string
	LegalEntityID string
	// EffectiveDate is the proposed pay-change effective date.
	EffectiveDate values.LocalDate
	// CurrentBasePay and NewBasePay are compared to decide whether a
	// pay-rate-change notice is triggered. Either may be the zero Money when
	// the caller does not have both figures; Evaluate then treats the pay
	// change as indeterminate and does not raise a notice obligation on that
	// basis alone.
	CurrentBasePay values.Money
	NewBasePay     values.Money
	// PayFrequency is the worker's current pay frequency, e.g. "SEMIMONTHLY".
	PayFrequency string
	// IsInternalPromotion marks the transaction as an internal promotion
	// rather than an external hire into the role.
	IsInternalPromotion bool
	// CollectsSalaryHistory marks that the compensation-setting process for
	// this transaction asked for or relied on the worker's salary history.
	CollectsSalaryHistory bool
	// OnProtectedLeave marks that the worker is currently on a protected
	// leave of absence.
	OnProtectedLeave bool
	// HasExistingNonCompete marks that the worker is subject to an existing
	// non-compete or non-solicit agreement.
	HasExistingNonCompete bool
	// IsNewHire marks that this transaction is a new hire rather than a
	// change to an existing worker. Always false for a promotion; carried so
	// [EVerifyStatusCheck] obligations have a fact to key on.
	IsNewHire bool
	// SeparationConcurrent marks that this transaction concurrently
	// separates the worker. Always false for a pure promotion; carried so
	// [FinalPayDeadline] obligations have a fact to key on.
	SeparationConcurrent bool
	// WorkforceReductionCount is the number of workers affected by a
	// concurrent workforce reduction this transaction is part of, if any.
	// Zero for an ordinary promotion.
	WorkforceReductionCount int
}

// payRateChanged reports whether the snapshot demonstrates a changed base pay
// rate. It returns false, not an error, when either amount is unset or they
// are in different currencies: an indeterminate pay comparison never manufactures
// a notice obligation on its own.
func (p PromotionProposalSnapshot) payRateChanged() bool {
	if p.CurrentBasePay.Validate() != nil || p.NewBasePay.Validate() != nil {
		return false
	}
	cmp, err := p.NewBasePay.Cmp(p.CurrentBasePay)
	if err != nil {
		return false
	}
	return cmp != 0
}

// LegalEvaluationStatus is the outcome of evaluating a [LegalContext] against
// a proposal, mirroring the vocabulary in
// planning/data/models/kernel-governance-and-evidence.md. This package
// implements the subset [Evaluate] can actually produce at P1B reduced depth;
// the remaining values from that vocabulary are declared for forward
// compatibility with the Phase 2 composition engine and are never returned
// here.
type LegalEvaluationStatus uint8

// Legal evaluation statuses.
const (
	LegalEvaluationStatusUnspecified LegalEvaluationStatus = iota
	// LegalEvaluationStatusResolvedAllow means the pinned rule-pack release
	// produced zero applicable obligations for this proposal.
	LegalEvaluationStatusResolvedAllow
	// LegalEvaluationStatusAllowWithObligations means the transaction may
	// proceed but one or more obligations are attached and must be bound.
	LegalEvaluationStatusAllowWithObligations
	// LegalEvaluationStatusRuleCoverageUnknown means a pinned release could
	// not be re-fetched from the registry at evaluation time.
	LegalEvaluationStatusRuleCoverageUnknown
)

var legalEvaluationStatusWire = map[LegalEvaluationStatus]string{
	LegalEvaluationStatusResolvedAllow:        "RESOLVED_ALLOW",
	LegalEvaluationStatusAllowWithObligations: "ALLOW_WITH_OBLIGATIONS",
	LegalEvaluationStatusRuleCoverageUnknown:  "RULE_COVERAGE_UNKNOWN",
}

// String returns the stable wire token.
func (s LegalEvaluationStatus) String() string {
	if w, ok := legalEvaluationStatusWire[s]; ok {
		return w
	}
	return "LEGAL_EVALUATION_STATUS_UNSPECIFIED"
}

// EvaluationResult is what [Evaluate] returns: the overall status and every
// obligation found applicable, sorted deterministically by type and then id.
type EvaluationResult struct {
	Status           LegalEvaluationStatus
	Jurisdiction     Jurisdiction
	RulePackReleases []RulePackRelease
	Obligations      []AppliedObligation
}

// Evaluate re-fetches every rule-pack release ctx pinned and returns the
// obligations that apply to proposal under it. It fails closed: an unverified
// context, or a pinned release the registry can no longer produce, is
// reported rather than silently evaluated against nothing.
func Evaluate(ctx *LegalContext, proposal PromotionProposalSnapshot, registry *Registry) (EvaluationResult, error) {
	if ctx == nil {
		return EvaluationResult{}, errors.New("legal: Evaluate needs a resolved LegalContext")
	}
	if registry == nil {
		return EvaluationResult{}, errors.New("legal: Evaluate needs a rule-pack registry")
	}
	if err := ctx.Verify(); err != nil {
		return EvaluationResult{}, fmt.Errorf("legal: context failed signature verification: %w", err)
	}

	releases := ctx.RulePackReleases()
	if len(releases) == 0 {
		return EvaluationResult{}, errors.New("legal: context carries no rule-pack releases")
	}

	var obligations []AppliedObligation
	for _, release := range releases {
		pack, err := registry.GetExact(release)
		if err != nil {
			return EvaluationResult{
				Status:           LegalEvaluationStatusRuleCoverageUnknown,
				Jurisdiction:     ctx.Jurisdiction(),
				RulePackReleases: releases,
			}, nil
		}
		obligations = append(obligations, applicableObligations(*pack, proposal)...)
	}

	sort.Slice(obligations, func(i, j int) bool {
		if obligations[i].Type != obligations[j].Type {
			return obligations[i].Type < obligations[j].Type
		}
		return obligations[i].ID < obligations[j].ID
	})

	status := LegalEvaluationStatusResolvedAllow
	if len(obligations) > 0 {
		status = LegalEvaluationStatusAllowWithObligations
	}
	return EvaluationResult{
		Status:           status,
		Jurisdiction:     ctx.Jurisdiction(),
		RulePackReleases: releases,
		Obligations:      obligations,
	}, nil
}

// applicableObligations applies the pack's obligations to proposal's facts.
// Each obligation type's trigger is documented next to it: some are
// unconditional statutory duties (retention, pay frequency), and some only
// apply when the proposal's facts raise them (salary-history collection,
// protected leave, an existing non-compete, a new hire, or a concurrent
// separation/reduction).
func applicableObligations(pack RulePack, proposal PromotionProposalSnapshot) []AppliedObligation {
	var out []AppliedObligation

	if proposal.payRateChanged() {
		for _, o := range pack.Notices {
			out = append(out, AppliedObligation{
				Type: ObligationTypeNotice,
				ID:   o.ID,
				Description: fmt.Sprintf("%s notice %s the effective date within %d day(s), channel=%s",
					o.Who, timingWord(o.TimingDirection), o.TimingDays, o.Channel),
				Citation: o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindNode, NonRemovable: true,
					Description: "workflow node: send pay-rate-change notice",
				},
			})
		}
	}

	if proposal.CollectsSalaryHistory {
		for _, o := range pack.FieldRestrictions {
			out = append(out, AppliedObligation{
				Type:        ObligationTypeFieldRestriction,
				ID:          o.ID,
				Description: fmt.Sprintf("restricted fields %v in context %q", o.RestrictedFields, o.Context),
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindFieldMask, NonRemovable: true,
					Description: "field mask: forbid salary-history collection",
				},
			})
		}
	}

	for _, o := range pack.RetentionRules {
		out = append(out, AppliedObligation{
			Type:        ObligationTypeRetention,
			ID:          o.ID,
			Description: fmt.Sprintf("retain %s for %d year(s) (%s)", o.RecordClass, o.DurationYears, o.DurationBasis),
			Citation:    o.Citation,
			Binding: ObligationBinding{
				ObligationID: o.ID, Kind: ObligationBindingKindNode, NonRemovable: true,
				Description: "record-retention schedule",
			},
		})
	}

	if proposal.OnProtectedLeave {
		for _, o := range pack.LeaveInteractions {
			out = append(out, AppliedObligation{
				Type:        ObligationTypeLeaveInteraction,
				ID:          o.ID,
				Description: fmt.Sprintf("%s: %s", o.LeaveType, o.InteractionRule),
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindGuard, NonRemovable: true,
					Description: "guard: preserve leave balance/accrual across the pay change",
				},
			})
		}
	}

	for _, o := range pack.PayFrequencyConstraints {
		out = append(out, AppliedObligation{
			Type:        ObligationTypePayFrequency,
			ID:          o.ID,
			Description: fmt.Sprintf("minimum frequency %s for %s", o.MinimumFrequency, o.AppliesToWorkerClass),
			Citation:    o.Citation,
			Binding: ObligationBinding{
				ObligationID: o.ID, Kind: ObligationBindingKindGuard, NonRemovable: true,
				Description: "guard: pay frequency floor",
			},
		})
	}

	if proposal.SeparationConcurrent {
		for _, o := range pack.FinalPayDeadlines {
			out = append(out, AppliedObligation{
				Type:        ObligationTypeFinalPayDeadline,
				ID:          o.ID,
				Description: fmt.Sprintf("%s: %s", o.Trigger, o.DeadlineDescription),
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindTimer, NonRemovable: true,
					Description: "timer: final pay deadline",
				},
			})
		}
	}

	if proposal.IsInternalPromotion {
		for _, o := range pack.PayTransparencyDuties {
			out = append(out, AppliedObligation{
				Type:        ObligationTypePayTransparency,
				ID:          o.ID,
				Description: fmt.Sprintf("%s: %s", o.Trigger, o.RequiredDisclosure),
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindNode, NonRemovable: true,
					Description: "workflow node: pay-range disclosure",
				},
			})
		}
	}

	if proposal.HasExistingNonCompete {
		for _, o := range pack.NonCompeteThresholds {
			out = append(out, AppliedObligation{
				Type:        ObligationTypeNonCompete,
				ID:          o.ID,
				Description: o.Rule,
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindHumanTask, NonRemovable: true,
					Description: "human task: non-compete re-check",
				},
			})
		}
	}

	if proposal.IsNewHire {
		for _, o := range pack.EVerifyChecks {
			out = append(out, AppliedObligation{
				Type:        ObligationTypeEVerify,
				ID:          o.ID,
				Description: o.Note,
				Citation:    o.Citation,
				Binding: ObligationBinding{
					ObligationID: o.ID, Kind: ObligationBindingKindGuard, NonRemovable: true,
					Description: "guard: work-authorization status check",
				},
			})
		}
	}

	for _, o := range pack.MiniWARNTriggers {
		if proposal.WorkforceReductionCount < o.EmployeeThreshold {
			continue
		}
		out = append(out, AppliedObligation{
			Type: ObligationTypeMiniWARN,
			ID:   o.ID,
			Description: fmt.Sprintf("%d+ affected within %d day(s) requires %d day(s) notice",
				o.EmployeeThreshold, o.LayoffWindowDays, o.NoticeDays),
			Citation: o.Citation,
			Binding: ObligationBinding{
				ObligationID: o.ID, Kind: ObligationBindingKindTimer, NonRemovable: true,
				Description: "timer: mini-WARN notice deadline",
			},
		})
	}

	return out
}

func timingWord(direction string) string {
	if direction == "BEFORE" {
		return "before"
	}
	return "after"
}
