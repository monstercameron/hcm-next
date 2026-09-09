// Package scenario owns immutable, descriptive scenario revisions. A scenario
// can describe hypothetical assumptions and lineage, but it cannot rewrite or
// stand in for authoritative domain facts.
package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's scenario vocabulary version.
func Version() int { return schemaVersion }

var (
	ErrInvalidScenario   = errors.New("scenario: invalid scenario revision")
	ErrMissingProvenance = errors.New("scenario: assumption provenance is required")
)

// Lifecycle is the closed lifecycle vocabulary for scenario revisions.
type Lifecycle string

const (
	LifecycleDraft   Lifecycle = "DRAFT"
	LifecycleReady   Lifecycle = "READY"
	LifecycleRetired Lifecycle = "RETIRED"
)

func (l Lifecycle) Valid() bool {
	return l == LifecycleDraft || l == LifecycleReady || l == LifecycleRetired
}

// ValueKind identifies the type carried by a typed assumption value.
type ValueKind string

const (
	ValueText    ValueKind = "TEXT"
	ValueDecimal ValueKind = "DECIMAL"
	ValueBoolean ValueKind = "BOOLEAN"
)

// TypedValue is a deliberately small, closed typed value vocabulary for
// assumptions. Decimal values use the kernel's exact decimal type.
type TypedValue struct {
	Kind    ValueKind
	Text    string
	Number  values.Decimal
	Boolean bool
}

func TextValue(text string) TypedValue { return TypedValue{Kind: ValueText, Text: text} }
func DecimalValue(number values.Decimal) TypedValue {
	return TypedValue{Kind: ValueDecimal, Number: number}
}
func BooleanValue(value bool) TypedValue { return TypedValue{Kind: ValueBoolean, Boolean: value} }

func (v TypedValue) Validate() error {
	switch v.Kind {
	case ValueText:
		if strings.TrimSpace(v.Text) == "" {
			return fmt.Errorf("%w: text value is empty", ErrInvalidScenario)
		}
		if v.Number.Validate() == nil || v.Boolean {
			return fmt.Errorf("%w: text value carries another type", ErrInvalidScenario)
		}
	case ValueDecimal:
		if err := v.Number.Validate(); err != nil {
			return fmt.Errorf("%w: decimal: %v", ErrInvalidScenario, err)
		}
	case ValueBoolean:
		if v.Text != "" || v.Number.Validate() == nil {
			return fmt.Errorf("%w: boolean value carries another type", ErrInvalidScenario)
		}
	default:
		return fmt.Errorf("%w: unknown value kind %q", ErrInvalidScenario, v.Kind)
	}
	return nil
}

