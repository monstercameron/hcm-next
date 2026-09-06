package configregistry

import "time"

// Kind is a configuration object's closed vocabulary. There is no open
// string here: a caller cannot silently invent a tenth kind, and the
// Postgres adapter's CHECK constraint (migrations/*_config_object.sql)
// enforces the identical list at the storage boundary.
type Kind string

// The nine declared kinds. Chosen to match the vocabulary
// internal/platform/config.ObjectKind already publishes for signed
// distribution, so a body this package resolves as active names the same
// kind a bundle later signs it under.
const (
	KindWorkflow   Kind = "WORKFLOW"
	KindPolicy     Kind = "POLICY"
	KindSchema     Kind = "SCHEMA"
	KindRule       Kind = "RULE"
	KindConnector  Kind = "CONNECTOR"
	KindAgent      Kind = "AGENT"
	KindReference  Kind = "REFERENCE"
	KindMapping    Kind = "MAPPING"
	KindCapability Kind = "CAPABILITY"
)

// Valid reports whether k is one of the declared kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindWorkflow, KindPolicy, KindSchema, KindRule, KindConnector,
		KindAgent, KindReference, KindMapping, KindCapability:
		return true
	default:
		return false
	}
}

// Scope names the tenant (and, optionally, the narrower cell within that
// tenant) a configuration object and its activation apply to. TenantID is
// always required: there is no tenant-less configuration object in this
// package, matching every other tenant-scoped table in migrations/. CellID
// is optional — an empty CellID means the object is scoped to the whole
// tenant rather than one cell within it.
type Scope struct {
	TenantID string `json:"tenant_id"`
	CellID   string `json:"cell_id,omitempty"`
}

// key returns Scope's identity as a single comparable string, used by the
// in-memory [Registry] to index by (scope, kind, id).
func (s Scope) key() string {
	return s.TenantID + "\x00" + s.CellID
}

// valid reports whether s carries the one field this package always
// requires.
func (s Scope) valid() bool {
	return s.TenantID != ""
}

// ObjectRef names one exact published revision: the identity [Activate] and
// [Store.GetByKey] look a [ConfigurationObject] up by.
type ObjectRef struct {
	Scope    Scope  `json:"scope"`
	Kind     Kind   `json:"kind"`
	ID       string `json:"id"`
	Revision uint32 `json:"revision"`
}

// ConfigurationObject is the immutable, durable record of one published
// configuration revision. It is a value, not a handle: every accessor and
// every function in this package returns a copy, never a pointer into a
// [Store]'s own storage. There are no setters — a new revision is always a
// new value [Publish] mints and a [Store] persists; nothing in this package
// ever edits one in place.
type ConfigurationObject struct {
	Kind     Kind   `json:"kind"`
	ID       string `json:"id"`
	Revision uint32 `json:"revision"`

	// Body is the canonical configuration payload this revision carries.
	// This package treats it as opaque bytes; SchemaRef names what it
	// conforms to.
	Body []byte `json:"body"`
	// CanonicalBodyDigest is the content address of Body: sha256 over Body
	// alone, under this package's own canonicalization profile. [Publish]
	// computes it; a caller-supplied value that disagrees with Body is
	// refused rather than trusted.
	CanonicalBodyDigest string `json:"canonical_body_digest"`

	// SchemaRef names the schema this revision's Body conforms to, e.g.
	// "hcmnext.workflow.definition/v3". Required: an unschema'd body has no
	// way to be validated before it is resolved and used.
	SchemaRef string `json:"schema_ref"`

	Scope Scope `json:"scope"`

	// PublisherPrincipal is the principal who ran [Publish]. Provenance
	// only — it is not an authorization decision; [Activate] carries the
	// separate governance evidence for that.
	PublisherPrincipal string    `json:"publisher_principal"`
	PublishedAt        time.Time `json:"published_at"`

	digest string
}

// Ref names this object by the identity a [Store] indexes it under.
func (o ConfigurationObject) Ref() ObjectRef {
	return ObjectRef{Scope: o.Scope, Kind: o.Kind, ID: o.ID, Revision: o.Revision}
}

