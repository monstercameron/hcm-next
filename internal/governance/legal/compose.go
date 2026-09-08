package legal

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

// ErrContradictoryRequirements is returned when two mandatory obligations
// cannot both be satisfied. The receipt remains available to the caller and
// contains the two complete pieces of evidence that caused the refusal.
var ErrContradictoryRequirements = errors.New("legal: CONTRADICTORY_REQUIREMENTS")

// ErrConflictingDuplicateRelease rejects two different release bodies claiming
// the same immutable registration identity.
var ErrConflictingDuplicateRelease = errors.New("legal: conflicting duplicate rule-pack release")

// ObligationComparator is the stable name of the operation registered for an
// obligation kind. The names are receipt data, so changing one is a wire
// compatibility change.
type ObligationComparator string

const (
	ComparatorMostProtectiveMin ObligationComparator = "MOST_PROTECTIVE_MIN"
	ComparatorMostProtectiveMax ObligationComparator = "MOST_PROTECTIVE_MAX"
	ComparatorUnion             ObligationComparator = "UNION"
	ComparatorIntersection      ObligationComparator = "INTERSECTION"
	ComparatorVoidIfAnyVoids    ObligationComparator = "VOID_IF_ANY_VOIDS"
	ComparatorCustom            ObligationComparator = "CUSTOM"
)

// CompositionStatus is the result of composing all requested jurisdictions.
type CompositionStatus string

const (
	CompositionResolved                  CompositionStatus = "RESOLVED"
	CompositionContradictoryRequirements CompositionStatus = "CONTRADICTORY_REQUIREMENTS"
)

// CompositionRequest supplies the jurisdiction set and the immutable rule
// packs to compose. Packs may be presented in any order. Duplicate releases
// are ignored, which makes composition idempotent.
type CompositionRequest struct {
	Jurisdictions JurisdictionSet
	Packs         []RulePack
}

// ObligationEvidence is the audit-safe identity of one input obligation. It
// deliberately carries the complete Citation so a contradiction never loses
// the source that established either side.
type ObligationEvidence struct {
	Jurisdiction Jurisdiction
	PackID       string
	PackVersion  uint32
	Type         ObligationType
	ID           string
	Description  string
	Citation     Citation
}

// ContradictoryRequirement records both sides of an impossible composition.
// For retention, for example, a maximum shorter than another jurisdiction's
// minimum is represented here rather than silently choosing one bound.
type ContradictoryRequirement struct {
	Code        string
	Kind        ObligationType
	Reason      string
	ObligationA ObligationEvidence
	ObligationB ObligationEvidence
}

// ComposedObligation is one surviving obligation after the per-kind
// comparator has run. Sources are sorted and retained so a union or an
// intersection remains explainable without reopening the packs.
type ComposedObligation struct {
	Type         ObligationType
	ID           string
	Description  string
	Citation     Citation
	Jurisdiction Jurisdiction
	Sources      []ObligationEvidence
}

// CompositionTrace is the per-kind explanation of the inputs considered,
// the registered comparator and its winner. Winner is an obligation ID,
// "ALL", "INTERSECTION", or "VOID" as appropriate.
type CompositionTrace struct {
	Kind       ObligationType
	Inputs     []ObligationEvidence
	Comparator ObligationComparator
	Winner     string
}

// PreemptionApplied records one subdivision release assertion consumed before
// composition. Removed IDs are locality obligations only; the assertion can
// never remove a subdivision/country obligation or an unrelated kind.
type PreemptionApplied struct {
	Kind                  ObligationType
	AssertingJurisdiction Jurisdiction
	RemovedObligationIDs  []string
	Citation              Citation
}

// CompositionReceipt is deterministic audit evidence for one composition.
// Bytes and Digest are derived only from the sorted fields in this value.
type CompositionReceipt struct {
	Status             CompositionStatus
	Jurisdictions      []Jurisdiction
	Inputs             []ObligationEvidence
	Obligations        []ComposedObligation
	Traces             []CompositionTrace
	Contradictions     []ContradictoryRequirement
	PreemptionsApplied []PreemptionApplied
	Digest             string
}

