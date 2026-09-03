// Package manifest defines and validates the delivery-manifest schema every
// todo's governance record must satisfy (GOV-001): owner, estimate, gate,
// dependencies, acceptance evidence, operations owner and displaced scope.
//
// REFACTOR note: this validation is handwritten today. Once a SchemaFlux
// definition exists for the delivery manifest, generate this validation from
// it instead of maintaining parallel handwritten rules.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Manifest is the delivery-manifest record for one todo. Dependencies is a
// pointer so a manifest can distinguish "dependencies never declared" (nil,
// a violation) from "explicitly has no dependencies" (a non-nil empty
// slice, valid - the delivery-manifest analogue of a todo's `Depends: none`).
type Manifest struct {
	ID                 string    `json:"id"`
	Owner              string    `json:"owner"`
	Estimate           string    `json:"estimate"`
	Gate               string    `json:"gate"`
	Dependencies       *[]string `json:"dependencies"`
	AcceptanceEvidence string    `json:"acceptance_evidence"`
	OperationsOwner    string    `json:"operations_owner"`
	DisplacedScope     string    `json:"displaced_scope"`
}

// Violation names one missing or invalid required field on a manifest.
type Violation struct {
	ID    string
	Field string
	Issue string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.ID, v.Field, v.Issue)
}

// Validate returns every required-field violation on m, in a stable field
// order (owner, estimate, gate, dependencies, acceptance evidence,
// operations owner, displaced scope). A complete manifest returns nil.
func Validate(m Manifest) []Violation {
	var violations []Violation
	add := func(field, issue string) {
		violations = append(violations, Violation{ID: m.ID, Field: field, Issue: issue})
	}

	if m.Owner == "" {
		add("owner", "missing owner")
	}
	if m.Estimate == "" {
		add("estimate", "missing estimate")
	}
	if m.Gate == "" {
		add("gate", "missing gate")
	}
	if m.Dependencies == nil {
		add("dependencies", "dependencies never declared (use an empty list for explicitly none)")
	}
	if m.AcceptanceEvidence == "" {
		add("acceptance_evidence", "missing acceptance evidence")
	}
	if m.OperationsOwner == "" {
		add("operations_owner", "missing operations owner")
	}
	if m.DisplacedScope == "" {
		add("displaced_scope", "missing displaced scope")
	}

	return violations
}

// canonicalManifest is the JSON projection used for digesting: field order
// is fixed by struct declaration order and dependencies are sorted so two
// manifests that differ only in dependency listing order digest identically.
type canonicalManifest struct {
	ID                 string   `json:"id"`
	Owner              string   `json:"owner"`
	Estimate           string   `json:"estimate"`
	Gate               string   `json:"gate"`
	Dependencies       []string `json:"dependencies"`
	AcceptanceEvidence string   `json:"acceptance_evidence"`
	OperationsOwner    string   `json:"operations_owner"`
	DisplacedScope     string   `json:"displaced_scope"`
}

// CanonicalDigest returns a stable hex-encoded SHA-256 digest of m's
// canonical JSON projection. Equal manifests (dependency order aside)
// always produce the same digest; the digest is undefined until m passes
// Validate with zero violations.
func CanonicalDigest(m Manifest) (string, error) {
	deps := []string{}
	if m.Dependencies != nil {
		deps = append(deps, (*m.Dependencies)...)
	}
	sort.Strings(deps)

	c := canonicalManifest{
		ID:                 m.ID,
		Owner:              m.Owner,
		Estimate:           m.Estimate,
		Gate:               m.Gate,
		Dependencies:       deps,
		AcceptanceEvidence: m.AcceptanceEvidence,
		OperationsOwner:    m.OperationsOwner,
		DisplacedScope:     m.DisplacedScope,
	}

	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
