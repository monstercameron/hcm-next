// ELIG-002: compile bounded eligibility criteria. A criteria tree is a
// composition of typed leaves - a fact comparison or a pinned rule reference -
// under AND/OR/NOT. The tree shape makes a cycle structurally impossible
// (a Go value tree cannot reference itself), bounds the same way
// population's compiler bounds a predicate tree, and rejects any leaf whose
// field or rule is not declared in the catalogs a caller supplies: there is
// no read this package can perform that is not named in the compiled plan.
package eligibility

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Bounds mirror population's: a criteria tree that exceeds either is an
// unbounded traversal, not a large-but-legal one.
const (
	MaxConditionDepth = 8
	MaxConditionNodes = 256
)

// Compiler errors. All are matchable with errors.Is.
var (
	ErrUnresolvedFact  = errors.New("eligibility: criteria references an unresolved fact field")
	ErrFactNotReadable = errors.New("eligibility: criteria references a fact field that is not eligibility-readable")
	ErrUnresolvedRule  = errors.New("eligibility: criteria references an unregistered rule")
	ErrRuleUnpinned    = errors.New("eligibility: criteria pins a rule version that does not match the registered version")
	ErrUnboundedTree   = errors.New("eligibility: criteria exceeds the bounded depth or node limit")
	ErrConditionShape  = errors.New("eligibility: criteria condition is malformed")
	ErrOperatorType    = errors.New("eligibility: operator is not defined for the field's declared type")
)

// FieldType is a fact field's declared comparison type.
type FieldType uint8

// Field types.
const (
	FieldTypeUnspecified FieldType = iota
	FieldTypeString
	FieldTypeDecimal
	FieldTypeDate
)

var fieldTypeWire = map[FieldType]string{
	FieldTypeString:  "STRING",
	FieldTypeDecimal: "DECIMAL",
	FieldTypeDate:    "DATE",
}

// String returns the wire token.
func (t FieldType) String() string {
	if s, ok := fieldTypeWire[t]; ok {
		return s
	}
	return "FIELD_TYPE_UNSPECIFIED"
}

// FactDescriptor is one catalog entry for a fact field a condition leaf may
// compare against.
type FactDescriptor struct {
	Type     FieldType
	Readable bool
}

// FactCatalog is the closed set of fact fields a criteria tree may reference.
type FactCatalog map[string]FactDescriptor

// RuleDescriptor is one catalog entry for a rule a condition leaf may cite. A
// leaf must pin the exact version this descriptor names: an unpinned or
// stale rule reference cannot be compiled, because a rule's meaning can
// change between versions.
type RuleDescriptor struct {
	Version string
}

// RuleCatalog is the closed set of rules a criteria tree may reference.
type RuleCatalog map[string]RuleDescriptor

// ConditionKind names one node in a criteria tree.
type ConditionKind uint8

// Condition kinds.
const (
	ConditionUnspecified ConditionKind = iota
	ConditionEquals
	ConditionNotEquals
	ConditionGreaterThan
	ConditionLessThan
	ConditionRule
	ConditionAnd
	ConditionOr
	ConditionNot
)

var conditionKindWire = map[ConditionKind]string{
	ConditionEquals:      "EQUALS",
	ConditionNotEquals:   "NOT_EQUALS",
	ConditionGreaterThan: "GREATER_THAN",
	ConditionLessThan:    "LESS_THAN",
	ConditionRule:        "RULE",
	ConditionAnd:         "AND",
	ConditionOr:          "OR",
	ConditionNot:         "NOT",
}

// Valid reports whether k is a legal condition kind.
func (k ConditionKind) Valid() bool { return conditionKindWire[k] != "" }

// String returns the wire token.
func (k ConditionKind) String() string {
	if s, ok := conditionKindWire[k]; ok {
		return s
	}
	return "CONDITION_UNSPECIFIED"
}

func (k ConditionKind) isComposite() bool {
	return k == ConditionAnd || k == ConditionOr || k == ConditionNot
}

func (k ConditionKind) isFactLeaf() bool {
	return k == ConditionEquals || k == ConditionNotEquals || k == ConditionGreaterThan || k == ConditionLessThan
}

// Condition is one typed node of a criteria tree. A fact leaf compares Field
// against Value; a rule leaf cites RuleID pinned at RuleVersion; a composite
// combines Children. There is no free-form expression anywhere in the shape.
type Condition struct {
	Kind ConditionKind

	Field string
	Value string

	RuleID      string
	RuleVersion string

	Children []Condition
}

