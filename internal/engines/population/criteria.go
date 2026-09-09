// POP-002: compile typed population criteria into a bounded, deterministic
// plan. The compiler is the only place that decides whether a predicate tree
// is resolvable at all; Resolve (POP-003) never re-derives that decision.
package population

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Compiler limits. A criteria tree that exceeds either is an unbounded
// traversal, not a large-but-legal query: an engine that silently walked an
// unbounded tree could be made to do unbounded work by an unbounded input.
const (
	// MaxPredicateDepth bounds AND/OR/NOT nesting.
	MaxPredicateDepth = 8
	// MaxPredicateNodes bounds the total number of predicate nodes.
	MaxPredicateNodes = 256
)

// Compiler errors. All are matchable with errors.Is.
var (
	// ErrUnresolvedField is returned when a leaf predicate names a field the
	// supplied FieldCatalog does not declare.
	ErrUnresolvedField = errors.New("population: criteria references an unresolved field")
	// ErrUnauthorizedInference is returned when a leaf predicate names a field
	// the catalog declares but marks not queryable: comparing on it would let a
	// caller infer a restricted fact through population membership rather than
	// through an authorized read.
	ErrUnauthorizedInference = errors.New("population: criteria references a field that is not population-queryable")
	// ErrUnboundedTraversal is returned when the tree exceeds the declared
	// depth or node-count bound.
	ErrUnboundedTraversal = errors.New("population: criteria exceeds the bounded depth or node limit")
	// ErrOperatorType is returned when an operator is applied to a field type
	// it is not defined for (e.g. a relational compare on a string field).
	ErrOperatorType = errors.New("population: operator is not defined for the field's declared type")
	// ErrPredicateShape is returned when a predicate node is malformed: a leaf
	// with children, a composite with none, or an unknown/unspecified kind.
	ErrPredicateShape = errors.New("population: criteria predicate is malformed")
)

// FieldType is the declared comparison type of a catalog field. A field's
// type determines which operators are legal against it and how its literal
// comparison values are parsed.
type FieldType uint8

// Field types.
const (
	FieldTypeUnspecified FieldType = iota
	FieldTypeString
	FieldTypeDecimal
	FieldTypeDate
	FieldTypeBool
)

var fieldTypeWire = map[FieldType]string{
	FieldTypeString:  "STRING",
	FieldTypeDecimal: "DECIMAL",
	FieldTypeDate:    "DATE",
	FieldTypeBool:    "BOOL",
}

// Valid reports whether t is a declared field type.
func (t FieldType) Valid() bool { return fieldTypeWire[t] != "" }

// String returns the wire token, or FIELD_TYPE_UNSPECIFIED.
func (t FieldType) String() string {
	if s, ok := fieldTypeWire[t]; ok {
		return s
	}
	return "FIELD_TYPE_UNSPECIFIED"
}

// FieldDescriptor is one catalog entry: a resolvable, typed fact field a
// criteria predicate may compare against.
type FieldDescriptor struct {
	Type FieldType
	// Queryable declares whether the field may be used to determine population
	// membership at all. A field can be readable elsewhere in the system and
	// still be excluded here, because deriving membership from it would let an
	// otherwise-permitted field create an impermissible sensitive population.
	Queryable bool
	// Sensitive marks a queryable field whose use elevates the compiled plan's
	// declared privacy risk, so a caller reviewing the plan sees it without
	// having to read every predicate.
	Sensitive bool
	// IndexHint names the index a resolver should prefer for this field. It is
	// advisory only; Resolve does not require an index to exist.
	IndexHint string
}

// FieldCatalog is the closed set of fields a criteria tree may reference. It
// is supplied by the caller compiling the criteria (never invented by this
// package) so the compiled boundary always matches one declared version of
// the fact model.
type FieldCatalog map[string]FieldDescriptor

// PredicateKind names one node in a criteria tree.
type PredicateKind uint8

// Predicate kinds.
const (
	PredicateUnspecified PredicateKind = iota
	PredicateEquals
	PredicateNotEquals
	PredicateIn
	PredicateGreaterThan
	PredicateLessThan
	PredicateAnd
	PredicateOr
	PredicateNot
)

var predicateKindWire = map[PredicateKind]string{
	PredicateEquals:      "EQUALS",
	PredicateNotEquals:   "NOT_EQUALS",
	PredicateIn:          "IN",
	PredicateGreaterThan: "GREATER_THAN",
	PredicateLessThan:    "LESS_THAN",
	PredicateAnd:         "AND",
	PredicateOr:          "OR",
	PredicateNot:         "NOT",
}

// Valid reports whether k is a legal predicate kind.
func (k PredicateKind) Valid() bool { return predicateKindWire[k] != "" }

