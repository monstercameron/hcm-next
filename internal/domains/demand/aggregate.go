package demand

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	AggregationFormulaVersion = "quantity*confidence/v1"
	AggregationSchemaVersion  = 1
)

var (
	ErrAggregationInvalid       = errors.New("demand: aggregation is invalid")
	ErrSourceRefRequired        = errors.New("demand: aggregation requires a source ref")
	ErrDuplicateSignalConflict  = errors.New("demand: duplicate natural-key signals disagree")
	ErrOverlappingSourceWindows = errors.New("demand: one source has overlapping windows")
	ErrWindowSpansBuckets       = errors.New("demand: signal window spans more than one bucket")
	ErrAggregationUnitMismatch  = errors.New("demand: aggregation has incompatible units")
)

// BucketRule makes the window partition explicit. Buckets are half-open,
// anchored at Origin, and a signal must fit wholly inside one bucket because
// this package has no rate or proration authority.
type BucketRule struct {
	ID          string
	Version     string
	Origin      values.Instant
	Width       time.Duration
	ResultScale int32
	Rounding    values.RoundingMode
}

func (r BucketRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: bucket id and version are required", ErrAggregationInvalid)
	}
	if err := r.Origin.Validate(); err != nil {
		return fmt.Errorf("%w: bucket origin: %v", ErrAggregationInvalid, err)
	}
	if r.Width <= 0 {
		return fmt.Errorf("%w: bucket width must be positive", ErrAggregationInvalid)
	}
	if r.ResultScale < 0 || r.ResultScale > values.MaxScale {
		return fmt.Errorf("%w: result scale is outside the decimal range", ErrAggregationInvalid)
	}
	if r.Rounding == values.RoundingUnspecified || !r.Rounding.Valid() {
		return fmt.Errorf("%w: bucket rounding mode is required", ErrAggregationInvalid)
	}
	return nil
}

