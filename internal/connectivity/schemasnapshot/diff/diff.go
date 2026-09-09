// Package diff compares admitted external schema snapshots without making
// any persistence or promotion decision. The body model is deliberately
// small and typed: callers normalize provider-specific documents before
// handing them to this package.
package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
)

var (
	ErrInvalidBody       = errors.New("schemasnapshot diff: invalid schema body")
	ErrDifferentProvider = errors.New("schemasnapshot diff: snapshots have different providers")
	ErrInvalidRename     = errors.New("schemasnapshot diff: invalid rename declaration")
)

// Impact is the effect of one schema change on the declared consumed-field
// mapping set.
type Impact string

const (
	ImpactBreaking Impact = "BREAKING"
	ImpactAdditive Impact = "ADDITIVE"
	ImpactCosmetic Impact = "COSMETIC"
)

// ChangeKind is the typed shape of a schema change.
type ChangeKind string

const (
	ChangeEntityAdded            ChangeKind = "ENTITY_ADDED"
	ChangeEntityRemoved          ChangeKind = "ENTITY_REMOVED"
	ChangeEntityRenamed          ChangeKind = "ENTITY_RENAMED"
	ChangeFieldAdded             ChangeKind = "FIELD_ADDED"
	ChangeFieldRemoved           ChangeKind = "FIELD_REMOVED"
	ChangeFieldRenamed           ChangeKind = "FIELD_RENAMED"
	ChangeFieldTypeChanged       ChangeKind = "FIELD_TYPE_CHANGED"
	ChangeFieldConstraintChanged ChangeKind = "FIELD_CONSTRAINT_CHANGED"
)

// Constraint is a named, normalized rule on a field. Value is intentionally
// opaque to this package except for the common required/nullable rules used
// to distinguish a tightening from a loosening.
type Constraint struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Field is one typed external field. RenamedFrom is meaningful only on the
// newer body; it is an explicit declaration, never an inferred similarity.
type Field struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Required    bool         `json:"required,omitempty"`
	Nullable    bool         `json:"nullable,omitempty"`
	Constraints []Constraint `json:"constraints,omitempty"`
	RenamedFrom string       `json:"renamed_from,omitempty"`
}

// Entity is one external object or resource.
type Entity struct {
	Name        string  `json:"name"`
	Fields      []Field `json:"fields,omitempty"`
	RenamedFrom string  `json:"renamed_from,omitempty"`
}

// RenameDeclaration is an explicit provider-declared rename. An empty Entity
// declares a field rename on the entity identified by the field's containing
// entity; otherwise it declares an entity rename.
type RenameDeclaration struct {
	Entity string `json:"entity,omitempty"`
	From   string `json:"from"`
	To     string `json:"to"`
}

// Body is the normalized typed body of one admitted snapshot.
type Body struct {
	Entities []Entity            `json:"entities"`
	Renames  []RenameDeclaration `json:"renames,omitempty"`
}

// Validate reports whether the normalized body is structurally comparable.
func (b Body) Validate() error { return b.validate() }

// SnapshotBody is a descriptive alias for callers that prefer the longer
// name. Schema and SchemaBody are retained as equivalent vocabulary.
type SnapshotBody = Body
type Schema = Body
type SchemaBody = Body
type SchemaEntity = Entity
type SchemaField = Field

// ConsumedFieldMapping identifies a field known to be consumed by a mapping,
// workflow, report, agent, or another downstream contract.
type ConsumedFieldMapping struct {
	ID     string `json:"id"`
	Entity string `json:"entity"`
	Field  string `json:"field"`
}

type ConsumedField = ConsumedFieldMapping
type Mapping = ConsumedFieldMapping

// Request is the pure in-memory input port for a schema comparison.
type Request struct {
	Before                schemasnapshot.SchemaSnapshot
	After                 schemasnapshot.SchemaSnapshot
	BeforeBody            Body
	AfterBody             Body
	Mappings              []ConsumedFieldMapping
	ConsumedFieldMappings []ConsumedFieldMapping
}

