package config

import "sort"

// ValueKind is the typed kind of a configuration value. A [Snapshot] never
// carries an untyped value: CONFIG-001 requires typed keys so that a type
// change between two snapshots is always detectable, even when the textual
// representation of the two values happens to look similar.
type ValueKind uint8

// Declared value kinds. The zero value, KindUnspecified, is never legal on a
// constructed [Entry].
const (
	KindUnspecified ValueKind = iota
	KindString
	KindInt
	KindBool
	KindFloat
	KindDuration
	KindList
	KindMap
	// KindSecretRef marks an entry whose value is a secret. Such an entry
	// never carries Value; see [NewSecretEntry].
	KindSecretRef
)

var valueKindWire = map[ValueKind]string{
	KindString:    "STRING",
	KindInt:       "INT",
	KindBool:      "BOOL",
	KindFloat:     "FLOAT",
	KindDuration:  "DURATION",
	KindList:      "LIST",
	KindMap:       "MAP",
	KindSecretRef: "SECRET_REF",
}

// String returns the stable wire token.
func (k ValueKind) String() string {
	if w, ok := valueKindWire[k]; ok {
		return w
	}
	return "VALUE_KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared kind.
func (k ValueKind) Valid() bool {
	_, ok := valueKindWire[k]
	return ok
}

// SemanticClass names the domain a configuration entry participates in. An
// entry outside SemanticGeneric can never be classified Compatible purely
// because its textual value changed: a workflow, schema, mapping, or policy
// pointer governs behavior other components rely on, so a change to one of
// them is always [CompatibilityBreaking]. This is the mechanism that keeps
// a semantic workflow/policy/schema/mapping change from being hidden inside
// an otherwise-quiet diff.
type SemanticClass uint8

// Declared semantic classes.
const (
	// SemanticGeneric is an ordinary preference or tuning value with no
	// special downstream binding.
	SemanticGeneric SemanticClass = iota
	SemanticWorkflow
	SemanticSchema
	SemanticMapping
	SemanticPolicy
)

var semanticClassWire = map[SemanticClass]string{
	SemanticGeneric:  "GENERIC",
	SemanticWorkflow: "WORKFLOW",
	SemanticSchema:   "SCHEMA",
	SemanticMapping:  "MAPPING",
	SemanticPolicy:   "POLICY",
}

// String returns the stable wire token.
func (s SemanticClass) String() string {
	if w, ok := semanticClassWire[s]; ok {
		return w
	}
	return "SEMANTIC_CLASS_UNSPECIFIED"
}

// Refs names the capability, workflow, and tenant identifiers a
// configuration entry's value references. Diff surfaces these as the
// "referenced capability/workflow/tenant impacts" of a change; a change to
// any of them is always [CompatibilityBreaking], regardless of the entry's
// [SemanticClass].
type Refs struct {
	Capabilities []string
	Workflows    []string
	Tenants      []string
}

func normalizeRefs(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (r Refs) normalized() Refs {
	return Refs{
		Capabilities: normalizeRefs(r.Capabilities),
		Workflows:    normalizeRefs(r.Workflows),
		Tenants:      normalizeRefs(r.Tenants),
	}
}

func equalStrings(a, b []string) bool {
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

// Entry is one typed configuration value. Construct it with [NewEntry] or
// [NewSecretEntry]; the zero Entry is invalid.
//
// A secret-kind entry never carries Value: only SecretFingerprint, an
// opaque comparison token the owner derives out of band (for example a
// salted hash of the secret material, or the version id of a vault
// reference). [NewSecretEntry] is the only constructor that can produce a
// KindSecretRef entry, and it refuses a caller that also supplies a literal
// value. [NewSnapshot] independently re-checks this invariant on every
// entry it is given, because Entry's fields are plain and exported: nothing
// stops a caller from building one with a struct literal instead of a
// constructor, and the platform cannot rely on every caller using the
// constructor.
type Entry struct {
	Key      string
	Kind     ValueKind
	Value    string
	Explicit bool
	Semantic SemanticClass

	// SecretFingerprint is set only when Kind is KindSecretRef.
	SecretFingerprint string

	Refs Refs
}

// NewEntry builds a non-secret typed entry.
func NewEntry(key string, kind ValueKind, value string, explicit bool, semantic SemanticClass, refs Refs) (Entry, error) {
	if key == "" {
		return Entry{}, newError("NewEntry", ErrEmptyKey, "")
	}
	if !kind.Valid() || kind == KindSecretRef {
		return Entry{}, newError("NewEntry", ErrInvalidKind, "%s", kind)
	}
	return Entry{
		Key:      key,
		Kind:     kind,
		Value:    value,
		Explicit: explicit,
		Semantic: semantic,
		Refs:     refs.normalized(),
	}, nil
}

// NewSecretEntry builds a secret-reference entry. fingerprint must be
// non-empty; it is compared as an opaque token, and a [Change] reports only
// whether it changed, never the fingerprint or the underlying secret.
func NewSecretEntry(key, fingerprint string, explicit bool, semantic SemanticClass, refs Refs) (Entry, error) {
	if key == "" {
		return Entry{}, newError("NewSecretEntry", ErrEmptyKey, "")
	}
	if fingerprint == "" {
		return Entry{}, newError("NewSecretEntry", ErrMissingFingerprint, "key %q", key)
	}
	return Entry{
		Key:               key,
		Kind:              KindSecretRef,
		SecretFingerprint: fingerprint,
		Explicit:          explicit,
		Semantic:          semantic,
		Refs:              refs.normalized(),
	}, nil
}

func (e Entry) validate() error {
	if e.Key == "" {
		return newError("Entry.validate", ErrEmptyKey, "")
	}
	if !e.Kind.Valid() {
		return newError("Entry.validate", ErrInvalidKind, "key %q", e.Key)
	}
	if e.Kind == KindSecretRef {
		if e.SecretFingerprint == "" {
			return newError("Entry.validate", ErrMissingFingerprint, "key %q", e.Key)
		}
		if e.Value != "" {
			return newError("Entry.validate", ErrSecretValueLeak, "key %q", e.Key)
		}
	}
	return nil
}

// Snapshot is an immutable set of typed configuration entries, keyed by
// their Key. Two snapshots built from the same entries in different
// construction order compare and hash identically: [Diff] and a snapshot's
// own [Snapshot.Entries] are always produced sorted by key, never by
// insertion or map iteration order.
type Snapshot struct {
	Name    string
	Version string
	entries map[string]Entry
}

// NewSnapshot validates and indexes entries by key. It rejects an invalid
// entry, a duplicate key, and — independently of [NewSecretEntry] — any
// secret-kind entry that carries a literal value, so a caller cannot bypass
// that invariant by building an Entry directly.
func NewSnapshot(name, version string, entries []Entry) (Snapshot, error) {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return Snapshot{}, err
		}
		if _, dup := m[e.Key]; dup {
			return Snapshot{}, newError("NewSnapshot", ErrDuplicateKey, "%q", e.Key)
		}
		e.Refs = e.Refs.normalized()
		m[e.Key] = e
	}
	return Snapshot{Name: name, Version: version, entries: m}, nil
}

// Entries returns every entry, sorted by key. The returned slice is a copy;
// mutating it never changes the Snapshot.
func (s Snapshot) Entries() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Len returns the number of entries in the snapshot.
func (s Snapshot) Len() int { return len(s.entries) }