// String returns the wire token, or PREDICATE_UNSPECIFIED.
func (k PredicateKind) String() string {
	if s, ok := predicateKindWire[k]; ok {
		return s
	}
	return "PREDICATE_UNSPECIFIED"
}

func (k PredicateKind) isComposite() bool {
	return k == PredicateAnd || k == PredicateOr || k == PredicateNot
}

// Predicate is one typed node of a criteria tree: either a leaf comparison
// against a catalog field, or a composite of child predicates. There is no
// free-form query string anywhere in the shape: a caller that wants to build
// criteria from text must parse it into this typed tree before it reaches the
// engine.
type Predicate struct {
	Kind PredicateKind
	// Field and Values are set for leaf predicates only.
	Field  string
	Values []string
	// Children is set for composite predicates only.
	Children []Predicate
}

// Criteria is the typed root of a population's membership predicate. The zero
// Criteria is invalid: a definition must carry an actual predicate tree, not
// an absent one standing in for "everyone".
type Criteria struct {
	Root Predicate
}

// Validate reports whether the criteria is a well-formed, non-empty typed
// tree. It does not check field resolution: that is Compile's job, because
// resolution requires a FieldCatalog the bare Criteria does not carry.
func (c Criteria) Validate() error {
	if c.Root.Kind == PredicateUnspecified {
		return fmt.Errorf("%w: empty criteria", ErrDefinitionCriteria)
	}
	return validatePredicateShape(c.Root)
}

func validatePredicateShape(p Predicate) error {
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: kind %d", ErrPredicateShape, uint8(p.Kind))
	}
	if p.Kind.isComposite() {
		if p.Field != "" || len(p.Values) != 0 {
			return fmt.Errorf("%w: composite %s carries leaf fields", ErrPredicateShape, p.Kind)
		}
		if len(p.Children) == 0 {
			return fmt.Errorf("%w: composite %s has no children", ErrPredicateShape, p.Kind)
		}
		if p.Kind == PredicateNot && len(p.Children) != 1 {
			return fmt.Errorf("%w: NOT must have exactly one child", ErrPredicateShape)
		}
		for _, child := range p.Children {
			if err := validatePredicateShape(child); err != nil {
				return err
			}
		}
		return nil
	}
	if p.Field == "" {
		return fmt.Errorf("%w: leaf %s has no field", ErrPredicateShape, p.Kind)
	}
	if len(p.Children) != 0 {
		return fmt.Errorf("%w: leaf %s carries children", ErrPredicateShape, p.Kind)
	}
	if p.Kind == PredicateIn {
		if len(p.Values) == 0 {
			return fmt.Errorf("%w: IN has no values", ErrPredicateShape)
		}
	} else if len(p.Values) != 1 {
		return fmt.Errorf("%w: %s requires exactly one value", ErrPredicateShape, p.Kind)
	}
	return nil
}

// Canonical returns the canonical byte encoding of the criteria, or nil when
// it fails Validate.
func (c Criteria) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.engines.population.Criteria", schemaVersion)
	writePredicate(w, c.Root)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func writePredicate(w *canonicalbytes.Writer, p Predicate) {
	w.String("kind", p.Kind.String())
	w.String("field", p.Field)
	w.Count("values", len(p.Values))
	for _, v := range p.Values {
		w.String("value", v)
	}
	w.Count("children", len(p.Children))
	for _, child := range p.Children {
		writePredicate(w, child)
	}
}

// PrivacyRisk is the compiled plan's declared privacy sensitivity, surfaced so
// a reviewer or a POP-004 restriction decision can see it without re-walking
// the tree.
type PrivacyRisk uint8

// Privacy risk levels.
const (
	PrivacyRiskUnspecified PrivacyRisk = iota
	PrivacyRiskStandard
	PrivacyRiskElevated
)

// String returns the wire token.
func (r PrivacyRisk) String() string {
	switch r {
	case PrivacyRiskStandard:
		return "STANDARD"
	case PrivacyRiskElevated:
		return "ELEVATED"
	default:
		return "PRIVACY_RISK_UNSPECIFIED"
	}
}

// CompiledPlan is the deterministic, typed output of Compile: the exact fields
// read, the index hints those reads should prefer, the declared privacy risk
// and a canonical digest binding all of it.
type CompiledPlan struct {
	Criteria    Criteria
	FieldsRead  []string
	Fields      map[string]FieldDescriptor
	IndexHints  []string
	PrivacyRisk PrivacyRisk
	Digest      string
}

