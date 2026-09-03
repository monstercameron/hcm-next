package model

import (
	"sort"
)

// Registry is the compiled-in, immutable publication of every entity,
// property, aggregate, relationship, source-authority assignment and
// retention class this package registers. Like
// [github.com/monstercameron/hcm-next/internal/intent.Registry], it exposes
// no Add, Remove or setter: publishing a new item is a source change to
// [Catalog], never a runtime call.
type Registry struct {
	entities   map[EntityRef]EntityDefinition
	entityKeys map[string]EntityRef

	properties map[PropertyRef]PropertyDefinition

	aggregates map[EntityRef]AggregateDefinition
	childOwner map[EntityRef]EntityRef // child ref -> owning root

	relationships map[RelationshipRef]RelationshipDefinition

	authorities map[string]SourceAuthorityAssignment

	retentionClasses map[string]RetentionClass

	entityOrder       []EntityRef
	propertyOrder     []PropertyRef
	relationshipOrder []RelationshipRef
}

// NewRegistry compiles and cross-validates a full catalog.
//
// Beyond each item's own Validate, it rejects: a property naming an unknown
// owner entity, or one whose PropertyRef entity-key does not match the
// owner's Key (MODEL-011); an aggregate naming an unknown root or child, a
// root with two aggregate registrations, a child owned by two aggregates, or
// an AGGREGATE_ROOT-class entity with no aggregate registration at all
// (MODEL-012); a relationship naming an unknown source or target entity
// (MODEL-013); a property naming an unresolved authority or retention-class
// reference; and two exclusive authority assignments for the same scope with
// overlapping effective intervals (MODEL-021).
func NewRegistry(
	entities []EntityDefinition,
	properties []PropertyDefinition,
	aggregates []AggregateDefinition,
	relationships []RelationshipDefinition,
	authorities []SourceAuthorityAssignment,
	retentionClasses []RetentionClass,
) (*Registry, error) {
	r := &Registry{
		entities:         map[EntityRef]EntityDefinition{},
		entityKeys:       map[string]EntityRef{},
		properties:       map[PropertyRef]PropertyDefinition{},
		aggregates:       map[EntityRef]AggregateDefinition{},
		childOwner:       map[EntityRef]EntityRef{},
		relationships:    map[RelationshipRef]RelationshipDefinition{},
		authorities:      map[string]SourceAuthorityAssignment{},
		retentionClasses: map[string]RetentionClass{},
	}

	for _, e := range entities {
		if err := e.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.entities[e.Ref]; dup {
			return nil, newError("NewRegistry", "entity_ref", ErrDuplicateEntity,
				"%s is registered twice", e.Ref)
		}
		if existing, dup := r.entityKeys[e.Key]; dup {
			return nil, newError("NewRegistry", "key", ErrDuplicateEntity,
				"key %q used by both %s and %s", e.Key, existing, e.Ref)
		}
		r.entities[e.Ref] = e
		r.entityKeys[e.Key] = e.Ref
		r.entityOrder = append(r.entityOrder, e.Ref)
	}

	for _, c := range retentionClasses {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.retentionClasses[c.ClassRef]; dup {
			return nil, newError("NewRegistry", "retention_class_ref", ErrInvalidRetention,
				"%s is registered twice", c.ClassRef)
		}
		r.retentionClasses[c.ClassRef] = c
	}

	for _, a := range authorities {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.authorities[a.AssignmentRef]; dup {
			return nil, newError("NewRegistry", "assignment_ref", ErrInvalidAuthorityAssignment,
				"%s is registered twice", a.AssignmentRef)
		}
		r.authorities[a.AssignmentRef] = a
	}
	if err := checkAuthorityOverlaps(authorities); err != nil {
		return nil, err
	}

	for _, p := range properties {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		owner, ok := r.entities[p.Entity]
		if !ok {
			return nil, newError("NewRegistry", "entity", ErrUnknownEntity,
				"%s owner %s is not registered", p.Ref, p.Entity)
		}
		if p.Ref.EntityKey() != owner.Key {
			return nil, newError("NewRegistry", "ref", ErrInvalidProperty,
				"%s does not start with owner key %q", p.Ref, owner.Key)
		}
		if _, dup := r.properties[p.Ref]; dup {
			return nil, newError("NewRegistry", "ref", ErrDuplicateProperty,
				"%s is registered twice", p.Ref)
		}
		if _, ok := r.authorities[p.AuthorityRef]; !ok {
			return nil, newError("NewRegistry", "authority_ref", ErrNoAuthority,
				"%s references unknown authority %q", p.Ref, p.AuthorityRef)
		}
		if _, ok := r.retentionClasses[p.RetentionClassRef]; !ok {
			return nil, newError("NewRegistry", "retention_class_ref", ErrInvalidRetention,
				"%s references unknown retention class %q", p.Ref, p.RetentionClassRef)
		}
		r.properties[p.Ref] = p
		r.propertyOrder = append(r.propertyOrder, p.Ref)
	}

	for _, a := range aggregates {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		root, ok := r.entities[a.Root]
		if !ok {
			return nil, newError("NewRegistry", "root", ErrUnknownEntity,
				"%s root %s is not registered", "AggregateDefinition", a.Root)
		}
		if root.Class != ClassAggregateRoot && root.Class != ClassReadModel {
			return nil, newError("NewRegistry", "root", ErrInvalidAggregate,
				"%s has class %s and cannot be an aggregate root", a.Root, root.Class)
		}
		if _, dup := r.aggregates[a.Root]; dup {
			return nil, newError("NewRegistry", "root", ErrInvalidAggregate,
				"%s has two aggregate registrations", a.Root)
		}
		for _, c := range a.ChildRefs {
			child, ok := r.entities[c]
			if !ok {
				return nil, newError("NewRegistry", "child_refs", ErrUnknownEntity,
					"%s names unregistered child %s", a.Root, c)
			}
			if child.Class != ClassChild {
				return nil, newError("NewRegistry", "child_refs", ErrInvalidAggregate,
					"%s claims %s as a child, but its class is %s, not CHILD", a.Root, c, child.Class)
			}
			if owner, dup := r.childOwner[c]; dup {
				return nil, newError("NewRegistry", "child_refs", ErrInvalidAggregate,
					"%s is claimed as a child by both %s and %s", c, owner, a.Root)
			}
			r.childOwner[c] = a.Root
		}
		r.aggregates[a.Root] = a
	}
	// Every AGGREGATE_ROOT-class entity must have an aggregate registration:
	// an unassigned root is exactly the MODEL-012 RED case.
	for _, ref := range r.entityOrder {
		e := r.entities[ref]
		if e.Class != ClassAggregateRoot {
			continue
		}
		if _, ok := r.aggregates[ref]; !ok {
			return nil, newError("NewRegistry", "root", ErrUnassignedRoot,
				"%s is an AGGREGATE_ROOT with no aggregate registration", ref)
		}
	}

	for _, rel := range relationships {
		if err := rel.Validate(); err != nil {
			return nil, err
		}
		if _, ok := r.entities[rel.SourceEntity]; !ok {
			return nil, newError("NewRegistry", "source_entity", ErrUnknownEntity,
				"%s source %s is not registered", rel.Ref, rel.SourceEntity)
		}
		if _, ok := r.entities[rel.TargetEntity]; !ok {
			return nil, newError("NewRegistry", "target_entity", ErrUnknownEntity,
				"%s target %s is not registered", rel.Ref, rel.TargetEntity)
		}
		if _, dup := r.relationships[rel.Ref]; dup {
			return nil, newError("NewRegistry", "ref", ErrInvalidRelationship,
				"%s is registered twice", rel.Ref)
		}
		r.relationships[rel.Ref] = rel
		r.relationshipOrder = append(r.relationshipOrder, rel.Ref)
	}

	sort.Slice(r.entityOrder, func(i, j int) bool { return refLess(r.entityOrder[i], r.entityOrder[j]) })
	sort.Slice(r.propertyOrder, func(i, j int) bool { return r.propertyOrder[i] < r.propertyOrder[j] })
	sort.Slice(r.relationshipOrder, func(i, j int) bool {
		return relRefLess(r.relationshipOrder[i], r.relationshipOrder[j])
	})

	return r, nil
}

