package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrInvalidFundedDomain       = errors.New("intentmanifests: invalid funded-domain declaration")
	ErrDraftedIntentNotCataloged = errors.New("intentmanifests: funded drafted intent is absent from the catalog")
	ErrFeatureFamilyConflict     = errors.New("intentmanifests: feature classification conflicts with intent family")
)

// FundedDomainDeclaration is the explicit design-time input that turns on a
// second domain. It names the domain and the exact drafted intent references
// funded for that domain; it does not mutate the intake or generated coverage.
type FundedDomainDeclaration struct {
	Domain           string   `json:"domain"`
	DraftedIntentIDs []string `json:"drafted_intent_ids"`
}

// FeatureBindingProposal is one safe convergence proposal. Applying it is a
// source/manifest change outside this package; this package only reports the
// binding and the family evidence that justified it.
type FeatureBindingProposal struct {
	FeatureID       string                 `json:"feature_id"`
	FutureIntentRef string                 `json:"future_intent_ref"`
	Classification  SemanticClassification `json:"classification"`
	IntentFamily    string                 `json:"intent_family"`
}

// FeatureBindingRefusal records a candidate that cannot be proposed because
// its semantic classification and the drafted intent family disagree.
type FeatureBindingRefusal struct {
	FeatureID       string                 `json:"feature_id"`
	FutureIntentRef string                 `json:"future_intent_ref"`
	Classification  SemanticClassification `json:"classification"`
	IntentFamily    string                 `json:"intent_family"`
	Reason          string                 `json:"reason"`
}

// FeatureVocabularyConvergenceReport is the deterministic, denominator-free
// report for a funded domain. Proposals contain only deferred features whose
// named future intent is in the funded drafted set.
type FeatureVocabularyConvergenceReport struct {
	SchemaVersion    int                      `json:"schema_version"`
	Domain           string                   `json:"domain"`
	DraftedIntentIDs []string                 `json:"drafted_intent_ids"`
	Proposals        []FeatureBindingProposal `json:"proposals"`
	Refusals         []FeatureBindingRefusal  `json:"refusals"`
	Digest           string                   `json:"digest"`
}

// Complete reports whether the funded candidates all have compatible
// classifications. It does not claim that a proposal has been applied.
func (r FeatureVocabularyConvergenceReport) Complete() bool { return len(r.Refusals) == 0 }

// ConvergeFeatureVocabulary joins the existing feature-intent coverage with
// a funded domain's drafted definitions. It is pure and deterministic: only
// DEFERRED_TO_INTENT rows in the funded domain whose DispositionTarget names
// one of the funded drafted definitions enter the report.
func ConvergeFeatureVocabulary(registry FeatureIntentCoverageRegistry, funded FundedDomainDeclaration, descriptors []IntentDescriptor) (FeatureVocabularyConvergenceReport, error) {
	report := FeatureVocabularyConvergenceReport{SchemaVersion: 1, Domain: strings.TrimSpace(funded.Domain)}
	if report.Domain == "" {
		return report, fmt.Errorf("%w: domain is required", ErrInvalidFundedDomain)
	}
	if len(funded.DraftedIntentIDs) == 0 {
		return report, fmt.Errorf("%w: at least one drafted intent is required", ErrInvalidFundedDomain)
	}

	drafted := make(map[string]IntentDescriptor, len(descriptors)*2)
	for _, descriptor := range descriptors {
		if strings.TrimSpace(descriptor.IntentTypeID) == "" || descriptor.Version < 1 || strings.TrimSpace(descriptor.Family) == "" {
			return report, fmt.Errorf("%w: descriptor %q is incomplete", ErrDraftedIntentNotCataloged, descriptor.IntentTypeID)
		}
		drafted[descriptor.IntentTypeID] = descriptor
		drafted[intentReference(descriptor.IntentTypeID, descriptor.Version)] = descriptor
	}

	fundedRefs := make([]string, 0, len(funded.DraftedIntentIDs))
	seenFunded := make(map[string]bool, len(funded.DraftedIntentIDs))
	for _, ref := range funded.DraftedIntentIDs {
		key, ok := normalizeIntentReference(ref, drafted)
		if !ok {
			return report, fmt.Errorf("%w: %q", ErrDraftedIntentNotCataloged, ref)
		}
		if seenFunded[key] {
			return report, fmt.Errorf("%w: drafted intent %q is repeated", ErrInvalidFundedDomain, ref)
		}
		seenFunded[key] = true
		fundedRefs = append(fundedRefs, key)
	}
	sort.Strings(fundedRefs)
	report.DraftedIntentIDs = fundedRefs

	for _, feature := range registry.Features {
		if !strings.EqualFold(strings.TrimSpace(feature.Owner), report.Domain) || feature.Disposition != DispositionDeferredToIntent {
			continue
		}
		futureRef := strings.TrimSpace(feature.DispositionTarget)
		futureKey, ok := normalizeIntentReference(futureRef, drafted)
		if !ok || !seenFunded[futureKey] {
			continue
		}
		descriptor, ok := drafted[futureKey]
		if !ok {
			return report, fmt.Errorf("%w: %q", ErrDraftedIntentNotCataloged, futureRef)
		}
		refusal := FeatureBindingRefusal{
			FeatureID: feature.FeatureID, FutureIntentRef: futureKey,
			Classification: feature.Classification, IntentFamily: descriptor.Family,
		}
		if !FeatureClassificationCompatible(feature.Classification, descriptor.Family) {
			refusal.Reason = fmt.Sprintf("classification %s cannot bind to family %s", feature.Classification, descriptor.Family)
			report.Refusals = append(report.Refusals, refusal)
			continue
		}
		report.Proposals = append(report.Proposals, FeatureBindingProposal{
			FeatureID: feature.FeatureID, FutureIntentRef: futureKey,
			Classification: feature.Classification, IntentFamily: descriptor.Family,
		})
	}
	sort.Slice(report.Proposals, func(i, j int) bool { return report.Proposals[i].FeatureID < report.Proposals[j].FeatureID })
	sort.Slice(report.Refusals, func(i, j int) bool { return report.Refusals[i].FeatureID < report.Refusals[j].FeatureID })
	report.Digest = report.computeDigest()
	if len(report.Refusals) != 0 {
		return report, fmt.Errorf("%w: %d refusal(s) in funded domain %s", ErrFeatureFamilyConflict, len(report.Refusals), report.Domain)
	}
	return report, nil
}