type composableObligation struct {
	evidence     ObligationEvidence
	rule         obligationRule
	jurisdiction Jurisdiction
	packKey      string
}

type comparatorDefinition struct {
	Name    ObligationComparator
	Resolve func([]composableObligation) ([]composableObligation, string)
}

// obligationComparators is the one registration table for every vocabulary
// kind. Call sites dispatch through this table; no caller switches on kind.
var obligationComparators = map[ObligationType]comparatorDefinition{
	ObligationTypeNotice:             {ComparatorUnion, resolveUnion},
	ObligationTypeFieldRestriction:   {ComparatorIntersection, resolveIntersection},
	ObligationTypeRetention:          {ComparatorMostProtectiveMax, resolveMaximum},
	ObligationTypeLeaveInteraction:   {ComparatorUnion, resolveUnion},
	ObligationTypePayFrequency:       {ComparatorCustom, resolvePayFrequency},
	ObligationTypeFinalPayDeadline:   {ComparatorMostProtectiveMin, resolveMinimum},
	ObligationTypePayTransparency:    {ComparatorUnion, resolveUnion},
	ObligationTypeNonCompete:         {ComparatorVoidIfAnyVoids, resolveVoidIfAny},
	ObligationTypeEVerify:            {ComparatorUnion, resolveUnion},
	ObligationTypeMiniWARN:           {ComparatorMostProtectiveMax, resolveMaximum},
	ObligationTypeWageFloor:          {ComparatorMostProtectiveMin, resolveMaximum},
	ObligationTypePayEquityReview:    {ComparatorIntersection, resolveIntersection},
	ObligationTypePayStatement:       {ComparatorUnion, resolveUnion},
	ObligationTypeClassification:     {ComparatorIntersection, resolveIntersection},
	ObligationTypePersonnelFile:      {ComparatorMostProtectiveMin, resolveMinimum},
	ObligationTypeAntiRetaliation:    {ComparatorUnion, resolveUnion},
	ObligationTypeJobSecurity:        {ComparatorUnion, resolveUnion},
	ObligationTypeSeparationFiling:   {ComparatorUnion, resolveUnion},
	ObligationTypeDrugTesting:        {ComparatorUnion, resolveUnion},
	ObligationTypeBreachNotification: {ComparatorUnion, resolveUnion},
	ObligationTypeAutomatedDecision:  {ComparatorIntersection, resolveIntersection},
	ObligationTypeMonitoringConsent:  {ComparatorUnion, resolveUnion},
}

// ComparatorForKind returns the registered comparator for kind.
func ComparatorForKind(kind ObligationType) (ObligationComparator, bool) {
	d, ok := obligationComparators[kind]
	return d.Name, ok
}

// RegisteredComparators returns a defensive copy of the per-kind comparator
// table. It is useful to conformance checks without exposing mutable state.
func RegisteredComparators() map[ObligationType]ObligationComparator {
	out := make(map[ObligationType]ObligationComparator, len(obligationComparators))
	for kind, definition := range obligationComparators {
		out[kind] = definition.Name
	}
	return out
}

