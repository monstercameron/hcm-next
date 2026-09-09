package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrReceiptInvalid = errors.New("legal: evaluation receipt is invalid")

type PinnedReleaseEvidence struct {
	Release      RulePackRelease
	Digest       string
	ReviewStatus ReviewStatus
}
type ReceiptAppliedObligation struct {
	AppliedObligation
	BodyDigest string
}
type ReceiptJurisdictionSet struct {
	Primary                          Jurisdiction
	Overlays, UnregisteredLocalities []Jurisdiction
}

// LegalEvaluationReceipt is immutable, offline-verifiable evidence of one evaluation.
type LegalEvaluationReceipt struct {
	LegalContextDigest       string
	JurisdictionSet          ReceiptJurisdictionSet
	PinnedReleases           []PinnedReleaseEvidence
	AttributionRuleFired     AttributionRule
	RemoteWorkPolicyApplied  string
	ObligationsApplied       []ReceiptAppliedObligation
	ObligationsNotApplicable []ConsideredObligation
	ObligationsNotConsidered []NotConsideredKind
	PreemptionsApplied       []PreemptionApplied
	CompositionTrace         []CompositionTrace
	Contradictions           []ContradictoryRequirement
	Notes                    []LegalEvaluationNote
	Status                   LegalEvaluationStatus
	EvaluatedAt              values.Instant
	EffectiveDate            values.LocalDate
	KnownAt                  values.KnownAt
	Digest                   string
	Signature                Signature
}

type receiptJSON struct {
	LegalContextDigest       string                     `json:"legal_context_digest"`
	JurisdictionSet          ReceiptJurisdictionSet     `json:"jurisdiction_set"`
	PinnedReleases           []PinnedReleaseEvidence    `json:"pinned_releases"`
	AttributionRuleFired     AttributionRule            `json:"attribution_rule_fired"`
	RemoteWorkPolicyApplied  string                     `json:"remote_work_policy_applied"`
	ObligationsApplied       []ReceiptAppliedObligation `json:"obligations_applied"`
	ObligationsNotApplicable []ConsideredObligation     `json:"obligations_not_applicable"`
	ObligationsNotConsidered []NotConsideredKind        `json:"obligations_not_considered"`
	PreemptionsApplied       []PreemptionApplied        `json:"preemptions_applied"`
	CompositionTrace         []CompositionTrace         `json:"composition_trace"`
	Contradictions           []ContradictoryRequirement `json:"contradictions"`
	Notes                    []LegalEvaluationNote      `json:"notes"`
	Status                   LegalEvaluationStatus      `json:"status"`
	EvaluatedAt              string                     `json:"evaluated_at"`
	EffectiveDate            string                     `json:"effective_date"`
	KnownAt                  string                     `json:"known_at"`
	Digest                   string                     `json:"digest"`
	Signature                Signature                  `json:"signature"`
}

func (r LegalEvaluationReceipt) MarshalJSON() ([]byte, error) {
	return json.Marshal(receiptJSON{r.LegalContextDigest, r.JurisdictionSet, r.PinnedReleases, r.AttributionRuleFired, r.RemoteWorkPolicyApplied, r.ObligationsApplied, r.ObligationsNotApplicable, r.ObligationsNotConsidered, r.PreemptionsApplied, r.CompositionTrace, r.Contradictions, r.Notes, r.Status, r.EvaluatedAt.String(), r.EffectiveDate.String(), r.KnownAt.String(), r.Digest, r.Signature})
}

func (r *LegalEvaluationReceipt) UnmarshalJSON(data []byte) error {
	var in receiptJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	evaluated, err := time.Parse(time.RFC3339Nano, in.EvaluatedAt)
	if err != nil {
		return err
	}
	effective, err := values.ParseLocalDate(in.EffectiveDate)
	if err != nil {
		return err
	}
	knownTime, err := time.Parse(time.RFC3339Nano, in.KnownAt)
	if err != nil {
		return err
	}
	known, err := values.NewKnownAt(values.NewInstant(knownTime))
	if err != nil {
		return err
	}
	*r = LegalEvaluationReceipt{in.LegalContextDigest, in.JurisdictionSet, in.PinnedReleases, in.AttributionRuleFired, in.RemoteWorkPolicyApplied, in.ObligationsApplied, in.ObligationsNotApplicable, in.ObligationsNotConsidered, in.PreemptionsApplied, in.CompositionTrace, in.Contradictions, in.Notes, in.Status, values.NewInstant(evaluated), effective, known, in.Digest, in.Signature}
	return nil
}