// CheckFeatureVocabularyConvergence is the descriptive alias used by
// conformance callers.
func CheckFeatureVocabularyConvergence(registry FeatureIntentCoverageRegistry, funded FundedDomainDeclaration, descriptors []IntentDescriptor) (FeatureVocabularyConvergenceReport, error) {
	return ConvergeFeatureVocabulary(registry, funded, descriptors)
}

// FeatureClassificationCompatible defines the small closed compatibility
// table between normalized feature semantics and intent families.
func FeatureClassificationCompatible(classification SemanticClassification, family string) bool {
	switch classification {
	case ClassCreate, ClassEmitChild:
		return family == FamilyChangeRequest
	case ClassConsume:
		return family == FamilyCalculationRequest || family == FamilyAnalyticalRequest
	case ClassObserve:
		return family == FamilyAnalyticalRequest
	case ClassNonMaterial, ClassReviewNeeded:
		return false
	default:
		return false
	}
}

// PromotionFundedFixture supplies the in-memory second-domain fixture used
// by the golden test. It intentionally represents the deferred Promotion
// domain row becoming draft-funded without editing the source manifests.
func PromotionFundedFixture() (FeatureIntentCoverageRegistry, FundedDomainDeclaration, []IntentDescriptor) {
	return FeatureIntentCoverageRegistry{
			Version: "1.0", FeatureGroups: 1, FeatureCount: 1,
			Features: []FeatureIntentCoverage{{
				FeatureID: "merit_cycle_manage", Owner: "rewards", Classification: ClassCreate,
				Role: RoleIntentCreator, CoverageStatus: CoverageDeferred,
				BoundIntentID: DeferredIntentBinding, Disposition: DispositionDeferredToIntent,
				DispositionTarget: "hcmnext.rewards.merit_cycle_manage/v1",
			}},
		},
		FundedDomainDeclaration{Domain: "rewards", DraftedIntentIDs: []string{"hcmnext.rewards.merit_cycle_manage/v1"}},
		[]IntentDescriptor{{IntentTypeID: "hcmnext.rewards.merit_cycle_manage", Version: 1, Family: FamilyChangeRequest}}
}

func (r FeatureVocabularyConvergenceReport) computeDigest() string {
	b := r.Canonical()
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Canonical returns the deterministic report encoding used to calculate its
// digest. The digest field is excluded so the representation is self-consistent.
func (r FeatureVocabularyConvergenceReport) Canonical() []byte {
	copy := r
	copy.Digest = ""
	b, _ := json.Marshal(copy)
	return b
}

func intentReference(id string, version int) string { return id + "/v" + strconv.Itoa(version) }

func normalizeIntentReference(ref string, descriptors map[string]IntentDescriptor) (string, bool) {
	ref = strings.TrimSpace(ref)
	if descriptor, ok := descriptors[ref]; ok {
		return intentReference(descriptor.IntentTypeID, descriptor.Version), true
	}
	if slash := strings.LastIndex(ref, "/v"); slash > 0 {
		version, err := strconv.Atoi(ref[slash+2:])
		if err == nil && version > 0 {
			key := intentReference(ref[:slash], version)
			if _, ok := descriptors[key]; ok {
				return key, true
			}
		}
	}
	return "", false
}
