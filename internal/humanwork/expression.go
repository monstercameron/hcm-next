package humanwork

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const (
	expressionSchema = "hcmnext.humanwork.ResolutionExpression"
	// MaxExpressionDepth bounds nesting. Four levels is enough for the deepest
	// shape the reference workflows need (quorum of any-of-role-or-group) and
	// short enough that a compiled expression can be read whole by a person.
	MaxExpressionDepth = 4
	// MaxExpressionNodes bounds total size. The point of both bounds is that
	// resolution cost is a property of the compiled expression, decided at
	// compile time, not a property of whatever the directory happens to hold.
	MaxExpressionNodes = 64
)

// ScopeKind is the organizational frame a term is resolved within. There is no
// "unscoped" kind: an unscoped role is exactly the defect the APPROVAL-001 RED
// clause names, so scope is required on every non-pinned term.
type ScopeKind string

// Scope kinds.
const (
	// ScopeUnspecified is the zero value and is never legal.
	ScopeUnspecified ScopeKind = ""
	// ScopeSubject frames the term on the worker the proposal is about, e.g.
	// ManagerOf(worker).
	ScopeSubject ScopeKind = "SUBJECT"
	// ScopeOrganization frames the term on an organization unit and its
	// descendants.
	ScopeOrganization ScopeKind = "ORGANIZATION"
	// ScopeLegalEntity frames the term on a legal entity.
	ScopeLegalEntity ScopeKind = "LEGAL_ENTITY"
	// ScopeCostCenter frames the term on a cost center.
	ScopeCostCenter ScopeKind = "COST_CENTER"
	// ScopeTenant frames the term on the whole tenant. It is the widest legal
	// frame and carries no reference.
	ScopeTenant ScopeKind = "GLOBAL_TENANT"
)

var scopeKindWire = map[ScopeKind]bool{
	ScopeSubject:      true,
	ScopeOrganization: true,
	ScopeLegalEntity:  true,
	ScopeCostCenter:   true,
	ScopeTenant:       true,
}

// Valid reports whether k is a declared scope kind.
func (k ScopeKind) Valid() bool { return scopeKindWire[k] }

// Scope is a scope kind plus the exact reference it frames. Every kind but
// ScopeTenant carries a reference; ScopeTenant carries none, so that "the whole
// tenant" can never be spelled by accidentally leaving a reference empty.
type Scope struct {
	Kind ScopeKind
	Ref  string
}

// Validate reports whether the scope is fully stated.
func (s Scope) Validate() error {
	if !s.Kind.Valid() {
		return newError("Validate", "scope.kind", ErrInvalidExpression,
			"scope kind %q is not declared", string(s.Kind))
	}
	if s.Kind == ScopeTenant {
		if s.Ref != "" {
			return newError("Validate", "scope.ref", ErrInvalidExpression,
				"GLOBAL_TENANT scope carries no reference, got %q", s.Ref)
		}
		return nil
	}
	if s.Ref == "" {
		return newError("Validate", "scope.ref", ErrInvalidExpression,
			"%s scope names no reference", string(s.Kind))
	}
	return nil
}

// Equal reports whether two scopes name the same frame.
func (s Scope) Equal(o Scope) bool { return s.Kind == o.Kind && s.Ref == o.Ref }

// String renders "KIND:ref", or "GLOBAL_TENANT".
func (s Scope) String() string {
	if s.Kind == ScopeTenant {
		return string(s.Kind)
	}
	return string(s.Kind) + ":" + s.Ref
}

// RelationshipKind is one of the fixed organizational relationships the
// language can resolve. It is a closed set on purpose: a new relationship is a
// reviewed addition here, never a string a tenant supplies.
type RelationshipKind string

// Relationship kinds.
const (
	// RelationshipUnspecified is the zero value and is never legal.
	RelationshipUnspecified RelationshipKind = ""
	// RelationshipManagerOf is the current manager of the scoped subject.
	RelationshipManagerOf RelationshipKind = "MANAGER_OF"
	// RelationshipManagerChainOf is every manager above the scoped subject.
	RelationshipManagerChainOf RelationshipKind = "MANAGER_CHAIN_OF"
	// RelationshipHRBPFor is the HR business partner for the scoped frame.
	RelationshipHRBPFor RelationshipKind = "HRBP_FOR"
	// RelationshipCompensationPartnerFor is the compensation partner for the frame.
	RelationshipCompensationPartnerFor RelationshipKind = "COMPENSATION_PARTNER_FOR"
	// RelationshipFinancePartnerFor is the finance partner for the frame.
	RelationshipFinancePartnerFor RelationshipKind = "FINANCE_PARTNER_FOR"
	// RelationshipLegalApproverFor is the legal approver for the frame.
	RelationshipLegalApproverFor RelationshipKind = "LEGAL_APPROVER_FOR"
)

