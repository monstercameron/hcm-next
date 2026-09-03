// Package abuse owns the publish-time contract for governed activity signals
// and the detectors that consume them.  It deliberately contains no scoring,
// accusation, persistence, or side effects: publication is a gate on the
// quality and governance metadata of a definition.
package abuse

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var (
	ErrIdentity        = errors.New("abuse: id and version are required")
	ErrPurpose         = errors.New("abuse: purpose is required")
	ErrFeatures        = errors.New("abuse: at least one feature is required")
	ErrSource          = errors.New("abuse: source and quality are required")
	ErrRetention       = errors.New("abuse: retention policy is required")
	ErrProtectedPolicy = errors.New("abuse: protected-attribute policy is required")
	ErrOwner           = errors.New("abuse: owner is required")
	ErrThreshold       = errors.New("abuse: threshold is required")
	ErrAction          = errors.New("abuse: action is required")
	ErrEvaluation      = errors.New("abuse: evaluation plan is required")
	ErrSignalBinding   = errors.New("abuse: detector must bind the published signal")
)

// RejectionCode is the stable wire-level code for a refused ABUSE-001
// publication. A publication is a preflight gate: a rejection must never
// create an authoritative row, event, outbox entry, human task, or provider
// request.
const RejectionCode = "ABUSE_001_REJECTED"

// ErrRejected is matched by every typed ABUSE-001 publication rejection.
var ErrRejected = errors.New(RejectionCode)

// Effects makes the zero-effect publication contract explicit to adapters.
type Effects struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWork         int
	ProviderRequests  int
}

func (e Effects) IsZero() bool { return e == Effects{} }

// Rejection names the offending contract field, state, and definition
// version. Cause remains available through errors.Is for existing callers.
type Rejection struct {
	Code, Field, State, Version string
	Effects                     Effects
	Cause                       error
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", r.Code, r.Field, r.State, r.Version)
}

func (r *Rejection) Unwrap() []error {
	if r.Cause == nil {
		return []error{ErrRejected}
	}
	return []error{ErrRejected, r.Cause}
}

func IsRejected(err error) bool { return errors.Is(err, ErrRejected) }

// Feature is a bounded, named input to a detector. Values and raw payloads
// never belong in a definition.
type Feature struct{ Name, Description string }

// Source declares where a signal is observed and the quality of that source.
type Source struct{ Name, Quality string }

// Threshold describes a deterministic trigger boundary. Value must be finite.
type Threshold struct {
	Metric string
	Value  float64
	Window string
}

// EvaluationPlan identifies how precision, recall and unknown outcomes are
// evaluated before a detector is allowed to publish.
type EvaluationPlan struct{ Method, Dataset, Metrics string }

// SignalDefinition is the governed declaration of an activity signal.
type SignalDefinition struct {
	ID, Version, Purpose     string
	Features                 []Feature
	Sources                  []Source
	Retention                string
	RetentionDays            int
	ProtectedAttributePolicy string
	Owner                    string
}

// DetectorDefinition binds a versioned detector to one or more signals.
type DetectorDefinition struct {
	ID, Version, Purpose     string
	SignalIDs                []string
	Features                 []Feature
	Sources                  []Source
	Retention                string
	RetentionDays            int
	ProtectedAttributePolicy string
	Owner                    string
	Threshold                Threshold
	Action                   string
	Evaluation               EvaluationPlan
}

// Revision is an immutable publication receipt. Digest changes whenever the
// definition or detector metadata changes, so historical evaluations can cite
// exactly what was published.
type Revision struct {
	ID, Version, Digest string
	Effects             Effects
}

func rejection(err error, field, state, version string) error {
	return &Rejection{Code: RejectionCode, Field: field, State: state, Version: version, Effects: Effects{}, Cause: err}
}

func classify(err error) (field, state string) {
	switch {
	case errors.Is(err, ErrIdentity):
		return "identity", "MISSING_OR_INVALID"
	case errors.Is(err, ErrPurpose):
		return "purpose", "MISSING"
	case errors.Is(err, ErrFeatures):
		return "features", "MISSING_OR_INVALID"
	case errors.Is(err, ErrSource):
		return "source", "MISSING_OR_INVALID"
	case errors.Is(err, ErrRetention):
		return "retention", "MISSING_OR_INVALID"
	case errors.Is(err, ErrProtectedPolicy):
		return "protected_attribute_policy", "MISSING"
	case errors.Is(err, ErrOwner):
		return "owner", "MISSING"
	case errors.Is(err, ErrThreshold):
		return "threshold", "MISSING_OR_INVALID"
	case errors.Is(err, ErrAction):
		return "action", "MISSING"
	case errors.Is(err, ErrEvaluation):
		return "evaluation", "MISSING_OR_INVALID"
	case errors.Is(err, ErrSignalBinding):
		return "signal_ids", "UNBOUND"
	default:
		return "publication", "INVALID"
	}
}

