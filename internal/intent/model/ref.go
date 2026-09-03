package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	entityNamePattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	entityKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	propertyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// EntityRef identifies one published entity definition by canonical PascalCase
// name and material version, for example Person/v1. A free-form alias never
// substitutes for this identity; see [EntityDefinition.Aliases].
type EntityRef struct {
	Name    string
	Version int
}

// Validate rejects a malformed entity reference.
func (r EntityRef) Validate() error {
	if !entityNamePattern.MatchString(r.Name) {
		return newError("EntityRef.Validate", "name", ErrInvalidReference,
			"%q is not a PascalCase entity name", r.Name)
	}
	if r.Version < 1 {
		return newError("EntityRef.Validate", "version", ErrInvalidReference,
			"version must be >= 1, got %d", r.Version)
	}
	return nil
}

// String renders the canonical "Name/vN" form.
func (r EntityRef) String() string { return fmt.Sprintf("%s/v%d", r.Name, r.Version) }

// ParseEntityRef parses the canonical "Name/vN" form.
func ParseEntityRef(s string) (EntityRef, error) {
	parts := strings.SplitN(s, "/v", 2)
	if len(parts) != 2 {
		return EntityRef{}, newError("ParseEntityRef", "", ErrInvalidReference,
			"%q is not Name/vN", s)
	}
	v, err := strconv.Atoi(parts[1])
	if err != nil {
		return EntityRef{}, newError("ParseEntityRef", "version", ErrInvalidReference,
			"%q has a non-numeric version: %v", s, err)
	}
	ref := EntityRef{Name: parts[0], Version: v}
	if err := ref.Validate(); err != nil {
		return EntityRef{}, err
	}
	return ref, nil
}

// RelationshipRef identifies one published relationship definition by
// canonical PascalCase name and material version, for example
// ManagerRelationship/v1.
type RelationshipRef struct {
	Name    string
	Version int
}

// Validate rejects a malformed relationship reference.
func (r RelationshipRef) Validate() error {
	if !entityNamePattern.MatchString(r.Name) {
		return newError("RelationshipRef.Validate", "name", ErrInvalidReference,
			"%q is not a PascalCase relationship name", r.Name)
	}
	if r.Version < 1 {
		return newError("RelationshipRef.Validate", "version", ErrInvalidReference,
			"version must be >= 1, got %d", r.Version)
	}
	return nil
}

// String renders the canonical "Name/vN" form.
func (r RelationshipRef) String() string { return fmt.Sprintf("%s/v%d", r.Name, r.Version) }

// PropertyRef is a schema path of the form "entity_key.path", for example
// "employment.status" or "assignment.manager_relationship". It is a plain
// string type deliberately: the fourteen intent definitions already carry
// these exact strings in [github.com/monstercameron/hcm-next/internal/intent.Binding],
// and a PropertyRef must interoperate with that untyped form without a
// conversion step.
type PropertyRef string

// Validate rejects a malformed property reference.
func (p PropertyRef) Validate() error {
	if !propertyPattern.MatchString(string(p)) {
		return newError("PropertyRef.Validate", "", ErrInvalidReference,
			"%q is not entity_key.path", string(p))
	}
	return nil
}

// EntityKey returns the leading entity-key segment, for example "employment"
// from "employment.status".
func (p PropertyRef) EntityKey() string {
	i := strings.IndexByte(string(p), '.')
	if i < 0 {
		return string(p)
	}
	return string(p)[:i]
}

// Path returns the trailing path segment(s) after the entity key.
func (p PropertyRef) Path() string {
	i := strings.IndexByte(string(p), '.')
	if i < 0 {
		return ""
	}
	return string(p)[i+1:]
}

// Column returns a SQL-safe column name derived from the path, replacing "."
// with "_".
func (p PropertyRef) Column() string { return strings.ReplaceAll(p.Path(), ".", "_") }

func validEntityKey(key string) error {
	if !entityKeyPattern.MatchString(key) {
		return newError("validEntityKey", "key", ErrInvalidReference,
			"%q is not a snake_case entity key", key)
	}
	return nil
}