var relationshipWire = map[RelationshipKind]bool{
	RelationshipManagerOf:              true,
	RelationshipManagerChainOf:         true,
	RelationshipHRBPFor:                true,
	RelationshipCompensationPartnerFor: true,
	RelationshipFinancePartnerFor:      true,
	RelationshipLegalApproverFor:       true,
}

// Valid reports whether k is a declared relationship kind.
func (k RelationshipKind) Valid() bool { return relationshipWire[k] }

// ExprKind is the node type of a resolution expression.
type ExprKind string

// Expression node kinds.
const (
	// ExprUnspecified is the zero value and is never legal.
	ExprUnspecified ExprKind = ""
	// ExprNamed pins one principal. It requires a policy reference, because a
	// pinned person with no policy behind them is a hardcoded approver.
	ExprNamed ExprKind = "NAMED"
	// ExprRole resolves the holders of a role within a scope.
	ExprRole ExprKind = "ROLE"
	// ExprRelationship resolves an organizational relationship within a scope.
	ExprRelationship ExprKind = "RELATIONSHIP"
	// ExprGroup resolves the members of a named group within a scope.
	ExprGroup ExprKind = "GROUP"
	// ExprAny is the union of its children.
	ExprAny ExprKind = "ANY"
	// ExprAll is the intersection of its children: a principal qualifies only
	// by satisfying every child.
	ExprAll ExprKind = "ALL"
	// ExprQuorum is the union of its children that additionally requires at
	// least MinDistinct of those children to have resolved anybody at all. It
	// is how "two of these three bodies must be represented" is stated without
	// a script.
	ExprQuorum ExprKind = "QUORUM"
)

var leafKinds = map[ExprKind]bool{
	ExprNamed: true, ExprRole: true, ExprRelationship: true, ExprGroup: true,
}

var combinatorKinds = map[ExprKind]bool{
	ExprAny: true, ExprAll: true, ExprQuorum: true,
}

// IsLeaf reports whether k resolves principals directly from the directory.
func (k ExprKind) IsLeaf() bool { return leafKinds[k] }

// IsCombinator reports whether k composes other expressions.
func (k ExprKind) IsCombinator() bool { return combinatorKinds[k] }

// Expression is one node of the bounded resolution language.
//
// It is a plain struct rather than an interface so that an expression is data
// all the way down: it can be compared, canonicalized, digested and stored as
// evidence without a type switch on a caller-supplied implementation, and there
// is no seam through which a tenant could inject executable behaviour.
type Expression struct {
	Kind ExprKind

	// PrincipalID and PinnedPolicyRef belong to ExprNamed.
	PrincipalID     string
	PinnedPolicyRef string

	// Role belongs to ExprRole.
	Role string

	// Relationship belongs to ExprRelationship.
	Relationship RelationshipKind

	// GroupID belongs to ExprGroup.
	GroupID string

	// Scope frames every leaf kind, including ExprNamed: a pinned approver is
	// pinned for a scope, not for the tenant at large.
	Scope Scope

	// Children belong to the combinator kinds.
	Children []Expression

	// MinDistinct belongs to ExprQuorum: how many children must each contribute
	// at least one principal.
	MinDistinct uint32
}

// Named builds a pinned-principal expression.
func Named(principalID, policyRef string, scope Scope) Expression {
	return Expression{Kind: ExprNamed, PrincipalID: principalID, PinnedPolicyRef: policyRef, Scope: scope}
}

// Role builds a role expression.
func Role(role string, scope Scope) Expression {
	return Expression{Kind: ExprRole, Role: role, Scope: scope}
}

// Relationship builds an organizational-relationship expression.
func Relationship(kind RelationshipKind, scope Scope) Expression {
	return Expression{Kind: ExprRelationship, Relationship: kind, Scope: scope}
}