// ComposeObligations composes the requested packs over their jurisdiction set.
// It is pure: packs and the request are never mutated, and the output is
// independent of input order.
func ComposeObligations(request CompositionRequest) (CompositionReceipt, error) {
	if len(request.Packs) == 0 {
		return CompositionReceipt{}, errors.New("legal: composition needs at least one rule pack")
	}

	allowed := jurisdictionSetMembers(request.Jurisdictions)
	seenPacks := map[string]string{}
	var all []composableObligation
	for _, pack := range request.Packs {
		if err := pack.Jurisdiction.Validate(); err != nil {
			return CompositionReceipt{}, fmt.Errorf("legal: composition pack %q: %w", pack.PackID, err)
		}
		if len(allowed) > 0 {
			if _, ok := allowed[pack.Jurisdiction]; !ok {
				return CompositionReceipt{}, fmt.Errorf("legal: pack %s jurisdiction %s is outside the jurisdiction set", pack.PackID, pack.Jurisdiction)
			}
		}
		packKey := releaseIdentity(pack.Release())
		fingerprint := pack.ComputeDigest()
		if previous, duplicate := seenPacks[packKey]; duplicate {
			if previous != fingerprint {
				return CompositionReceipt{}, fmt.Errorf("%w: %s", ErrConflictingDuplicateRelease, packKey)
			}
			continue
		}
		seenPacks[packKey] = fingerprint
		for _, item := range pack.obligations() {
			all = append(all, composableObligation{
				evidence: ObligationEvidence{
					Jurisdiction: pack.Jurisdiction,
					PackID:       pack.PackID,
					PackVersion:  pack.Version,
					Type:         item.Type,
					ID:           item.Rule.obligationID(),
					Description:  item.Rule.describe(),
					Citation:     item.Rule.obligationCitation(),
				},
				rule:         item.Rule,
				jurisdiction: pack.Jurisdiction,
				packKey:      packKey,
			})
		}
	}
	sortComposable(all)
	all, preemptions, err := applyPreemptionsToComposable(all, request.Packs)
	if err != nil {
		return CompositionReceipt{}, err
	}

	byKind := map[ObligationType][]composableObligation{}
	for _, item := range all {
		byKind[item.evidence.Type] = append(byKind[item.evidence.Type], item)
	}

	receipt := CompositionReceipt{Status: CompositionResolved}
	receipt.PreemptionsApplied = preemptions
	receipt.Jurisdictions = sortedJurisdictions(request.Jurisdictions)
	for _, item := range all {
		receipt.Inputs = append(receipt.Inputs, item.evidence)
	}
	for _, kind := range sortedKinds(byKind) {
		items := byKind[kind]
		definition, ok := obligationComparators[kind]
		if !ok {
			definition = comparatorDefinition{Name: ComparatorUnion, Resolve: resolveUnion}
		}
		selected, winner := definition.Resolve(items)
		trace := CompositionTrace{Kind: kind, Comparator: definition.Name, Winner: winner}
		for _, item := range items {
			trace.Inputs = append(trace.Inputs, item.evidence)
		}
		receipt.Traces = append(receipt.Traces, trace)
		for _, item := range selected {
			receipt.Obligations = append(receipt.Obligations, ComposedObligation{
				Type:         item.evidence.Type,
				ID:           item.evidence.ID,
				Description:  item.evidence.Description,
				Citation:     item.evidence.Citation,
				Jurisdiction: item.jurisdiction,
				Sources:      []ObligationEvidence{item.evidence},
			})
		}
	}

	receipt.Contradictions = findRetentionContradictions(byKind[ObligationTypeRetention])
	if len(receipt.Contradictions) > 0 {
		receipt.Status = CompositionContradictoryRequirements
		receipt.Obligations = nil
	}
	receipt.sort()
	receipt.refreshDigest()
	if len(receipt.Contradictions) > 0 {
		return receipt, fmt.Errorf("%w: %s", ErrContradictoryRequirements, receipt.Contradictions[0].Reason)
	}
	return receipt, nil
}

func releaseIdentity(release RulePackRelease) string {
	return release.Jurisdiction.String() + "|" + release.PackID + "|" + strconv.FormatUint(uint64(release.Version), 10) + "." + strconv.FormatUint(uint64(release.MinorVersion), 10)
}

// Compose is the concise entry point for callers that already have a
// CompositionRequest.
func Compose(request CompositionRequest) (CompositionReceipt, error) {
	return ComposeObligations(request)
}

// ComposeJurisdictionSet is a convenience wrapper for the common pack-based
// composition path.
func ComposeJurisdictionSet(set JurisdictionSet, packs ...RulePack) (CompositionReceipt, error) {
	return ComposeObligations(CompositionRequest{Jurisdictions: set, Packs: packs})
}

