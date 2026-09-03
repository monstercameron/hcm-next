package config

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
)

// ChangeKind classifies how a key's presence differs between two snapshots.
type ChangeKind uint8

// Declared change kinds.
const (
	ChangeUnspecified ChangeKind = iota
	ChangeAdded
	ChangeRemoved
	ChangeChanged
)

var changeKindWire = map[ChangeKind]string{
	ChangeAdded:   "ADDED",
	ChangeRemoved: "REMOVED",
	ChangeChanged: "CHANGED",
}

// String returns the stable wire token.
func (k ChangeKind) String() string {
	if w, ok := changeKindWire[k]; ok {
		return w
	}
	return "CHANGE_KIND_UNSPECIFIED"
}

// Compatibility classifies whether a change can be adopted without review.
type Compatibility uint8

// Declared compatibility classes.
const (
	CompatibilityUnspecified Compatibility = iota
	CompatibilityCompatible
	CompatibilityBreaking
)

var compatibilityWire = map[Compatibility]string{
	CompatibilityCompatible: "COMPATIBLE",
	CompatibilityBreaking:   "BREAKING",
}

// String returns the stable wire token.
func (c Compatibility) String() string {
	if w, ok := compatibilityWire[c]; ok {
		return w
	}
	return "COMPATIBILITY_UNSPECIFIED"
}

// Change is one semantic difference between two snapshots for a single key.
//
// BeforeValue and AfterValue are always empty for a secret-kind entry:
// SecretFingerprintChanged is the only signal a Change ever carries about a
// secret, because Entry structurally cannot carry a secret literal value in
// the first place.
type Change struct {
	Key           string
	Kind          ChangeKind
	Compatibility Compatibility

	BeforeKind ValueKind
	AfterKind  ValueKind

	BeforeValue string
	AfterValue  string

	SecretFingerprintChanged bool

	// ImpactedCapabilities, ImpactedWorkflows, and ImpactedTenants are the
	// union of the before and after entry referenced ids: the referenced
	// capability/workflow/tenant impacts CONFIG-001 requires every changed,
	// added, or removed entry to surface.
	ImpactedCapabilities []string
	ImpactedWorkflows    []string
	ImpactedTenants      []string
}

// DiffResult is the deterministic outcome of comparing two snapshots. Added,
// Removed, and Changed are each sorted by key. Digest is the hex sha256 of
// the canonical, order-independent encoding of the three lists: two Diff
// calls over equivalent snapshots, however their entries were assembled,
// always produce the same Digest.
type DiffResult struct {
	Added   []Change
	Removed []Change
	Changed []Change
	Digest  string
}