// Group builds a group-membership expression.
func Group(groupID string, scope Scope) Expression {
	return Expression{Kind: ExprGroup, GroupID: groupID, Scope: scope}
}

// AnyOf builds a union expression.
func AnyOf(children ...Expression) Expression {
	return Expression{Kind: ExprAny, Children: children}
}

// AllOf builds an intersection expression.
func AllOf(children ...Expression) Expression {
	return Expression{Kind: ExprAll, Children: children}
}

// QuorumOf builds a quorum expression requiring minDistinct of the children to
// contribute at least one principal.
func QuorumOf(minDistinct uint32, children ...Expression) Expression {
	return Expression{Kind: ExprQuorum, MinDistinct: minDistinct, Children: children}
}

// Validate compiles the expression: it checks every node's own completeness and
// the two global bounds. A tree that validates can be resolved in bounded work
// against any directory.
func (e Expression) Validate() error {
	nodes := 0
	return e.validate(1, &nodes)
}

func (e Expression) validate(depth int, nodes *int) error {
	*nodes++
	if depth > MaxExpressionDepth {
		return newError("Validate", "expression", ErrInvalidExpression,
			"expression nests deeper than the %d-level bound", MaxExpressionDepth)
	}
	if *nodes > MaxExpressionNodes {
		return newError("Validate", "expression", ErrInvalidExpression,
			"expression exceeds the %d-node bound", MaxExpressionNodes)
	}
	switch {
	case e.Kind.IsLeaf():
		if len(e.Children) != 0 {
			return newError("Validate", "expression.children", ErrInvalidExpression,
				"%s is a leaf and carries no children", string(e.Kind))
		}
		if e.MinDistinct != 0 {
			return newError("Validate", "expression.min_distinct", ErrInvalidExpression,
				"%s is a leaf and carries no quorum", string(e.Kind))
		}
		return e.validateLeaf()
	case e.Kind.IsCombinator():
		return e.validateCombinator(depth, nodes)
	default:
		return newError("Validate", "expression.kind", ErrInvalidExpression,
			"expression kind %q is not declared", string(e.Kind))
	}
}

func (e Expression) validateLeaf() error {
	if err := e.Scope.Validate(); err != nil {
		return err
	}
	switch e.Kind {
	case ExprNamed:
		if e.PrincipalID == "" {
			return newError("Validate", "expression.principal_id", ErrInvalidExpression,
				"NAMED expression names no principal")
		}
		if e.PinnedPolicyRef == "" {
			return newError("Validate", "expression.pinned_policy_ref", ErrInvalidExpression,
				"NAMED expression pins %q with no policy behind it", e.PrincipalID)
		}
	case ExprRole:
		if e.Role == "" {
			return newError("Validate", "expression.role", ErrInvalidExpression,
				"ROLE expression names no role")
		}
	case ExprRelationship:
		if !e.Relationship.Valid() {
			return newError("Validate", "expression.relationship", ErrInvalidExpression,
				"relationship %q is not declared", string(e.Relationship))
		}
	case ExprGroup:
		if e.GroupID == "" {
			return newError("Validate", "expression.group_id", ErrInvalidExpression,
				"GROUP expression names no group")
		}
	}
	return e.rejectForeignFields()
}

// rejectForeignFields refuses a leaf that carries another leaf kind's payload.
// Without it a ROLE node could quietly carry a PrincipalID that resolution
// ignores but a reader of the stored evidence would believe.
func (e Expression) rejectForeignFields() error {
	set := []struct {
		field string
		owned bool
		empty bool
	}{
		{"principal_id", e.Kind == ExprNamed, e.PrincipalID == ""},
		{"pinned_policy_ref", e.Kind == ExprNamed, e.PinnedPolicyRef == ""},
		{"role", e.Kind == ExprRole, e.Role == ""},
		{"relationship", e.Kind == ExprRelationship, e.Relationship == RelationshipUnspecified},
		{"group_id", e.Kind == ExprGroup, e.GroupID == ""},
	}
	for _, f := range set {
		if !f.owned && !f.empty {
			return newError("Validate", "expression."+f.field, ErrInvalidExpression,
				"%s expression carries a %s belonging to another kind", string(e.Kind), f.field)
		}
	}
	return nil
}