func jurisdictionSetMembers(set JurisdictionSet) map[Jurisdiction]struct{} {
	members := map[Jurisdiction]struct{}{}
	if !set.Primary.IsZero() {
		members[set.Primary] = struct{}{}
	}
	for _, j := range set.Overlays {
		members[j] = struct{}{}
	}
	for _, j := range set.MultiStateExposures {
		members[j] = struct{}{}
	}
	return members
}

func sortedJurisdictions(set JurisdictionSet) []Jurisdiction {
	members := jurisdictionSetMembers(set)
	out := make([]Jurisdiction, 0, len(members))
	for j := range members {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func sortComposable(items []composableObligation) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].evidence, items[j].evidence
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if left.Jurisdiction.String() != right.Jurisdiction.String() {
			return left.Jurisdiction.String() < right.Jurisdiction.String()
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.PackID < right.PackID
	})
}

type preemptionKey struct {
	jurisdiction Jurisdiction
	kind         ObligationType
	id           string
}

// applyPreemptionsToComposable is the standalone evaluation stage shared by
// composition. Assertions are read from subdivision releases, then applied to
// already-pinned inputs before any comparator is selected.
func applyPreemptionsToComposable(items []composableObligation, packs []RulePack) ([]composableObligation, []PreemptionApplied, error) {
	removed := make(map[preemptionKey]struct{})
	var records []PreemptionApplied
	seenAssertions := make(map[string]struct{})
	for _, pack := range packs {
		if pack.Jurisdiction.Locality != "" || pack.Jurisdiction.State == "" {
			if len(pack.PreemptionAssertions) > 0 {
				return nil, nil, fmt.Errorf("legal: preemption asserting jurisdiction %s is not subdivision-level", pack.Jurisdiction)
			}
			continue
		}
		for _, assertion := range pack.PreemptionAssertions {
			if err := assertion.Validate(); err != nil {
				return nil, nil, err
			}
			assertionKey := releaseIdentity(pack.Release()) + "|" + assertion.Kind.String() + "|" + assertion.Citation.SourceFile + "|" + assertion.Citation.Section
			if _, duplicate := seenAssertions[assertionKey]; duplicate {
				continue
			}
			seenAssertions[assertionKey] = struct{}{}
			record := PreemptionApplied{Kind: assertion.Kind, AssertingJurisdiction: pack.Jurisdiction, Citation: assertion.Citation}
			for _, item := range items {
				if item.jurisdiction.Locality == "" || item.jurisdiction.Country != pack.Jurisdiction.Country || item.jurisdiction.State != pack.Jurisdiction.State || item.evidence.Type != assertion.Kind {
					continue
				}
				key := preemptionKey{jurisdiction: item.jurisdiction, kind: item.evidence.Type, id: item.evidence.ID}
				removed[key] = struct{}{}
				record.RemovedObligationIDs = append(record.RemovedObligationIDs, item.evidence.ID)
			}
			if len(record.RemovedObligationIDs) > 0 {
				sort.Strings(record.RemovedObligationIDs)
				records = append(records, record)
			}
		}
	}
	filtered := make([]composableObligation, 0, len(items))
	for _, item := range items {
		if _, ok := removed[preemptionKey{jurisdiction: item.jurisdiction, kind: item.evidence.Type, id: item.evidence.ID}]; !ok {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].AssertingJurisdiction.String() != records[j].AssertingJurisdiction.String() {
			return records[i].AssertingJurisdiction.String() < records[j].AssertingJurisdiction.String()
		}
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		if records[i].Citation.SourceFile != records[j].Citation.SourceFile {
			return records[i].Citation.SourceFile < records[j].Citation.SourceFile
		}
		return records[i].Citation.Section < records[j].Citation.Section
	})
	return filtered, records, nil
}