// Digest is the record's own content identity: the canonical digest over
// every field. Use it to detect whether a record retrieved from a [Store]
// was ever tampered with (see [ConfigurationObject.Verify]); use
// [ConfigurationObject.CanonicalBodyDigest] to address the body alone.
func (o ConfigurationObject) Digest() string { return o.digest }

// Verify recomputes o's digest from its current content and reports whether
// it still matches the digest minted at publication.
func (o ConfigurationObject) Verify() error {
	if got := computeRecordDigest(o); got != o.digest {
		return refuse(CodeRecordMutated, o.ID,
			"configuration object %s/%s@%d content no longer matches its digest", o.Kind, o.ID, o.Revision)
	}
	return nil
}

// Rehydrate reconstructs a [ConfigurationObject] a [Store] adapter outside
// this package loaded from durable storage (e.g.
// internal/data/configregistry's PostgreSQL adapter), given the digest the
// durable record itself carries. It recomputes the digest from o's exported
// fields and refuses with [CodeRecordMutated] if it disagrees with
// storedDigest — the same check [ConfigurationObject.Verify] performs,
// applied at load time so a durable adapter never needs (and is never
// given) a way to poke the unexported digest field directly.
//
// A [Store] adapter's Digest() and Verify() must behave identically whether
// the value passed through [Publish] in-process or was loaded back from
// storage; Rehydrate is what makes that true without exporting the digest
// field itself for a caller to forge.
func Rehydrate(o ConfigurationObject, storedDigest string) (ConfigurationObject, error) {
	got := computeRecordDigest(o)
	if got != storedDigest {
		return ConfigurationObject{}, refuse(CodeRecordMutated, o.ID,
			"stored configuration object %s/%s@%d does not match its own recorded digest", o.Kind, o.ID, o.Revision)
	}
	o.digest = got
	return o, nil
}

// clone returns a deep copy of o so a caller mutating a value returned by
// this package can never reach into a [Store]'s own storage.
func (o ConfigurationObject) clone() ConfigurationObject {
	c := o
	c.Body = append([]byte(nil), o.Body...)
	return c
}

// ActivationEvidence is what a caller presents to [Activate]. It is
// provenance for the activation act itself, not an approval workflow — this
// package does not adjudicate authority the way, say, ledger governance
// does; a caller that needs a stronger gate composes one in front of
// [Activate].
type ActivationEvidence struct {
	// ActivatedBy names the principal performing the activation. Required:
	// an empty value is an unattributed activation and [Activate] refuses
	// it.
	ActivatedBy string
	// Authority names the role or authority the activating principal acted
	// under. Optional context, not itself validated.
	Authority string
	// Reason is a free-text justification. Optional.
	Reason string
	// ActivatedAt is the moment of activation. Required — this package has
	// no clock of its own, so every timestamp it records is supplied by the
	// caller.
	ActivatedAt time.Time
}

// ActivationRecord is one governed activation act, permanently recorded.
// [Activate] appends one on every successful call; it never edits or
// deletes an earlier record for the same (scope, kind, id) — a later
// activation simply supersedes an earlier one by recency, and the earlier
// record's evidence remains in [Store.ListActivations]'s history.
type ActivationRecord struct {
	Scope    Scope  `json:"scope"`
	Kind     Kind   `json:"kind"`
	ID       string `json:"id"`
	Revision uint32 `json:"revision"`

	ActivatedBy string    `json:"activated_by"`
	Authority   string    `json:"authority,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	ActivatedAt time.Time `json:"activated_at"`

	// ObjectDigest is the activated [ConfigurationObject.Digest] at the
	// moment of activation, so a later [ConfigurationObject.Verify] failure
	// against the stored revision is distinguishable from "the object
	// simply changed identity after this activation was recorded" — it
	// cannot, because revisions are immutable, but the record still names
	// exactly what it activated.
	ObjectDigest string `json:"object_digest"`
}

// Ref names the revision this activation record activated.
func (a ActivationRecord) Ref() ObjectRef {
	return ObjectRef{Scope: a.Scope, Kind: a.Kind, ID: a.ID, Revision: a.Revision}
}

// clone returns a deep copy of a. ActivationRecord currently holds no
// reference types, so this is a plain value copy; it exists so callers of
// this package never need to know that fact to stay safe if the type grows
// one.
func (a ActivationRecord) clone() ActivationRecord { return a }