// Criteria is the typed root of an eligibility request's qualifying
// condition.
type Criteria struct {
	Root Condition
}

// Validate reports whether the criteria is a well-formed, non-empty typed
// tree. It does not check catalog resolution: that is Compile's job.
func (c Criteria) Validate() error {
	if c.Root.Kind == ConditionUnspecified {
		return fmt.Errorf("%w: empty criteria", ErrConditionShape)
	}
	return validateShape(c.Root)
}

func validateShape(c Condition) error {
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: kind %d", ErrConditionShape, uint8(c.Kind))
	}
	if c.Kind.isComposite() {
		if c.Field != "" || c.Value != "" || c.RuleID != "" || c.RuleVersion != "" {
			return fmt.Errorf("%w: composite %s carries leaf fields", ErrConditionShape, c.Kind)
		}
		if len(c.Children) == 0 {
			return fmt.Errorf("%w: composite %s has no children", ErrConditionShape, c.Kind)
		}
		if c.Kind == ConditionNot && len(c.Children) != 1 {
			return fmt.Errorf("%w: NOT must have exactly one child", ErrConditionShape)
		}
		for _, child := range c.Children {
			if err := validateShape(child); err != nil {
				return err
			}
		}
		return nil
	}
	if len(c.Children) != 0 {
		return fmt.Errorf("%w: leaf %s carries children", ErrConditionShape, c.Kind)
	}
	if c.Kind.isFactLeaf() {
		if c.Field == "" || c.Value == "" {
			return fmt.Errorf("%w: fact leaf %s requires field and value", ErrConditionShape, c.Kind)
		}
		if c.RuleID != "" || c.RuleVersion != "" {
			return fmt.Errorf("%w: fact leaf %s carries rule fields", ErrConditionShape, c.Kind)
		}
		return nil
	}
	// ConditionRule
	if c.RuleID == "" || c.RuleVersion == "" {
		return fmt.Errorf("%w: rule leaf requires a pinned id and version", ErrConditionShape)
	}
	if c.Field != "" || c.Value != "" {
		return fmt.Errorf("%w: rule leaf carries fact fields", ErrConditionShape)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c Criteria) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.engines.eligibility.Criteria", schemaVersion)
	writeCondition(w, c.Root)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func writeCondition(w *canonicalbytes.Writer, c Condition) {
	w.String("kind", c.Kind.String())
	w.String("field", c.Field)
	w.String("value", c.Value)
	w.String("rule_id", c.RuleID)
	w.String("rule_version", c.RuleVersion)
	w.Count("children", len(c.Children))
	for _, child := range c.Children {
		writeCondition(w, child)
	}
}

// CompiledPlan is the deterministic, typed output of Compile.
type CompiledPlan struct {
	Criteria    Criteria
	FactsRead   []string
	Rules       []RuleDescriptorRef
	Obligations []ObligationTemplate
	Digest      string
	facts       map[string]FactDescriptor
	rules       map[string]RuleDescriptor
}

// RuleDescriptorRef names one pinned rule the plan reads.
type RuleDescriptorRef struct {
	RuleID  string
	Version string
}

// ObligationTemplate is the obligation a rule leaf contributes when that
// rule's outcome is PARTIAL rather than a clean PASS/FAIL.
type ObligationTemplate struct {
	RuleID string
	Reason ObligationReason
}