func refLess(a, b EntityRef) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Version < b.Version
}

func relRefLess(a, b RelationshipRef) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Version < b.Version
}

func checkAuthorityOverlaps(authorities []SourceAuthorityAssignment) error {
	byScope := map[string][]SourceAuthorityAssignment{}
	for _, a := range authorities {
		if !a.Exclusive {
			continue
		}
		byScope[a.DomainScope] = append(byScope[a.DomainScope], a)
	}
	for scope, group := range byScope {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				overlap, err := group[i].Effective.Overlaps(group[j].Effective)
				if err == nil && overlap {
					return newError("NewRegistry", "domain_scope", ErrInvalidAuthorityAssignment,
						"%s and %s are both exclusive over %q with overlapping effective intervals",
						group[i].AssignmentRef, group[j].AssignmentRef, scope)
				}
			}
		}
	}
	return nil
}

// Entities returns every published entity, sorted.
func (r *Registry) Entities() []EntityDefinition {
	out := make([]EntityDefinition, 0, len(r.entityOrder))
	for _, ref := range r.entityOrder {
		out = append(out, r.entities[ref])
	}
	return out
}

// Entity resolves one entity by reference.
func (r *Registry) Entity(ref EntityRef) (EntityDefinition, error) {
	e, ok := r.entities[ref]
	if !ok {
		return EntityDefinition{}, newError("Entity", "ref", ErrUnknownEntity, "%s is not published", ref)
	}
	return e, nil
}

