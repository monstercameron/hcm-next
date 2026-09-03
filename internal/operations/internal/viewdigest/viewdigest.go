// Package viewdigest computes deterministic "sha256:<hex>" digests over the
// read-only projections internal/operations/inspector, .../explorer and
// .../authzsim return.
//
// It exists so those three packages can each attach a content digest to
// their views ("every view is deterministic, carries a digest" -
// planning/todos.md ADMIN-002/ADMIN-003) without importing
// internal/engines/canonicalbytes, which sits outside this root's declared
// import allow-list. The technique it uses - a length-prefixed, labeled
// concatenation hashed with sha256 - is the same discipline
// internal/trust/authz's canonicalRequestDigest and
// internal/data/ledger.SHA256Digester already use: length-prefixing every
// field makes the preimage unambiguous, so no two distinct field sequences
// can collide merely by concatenation, and labeling every field means a
// caller can never mix up which value contributed what.
//
// This package is unexported below internal/operations/internal, so only
// code rooted at internal/operations can import it (Go's internal/ import
// rule): it is a private implementation detail of this root, not a shared
// platform utility.
package viewdigest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strconv"
)

// Builder accumulates labeled fields into one running hash and renders the
// final digest. The zero value is not usable; construct one with [New].
type Builder struct {
	h hash.Hash
}

// New returns an empty Builder.
func New() *Builder {
	return &Builder{h: sha256.New()}
}

// String records one labeled string field.
func (b *Builder) String(label, v string) *Builder {
	fmt.Fprintf(b.h, "%s=%d:%s;", label, len(v), v)
	return b
}

// Bool records one labeled boolean field.
func (b *Builder) Bool(label string, v bool) *Builder {
	if v {
		return b.String(label, "1")
	}
	return b.String(label, "0")
}

// Int records one labeled integer field.
func (b *Builder) Int(label string, v int64) *Builder {
	return b.String(label, strconv.FormatInt(v, 10))
}

// Uint records one labeled unsigned integer field.
func (b *Builder) Uint(label string, v uint64) *Builder {
	return b.String(label, strconv.FormatUint(v, 10))
}

// Strings records one labeled, ordered list of strings exactly as given. Use
// this when the caller-visible order is itself material (for example, a
// node traversal order); use [Builder.SortedStrings] when only membership is
// material.
func (b *Builder) Strings(label string, values []string) *Builder {
	fmt.Fprintf(b.h, "%s#%d:", label, len(values))
	for i, v := range values {
		b.String(fmt.Sprintf("%s[%d]", label, i), v)
	}
	return b
}

// SortedStrings records one labeled list of strings after sorting a copy, so
// caller-supplied ordering (typically a map iteration) can never change the
// digest.
func (b *Builder) SortedStrings(label string, values []string) *Builder {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return b.Strings(label, sorted)
}

// Digest renders the accumulated hash as "sha256:<hex>".
func (b *Builder) Digest() string {
	return "sha256:" + hex.EncodeToString(b.h.Sum(nil))
}