// Change records one normalized difference and its known downstream impact.
type Change struct {
	Kind              ChangeKind   `json:"kind"`
	Entity            string       `json:"entity,omitempty"`
	FromEntity        string       `json:"from_entity,omitempty"`
	ToEntity          string       `json:"to_entity,omitempty"`
	Field             string       `json:"field,omitempty"`
	FromField         string       `json:"from_field,omitempty"`
	ToField           string       `json:"to_field,omitempty"`
	BeforeType        string       `json:"before_type,omitempty"`
	AfterType         string       `json:"after_type,omitempty"`
	BeforeConstraints []Constraint `json:"before_constraints,omitempty"`
	AfterConstraints  []Constraint `json:"after_constraints,omitempty"`
	Impact            Impact       `json:"impact"`
	Consumers         []string     `json:"consumers,omitempty"`
	Rule              string       `json:"rule"`
}

// SchemaDiff is the immutable result of comparing two admitted snapshots.
// Digest covers the two snapshot digests, the declared mapping set, and the
// complete canonical change list.
type SchemaDiff struct {
	Provider       schemasnapshot.ProviderRef `json:"provider"`
	BeforeSnapshot string                     `json:"before_snapshot"`
	AfterSnapshot  string                     `json:"after_snapshot"`
	Changes        []Change                   `json:"changes"`
	Digest         string                     `json:"digest"`
}

// Diff is the primary comparison operation.
func Diff(req Request) (SchemaDiff, error) {
	if err := req.Before.RequireAdmitted(); err != nil {
		return SchemaDiff{}, err
	}
	if err := req.After.RequireAdmitted(); err != nil {
		return SchemaDiff{}, err
	}
	if !req.Before.Provider.Equal(req.After.Provider) {
		return SchemaDiff{}, fmt.Errorf("%w: %v != %v", ErrDifferentProvider, req.Before.Provider, req.After.Provider)
	}
	if err := req.BeforeBody.validate(); err != nil {
		return SchemaDiff{}, err
	}
	if err := req.AfterBody.validate(); err != nil {
		return SchemaDiff{}, err
	}
	mappings := append([]ConsumedFieldMapping(nil), req.Mappings...)
	mappings = append(mappings, req.ConsumedFieldMappings...)
	if err := validateMappings(mappings); err != nil {
		return SchemaDiff{}, err
	}
	changes := compareBodies(req.BeforeBody, req.AfterBody, mappings)
	result := SchemaDiff{
		Provider:       req.Before.Provider,
		BeforeSnapshot: req.Before.CanonicalDigest,
		AfterSnapshot:  req.After.CanonicalDigest,
		Changes:        changes,
	}
	result.Digest = result.computeDigest(mappings)
	return result, nil
}

// Compare is a concise synonym for Diff.
func Compare(req Request) (SchemaDiff, error) { return Diff(req) }

// DiffSnapshots compares two snapshots and their normalized bodies.
func DiffSnapshots(before schemasnapshot.SchemaSnapshot, beforeBody Body, after schemasnapshot.SchemaSnapshot, afterBody Body, mappings []ConsumedFieldMapping) (SchemaDiff, error) {
	return Diff(Request{Before: before, BeforeBody: beforeBody, After: after, AfterBody: afterBody, Mappings: mappings})
}

// CanonicalDigest returns the stable digest of the diff.
func (d SchemaDiff) CanonicalDigest() string { return d.Digest }

// Explain returns a deterministic human-readable explanation of every
// change. An empty change set is explicitly explained as unchanged.
func (d SchemaDiff) Explain() string {
	if len(d.Changes) == 0 {
		return "no schema changes between admitted snapshots"
	}
	parts := make([]string, 0, len(d.Changes))
	for _, c := range d.Changes {
		consumer := "no declared consumer"
		if len(c.Consumers) > 0 {
			consumer = "consumers=" + strings.Join(c.Consumers, ",")
		}
		parts = append(parts, fmt.Sprintf("%s %s impact=%s (%s; %s)", c.Kind, changeSubject(c), c.Impact, consumer, c.Rule))
	}
	return strings.Join(parts, "; ")
}

// Explain is the package-level form for callers that do not use methods.
func Explain(d SchemaDiff) string { return d.Explain() }