// Diff computes the semantic difference between before and after. It never
// reads or reports a secret literal value, and it never hides a change to a
// key whose SemanticClass is not SemanticGeneric or that references a
// capability, workflow, or tenant: such a change is always classified
// CompatibilityBreaking.
func Diff(before, after Snapshot) DiffResult {
	beforeEntries := before.Entries()
	afterEntries := after.Entries()

	beforeIdx := make(map[string]Entry, len(beforeEntries))
	for _, e := range beforeEntries {
		beforeIdx[e.Key] = e
	}
	afterIdx := make(map[string]Entry, len(afterEntries))
	for _, e := range afterEntries {
		afterIdx[e.Key] = e
	}

	keySet := make(map[string]struct{}, len(beforeIdx)+len(afterIdx))
	for k := range beforeIdx {
		keySet[k] = struct{}{}
	}
	for k := range afterIdx {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result DiffResult
	for _, key := range keys {
		b, hasBefore := beforeIdx[key]
		a, hasAfter := afterIdx[key]
		switch {
		case !hasBefore && hasAfter:
			result.Added = append(result.Added, addedOrRemoved(ChangeAdded, a))
		case hasBefore && !hasAfter:
			result.Removed = append(result.Removed, addedOrRemoved(ChangeRemoved, b))
		default:
			if c, changed := diffEntry(b, a); changed {
				result.Changed = append(result.Changed, c)
			}
		}
	}

	result.Digest = computeDiffDigest(result)
	return result
}

// hasImpact reports whether an entry references any capability, workflow,
// or tenant.
func hasImpact(e Entry) bool {
	return len(e.Refs.Capabilities) > 0 || len(e.Refs.Workflows) > 0 || len(e.Refs.Tenants) > 0
}

func addedOrRemoved(kind ChangeKind, e Entry) Change {
	compat := CompatibilityCompatible
	switch {
	case e.Semantic != SemanticGeneric:
		compat = CompatibilityBreaking
	case hasImpact(e):
		compat = CompatibilityBreaking
	case kind == ChangeRemoved && e.Explicit:
		// Removing a value the operator explicitly set is never a free
		// pass, even for a generic key: the effective value in force
		// changes, unlike removing a key that only mirrored its default.
		compat = CompatibilityBreaking
	}

	c := Change{
		Key:                  e.Key,
		Kind:                 kind,
		Compatibility:        compat,
		ImpactedCapabilities: e.Refs.Capabilities,
		ImpactedWorkflows:    e.Refs.Workflows,
		ImpactedTenants:      e.Refs.Tenants,
	}
	if e.Kind != KindSecretRef {
		if kind == ChangeAdded {
			c.AfterKind, c.AfterValue = e.Kind, e.Value
		} else {
			c.BeforeKind, c.BeforeValue = e.Kind, e.Value
		}
	} else {
		if kind == ChangeAdded {
			c.AfterKind = e.Kind
		} else {
			c.BeforeKind = e.Kind
		}
	}
	return c
}

func diffEntry(before, after Entry) (Change, bool) {
	kindChanged := before.Kind != after.Kind
	valueChanged := before.Value != after.Value
	fingerprintChanged := before.SecretFingerprint != after.SecretFingerprint
	semanticChanged := before.Semantic != after.Semantic
	explicitChanged := before.Explicit != after.Explicit
	refsChanged := !equalStrings(before.Refs.Capabilities, after.Refs.Capabilities) ||
		!equalStrings(before.Refs.Workflows, after.Refs.Workflows) ||
		!equalStrings(before.Refs.Tenants, after.Refs.Tenants)

	changed := kindChanged || valueChanged || fingerprintChanged || semanticChanged || explicitChanged || refsChanged
	if !changed {
		return Change{}, false
	}

	compat := CompatibilityCompatible
	switch {
	case kindChanged:
		compat = CompatibilityBreaking
	case refsChanged:
		compat = CompatibilityBreaking
	case before.Semantic != SemanticGeneric || after.Semantic != SemanticGeneric:
		// A workflow/schema/mapping/policy-classed entry never gets a free
		// pass on a value, fingerprint, or semantic reclassification
		// change: whatever consumes that pointer is affected either way.
		if semanticChanged || valueChanged || fingerprintChanged {
			compat = CompatibilityBreaking
		}
	}

	c := Change{
		Key:                      before.Key,
		Kind:                     ChangeChanged,
		Compatibility:            compat,
		BeforeKind:               before.Kind,
		AfterKind:                after.Kind,
		SecretFingerprintChanged: fingerprintChanged,
		ImpactedCapabilities:     unionStrings(before.Refs.Capabilities, after.Refs.Capabilities),
		ImpactedWorkflows:        unionStrings(before.Refs.Workflows, after.Refs.Workflows),
		ImpactedTenants:          unionStrings(before.Refs.Tenants, after.Refs.Tenants),
	}
	if before.Kind != KindSecretRef {
		c.BeforeValue = before.Value
	}
	if after.Kind != KindSecretRef {
		c.AfterValue = after.Value
	}
	return c, true
}

func unionStrings(a, b []string) []string {
	return normalizeRefs(append(append([]string(nil), a...), b...))
}

func appendStr(dst []byte, s string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(s)))
	return append(dst, s...)
}

func appendStrs(dst []byte, ss []string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(ss)))
	for _, s := range ss {
		dst = appendStr(dst, s)
	}
	return dst
}

func appendBool(dst []byte, b bool) []byte {
	if b {
		return append(dst, 1)
	}
	return append(dst, 0)
}

func appendChange(dst []byte, c Change) []byte {
	dst = appendStr(dst, c.Key)
	dst = append(dst, byte(c.Kind), byte(c.Compatibility), byte(c.BeforeKind), byte(c.AfterKind))
	dst = appendStr(dst, c.BeforeValue)
	dst = appendStr(dst, c.AfterValue)
	dst = appendBool(dst, c.SecretFingerprintChanged)
	dst = appendStrs(dst, c.ImpactedCapabilities)
	dst = appendStrs(dst, c.ImpactedWorkflows)
	dst = appendStrs(dst, c.ImpactedTenants)
	return dst
}

// computeDiffDigest returns the hex sha256 of a deterministic encoding of
// the three change lists carried by r. Each list is already sorted by key,
// and the encoding itself never depends on Go map iteration order.
func computeDiffDigest(r DiffResult) string {
	b := []byte("hcmnext.config.diff.v1")
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.Added)))
	for _, c := range r.Added {
		b = appendChange(b, c)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.Removed)))
	for _, c := range r.Removed {
		b = appendChange(b, c)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.Changed)))
	for _, c := range r.Changed {
		b = appendChange(b, c)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