func (v TypedValue) Canonical() []byte {
	if v.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.scenario.TypedValue", schemaVersion).String("kind", string(v.Kind))
	switch v.Kind {
	case ValueText:
		w.String("text", v.Text)
	case ValueDecimal:
		w.Value("number", v.Number)
	case ValueBoolean:
		w.Bool("boolean", v.Boolean)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Assumption is a typed hypothetical input. Every assumption cites at least
// one provenance reference; an ungrounded guess is refused.
type Assumption struct {
	Key            string
	Value          TypedValue
	Unit           string
	ProvenanceRefs []string
}

func (a Assumption) Validate() error {
	if strings.TrimSpace(a.Key) == "" || strings.TrimSpace(a.Unit) == "" {
		return fmt.Errorf("%w: assumption key and unit are required", ErrInvalidScenario)
	}
	if err := a.Value.Validate(); err != nil {
		return err
	}
	if len(a.ProvenanceRefs) == 0 {
		return ErrMissingProvenance
	}
	seen := make(map[string]struct{}, len(a.ProvenanceRefs))
	for _, ref := range a.ProvenanceRefs {
		if strings.TrimSpace(ref) == "" {
			return ErrMissingProvenance
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: duplicate provenance ref %q", ErrInvalidScenario, ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func (a Assumption) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	refs := append([]string(nil), a.ProvenanceRefs...)
	sort.Strings(refs)
	w := canonicalbytes.New("hcmnext.domains.scenario.Assumption", schemaVersion).
		String("key", a.Key).Value("value", a.Value).String("unit", a.Unit).SortedStrings("provenance_ref", refs)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ScenarioRevision is an immutable descriptive revision. ParentDigest binds a
// fork to the exact parent bytes, while BaselineSnapshotRef binds the facts
// the hypothetical scenario is based on.
type ScenarioRevision struct {
	ScenarioID          string
	Revision            uint64
	ParentRevision      uint64
	ParentDigest        string
	Owner               string
	Scope               string
	Horizon             values.EffectiveInterval
	Assumptions         []Assumption
	BaselineSnapshotRef string
	Author              string
	AuthorityDisclaimer string
	Lifecycle           Lifecycle
	CanonicalDigest     string
}

func (s ScenarioRevision) Validate() error {
	if strings.TrimSpace(s.ScenarioID) == "" || s.Revision == 0 {
		return fmt.Errorf("%w: scenario id and non-zero revision are required", ErrInvalidScenario)
	}
	if s.Revision == 1 && (s.ParentRevision != 0 || s.ParentDigest != "") {
		return fmt.Errorf("%w: first revision cannot have a parent", ErrInvalidScenario)
	}
	if s.Revision > 1 && (s.ParentRevision == 0 || s.ParentRevision >= s.Revision || strings.TrimSpace(s.ParentDigest) == "") {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidScenario)
	}
	for name, field := range map[string]string{"owner": s.Owner, "scope": s.Scope, "baseline snapshot ref": s.BaselineSnapshotRef, "author": s.Author, "authority disclaimer": s.AuthorityDisclaimer} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidScenario, name)
		}
	}
	if err := s.Horizon.Validate(); err != nil {
		return fmt.Errorf("%w: horizon: %v", ErrInvalidScenario, err)
	}
	if !s.Lifecycle.Valid() {
		return fmt.Errorf("%w: lifecycle %q is not declared", ErrInvalidScenario, s.Lifecycle)
	}
	if len(s.Assumptions) == 0 {
		return fmt.Errorf("%w: at least one assumption is required", ErrInvalidScenario)
	}
	seen := make(map[string]struct{}, len(s.Assumptions))
	for _, assumption := range s.Assumptions {
		if err := assumption.Validate(); err != nil {
			return err
		}
		if _, ok := seen[assumption.Key]; ok {
			return fmt.Errorf("%w: duplicate assumption key %q", ErrInvalidScenario, assumption.Key)
		}
		seen[assumption.Key] = struct{}{}
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (s ScenarioRevision) body() []byte {
	assumptions := append([]Assumption(nil), s.Assumptions...)
	sort.Slice(assumptions, func(i, j int) bool { return assumptions[i].Key < assumptions[j].Key })
	w := canonicalbytes.New("hcmnext.domains.scenario.ScenarioRevision", schemaVersion).
		String("scenario_id", s.ScenarioID).Int("revision", int64(s.Revision)).Int("parent_revision", int64(s.ParentRevision)).
		String("parent_digest", s.ParentDigest).String("owner", s.Owner).String("scope", s.Scope).Value("horizon", s.Horizon).
		String("baseline_snapshot_ref", s.BaselineSnapshotRef).String("author", s.Author).
		String("authority_disclaimer", s.AuthorityDisclaimer).String("lifecycle", string(s.Lifecycle)).Count("assumptions", len(assumptions))
	for _, a := range assumptions {
		w.Value("assumption", a)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s ScenarioRevision) computedDigest() string {
	b := s.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewScenarioRevision validates, copies, and digests a revision.
func NewScenarioRevision(s ScenarioRevision) (ScenarioRevision, error) {
	s.Assumptions = cloneAssumptions(s.Assumptions)
	s.CanonicalDigest = s.computedDigest()
	if err := s.Validate(); err != nil {
		return ScenarioRevision{}, err
	}
	return s, nil
}

func cloneAssumptions(in []Assumption) []Assumption {
	out := make([]Assumption, len(in))
	copy(out, in)
	for i := range out {
		out[i].ProvenanceRefs = append([]string(nil), out[i].ProvenanceRefs...)
	}
	return out
}

// AssumptionsCopy returns detached assumptions for read-only consumers.
func (s ScenarioRevision) AssumptionsCopy() []Assumption { return cloneAssumptions(s.Assumptions) }
func (s ScenarioRevision) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}
func (s ScenarioRevision) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return s.computedDigest(), nil
}

// Fork creates the next immutable revision. supplied assumptions are deltas by
// key: an existing key is replaced and a new key is added. The parent remains
// unchanged and its digest is copied into the successor lineage.
func (s ScenarioRevision) Fork(supplied ...Assumption) (ScenarioRevision, error) {
	if err := s.Validate(); err != nil {
		return ScenarioRevision{}, err
	}
	merged := s.AssumptionsCopy()
	index := make(map[string]int, len(merged))
	for i, a := range merged {
		index[a.Key] = i
	}
	for _, a := range supplied {
		if err := a.Validate(); err != nil {
			return ScenarioRevision{}, err
		}
		if i, ok := index[a.Key]; ok {
			merged[i] = a
		} else {
			index[a.Key] = len(merged)
			merged = append(merged, a)
		}
	}
	return NewScenarioRevision(ScenarioRevision{
		ScenarioID: s.ScenarioID, Revision: s.Revision + 1, ParentRevision: s.Revision,
		ParentDigest: s.computedDigest(), Owner: s.Owner, Scope: s.Scope, Horizon: s.Horizon,
		Assumptions: merged, BaselineSnapshotRef: s.BaselineSnapshotRef, Author: s.Author,
		AuthorityDisclaimer: s.AuthorityDisclaimer, Lifecycle: LifecycleDraft,
	})
}

// ScenarioExplanation reports lineage and digest without granting authority.
type ScenarioExplanation struct {
	ScenarioID     string
	Revision       uint64
	ParentRevision uint64
	AssumptionKeys []string
	Digest         string
}

func (s ScenarioRevision) Explain() (ScenarioExplanation, error) {
	if err := s.Validate(); err != nil {
		return ScenarioExplanation{}, err
	}
	keys := make([]string, 0, len(s.Assumptions))
	for _, a := range s.Assumptions {
		keys = append(keys, a.Key)
	}
	sort.Strings(keys)
	return ScenarioExplanation{ScenarioID: s.ScenarioID, Revision: s.Revision, ParentRevision: s.ParentRevision, AssumptionKeys: keys, Digest: s.computedDigest()}, nil
}

// Explain returns a stable summary for audit and presentation consumers.
func Explain(s ScenarioRevision) (ScenarioExplanation, error) { return s.Explain() }