// Compile resolves criteria against catalog, rejecting any predicate that
// names an unresolved or non-queryable field, and any tree that exceeds the
// bounded depth or node count. A successful compile is a total description of
// what Resolve will read: nothing outside FieldsRead is ever consulted.
func Compile(criteria Criteria, catalog FieldCatalog) (CompiledPlan, error) {
	if err := criteria.Validate(); err != nil {
		return CompiledPlan{}, err
	}
	nodes := 0
	fields := map[string]FieldDescriptor{}
	if err := walkPredicate(criteria.Root, catalog, 1, &nodes, fields); err != nil {
		return CompiledPlan{}, err
	}

	fieldNames := make([]string, 0, len(fields))
	indexHints := map[string]bool{}
	risk := PrivacyRiskStandard
	for name, desc := range fields {
		fieldNames = append(fieldNames, name)
		if desc.IndexHint != "" {
			indexHints[desc.IndexHint] = true
		}
		if desc.Sensitive {
			risk = PrivacyRiskElevated
		}
	}
	sort.Strings(fieldNames)
	hints := make([]string, 0, len(indexHints))
	for h := range indexHints {
		hints = append(hints, h)
	}
	sort.Strings(hints)

	w := canonicalbytes.New(planSchema, schemaVersion).
		Value("criteria", criteria).
		SortedStrings("fields_read", fieldNames).
		SortedStrings("index_hints", hints).
		String("privacy_risk", risk.String())
	digest, err := w.Digest()
	if err != nil {
		return CompiledPlan{}, fmt.Errorf("population: compile digest: %w", err)
	}

	fieldsCopy := make(map[string]FieldDescriptor, len(fields))
	for k, v := range fields {
		fieldsCopy[k] = v
	}

	return CompiledPlan{
		Criteria:    criteria,
		FieldsRead:  fieldNames,
		Fields:      fieldsCopy,
		IndexHints:  hints,
		PrivacyRisk: risk,
		Digest:      digest,
	}, nil
}

func walkPredicate(p Predicate, catalog FieldCatalog, depth int, nodes *int, fields map[string]FieldDescriptor) error {
	*nodes++
	if depth > MaxPredicateDepth || *nodes > MaxPredicateNodes {
		return fmt.Errorf("%w: depth %d, nodes %d", ErrUnboundedTraversal, depth, *nodes)
	}
	if p.Kind.isComposite() {
		for _, child := range p.Children {
			if err := walkPredicate(child, catalog, depth+1, nodes, fields); err != nil {
				return err
			}
		}
		return nil
	}
	desc, ok := catalog[p.Field]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnresolvedField, p.Field)
	}
	if !desc.Queryable {
		return fmt.Errorf("%w: %q", ErrUnauthorizedInference, p.Field)
	}
	if (p.Kind == PredicateGreaterThan || p.Kind == PredicateLessThan) && desc.Type != FieldTypeDecimal && desc.Type != FieldTypeDate {
		return fmt.Errorf("%w: %q is %s", ErrOperatorType, p.Field, desc.Type)
	}
	fields[p.Field] = desc
	return nil
}

// evaluateLeaf compares a resolved fact text value against a leaf predicate.
// It is used by Resolve (POP-003), kept here because it is a pure function of
// the compiled predicate shape and the catalog's declared field type.
func evaluateLeaf(p Predicate, desc FieldDescriptor, factText string) (bool, error) {
	switch p.Kind {
	case PredicateEquals:
		return compareEqual(desc, factText, p.Values[0])
	case PredicateNotEquals:
		eq, err := compareEqual(desc, factText, p.Values[0])
		if err != nil {
			return false, err
		}
		return !eq, nil
	case PredicateIn:
		for _, v := range p.Values {
			eq, err := compareEqual(desc, factText, v)
			if err != nil {
				return false, err
			}
			if eq {
				return true, nil
			}
		}
		return false, nil
	case PredicateGreaterThan, PredicateLessThan:
		return compareOrdered(p.Kind, desc, factText, p.Values[0])
	default:
		return false, fmt.Errorf("%w: %s has no evaluator", ErrPredicateShape, p.Kind)
	}
}

func compareEqual(desc FieldDescriptor, a, b string) (bool, error) {
	switch desc.Type {
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

// decimalCompareScale is the scale every decimal criteria comparison is
// normalized to. It is generous enough for any P1A fact and declared once so
// two comparisons against the same field are never at silently different
// scales.
const decimalCompareScale int32 = 6

func compareOrdered(kind PredicateKind, desc FieldDescriptor, a, b string) (bool, error) {
	switch desc.Type {
	case FieldTypeDecimal:
		da, err := values.NewDecimal(a, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		db, err := values.NewDecimal(b, decimalCompareScale, values.RoundingHalfEven)
		if err != nil {
			return false, err
		}
		if kind == PredicateGreaterThan {
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
		if kind == PredicateGreaterThan {
			return da.Compare(db) > 0, nil
		}
		return da.Compare(db) < 0, nil
	default:
		return false, fmt.Errorf("%w: %s is not ordered", ErrOperatorType, desc.Type)
	}
}