func EvaluateReceipt(ctx *LegalContext, proposal PromotionProposalSnapshot, registry *Registry, signer *Signer, at values.Instant) (LegalEvaluationReceipt, error) {
	if ctx == nil || registry == nil || signer == nil || at.Validate() != nil {
		return LegalEvaluationReceipt{}, fmt.Errorf("%w: missing evaluation input", ErrReceiptInvalid)
	}
	if err := ctx.VerifyWithKey(signer.PublicKey()); err != nil {
		return LegalEvaluationReceipt{}, fmt.Errorf("%w: legal context authority: %v", ErrReceiptInvalid, err)
	}
	result, err := Evaluate(ctx, proposal, registry)
	if err != nil {
		return LegalEvaluationReceipt{}, err
	}
	r := LegalEvaluationReceipt{LegalContextDigest: ctx.Digest(), JurisdictionSet: ReceiptJurisdictionSet{Primary: ctx.Jurisdiction(), UnregisteredLocalities: ctx.UnregisteredLocalities()}, AttributionRuleFired: ctx.AttributionRule(), RemoteWorkPolicyApplied: ctx.Provenance().RemoteWorkPolicyApplied, ObligationsNotApplicable: slices.Clone(result.NotApplicable), ObligationsNotConsidered: slices.Clone(result.NotConsidered), PreemptionsApplied: slices.Clone(result.PreemptionsApplied), CompositionTrace: slices.Clone(result.Composition.Traces), Contradictions: slices.Clone(result.Composition.Contradictions), Notes: slices.Clone(result.ReceiptNotes), Status: result.Status, EvaluatedAt: at, EffectiveDate: ctx.EffectiveDate(), KnownAt: ctx.KnownAt()}
	if r.ObligationsNotApplicable == nil {
		r.ObligationsNotApplicable = []ConsideredObligation{}
	}
	if r.CompositionTrace == nil {
		r.CompositionTrace = []CompositionTrace{}
	}
	for _, release := range result.RulePackReleases {
		pack, e := registry.GetExact(release)
		if e != nil {
			return LegalEvaluationReceipt{}, fmt.Errorf("%w: pinned release unavailable", ErrReceiptInvalid)
		}
		digest := pack.Digest
		if digest == "" {
			digest = pack.ComputeDigest()
		}
		r.PinnedReleases = append(r.PinnedReleases, PinnedReleaseEvidence{release, digest, pack.ReviewStatus})
		if !release.Jurisdiction.Equal(ctx.Jurisdiction()) {
			r.JurisdictionSet.Overlays = append(r.JurisdictionSet.Overlays, release.Jurisdiction)
		}
	}
	for _, obligation := range result.Obligations {
		bodyDigest, e := appliedBodyDigest(obligation, result.pinnedPacks)
		if e != nil {
			return LegalEvaluationReceipt{}, e
		}
		r.ObligationsApplied = append(r.ObligationsApplied, ReceiptAppliedObligation{obligation, bodyDigest})
	}
	r.Digest, r.Signature, err = signer.SignDigestChecked(r.CanonicalBytes())
	if err != nil {
		return LegalEvaluationReceipt{}, fmt.Errorf("%w: signing receipt: %w", ErrReceiptInvalid, err)
	}
	return r, nil
}

func appliedBodyDigest(applied AppliedObligation, packs []RulePack) (string, error) {
	var body []byte
	matches := 0
	for _, pack := range packs {
		if pack.PackID != applied.PackID || pack.Version != applied.PackVersion || !pack.Jurisdiction.Equal(applied.Jurisdiction) {
			continue
		}
		for _, obligation := range pack.obligations() {
			if obligation.Type != applied.Type || obligation.Rule.obligationID() != applied.ID {
				continue
			}
			matches++
			body = obligation.Rule.canonicalBody(nil)
		}
	}
	if matches != 1 {
		return "", fmt.Errorf("%w: applied obligation %s/%s resolves to %d typed bodies", ErrReceiptInvalid, applied.Type, applied.ID, matches)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func (r LegalEvaluationReceipt) Verify() error                  { return r.verify(nil) }
func (r LegalEvaluationReceipt) VerifyWithKey(key []byte) error { return r.verify(key) }
func (r LegalEvaluationReceipt) verify(key []byte) error {
	if r.LegalContextDigest == "" || len(r.PinnedReleases) == 0 || r.AttributionRuleFired == "" || r.RemoteWorkPolicyApplied == "" || r.ObligationsNotApplicable == nil || r.CompositionTrace == nil || r.Status == LegalEvaluationStatusUnspecified || r.EvaluatedAt.Validate() != nil || r.EffectiveDate.Validate() != nil || r.KnownAt.Instant().Validate() != nil {
		return ErrReceiptInvalid
	}
	for _, release := range r.PinnedReleases {
		if release.Release.PackID == "" || release.Release.Version == 0 || release.Release.Jurisdiction.Validate() != nil || release.Digest == "" {
			return ErrReceiptInvalid
		}
	}
	for _, obligation := range r.ObligationsApplied {
		if obligation.Type == ObligationTypeUnspecified || obligation.ID == "" || obligation.BodyDigest == "" {
			return ErrReceiptInvalid
		}
	}
	if key != nil && (len(key) != len(r.Signature.PublicKey) || !slices.Equal(key, r.Signature.PublicKey)) {
		return ErrReceiptInvalid
	}
	if err := VerifySignature(r.CanonicalBytes(), r.Digest, r.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrReceiptInvalid, err)
	}
	return nil
}

func (r LegalEvaluationReceipt) CanonicalBytes() []byte {
	var out = []byte{'L', 'E', 'R', '1'}
	appendJSON := func(label string, value any) { b, _ := json.Marshal(value); out = appendField(out, label, string(b)) }
	out = appendField(out, "legal_context_digest", r.LegalContextDigest)
	appendJSON("jurisdiction_set", r.JurisdictionSet)
	appendJSON("pinned_releases", r.PinnedReleases)
	out = appendField(out, "attribution_rule_fired", string(r.AttributionRuleFired))
	out = appendField(out, "remote_work_policy_applied", r.RemoteWorkPolicyApplied)
	appendJSON("obligations_applied", r.ObligationsApplied)
	appendJSON("obligations_not_applicable", r.ObligationsNotApplicable)
	appendJSON("obligations_not_considered", r.ObligationsNotConsidered)
	appendJSON("preemptions_applied", r.PreemptionsApplied)
	appendJSON("composition_trace", r.CompositionTrace)
	appendJSON("contradictions", r.Contradictions)
	appendJSON("notes", r.Notes)
	out = appendField(out, "status", r.Status.String())
	out = appendField(out, "evaluated_at", r.EvaluatedAt.String())
	out = appendField(out, "effective_date", r.EffectiveDate.String())
	out = appendField(out, "known_at", r.KnownAt.String())
	return out
}