func (d SchemaDiff) computeDigest(mappings []ConsumedFieldMapping) string {
	mappings = append([]ConsumedFieldMapping(nil), mappings...)
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].Entity != mappings[j].Entity {
			return mappings[i].Entity < mappings[j].Entity
		}
		if mappings[i].Field != mappings[j].Field {
			return mappings[i].Field < mappings[j].Field
		}
		return mappings[i].ID < mappings[j].ID
	})
	b, _ := json.Marshal(struct {
		Provider      schemasnapshot.ProviderRef
		Before, After string
		Mappings      []ConsumedFieldMapping
		Changes       []Change
	}{d.Provider, d.BeforeSnapshot, d.AfterSnapshot, mappings, d.Changes})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (b Body) validate() error {
	entities := make(map[string]struct{}, len(b.Entities))
	for _, entity := range b.Entities {
		if strings.TrimSpace(entity.Name) == "" {
			return fmt.Errorf("%w: entity name is required", ErrInvalidBody)
		}
		if _, ok := entities[entity.Name]; ok {
			return fmt.Errorf("%w: duplicate entity %q", ErrInvalidBody, entity.Name)
		}
		entities[entity.Name] = struct{}{}
		fields := make(map[string]struct{}, len(entity.Fields))
		for _, field := range entity.Fields {
			if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Type) == "" {
				return fmt.Errorf("%w: entity %q has a field without name and type", ErrInvalidBody, entity.Name)
			}
			if _, ok := fields[field.Name]; ok {
				return fmt.Errorf("%w: entity %q repeats field %q", ErrInvalidBody, entity.Name, field.Name)
			}
			fields[field.Name] = struct{}{}
			if err := validateConstraints(field); err != nil {
				return err
			}
		}
	}
	for _, rename := range b.Renames {
		if strings.TrimSpace(rename.From) == "" || strings.TrimSpace(rename.To) == "" || rename.From == rename.To {
			return fmt.Errorf("%w: %q -> %q", ErrInvalidRename, rename.From, rename.To)
		}
		if rename.Entity != "" {
			if _, ok := entities[rename.Entity]; !ok {
				return fmt.Errorf("%w: unknown entity %q", ErrInvalidRename, rename.Entity)
			}
			found := false
			for _, field := range entityMap(b.Entities)[rename.Entity].Fields {
				if field.Name == rename.To {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: entity %q has no destination field %q", ErrInvalidRename, rename.Entity, rename.To)
			}
		} else if _, ok := entities[rename.To]; !ok {
			return fmt.Errorf("%w: no destination entity %q", ErrInvalidRename, rename.To)
		}
	}
	return nil
}

func validateConstraints(field Field) error {
	seen := make(map[string]struct{}, len(field.Constraints)+2)
	for _, c := range field.Constraints {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("%w: field %q has unnamed constraint", ErrInvalidBody, field.Name)
		}
		if _, ok := seen[c.Name]; ok {
			return fmt.Errorf("%w: field %q repeats constraint %q", ErrInvalidBody, field.Name, c.Name)
		}
		seen[c.Name] = struct{}{}
	}
	return nil
}

func validateMappings(mappings []ConsumedFieldMapping) error {
	seen := map[string]struct{}{}
	for _, m := range mappings {
		if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Entity) == "" || strings.TrimSpace(m.Field) == "" {
			return fmt.Errorf("%w: consumed mapping requires id, entity and field", ErrInvalidBody)
		}
		key := m.Entity + "\x00" + m.Field + "\x00" + m.ID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate consumed mapping %q", ErrInvalidBody, m.ID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func compareBodies(before, after Body, mappings []ConsumedFieldMapping) []Change {
	be, ae := entityMap(before.Entities), entityMap(after.Entities)
	matched := map[string]string{}
	for name, entity := range ae {
		if _, ok := be[name]; ok {
			matched[name] = name
			continue
		}
		if entity.RenamedFrom != "" {
			if _, ok := be[entity.RenamedFrom]; ok {
				matched[entity.RenamedFrom] = name
			}
		}
	}
	for _, r := range append(append([]RenameDeclaration(nil), before.Renames...), after.Renames...) {
		if r.Entity == "" {
			if _, ok := be[r.From]; ok {
				if _, exists := ae[r.To]; exists {
					matched[r.From] = r.To
				}
			}
		}
	}
	changes := make([]Change, 0)
	for oldName, oldEntity := range be {
		newName, ok := matched[oldName]
		if !ok {
			changes = append(changes, makeChange(Change{Kind: ChangeEntityRemoved, Entity: oldName}, mappings, oldName, ""))
			continue
		}
		if oldName != newName {
			changes = append(changes, makeChange(Change{Kind: ChangeEntityRenamed, Entity: newName, FromEntity: oldName, ToEntity: newName}, mappings, oldName, newName))
		}
		changes = append(changes, compareFields(oldName, newName, oldEntity.Fields, ae[newName].Fields, before, after, mappings)...)
	}
	for newName := range ae {
		if _, ok := matchedInverse(matched, newName); !ok {
			changes = append(changes, makeChange(Change{Kind: ChangeEntityAdded, Entity: newName}, mappings, "", newName))
		}
	}
	sortChanges(changes)
	return changes
}