func (e Expression) validateCombinator(depth int, nodes *int) error {
	if e.PrincipalID != "" || e.Role != "" || e.GroupID != "" ||
		e.Relationship != RelationshipUnspecified || e.PinnedPolicyRef != "" {
		return newError("Validate", "expression", ErrInvalidExpression,
			"%s is a combinator and carries a leaf payload", string(e.Kind))
	}
	if e.Scope.Kind != ScopeUnspecified {
		return newError("Validate", "expression.scope", ErrInvalidExpression,
			"%s is a combinator; its children carry the scope", string(e.Kind))
	}
	if len(e.Children) < 2 {
		return newError("Validate", "expression.children", ErrInvalidExpression,
			"%s composes at least two expressions, got %d", string(e.Kind), len(e.Children))
	}
	if e.Kind == ExprQuorum {
		if e.MinDistinct == 0 {
			return newError("Validate", "expression.min_distinct", ErrInvalidExpression,
				"QUORUM requires a minimum of at least one child")
		}
		if int(e.MinDistinct) > len(e.Children) {
			return newError("Validate", "expression.min_distinct", ErrInvalidExpression,
				"QUORUM requires %d of %d children, which can never be met",
				e.MinDistinct, len(e.Children))
		}
	} else if e.MinDistinct != 0 {
		return newError("Validate", "expression.min_distinct", ErrInvalidExpression,
			"%s carries a quorum minimum it cannot use", string(e.Kind))
	}
	for _, c := range e.Children {
		if err := c.validate(depth+1, nodes); err != nil {
			return err
		}
	}
	return nil
}

// Term is one atomic directory question: the leaf payload plus its scope. The
// Directory port answers terms, which is why a new leaf kind is a change to a
// closed enum here rather than a new port method.
type Term struct {
	Kind         ExprKind
	PrincipalID  string
	Role         string
	Relationship RelationshipKind
	GroupID      string
	Scope        Scope
}

// Ref returns the stable identifier this term is cited by in resolution
// evidence.
func (t Term) Ref() string {
	var b strings.Builder
	b.WriteString(string(t.Kind))
	b.WriteString("(")
	switch t.Kind {
	case ExprNamed:
		b.WriteString(t.PrincipalID)
	case ExprRole:
		b.WriteString(t.Role)
	case ExprRelationship:
		b.WriteString(string(t.Relationship))
	case ExprGroup:
		b.WriteString(t.GroupID)
	}
	b.WriteString(")@")
	b.WriteString(t.Scope.String())
	return b.String()
}

func (e Expression) term() Term {
	return Term{
		Kind:         e.Kind,
		PrincipalID:  e.PrincipalID,
		Role:         e.Role,
		Relationship: e.Relationship,
		GroupID:      e.GroupID,
		Scope:        e.Scope,
	}
}

// Terms returns every leaf term in the tree, in pre-order, deduplicated by Ref
// and then sorted, so that two expressions with the same leaves cite the same
// term list regardless of how they were nested.
func (e Expression) Terms() []Term {
	seen := map[string]Term{}
	e.walkTerms(seen)
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]Term, 0, len(refs))
	for _, ref := range refs {
		out = append(out, seen[ref])
	}
	return out
}

func (e Expression) walkTerms(into map[string]Term) {
	if e.Kind.IsLeaf() {
		t := e.term()
		into[t.Ref()] = t
		return
	}
	for _, c := range e.Children {
		c.walkTerms(into)
	}
}

// Canonical returns the deterministic byte encoding of the expression, or nil
// when it does not validate.
func (e Expression) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(expressionSchema, 1)
	e.encode(w, "root")
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (e Expression) encode(w *canonicalbytes.Writer, path string) {
	w.String(path+".kind", string(e.Kind)).
		String(path+".principal_id", e.PrincipalID).
		String(path+".pinned_policy_ref", e.PinnedPolicyRef).
		String(path+".role", e.Role).
		String(path+".relationship", string(e.Relationship)).
		String(path+".group_id", e.GroupID).
		String(path+".scope", e.Scope.String()).
		Int(path+".min_distinct", int64(e.MinDistinct)).
		Count(path+".children", len(e.Children))
	for i, c := range e.Children {
		c.encode(w, path+"."+itoa(i))
	}
}

// Digest returns the hex digest of the canonical encoding. Resolution evidence
// cites it so that a stored candidate set can be proved to have come from this
// exact expression.
func (e Expression) Digest() string {
	raw := e.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// itoa avoids pulling strconv into the hot encode path for a value that is
// always a small non-negative index.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