// EntityByKey resolves one entity by its snake_case key.
func (r *Registry) EntityByKey(key string) (EntityDefinition, error) {
	ref, ok := r.entityKeys[key]
	if !ok {
		return EntityDefinition{}, newError("EntityByKey", "key", ErrUnknownEntity, "%q is not published", key)
	}
	return r.entities[ref], nil
}

// Properties returns every published property, sorted.
func (r *Registry) Properties() []PropertyDefinition {
	out := make([]PropertyDefinition, 0, len(r.propertyOrder))
	for _, ref := range r.propertyOrder {
		out = append(out, r.properties[ref])
	}
	return out
}

// Property resolves one property by reference.
func (r *Registry) Property(ref PropertyRef) (PropertyDefinition, error) {
	p, ok := r.properties[ref]
	if !ok {
		return PropertyDefinition{}, newError("Property", "ref", ErrUnknownProperty, "%s is not published", ref)
	}
	return p, nil
}

// Aggregates returns every published aggregate definition, sorted by root.
func (r *Registry) Aggregates() []AggregateDefinition {
	out := make([]AggregateDefinition, 0, len(r.aggregates))
	for _, ref := range r.entityOrder {
		if a, ok := r.aggregates[ref]; ok {
			out = append(out, a)
		}
	}
	return out
}

// Aggregate resolves the aggregate definition of one root.
func (r *Registry) Aggregate(root EntityRef) (AggregateDefinition, error) {
	a, ok := r.aggregates[root]
	if !ok {
		return AggregateDefinition{}, newError("Aggregate", "root", ErrUnassignedRoot,
			"%s has no aggregate registration", root)
	}
	return a, nil
}

