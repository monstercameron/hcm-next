package conflict

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FieldPath is a normalized, dot-segmented, schema-registry canonical field
// path a write touches. It is never a display name: two different display
// spellings of the same schema-registry field must resolve to one FieldPath,
// through a [FieldAliasTable], before any comparison happens.
type FieldPath string

// Validate rejects an empty path or one with an empty segment.
func (p FieldPath) Validate() error {
	if p == "" {
		return fmt.Errorf("%w: field path is empty", ErrInvalidFootprint)
	}
	for _, seg := range strings.Split(string(p), ".") {
		if seg == "" {
			return fmt.Errorf("%w: field path %q has an empty segment", ErrInvalidFootprint, p)
		}
	}
	return nil
}

// IsAncestorOf reports whether p is a strict dot-segment ancestor of other:
// every segment of p matches other's corresponding segment, and other has at
// least one more segment. "employment.assignment" is an ancestor of
// "employment.assignment.grade" but not of "employment.assignment2" -- the
// comparison is over whole segments, never a raw string prefix, so a field
// named similarly to a parent path can never evade detection by accident.
func (p FieldPath) IsAncestorOf(other FieldPath) bool {
	pSegs := strings.Split(string(p), ".")
	oSegs := strings.Split(string(other), ".")
	if len(oSegs) <= len(pSegs) {
		return false
	}
	for i, seg := range pSegs {
		if oSegs[i] != seg {
			return false
		}
	}
	return true
}

// Overlaps reports whether two normalized field paths name the same field or
// stand in a parent/child relationship, either of which means a write to one
// can affect the other.
func (p FieldPath) Overlaps(other FieldPath) bool {
	return p == other || p.IsAncestorOf(other) || other.IsAncestorOf(p)
}

// FieldAliasTable is a versioned, domain-supplied mapping from a historical
// or alternate field spelling to its current canonical path. It is the only
// place alias resolution happens, so no two call sites can disagree about
// what a given alias means, and a client can never underdeclare a footprint
// by presenting a stale alias instead of the field it actually names.
type FieldAliasTable struct {
	RuleRef string
	Version string
	Aliases map[FieldPath]FieldPath
}

// Resolve follows the alias chain to its canonical path. A cycle or a
// self-referential entry stops the walk at the point it was already visited,
// rather than looping forever.
func (t FieldAliasTable) Resolve(p FieldPath) FieldPath {
	seen := map[FieldPath]bool{}
	cur := p
	for {
		next, ok := t.Aliases[cur]
		if !ok || next == cur || seen[cur] {
			return cur
		}
		seen[cur] = true
		cur = next
	}
}

// Operation names the write semantics a footprint declares.
type Operation string

// Declared operations. OperationUnspecified is the zero value and never
// legal on a resolvable footprint.
const (
	OperationUnspecified Operation = ""
	OperationCreate      Operation = "CREATE"
	OperationUpdate      Operation = "UPDATE"
	OperationDelete      Operation = "DELETE"
	OperationUpsert      Operation = "UPSERT"
)

var validOperations = map[Operation]bool{
	OperationCreate: true, OperationUpdate: true, OperationDelete: true, OperationUpsert: true,
}

// Valid reports whether o is a declared operation.
func (o Operation) Valid() bool { return validOperations[o] }

// AuthorityScope names the source-authority domain and policy a write
// claims. Two footprints that overlap but claim different authority scopes
// are exactly the case a domain rule must resolve explicitly; this package
// never infers which authority wins.
type AuthorityScope struct {
	Domain    string
	PolicyRef string
}

// Validate rejects an authority scope missing its domain or policy
// reference.
func (s AuthorityScope) Validate() error {
	if s.Domain == "" || s.PolicyRef == "" {
		return fmt.Errorf("%w: authority scope needs a domain and a policy reference", ErrInvalidFootprint)
	}
	return nil
}

// WriteFootprint is one normalized write a proposal declares: the exact
// resource, canonical field path, effective interval, operation and expected
// revision it touches, plus the authority scope it claims. Every dimension
// is mandatory: a write set that omits one can silently evade overlap
// detection, which is exactly the failure CONFLICT-001 exists to close.
type WriteFootprint struct {
	Resource         values.ResourceKey
	Field            FieldPath
	Interval         values.EffectiveInterval
	Operation        Operation
	ExpectedRevision values.RevisionToken
	Authority        AuthorityScope
}