// Compile resolves criteria against catalogs, rejecting any leaf that names
// an unresolved or non-readable fact, an unregistered or unpinned rule, or
// any tree that exceeds the bounded depth or node count.
func Compile(criteria Criteria, facts FactCatalog, rules RuleCatalog) (CompiledPlan, error) {
	if err := criteria.Validate(); err != nil {
		return CompiledPlan{}, err
	}
	nodes := 0
	usedFacts := map[string]FactDescriptor{}
	usedRules := map[string]RuleDescriptor{}
	if err := walkCondition(criteria.Root, facts, rules, 1, &nodes, usedFacts, usedRules); err != nil {
		return CompiledPlan{}, err
	}

	factNames := make([]string, 0, len(usedFacts))
	for f := range usedFacts {
		factNames = append(factNames, f)
	}
	sort.Strings(factNames)

	ruleRefs := make([]RuleDescriptorRef, 0, len(usedRules))
	obligations := make([]ObligationTemplate, 0, len(usedRules))
	for id, desc := range usedRules {
		ruleRefs = append(ruleRefs, RuleDescriptorRef{RuleID: id, Version: desc.Version})
		obligations = append(obligations, ObligationTemplate{RuleID: id, Reason: ObligationRulePartial})
	}
	sort.Slice(ruleRefs, func(i, j int) bool { return ruleRefs[i].RuleID < ruleRefs[j].RuleID })
	sort.Slice(obligations, func(i, j int) bool { return obligations[i].RuleID < obligations[j].RuleID })

	w := writerFor("hcmnext.engines.eligibility.CompiledPlan").
		Value("criteria", criteria).
		SortedStrings("facts_read", factNames)
	w.Count("rules", len(ruleRefs))
	for _, r := range ruleRefs {
		w.String("rule.id", r.RuleID)
		w.String("rule.version", r.Version)
	}
	digest, err := w.Digest()
	if err != nil {
		return CompiledPlan{}, fmt.Errorf("eligibility: compile digest: %w", err)
	}

	factsCopy := make(map[string]FactDescriptor, len(usedFacts))
	for k, v := range usedFacts {
		factsCopy[k] = v
	}
	rulesCopy := make(map[string]RuleDescriptor, len(usedRules))
	for k, v := range usedRules {
		rulesCopy[k] = v
	}

	return CompiledPlan{
		Criteria:    criteria,
		FactsRead:   factNames,
		Rules:       ruleRefs,
		Obligations: obligations,
		Digest:      digest,
		facts:       factsCopy,
		rules:       rulesCopy,
	}, nil
}

func walkCondition(c Condition, facts FactCatalog, rules RuleCatalog, depth int, nodes *int, usedFacts map[string]FactDescriptor, usedRules map[string]RuleDescriptor) error {
	*nodes++
	if depth > MaxConditionDepth || *nodes > MaxConditionNodes {
		return fmt.Errorf("%w: depth %d, nodes %d", ErrUnboundedTree, depth, *nodes)
	}
	if c.Kind.isComposite() {
		for _, child := range c.Children {
			if err := walkCondition(child, facts, rules, depth+1, nodes, usedFacts, usedRules); err != nil {
				return err
			}
		}
		return nil
	}
	if c.Kind.isFactLeaf() {
		desc, ok := facts[c.Field]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnresolvedFact, c.Field)
		}
		if !desc.Readable {
			return fmt.Errorf("%w: %q", ErrFactNotReadable, c.Field)
		}
		if (c.Kind == ConditionGreaterThan || c.Kind == ConditionLessThan) && desc.Type != FieldTypeDecimal && desc.Type != FieldTypeDate {
			return fmt.Errorf("%w: %q is %s", ErrOperatorType, c.Field, desc.Type)
		}
		usedFacts[c.Field] = desc
		return nil
	}
	// ConditionRule
	desc, ok := rules[c.RuleID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnresolvedRule, c.RuleID)
	}
	if desc.Version != c.RuleVersion {
		return fmt.Errorf("%w: %q pinned %q, registered %q", ErrRuleUnpinned, c.RuleID, c.RuleVersion, desc.Version)
	}
	usedRules[c.RuleID] = desc
	return nil
}

// compareEqual and compareOrdered are shared by Compile-time type checking
// context and by Evaluate's leaf comparisons.
func compareEqual(t FieldType, a, b string) (bool, error) {
	switch t {
	case FieldTypeDecimal:
		da, err := values.NewDecimal(a, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		db, err := values.NewDecimal(b, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		return da.Cmp(db) == 0, nil
	case FieldTypeDate:
		da, err := values.ParseLocalDate(a)
		if err != nil {
			return false, err
		}
		db, err := values.ParseLocalDate(b)
		if err != nil {
			return false, err
		}
		return da.Compare(db) == 0, nil
	default:
		return a == b, nil
	}
}

const decimalCompareScale int32 = 6

func compareOrdered(kind ConditionKind, t FieldType, a, b string) (bool, error) {
	switch t {
	case FieldTypeDecimal:
		da, err := values.NewDecimal(a, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		db, err := values.NewDecimal(b, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		if kind == ConditionGreaterThan {
			return da.Cmp(db) > 0, nil
		}
		return da.Cmp(db) < 0, nil
	case FieldTypeDate:
		da, err := values.ParseLocalDate(a)
		if err != nil {
			return false, err
		}
		db, err := values.ParseLocalDate(b)
		if err != nil {
			return false, err
		}
		if kind == ConditionGreaterThan {
			return da.Compare(db) > 0, nil
		}
		return da.Compare(db) < 0, nil
	default:
		return false, fmt.Errorf("%w: %s is not ordered", ErrOperatorType, t)
	}
}