// OwnerRoot resolves the aggregate root that owns entity: itself if it is
// already a root, the root that claims it as a child, or itself if it is a
// standalone EVIDENCE, VALUE, EVENT or READ_MODEL entity — those classes are
// self-contained records with no separate command boundary to own them, so
// "owner root" is the entity's own identity rather than an absent one. A
// CHILD claimed by no root cannot resolve here — that inconsistency is
// rejected at [NewRegistry] time, never observed by a caller.
func (r *Registry) OwnerRoot(entity EntityRef) (EntityRef, error) {
	if _, ok := r.aggregates[entity]; ok {
		return entity, nil
	}
	if root, ok := r.childOwner[entity]; ok {
		return root, nil
	}
	if e, ok := r.entities[entity]; ok {
		switch e.Class {
		case ClassEvidence, ClassValue, ClassEvent, ClassReadModel:
			return entity, nil
		}
	}
	return EntityRef{}, newError("OwnerRoot", "entity", ErrInvalidAggregate,
		"%s resolves to no aggregate root", entity)
}

// Relationships returns every published relationship definition, sorted.
func (r *Registry) Relationships() []RelationshipDefinition {
	out := make([]RelationshipDefinition, 0, len(r.relationshipOrder))
	for _, ref := range r.relationshipOrder {
		out = append(out, r.relationships[ref])
	}
	return out
}

// Relationship resolves one relationship definition by reference.
func (r *Registry) Relationship(ref RelationshipRef) (RelationshipDefinition, error) {
	d, ok := r.relationships[ref]
	if !ok {
		return RelationshipDefinition{}, newError("Relationship", "ref", ErrInvalidRelationship,
			"%s is not published", ref)
	}
	return d, nil
}

// Authorities returns every published authority assignment.
func (r *Registry) Authorities() []SourceAuthorityAssignment {
	out := make([]SourceAuthorityAssignment, 0, len(r.authorities))
	for _, a := range r.authorities {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssignmentRef < out[j].AssignmentRef })
	return out
}

// Authority resolves one authority assignment by reference.
func (r *Registry) Authority(ref string) (SourceAuthorityAssignment, error) {
	a, ok := r.authorities[ref]
	if !ok {
		return SourceAuthorityAssignment{}, newError("Authority", "ref", ErrNoAuthority,
			"%q is not published", ref)
	}
	return a, nil
}

// RetentionClasses returns every published retention class.
func (r *Registry) RetentionClasses() []RetentionClass {
	out := make([]RetentionClass, 0, len(r.retentionClasses))
	for _, c := range r.retentionClasses {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClassRef < out[j].ClassRef })
	return out
}

// RetentionClass resolves one retention class by reference.
func (r *Registry) RetentionClass(ref string) (RetentionClass, error) {
	c, ok := r.retentionClasses[ref]
	if !ok {
		return RetentionClass{}, newError("RetentionClass", "ref", ErrInvalidRetention,
			"%q is not published", ref)
	}
	return c, nil
}

// PropertyResolution is what [Registry.ResolveProperty] returns: the property
// itself, its owning entity, the aggregate root that owns that entity, and
// the authority and retention policies that apply to it (MODEL-011 GREEN —
// "every registered property resolves owner aggregate, schema path and
// applicable policies").
type PropertyResolution struct {
	Property       PropertyDefinition
	OwnerEntity    EntityDefinition
	OwnerRoot      EntityRef
	Authority      SourceAuthorityAssignment
	RetentionClass RetentionClass
}

// ResolveProperty resolves a property to its owner aggregate, schema path and
// applicable authority/retention policies.
func (r *Registry) ResolveProperty(ref PropertyRef) (PropertyResolution, error) {
	p, err := r.Property(ref)
	if err != nil {
		return PropertyResolution{}, err
	}
	owner, err := r.Entity(p.Entity)
	if err != nil {
		return PropertyResolution{}, err
	}
	root, err := r.OwnerRoot(p.Entity)
	if err != nil {
		return PropertyResolution{}, err
	}
	authority, err := r.Authority(p.AuthorityRef)
	if err != nil {
		return PropertyResolution{}, err
	}
	retention, err := r.RetentionClass(p.RetentionClassRef)
	if err != nil {
		return PropertyResolution{}, err
	}
	return PropertyResolution{
		Property:       p,
		OwnerEntity:    owner,
		OwnerRoot:      root,
		Authority:      authority,
		RetentionClass: retention,
	}, nil
}