func compareFields(oldEntity, newEntity string, oldFields, newFields []Field, before, after Body, mappings []ConsumedFieldMapping) []Change {
	of, nf := fieldMap(oldFields), fieldMap(newFields)
	matched := map[string]string{}
	for name, field := range nf {
		if _, ok := of[name]; ok {
			matched[name] = name
			continue
		}
		if field.RenamedFrom != "" {
			if _, ok := of[field.RenamedFrom]; ok {
				matched[field.RenamedFrom] = name
			}
		}
	}
	for _, r := range append(append([]RenameDeclaration(nil), before.Renames...), after.Renames...) {
		if r.Entity == oldEntity || r.Entity == newEntity {
			if _, ok := of[r.From]; ok {
				if _, exists := nf[r.To]; exists {
					matched[r.From] = r.To
				}
			}
		}
	}
	changes := make([]Change, 0)
	for oldName, oldField := range of {
		newName, ok := matched[oldName]
		if !ok {
			changes = append(changes, makeChange(Change{Kind: ChangeFieldRemoved, Entity: newEntity, Field: oldName, FromField: oldName}, mappings, oldEntity, ""))
			continue
		}
		newField := nf[newName]
		if oldName != newName {
			changes = append(changes, makeChange(Change{Kind: ChangeFieldRenamed, Entity: newEntity, Field: newName, FromField: oldName, ToField: newName}, mappings, oldEntity+"."+oldName, newEntity+"."+newName))
		}
		if oldField.Type != newField.Type {
			changes = append(changes, makeChange(Change{Kind: ChangeFieldTypeChanged, Entity: newEntity, Field: newName, BeforeType: oldField.Type, AfterType: newField.Type}, mappings, oldEntity+"."+oldName, newEntity+"."+newName))
		}
		if !sameConstraints(oldField, newField) {
			c := Change{Kind: ChangeFieldConstraintChanged, Entity: newEntity, Field: newName, BeforeConstraints: canonicalConstraints(oldField), AfterConstraints: canonicalConstraints(newField)}
			c.Impact = constraintImpact(oldField, newField, mappings, newEntity, newName)
			c.Consumers = consumersFor(mappings, newEntity, newName, oldEntity, oldName)
			c.Rule = constraintRule(oldField, newField, len(c.Consumers) > 0)
			changes = append(changes, c)
		}
	}
	for newName := range nf {
		if _, ok := matchedInverse(matched, newName); !ok {
			changes = append(changes, makeChange(Change{Kind: ChangeFieldAdded, Entity: newEntity, Field: newName, ToField: newName}, mappings, "", newEntity+"."+newName))
		}
	}
	sortChanges(changes)
	return changes
}

func makeChange(c Change, mappings []ConsumedFieldMapping, oldSubject, newSubject string) Change {
	oldEntity, oldField := oldSubject, c.FromField
	if dot := strings.LastIndex(oldSubject, "."); dot >= 0 {
		oldEntity, oldField = oldSubject[:dot], oldSubject[dot+1:]
	}
	c.Consumers = consumersFor(mappings, c.Entity, c.Field, oldEntity, oldField)
	if c.Kind == ChangeEntityRemoved {
		c.Consumers = consumersForEntity(mappings, oldSubject)
	}
	if c.Kind == ChangeEntityRenamed {
		c.Consumers = append(c.Consumers, consumersForEntity(mappings, oldSubject)...)
		c.Consumers = uniqueSorted(c.Consumers)
	}
	switch c.Kind {
	case ChangeEntityAdded, ChangeFieldAdded:
		c.Impact, c.Rule = ImpactAdditive, "a new entity or field expands the declared schema surface"
	case ChangeEntityRenamed, ChangeFieldRenamed:
		c.Impact, c.Rule = ImpactCosmetic, "rename is reported only because it was explicitly declared"
	case ChangeEntityRemoved, ChangeFieldRemoved, ChangeFieldTypeChanged:
		if len(c.Consumers) > 0 {
			c.Impact, c.Rule = ImpactBreaking, "a declared consumer references the removed or changed field"
		} else {
			c.Impact, c.Rule = ImpactCosmetic, "no declared consumer references the removed or changed schema member"
		}
	}
	return c
}