// Validate rejects an underdeclared footprint.
func (w WriteFootprint) Validate() error {
	if err := w.Resource.Validate(); err != nil {
		return fmt.Errorf("%w: resource: %v", ErrInvalidFootprint, err)
	}
	if err := w.Field.Validate(); err != nil {
		return err
	}
	if err := w.Interval.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidFootprint, err)
	}
	if !w.Operation.Valid() {
		return fmt.Errorf("%w: operation %q is not declared", ErrInvalidFootprint, w.Operation)
	}
	if !w.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: write on %s pins no expected revision", ErrInvalidFootprint, w.Field)
	}
	if err := w.Authority.Validate(); err != nil {
		return err
	}
	return nil
}

// NormalizeFootprint resolves field aliases under the current rule version
// and re-validates. Domain compilers and capabilities call this before a
// footprint enters a write set, so a client presenting a stale alias can
// never underdeclare what field it actually touches.
func NormalizeFootprint(raw WriteFootprint, aliases FieldAliasTable) (WriteFootprint, error) {
	raw.Field = aliases.Resolve(raw.Field)
	if err := raw.Validate(); err != nil {
		return WriteFootprint{}, err
	}
	return raw, nil
}

// Overlaps reports whether two normalized footprints touch the same resource
// in overlapping field paths and effective time. Field-path aliasing must
// already be resolved (see [NormalizeFootprint]); an open-ended interval
// always overlaps forward, per [values.EffectiveInterval.Overlaps], so an
// open-ended write can never be used to dodge overlap detection.
func (w WriteFootprint) Overlaps(other WriteFootprint) (bool, error) {
	if err := w.Validate(); err != nil {
		return false, err
	}
	if err := other.Validate(); err != nil {
		return false, err
	}
	if !w.Resource.Equal(other.Resource) {
		return false, nil
	}
	if !w.Field.Overlaps(other.Field) {
		return false, nil
	}
	return w.Interval.Overlaps(other.Interval)
}

// footprintMagic prefixes every canonical byte stream this file produces.
// Changing it is a new encoding generation, never an edit in place.
const footprintMagic = "hcmnext.transaction.conflict.write_footprint.v1"

// Canonical returns the footprint's canonical semantic encoding, or nil when
// the footprint is invalid. It uses the same length-framed rules as
// internal/intent's material and plan digests, so field order and struct
// layout can never change the result.
func (w WriteFootprint) Canonical() []byte {
	if err := w.Validate(); err != nil {
		return nil
	}
	e := newEnc(footprintMagic)
	e.raw(w.Resource.Canonical())
	e.str(string(w.Field))
	e.raw(w.Interval.Canonical())
	e.str(string(w.Operation))
	e.raw(w.ExpectedRevision.Canonical())
	e.str(w.Authority.Domain).str(w.Authority.PolicyRef)
	return e.bytes()
}

// Digest returns the hex SHA-256 of the footprint's canonical bytes, or ""
// when the footprint is invalid.
func (w WriteFootprint) Digest() string {
	b := w.Canonical()
	if b == nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ScopeDigest identifies the normalized write scope independently of the
// baseline revision. Competing writers at different observed revisions still
// contend for the same durable scope fence.
func (w WriteFootprint) ScopeDigest() string {
	if err := w.Validate(); err != nil {
		return ""
	}
	e := newEnc("hcmnext.transaction.conflict.write_scope.v1")
	e.raw(w.Resource.Canonical()).str(string(w.Field)).raw(w.Interval.Canonical())
	e.str(string(w.Operation)).str(w.Authority.Domain).str(w.Authority.PolicyRef)
	sum := sha256.Sum256(e.bytes())
	return hex.EncodeToString(sum[:])
}

// enc is the length-framed canonical byte builder, mirroring the pattern
// internal/intent uses for its own material and plan digests.
type enc struct{ buf []byte }

func newEnc(magic string) *enc {
	e := &enc{}
	e.str(magic)
	return e
}

func (e *enc) bytes() []byte { return e.buf }

func (e *enc) str(s string) *enc {
	e.buf = binary.AppendUvarint(e.buf, uint64(len(s)))
	e.buf = append(e.buf, s...)
	return e
}

func (e *enc) raw(b []byte) *enc {
	e.buf = binary.AppendUvarint(e.buf, uint64(len(b)))
	e.buf = append(e.buf, b...)
	return e
}