func sortedKinds(groups map[ObligationType][]composableObligation) []ObligationType {
	out := make([]ObligationType, 0, len(groups))
	for kind := range groups {
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func resolveUnion(items []composableObligation) ([]composableObligation, string) {
	return uniqueItems(items), "ALL"
}

func resolveVoidIfAny(items []composableObligation) ([]composableObligation, string) {
	for _, item := range items {
		if ruleIsVoid(item.rule) {
			return nil, "VOID"
		}
	}
	return uniqueItems(items), "ALL"
}

func resolveMinimum(items []composableObligation) ([]composableObligation, string) {
	if wageFloorItems(items) {
		return resolveWageFloor(items, -1)
	}
	return resolveMetric(items, func(left, right float64) bool { return left < right })
}

func resolveMaximum(items []composableObligation) ([]composableObligation, string) {
	if wageFloorItems(items) {
		return resolveWageFloor(items, 1)
	}
	return resolveMetric(items, func(left, right float64) bool { return left > right })
}

func wageFloorItems(items []composableObligation) bool {
	if len(items) == 0 {
		return false
	}
	_, ok := items[0].rule.(WageFloorRule)
	return ok
}

// resolveWageFloor compares the already-typed Money values directly. It
// deliberately refuses to rank different currencies: doing so would be an
// undeclared FX conversion, not a legal-composition decision.
func resolveWageFloor(items []composableObligation, direction int) ([]composableObligation, string) {
	best := -1
	for i, item := range items {
		floor, ok := item.rule.(WageFloorRule)
		if !ok || floor.FloorAmount.Validate() != nil {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		bestFloor := items[best].rule.(WageFloorRule)
		comparison, err := floor.FloorAmount.Cmp(bestFloor.FloorAmount)
		if err != nil {
			return uniqueItems(items), "ALL"
		}
		if comparison*direction > 0 || comparison == 0 && item.evidence.ID < items[best].evidence.ID {
			best = i
		}
	}
	if best < 0 {
		return uniqueItems(items), "ALL"
	}
	return []composableObligation{items[best]}, items[best].evidence.ID
}

func resolveMetric(items []composableObligation, better func(float64, float64) bool) ([]composableObligation, string) {
	if len(items) == 0 {
		return nil, ""
	}
	best := -1
	var bestMetric float64
	for i, item := range items {
		metric, ok := obligationMetric(item.rule)
		if !ok {
			continue
		}
		if best < 0 || better(metric, bestMetric) || (metric == bestMetric && item.evidence.ID < items[best].evidence.ID) {
			best, bestMetric = i, metric
		}
	}
	if best < 0 {
		return uniqueItems(items), "ALL"
	}
	return []composableObligation{items[best]}, items[best].evidence.ID
}

func resolvePayFrequency(items []composableObligation) ([]composableObligation, string) {
	best := -1
	bestRank := -1
	for i, item := range items {
		rank := frequencyRank(item.rule)
		if rank > bestRank || (rank == bestRank && best >= 0 && item.evidence.ID < items[best].evidence.ID) {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return uniqueItems(items), "ALL"
	}
	return []composableObligation{items[best]}, items[best].evidence.ID
}

func resolveIntersection(items []composableObligation) ([]composableObligation, string) {
	if len(items) < 2 {
		return uniqueItems(items), "INTERSECTION"
	}
	jurisdictions := map[Jurisdiction]struct{}{}
	for _, item := range items {
		jurisdictions[item.jurisdiction] = struct{}{}
	}
	counts := map[string]map[Jurisdiction]struct{}{}
	for _, item := range items {
		key := semanticKey(item.rule)
		if _, ok := counts[key]; !ok {
			counts[key] = map[Jurisdiction]struct{}{}
		}
		counts[key][item.jurisdiction] = struct{}{}
	}
	var selected []composableObligation
	for _, item := range items {
		if len(counts[semanticKey(item.rule)]) == len(jurisdictions) {
			selected = append(selected, item)
			delete(counts, semanticKey(item.rule))
		}
	}
	if len(selected) == 0 {
		return nil, "INTERSECTION"
	}
	return selected[:1], selected[0].evidence.ID
}

func uniqueItems(items []composableObligation) []composableObligation {
	seen := map[string]struct{}{}
	out := make([]composableObligation, 0, len(items))
	for _, item := range items {
		key := item.packKey + "|" + item.evidence.Type.String() + "|" + item.evidence.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func obligationMetric(rule obligationRule) (float64, bool) {
	switch value := rule.(type) {
	case RetentionRule:
		return float64(value.DurationYears), true
	case FinalPayDeadline:
		return firstNumber(value.DeadlineDescription)
	case PersonnelFileRule:
		return float64(value.ResponseDays), true
	case MiniWARNTrigger:
		return float64(value.NoticeDays), true
	default:
		return 0, false
	}
}

func frequencyRank(rule obligationRule) int {
	value, ok := rule.(PayFrequencyConstraint)
	if !ok {
		return -1
	}
	switch strings.ToUpper(strings.TrimSpace(value.MinimumFrequency)) {
	case "DAILY":
		return 4
	case "WEEKLY":
		return 3
	case "SEMIMONTHLY":
		return 2
	case "MONTHLY":
		return 1
	default:
		return 0
	}
}

func firstNumber(text string) (float64, bool) {
	start := -1
	for i := 0; i < len(text); i++ {
		if text[i] >= '0' && text[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		if strings.Contains(strings.ToLower(text), "immediate") {
			return 0, true
		}
		return 0, false
	}
	end := start
	for end < len(text) && ((text[end] >= '0' && text[end] <= '9') || text[end] == '.') {
		end++
	}
	parsed, err := strconv.ParseFloat(text[start:end], 64)
	return parsed, err == nil
}

func semanticKey(rule obligationRule) string {
	switch value := rule.(type) {
	case FieldRestriction:
		return "fields:" + strings.Join(sortedStrings(value.RestrictedFields), "|")
	case PayEquityReviewRule:
		return "bases:" + strings.Join(sortedStrings(value.ProtectedBases), "|") + ";standard:" + value.ComparatorStandard
	case ClassificationRule:
		return "dimension:" + value.Dimension + ";test:" + value.TestDescription
	case AutomatedDecisionRule:
		return "uses:" + strings.Join(sortedStrings(value.CoveredUses), "|")
	default:
		return valueDescription(rule)
	}
}

func valueDescription(rule obligationRule) string { return rule.describe() }

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func ruleIsVoid(rule obligationRule) bool {
	value, ok := rule.(NonCompeteThreshold)
	if !ok {
		return false
	}
	text := strings.ToLower(value.Rule)
	return strings.Contains(text, "void") || strings.Contains(text, "unenforceable")
}

func findRetentionContradictions(items []composableObligation) []ContradictoryRequirement {
	var out []ContradictoryRequirement
	for i := 0; i < len(items); i++ {
		_, leftIsMax, ok := retentionBound(items[i].rule)
		if !ok {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			_, rightIsMax, rightOK := retentionBound(items[j].rule)
			if !rightOK || leftIsMax == rightIsMax {
				continue
			}
			if retentionRecordClass(items[i].rule) != retentionRecordClass(items[j].rule) {
				continue
			}
			maximum, minimum := items[i], items[j]
			if !leftIsMax {
				maximum, minimum = items[j], items[i]
			}
			maxValue, _, _ := retentionBound(maximum.rule)
			minValue, _, _ := retentionBound(minimum.rule)
			if maxValue < minValue {
				out = append(out, ContradictoryRequirement{
					Code:        "CONTRADICTORY_REQUIREMENTS",
					Kind:        ObligationTypeRetention,
					Reason:      fmt.Sprintf("retention maximum %d year(s) is shorter than minimum %d year(s) for %s", maxValue, minValue, retentionRecordClass(minimum.rule)),
					ObligationA: maximum.evidence,
					ObligationB: minimum.evidence,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ObligationA.ID != out[j].ObligationA.ID {
			return out[i].ObligationA.ID < out[j].ObligationA.ID
		}
		return out[i].ObligationB.ID < out[j].ObligationB.ID
	})
	return out
}

func retentionBound(rule obligationRule) (int, bool, bool) {
	value, ok := rule.(RetentionRule)
	if !ok {
		return 0, false, false
	}
	basis := strings.ToLower(value.DurationBasis)
	isMaximum := strings.Contains(basis, "maximum") || strings.Contains(basis, "max") || strings.Contains(basis, "cap")
	return value.DurationYears, isMaximum, true
}

func retentionRecordClass(rule obligationRule) string {
	value, ok := rule.(RetentionRule)
	if !ok {
		return ""
	}
	return value.RecordClass
}

type canonicalComposition struct {
	Status             CompositionStatus          `json:"status"`
	Jurisdictions      []Jurisdiction             `json:"jurisdictions"`
	Inputs             []ObligationEvidence       `json:"inputs"`
	Obligations        []ComposedObligation       `json:"obligations"`
	Traces             []CompositionTrace         `json:"traces"`
	Contradictions     []ContradictoryRequirement `json:"contradictions"`
	PreemptionsApplied []PreemptionApplied        `json:"preemptions_applied,omitempty"`
}

func (r CompositionReceipt) canonicalValue() canonicalComposition {
	return canonicalComposition{
		Status: r.Status, Jurisdictions: r.Jurisdictions, Inputs: r.Inputs,
		Obligations: r.Obligations, Traces: r.Traces, Contradictions: r.Contradictions,
		PreemptionsApplied: r.PreemptionsApplied,
	}
}

// CanonicalBytes returns the stable payload covered by Digest.
func (r CompositionReceipt) CanonicalBytes() []byte {
	bytes, _ := json.Marshal(r.canonicalValue())
	return bytes
}

// Bytes returns the stable receipt including its digest.
func (r CompositionReceipt) Bytes() []byte {
	value := struct {
		canonicalComposition
		Digest string `json:"digest"`
	}{canonicalComposition: r.canonicalValue(), Digest: r.Digest}
	bytes, _ := json.Marshal(value)
	return bytes
}

// Explain returns deterministic, audit-oriented composition metadata.
func (r CompositionReceipt) Explain() string {
	return fmt.Sprintf("status=%s digest=%s jurisdictions=%d inputs=%d traces=%d obligations=%d contradictions=%d", r.Status, r.Digest, len(r.Jurisdictions), len(r.Inputs), len(r.Traces), len(r.Obligations), len(r.Contradictions))
}

// ExplainComposition is the package-level Explain-shaped helper for callers
// that do not need to retain the receipt as a method receiver.
func ExplainComposition(r CompositionReceipt) string { return r.Explain() }

func (r *CompositionReceipt) sort() {
	sort.Slice(r.Inputs, func(i, j int) bool { return evidenceKey(r.Inputs[i]) < evidenceKey(r.Inputs[j]) })
	sort.Slice(r.Jurisdictions, func(i, j int) bool { return r.Jurisdictions[i].String() < r.Jurisdictions[j].String() })
	sort.Slice(r.Obligations, func(i, j int) bool {
		if r.Obligations[i].Type != r.Obligations[j].Type {
			return r.Obligations[i].Type < r.Obligations[j].Type
		}
		return r.Obligations[i].ID < r.Obligations[j].ID
	})
	sort.Slice(r.Traces, func(i, j int) bool { return r.Traces[i].Kind < r.Traces[j].Kind })
	for i := range r.Traces {
		sort.Slice(r.Traces[i].Inputs, func(a, b int) bool { return evidenceKey(r.Traces[i].Inputs[a]) < evidenceKey(r.Traces[i].Inputs[b]) })
	}
}

func (r *CompositionReceipt) refreshDigest() {
	sum := sha256.Sum256(r.CanonicalBytes())
	r.Digest = hex.EncodeToString(sum[:])
}

func evidenceKey(e ObligationEvidence) string {
	return e.Jurisdiction.String() + "|" + e.PackID + "|" + strconv.FormatUint(uint64(e.PackVersion), 10) + "|" + e.Type.String() + "|" + e.ID
}