func validateCommon(id, version, purpose string, features []Feature, sources []Source, retention string, retentionDays int, protected, owner string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" {
		return ErrIdentity
	}
	if strings.TrimSpace(purpose) == "" {
		return ErrPurpose
	}
	if len(features) == 0 {
		return ErrFeatures
	}
	for _, f := range features {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Description) == "" {
			return fmt.Errorf("%w: feature", ErrFeatures)
		}
	}
	if len(sources) == 0 {
		return ErrSource
	}
	for _, s := range sources {
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Quality) == "" {
			return fmt.Errorf("%w: source", ErrSource)
		}
	}
	if strings.TrimSpace(retention) == "" && retentionDays <= 0 {
		return ErrRetention
	}
	if retentionDays < 0 {
		return ErrRetention
	}
	if strings.TrimSpace(protected) == "" {
		return ErrProtectedPolicy
	}
	if strings.TrimSpace(owner) == "" {
		return ErrOwner
	}
	return nil
}

func (s SignalDefinition) Validate() error {
	return validateCommon(s.ID, s.Version, s.Purpose, s.Features, s.Sources, s.Retention, s.RetentionDays, s.ProtectedAttributePolicy, s.Owner)
}

func (d DetectorDefinition) Validate() error {
	if err := validateCommon(d.ID, d.Version, d.Purpose, d.Features, d.Sources, d.Retention, d.RetentionDays, d.ProtectedAttributePolicy, d.Owner); err != nil {
		return err
	}
	if len(d.SignalIDs) == 0 {
		return fmt.Errorf("%w: signal ids", ErrSource)
	}
	for _, id := range d.SignalIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: signal id", ErrSource)
		}
	}
	if strings.TrimSpace(d.Threshold.Metric) == "" || strings.TrimSpace(d.Threshold.Window) == "" || math.IsNaN(d.Threshold.Value) || math.IsInf(d.Threshold.Value, 0) {
		return ErrThreshold
	}
	if strings.TrimSpace(d.Action) == "" {
		return ErrAction
	}
	if strings.TrimSpace(d.Evaluation.Method) == "" || strings.TrimSpace(d.Evaluation.Dataset) == "" || strings.TrimSpace(d.Evaluation.Metrics) == "" {
		return ErrEvaluation
	}
	return nil
}

func digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

// Publish validates and returns a receipt without creating any side effect.
func Publish(s SignalDefinition, d DetectorDefinition) (Revision, error) {
	if err := s.Validate(); err != nil {
		field, state := classify(err)
		return Revision{}, rejection(err, field, state, d.Version)
	}
	if err := d.Validate(); err != nil {
		field, state := classify(err)
		return Revision{}, rejection(err, field, state, d.Version)
	}
	bound := false
	for _, id := range d.SignalIDs {
		if id == s.ID {
			bound = true
			break
		}
	}
	if !bound {
		return Revision{}, rejection(ErrSignalBinding, "signal_ids", "UNBOUND", d.Version)
	}
	// Canonicalize all unordered definition collections only in local copies;
	// callers retain ownership of their definitions and publication is immutable.
	s.Features = append([]Feature(nil), s.Features...)
	sort.Slice(s.Features, func(i, j int) bool { return s.Features[i].Name < s.Features[j].Name })
	s.Sources = append([]Source(nil), s.Sources...)
	sort.Slice(s.Sources, func(i, j int) bool { return s.Sources[i].Name < s.Sources[j].Name })
	ids := append([]string(nil), d.SignalIDs...)
	sort.Strings(ids)
	d.SignalIDs = ids
	features := append([]Feature(nil), d.Features...)
	sort.Slice(features, func(i, j int) bool { return features[i].Name < features[j].Name })
	d.Features = features
	sources := append([]Source(nil), d.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	d.Sources = sources
	h, err := digest(struct {
		Signal   SignalDefinition
		Detector DetectorDefinition
	}{s, d})
	if err != nil {
		return Revision{}, err
	}
	return Revision{ID: d.ID, Version: d.Version, Digest: h, Effects: Effects{}}, nil
}