func constraintImpact(oldField, newField Field, mappings []ConsumedFieldMapping, entity, field string) Impact {
	if len(consumersFor(mappings, entity, field, entity, field)) == 0 {
		return ImpactCosmetic
	}
	if constraintsLoosened(oldField, newField) {
		return ImpactAdditive
	}
	return ImpactBreaking
}

func constraintRule(oldField, newField Field, consumed bool) string {
	if !consumed {
		return "constraint drift has no declared consumer"
	}
	if constraintsLoosened(oldField, newField) {
		return "the declared constraint became less restrictive"
	}
	return "the declared constraint became more restrictive or changed incompatibly"
}

func constraintsLoosened(oldField, newField Field) bool {
	if oldField.Required && !newField.Required {
		return true
	}
	if !oldField.Nullable && newField.Nullable {
		return true
	}
	old := canonicalConstraints(oldField)
	next := canonicalConstraints(newField)
	return len(next) < len(old)
}

func sameConstraints(a, b Field) bool {
	return a.Required == b.Required && a.Nullable == b.Nullable && equalConstraints(canonicalConstraints(a), canonicalConstraints(b))
}
func canonicalConstraints(f Field) []Constraint {
	out := append([]Constraint(nil), f.Constraints...)
	if f.Required {
		out = append(out, Constraint{Name: "required", Value: "true"})
	}
	if f.Nullable {
		out = append(out, Constraint{Name: "nullable", Value: "true"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Value < out[j].Value
	})
	return out
}
func equalConstraints(a, b []Constraint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func entityMap(in []Entity) map[string]Entity {
	out := make(map[string]Entity, len(in))
	for _, e := range in {
		out[e.Name] = e
	}
	return out
}
func fieldMap(in []Field) map[string]Field {
	out := make(map[string]Field, len(in))
	for _, f := range in {
		out[f.Name] = f
	}
	return out
}
func matchedInverse(m map[string]string, value string) (string, bool) {
	for old, next := range m {
		if next == value {
			return old, true
		}
	}
	return "", false
}
func consumersFor(mappings []ConsumedFieldMapping, entity, field, oldEntity, oldField string) []string {
	ids := map[string]struct{}{}
	for _, m := range mappings {
		if (m.Entity == entity && m.Field == field) || (oldEntity != "" && m.Entity == oldEntity && m.Field == oldField) {
			ids[m.ID] = struct{}{}
		}
	}
	return sortedKeys(ids)
}
func consumersForEntity(mappings []ConsumedFieldMapping, entity string) []string {
	ids := map[string]struct{}{}
	for _, m := range mappings {
		if m.Entity == entity {
			ids[m.ID] = struct{}{}
		}
	}
	return sortedKeys(ids)
}
func uniqueSorted(in []string) []string {
	ids := map[string]struct{}{}
	for _, id := range in {
		ids[id] = struct{}{}
	}
	return sortedKeys(ids)
}
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		ka := string(a.Kind) + "\x00" + a.Entity + "\x00" + a.Field + "\x00" + a.FromEntity + "\x00" + a.FromField
		kb := string(b.Kind) + "\x00" + b.Entity + "\x00" + b.Field + "\x00" + b.FromEntity + "\x00" + b.FromField
		return ka < kb
	})
}
func changeSubject(c Change) string {
	if c.Field != "" {
		if c.FromField != "" && c.ToField != "" {
			return c.Entity + "." + c.FromField + "->" + c.ToField
		}
		return c.Entity + "." + c.Field
	}
	if c.FromEntity != "" {
		return c.FromEntity + "->" + c.ToEntity
	}
	return c.Entity
}