func (r BucketRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.demand.BucketRule", AggregationSchemaVersion).
		String("id", r.ID).String("version", r.Version).Value("origin", r.Origin).
		Int("width_nanos", int64(r.Width)).Int("result_scale", int64(r.ResultScale)).String("rounding", r.Rounding.String())
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// WindowBucket is the canonical bucket assigned by a BucketRule.
type WindowBucket struct {
	Key    string
	Window values.EffectiveInterval
}

func (r BucketRule) Bucket(window values.EffectiveInterval) (WindowBucket, error) {
	if err := r.Validate(); err != nil {
		return WindowBucket{}, err
	}
	if err := window.Validate(); err != nil {
		return WindowBucket{}, fmt.Errorf("%w: signal window: %v", ErrAggregationInvalid, err)
	}
	if window.Kind() != values.IntervalKindInstant || window.IsOpenEnded() {
		return WindowBucket{}, fmt.Errorf("%w: signal window must be a closed instant interval", ErrAggregationInvalid)
	}
	start, _ := window.StartInstant()
	end, _ := window.EndInstant()
	delta := start.Time().Sub(r.Origin.Time())
	index := int64(delta / r.Width)
	if delta < 0 && delta%r.Width != 0 {
		index--
	}
	startTime := r.Origin.Time().Add(time.Duration(index) * r.Width)
	endTime := startTime.Add(r.Width)
	bucketStart := values.NewInstant(startTime)
	bucketEnd := values.NewInstant(endTime)
	if end.After(bucketEnd) {
		return WindowBucket{}, fmt.Errorf("%w: %s does not fit in bucket starting %s", ErrWindowSpansBuckets, window, bucketStart)
	}
	bucketWindow, err := values.NewInstantInterval(bucketStart, bucketEnd)
	if err != nil {
		return WindowBucket{}, err
	}
	return WindowBucket{Key: r.ID + "@" + bucketStart.String(), Window: bucketWindow}, nil
}

// SourceContribution is the source partition inside one aggregate. Signal
// refs are sorted and copied; the quantity is confidence-weighted decimal
// output at the rule's declared result scale.
type SourceContribution struct {
	SourceRef        string
	SignalRefs       []string
	WeightedQuantity values.Quantity
}

// DemandAggregate is one (org unit, role or skill, window bucket) total.
type DemandAggregate struct {
	OrgUnit          string
	RoleOrSkillRef   string
	Bucket           WindowBucket
	Unit             string
	WeightedQuantity values.Quantity
	SourceRefs       []string
	SignalRefs       []string
	Sources          []SourceContribution
	FormulaVersion   string
	RuleVersion      string
	CanonicalDigest  string
}

func (a DemandAggregate) body() []byte {
	sources := append([]SourceContribution(nil), a.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].SourceRef < sources[j].SourceRef })
	w := canonicalbytes.New("hcmnext.domains.demand.DemandAggregate", AggregationSchemaVersion).
		String("org_unit", a.OrgUnit).String("role_or_skill_ref", a.RoleOrSkillRef).
		String("bucket_key", a.Bucket.Key).Value("bucket_window", a.Bucket.Window).
		Value("weighted_quantity", a.WeightedQuantity).SortedStrings("source_ref", a.SourceRefs).
		SortedStrings("signal_ref", a.SignalRefs).String("formula_version", a.FormulaVersion).String("rule_version", a.RuleVersion).
		Count("sources", len(sources))
	for _, source := range sources {
		w.String("source.ref", source.SourceRef).SortedStrings("source.signal_ref", source.SignalRefs).
			Value("source.weighted_quantity", source.WeightedQuantity)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (a DemandAggregate) Canonical() []byte { return a.body() }

// DemandAggregation is a pure, deterministic aggregation result.
type DemandAggregation struct {
	Rule            BucketRule
	FormulaVersion  string
	Aggregates      []DemandAggregate
	CanonicalDigest string
}

func (a DemandAggregation) Canonical() []byte {
	if a.Rule.Validate() != nil || a.FormulaVersion == "" || len(a.Aggregates) == 0 {
		return nil
	}
	items := append([]DemandAggregate(nil), a.Aggregates...)
	sortAggregates(items)
	w := canonicalbytes.New("hcmnext.domains.demand.DemandAggregation", AggregationSchemaVersion).
		Value("rule", a.Rule).String("formula_version", a.FormulaVersion).Count("aggregates", len(items))
	for _, item := range items {
		w.Field("aggregate", item.body())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (a DemandAggregation) Digest() (string, error) {
	if len(a.Canonical()) == 0 {
		return "", ErrAggregationInvalid
	}
	return canonicalbytes.Digest(a.Canonical()), nil
}

func (a DemandAggregation) Validate() error {
	if err := a.Rule.Validate(); err != nil {
		return err
	}
	if a.FormulaVersion != AggregationFormulaVersion || len(a.Aggregates) == 0 {
		return fmt.Errorf("%w: formula and at least one aggregate are required", ErrAggregationInvalid)
	}
	for _, item := range a.Aggregates {
		if item.WeightedQuantity.Validate() != nil || item.WeightedQuantity.Value().Sign() < 0 {
			return fmt.Errorf("%w: aggregate quantity is invalid", ErrAggregationInvalid)
		}
		if item.FormulaVersion != a.FormulaVersion || item.RuleVersion != a.Rule.Version {
			return fmt.Errorf("%w: aggregate formula or rule version mismatch", ErrAggregationInvalid)
		}
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != canonicalbytes.Digest(a.Canonical()) {
		return fmt.Errorf("%w: aggregate digest mismatch", ErrAggregationInvalid)
	}
	return nil
}

func sortAggregates(items []DemandAggregate) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		lk := left.OrgUnit + "\x00" + left.RoleOrSkillRef + "\x00" + left.Bucket.Key + "\x00" + left.Unit
		rk := right.OrgUnit + "\x00" + right.RoleOrSkillRef + "\x00" + right.Bucket.Key + "\x00" + right.Unit
		return lk < rk
	})
}

func aggregationScope(s DemandSignal) string {
	if s.OrgUnit != "" {
		return s.OrgUnit
	}
	return s.Location
}

func aggregationTarget(s DemandSignal) string {
	if s.RoleOrSkillRef != "" {
		return s.RoleOrSkillRef
	}
	if s.Role != "" {
		return s.Role
	}
	return s.Skill
}

func signalNaturalKey(s DemandSignal) string {
	return s.SourceRef + "\x00" + string(s.Work.Canonical())
}

func signalShapeDigest(s DemandSignal) string {
	copy := s
	copy.SignalID = "duplicate-natural-key"
	return canonicalbytes.Digest(copy.Canonical())
}

func confidence(s DemandSignal) (values.Decimal, error) {
	if s.Confidence.Validate() == nil {
		return s.Confidence, nil
	}
	switch s.ConfidenceClass {
	case ConfidenceLow:
		return values.NewDecimal("0.50", 2, values.RoundingHalfEven)
	case ConfidenceMedium:
		return values.NewDecimal("0.75", 2, values.RoundingHalfEven)
	case ConfidenceHigh:
		return values.NewDecimal("1.00", 2, values.RoundingHalfEven)
	case ConfidenceUnknown:
		return values.NewDecimal("0.00", 2, values.RoundingHalfEven)
	default:
		return values.Decimal{}, fmt.Errorf("%w: confidence is not declared", ErrAggregationInvalid)
	}
}

func weightedQuantity(s DemandSignal, rule BucketRule) (values.Quantity, error) {
	factor, err := confidence(s)
	if err != nil {
		return values.Quantity{}, err
	}
	weighted, err := s.Quantity.Value().Mul(factor, rule.ResultScale, rule.Rounding)
	if err != nil {
		return values.Quantity{}, fmt.Errorf("%w: confidence weighting: %v", ErrAggregationInvalid, err)
	}
	return values.NewQuantity(weighted.String(), s.Unit, rule.ResultScale, rule.Rounding)
}

func addQuantities(left, right values.Quantity) (values.Quantity, error) {
	added, err := left.Add(right)
	if err != nil {
		return values.Quantity{}, fmt.Errorf("%w: %v", ErrAggregationUnitMismatch, err)
	}
	return added, nil
}

// AggregateSignals validates, de-duplicates, buckets, and confidence-weights
// demand signals. It performs no persistence or workforce mutation.
func AggregateSignals(signals []DemandSignal, rule BucketRule) (DemandAggregation, error) {
	if err := rule.Validate(); err != nil {
		return DemandAggregation{}, err
	}
	if len(signals) == 0 {
		return DemandAggregation{}, fmt.Errorf("%w: no demand signals", ErrAggregationInvalid)
	}
	unique := make(map[string]DemandSignal, len(signals))
	for i, signal := range signals {
		if err := signal.Validate(); err != nil {
			return DemandAggregation{}, fmt.Errorf("%w: signal %d: %v", ErrAggregationInvalid, i, err)
		}
		if strings.TrimSpace(signal.SourceRef) == "" {
			return DemandAggregation{}, fmt.Errorf("%w: signal %s", ErrSourceRefRequired, signal.SignalID)
		}
		if _, err := rule.Bucket(signal.Work); err != nil {
			return DemandAggregation{}, err
		}
		key := signalNaturalKey(signal)
		if previous, ok := unique[key]; ok {
			if signalShapeDigest(previous) != signalShapeDigest(signal) {
				return DemandAggregation{}, fmt.Errorf("%w: source %s and window %s", ErrDuplicateSignalConflict, signal.SourceRef, signal.Work)
			}
			if signal.SignalID < previous.SignalID {
				unique[key] = signal
			}
			continue
		}
		unique[key] = signal
	}
	bySource := make(map[string][]DemandSignal)
	for _, signal := range unique {
		bySource[signal.SourceRef] = append(bySource[signal.SourceRef], signal)
	}
	for source, sourceSignals := range bySource {
		sort.Slice(sourceSignals, func(i, j int) bool {
			return string(sourceSignals[i].Work.Canonical()) < string(sourceSignals[j].Work.Canonical())
		})
		for i := 1; i < len(sourceSignals); i++ {
			overlaps, err := sourceSignals[i-1].Work.Overlaps(sourceSignals[i].Work)
			if err != nil {
				return DemandAggregation{}, err
			}
			if overlaps {
				return DemandAggregation{}, fmt.Errorf("%w: source %s windows %s and %s", ErrOverlappingSourceWindows, source, sourceSignals[i-1].Work, sourceSignals[i].Work)
			}
		}
	}

	type aggregateState struct {
		item   DemandAggregate
		source map[string]int
	}
	states := make(map[string]*aggregateState)
	for _, signal := range unique {
		bucket, err := rule.Bucket(signal.Work)
		if err != nil {
			return DemandAggregation{}, err
		}
		weighted, err := weightedQuantity(signal, rule)
		if err != nil {
			return DemandAggregation{}, err
		}
		key := aggregationScope(signal) + "\x00" + aggregationTarget(signal) + "\x00" + bucket.Key
		state := states[key]
		if state == nil {
			state = &aggregateState{item: DemandAggregate{
				OrgUnit: aggregationScope(signal), RoleOrSkillRef: aggregationTarget(signal), Bucket: bucket,
				Unit: signal.Unit, WeightedQuantity: weighted, FormulaVersion: AggregationFormulaVersion, RuleVersion: rule.Version,
				SourceRefs: []string{}, SignalRefs: []string{}, Sources: []SourceContribution{}}, source: make(map[string]int)}
			states[key] = state
		} else if state.item.Unit != signal.Unit {
			return DemandAggregation{}, fmt.Errorf("%w: %s versus %s for %s", ErrAggregationUnitMismatch, state.item.Unit, signal.Unit, key)
		} else {
			state.item.WeightedQuantity, err = addQuantities(state.item.WeightedQuantity, weighted)
			if err != nil {
				return DemandAggregation{}, err
			}
		}
		state.item.SourceRefs = appendUnique(state.item.SourceRefs, signal.SourceRef)
		state.item.SignalRefs = appendUnique(state.item.SignalRefs, signal.SignalID)
		if sourceIndex, ok := state.source[signal.SourceRef]; ok {
			source := &state.item.Sources[sourceIndex]
			source.WeightedQuantity, err = addQuantities(source.WeightedQuantity, weighted)
			if err != nil {
				return DemandAggregation{}, err
			}
			source.SignalRefs = appendUnique(source.SignalRefs, signal.SignalID)
		} else {
			state.source[signal.SourceRef] = len(state.item.Sources)
			state.item.Sources = append(state.item.Sources, SourceContribution{SourceRef: signal.SourceRef, SignalRefs: []string{signal.SignalID}, WeightedQuantity: weighted})
		}
	}
	result := DemandAggregation{Rule: rule, FormulaVersion: AggregationFormulaVersion}
	for _, state := range states {
		sort.Strings(state.item.SourceRefs)
		sort.Strings(state.item.SignalRefs)
		for i := range state.item.Sources {
			sort.Strings(state.item.Sources[i].SignalRefs)
		}
		state.item.CanonicalDigest = canonicalbytes.Digest(state.item.body())
		result.Aggregates = append(result.Aggregates, state.item)
	}
	sortAggregates(result.Aggregates)
	result.CanonicalDigest = canonicalbytes.Digest(result.Canonical())
	return result, nil
}

func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

type AggregationRequest struct {
	Signals    []DemandSignal
	BucketRule BucketRule
}

func (r AggregationRequest) Validate() error { return r.BucketRule.Validate() }

func Aggregate(r AggregationRequest) (DemandAggregation, error) {
	return AggregateSignals(r.Signals, r.BucketRule)
}

// AggregationBucketExplanation omits individual signal and evidence contents
// while retaining the exact result needed to explain a planning calculation.
type AggregationBucketExplanation struct {
	OrgUnit        string
	RoleOrSkillRef string
	BucketKey      string
	Unit           string
	Total          string
	SourceCount    int
}

type AggregationExplanation struct {
	RuleID         string
	RuleVersion    string
	FormulaVersion string
	Buckets        []AggregationBucketExplanation
	ResultDigest   string
}

func (a DemandAggregation) Explain() (AggregationExplanation, error) {
	if err := a.Validate(); err != nil {
		return AggregationExplanation{}, err
	}
	x := AggregationExplanation{RuleID: a.Rule.ID, RuleVersion: a.Rule.Version, FormulaVersion: a.FormulaVersion, ResultDigest: a.CanonicalDigest}
	for _, item := range a.Aggregates {
		x.Buckets = append(x.Buckets, AggregationBucketExplanation{
			OrgUnit: item.OrgUnit, RoleOrSkillRef: item.RoleOrSkillRef, BucketKey: item.Bucket.Key,
			Unit: item.Unit, Total: item.WeightedQuantity.String(), SourceCount: len(item.Sources),
		})
	}
	return x, nil
}

func ExplainAggregation(a DemandAggregation) (AggregationExplanation, error) {
	return a.Explain()
}
