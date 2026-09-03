package onboarding

import (
	"crypto/ed25519"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// Budget bounds the total resources one onboarding extraction job may spend
// across every object it reads.
//
// It is deliberately separate from [connectivity.Bounds]: Bounds negotiates
// what one page request may ask a provider for; Budget is the onboarding
// job's own ceiling on how much of that negotiated envelope it is willing to
// spend, cumulatively, before it must stop and hand back a resumable
// checkpoint. Two objects sharing one [BudgetGuard] exhaust one shared
// ceiling, because the budget is a property of the job, not of one object's
// traversal.
type Budget struct {
	// MaxPages caps the total pages read across the job.
	MaxPages uint64
	// MaxRecords caps the total records accepted across the job. A
	// quarantined record does not count against it: a poison row was never
	// admitted, so it never spent budget.
	MaxRecords uint64
	// MaxBytes caps the total canonical record bytes accepted across the job.
	MaxBytes uint64
	// MaxWallTime caps how long the job may run, measured from its own
	// injected clock rather than the host wall clock, so a test can prove
	// the bound fires without sleeping.
	MaxWallTime time.Duration
}

// Validate reports whether the budget is internally coherent. Every axis is
// required: a zero-valued axis reads as "no limit was ever decided", and an
// undecided limit must not be publishable as an unbounded one.
func (b Budget) Validate() error {
	const op = "onboarding.Budget.Validate"
	switch {
	case b.MaxPages == 0:
		return newError(op, ErrInvalidManifest, "budget has no page limit")
	case b.MaxRecords == 0:
		return newError(op, ErrInvalidManifest, "budget has no record limit")
	case b.MaxBytes == 0:
		return newError(op, ErrInvalidManifest, "budget has no byte limit")
	case b.MaxWallTime <= 0:
		return newError(op, ErrInvalidManifest, "budget has no wall-time limit")
	}
	return nil
}

// ReferencePin pins one reference-data or crosswalk snapshot an onboarding
// job adjudicates identities against.
//
// Pinning at manifest-signing time is what makes "the crosswalk changed
// underneath the import" detectable rather than silently absorbed:
// [Adjudicator.Adjudicate] refuses to run against a loaded snapshot other
// than the one named here.
type ReferencePin struct {
	// Name identifies the pinned reference set, e.g.
	// "crosswalk.worker.workday->hcmnext".
	Name string
	// Version is the pinned snapshot version.
	Version string
	// Digest is the lowercase hex sha256 over the pinned snapshot content.
	Digest string
}

// Validate reports whether the pin names a specific, checkable snapshot.
func (p ReferencePin) Validate() error {
	const op = "onboarding.ReferencePin.Validate"
	switch {
	case strings.TrimSpace(p.Name) == "":
		return newError(op, ErrInvalidManifest, "reference pin has no name")
	case strings.TrimSpace(p.Version) == "":
		return newError(op, ErrInvalidManifest, "reference pin %q has no version", p.Name)
	case strings.TrimSpace(p.Digest) == "":
		return newError(op, ErrInvalidManifest, "reference pin %q has no digest", p.Name)
	}
	return nil
}

// OnboardingManifest is the immutable, versioned statement of what one
// tenant's onboarding job may read.
//
// Every field [OnboardingManifest.Validate] requires is required at
// publication for the same reason [connectivity.ConnectorDefinition] requires
// every contract: a manifest missing its owner, authority, classification,
// residency, retention, budget or rollback policy reads as "nobody decided
// this yet", and an undecided onboarding must not be publishable as a
// startable one.
type OnboardingManifest struct {
	// ManifestID is the caller-supplied identity of this manifest.
	ManifestID string
	// TenantID is the tenant this onboarding job observes on behalf of.
	TenantID string
	// OwnerRef identifies the human or role accountable for this onboarding.
	OwnerRef string
	// SourceAuthorityRef names the source-authority assignment under which
	// the incumbent's statements are recorded. It must match the connector's
	// own declared [connectivity.Descriptor.AuthorityRef]: a manifest naming
	// an authority the connector does not observe under is an unresolved
	// authority, and [Preflight] refuses it.
	SourceAuthorityRef string
	// Classification is the data classification label governing everything
	// this job reads, e.g. "PII,COMPENSATION".
	Classification string
	// Residency is the residency profile the extracted data must honour.
	Residency string
	// RetentionPolicy names the retention policy governing staged output.
	RetentionPolicy string
	// RollbackPolicy names the policy that governs undoing this onboarding.
	RollbackPolicy string
	// IdempotencyKey is the caller-supplied key that makes re-submitting this
	// manifest a no-op rather than a second onboarding job.
	IdempotencyKey string

	// ConnectorID and ConnectorVersion pin the connector definition this job
	// reads through.
	ConnectorID      string
	ConnectorVersion connectivity.Version
	// ConnectionID names the tenant connection this job reads through.
	ConnectionID string

	// Objects are the record families this job extracts.
	Objects []connectivity.ObjectKind
	// FieldAllowList narrows, per object, which fields this job admits. An
	// object absent from the map, or mapped to an empty slice, admits every
	// field the connector returns.
	FieldAllowList map[connectivity.ObjectKind][]string
	// SchemaVersionPins is the external schema version this job expects to
	// read each object under. Every declared object requires an entry.
	SchemaVersionPins map[connectivity.ObjectKind]string
	// ExpectedPopulation is the approximate record count this job expects to
	// observe per object, used downstream to sanity-check a completed
	// extraction against expectation rather than against nothing.
	ExpectedPopulation map[connectivity.ObjectKind]int64

	// ReferencePins are the reference-data and crosswalk snapshots identity
	// adjudication runs against. May be empty for a job that adjudicates
	// nothing.
	ReferencePins []ReferencePin
	// Budget is this job's whole-run resource ceiling.
	Budget Budget

	// CreatedAt is the drafting instant, supplied rather than read from the
	// wall clock so a manifest's digest is a pure function of its inputs.
	CreatedAt time.Time
	// CreatedBy identifies who drafted the manifest.
	CreatedBy string
}

const (
	manifestSchema  = "hcmnext.connectivity.onboarding.OnboardingManifest"
	manifestVersion = 1
)

// Validate reports every reason the manifest cannot be signed or activated.
// It fails on the first, matching [connectivity.ConnectorDefinition.Validate]:
// a caller fixing a manifest fixes one field at a time.
func (m OnboardingManifest) Validate() error {
	const op = "onboarding.OnboardingManifest.Validate"
	switch {
	case strings.TrimSpace(m.ManifestID) == "":
		return newError(op, ErrInvalidManifest, "manifest has no id")
	case strings.TrimSpace(m.TenantID) == "":
		return newError(op, ErrInvalidManifest, "manifest has no tenant")
	case strings.TrimSpace(m.OwnerRef) == "":
		return newError(op, ErrInvalidManifest, "manifest has no owner")
	case strings.TrimSpace(m.SourceAuthorityRef) == "":
		return newError(op, ErrInvalidManifest, "manifest has no source authority")
	case strings.TrimSpace(m.Classification) == "":
		return newError(op, ErrInvalidManifest, "manifest has no data classification")
	case strings.TrimSpace(m.Residency) == "":
		return newError(op, ErrInvalidManifest, "manifest has no residency profile")
	case strings.TrimSpace(m.RetentionPolicy) == "":
		return newError(op, ErrInvalidManifest, "manifest has no retention policy")
	case strings.TrimSpace(m.RollbackPolicy) == "":
		return newError(op, ErrInvalidManifest, "manifest has no rollback policy")
	case strings.TrimSpace(m.IdempotencyKey) == "":
		return newError(op, ErrInvalidManifest, "manifest has no idempotency key")
	case strings.TrimSpace(m.ConnectorID) == "":
		return newError(op, ErrInvalidManifest, "manifest names no connector")
	case m.ConnectorVersion.IsZero():
		return newError(op, ErrInvalidManifest, "manifest pins no connector version")
	case strings.TrimSpace(m.ConnectionID) == "":
		return newError(op, ErrInvalidManifest, "manifest names no connection")
	case len(m.Objects) == 0:
		return newError(op, ErrInvalidManifest, "manifest declares no objects")
	case strings.TrimSpace(m.CreatedBy) == "":
		return newError(op, ErrInvalidManifest, "manifest has no author")
	case m.CreatedAt.IsZero():
		return newError(op, ErrInvalidManifest, "manifest has no creation time")
	}

	declared := make(map[connectivity.ObjectKind]bool, len(m.Objects))
	for _, o := range m.Objects {
		if !o.Valid() {
			return newError(op, ErrInvalidManifest, "manifest declares unknown object %q", string(o))
		}
		if declared[o] {
			return newError(op, ErrInvalidManifest, "manifest declares object %q twice", string(o))
		}
		declared[o] = true
	}
	for _, o := range m.Objects {
		if strings.TrimSpace(m.SchemaVersionPins[o]) == "" {
			return newError(op, ErrInvalidManifest, "object %q has no pinned schema version", string(o))
		}
	}
	for o := range m.SchemaVersionPins {
		if !declared[o] {
			return newError(op, ErrInvalidManifest,
				"schema version pin names object %q the manifest does not declare", string(o))
		}
	}
	for o, fields := range m.FieldAllowList {
		if !declared[o] {
			return newError(op, ErrInvalidManifest,
				"field allow list names object %q the manifest does not declare", string(o))
		}
		seen := make(map[string]bool, len(fields))
		for _, f := range fields {
			if strings.TrimSpace(f) == "" {
				return newError(op, ErrInvalidManifest, "field allow list for %q has an empty field name", string(o))
			}
			if seen[f] {
				return newError(op, ErrInvalidManifest, "field allow list for %q names %q twice", string(o), f)
			}
			seen[f] = true
		}
	}
	for o, count := range m.ExpectedPopulation {
		if !declared[o] {
			return newError(op, ErrInvalidManifest,
				"expected population names object %q the manifest does not declare", string(o))
		}
		if count < 0 {
			return newError(op, ErrInvalidManifest, "expected population for %q is negative", string(o))
		}
	}
	for _, r := range m.ReferencePins {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	return m.Budget.Validate()
}

// sortedObjects returns objects in ascending order, leaving the input slice
// untouched.
func sortedObjects(objects []connectivity.ObjectKind) []connectivity.ObjectKind {
	out := append([]connectivity.ObjectKind(nil), objects...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func stringsOfObjects(objects []connectivity.ObjectKind) []string {
	out := make([]string, len(objects))
	for i, o := range objects {
		out[i] = string(o)
	}
	return out
}

// sortedObjectKeys returns a map's object keys in ascending order, so a
// canonical encoding never depends on map iteration order.
func sortedObjectKeys[V any](m map[connectivity.ObjectKind]V) []connectivity.ObjectKind {
	keys := make([]connectivity.ObjectKind, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// canonical encodes the manifest's material content through canonicalbytes,
// the same tagged length-prefixed framing [connectivity.ConnectorDefinition]
// and every other digested type in this codebase uses.
func (m OnboardingManifest) canonical() ([]byte, error) {
	const op = "onboarding.OnboardingManifest.Digest"
	w := canonicalbytes.New(manifestSchema, manifestVersion).
		String("manifest_id", m.ManifestID).
		String("tenant_id", m.TenantID).
		String("owner_ref", m.OwnerRef).
		String("source_authority_ref", m.SourceAuthorityRef).
		String("classification", m.Classification).
		String("residency", m.Residency).
		String("retention_policy", m.RetentionPolicy).
		String("rollback_policy", m.RollbackPolicy).
		String("idempotency_key", m.IdempotencyKey).
		String("connector_id", m.ConnectorID).
		String("connector_version", m.ConnectorVersion.String()).
		String("connection_id", m.ConnectionID)

	w.SortedStrings("objects", stringsOfObjects(m.Objects))

	w.Count("field_allow_list", len(m.FieldAllowList))
	for _, obj := range sortedObjectKeys(m.FieldAllowList) {
		w.String("field_allow_list.object", string(obj))
		w.SortedStrings("field_allow_list.fields", append([]string(nil), m.FieldAllowList[obj]...))
	}

	for _, obj := range sortedObjects(m.Objects) {
		w.String("schema_version_pin.object", string(obj)).
			String("schema_version_pin.version", m.SchemaVersionPins[obj])
	}

	w.Count("expected_population", len(m.ExpectedPopulation))
	for _, obj := range sortedObjectKeys(m.ExpectedPopulation) {
		w.String("expected_population.object", string(obj)).
			Int("expected_population.count", m.ExpectedPopulation[obj])
	}

	refs := append([]ReferencePin(nil), m.ReferencePins...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	w.Count("reference_pin", len(refs))
	for _, r := range refs {
		w.String("reference_pin.name", r.Name).
			String("reference_pin.version", r.Version).
			String("reference_pin.digest", strings.ToLower(r.Digest))
	}

	w.Int("budget.max_pages", int64(m.Budget.MaxPages)).
		Int("budget.max_records", int64(m.Budget.MaxRecords)).
		Int("budget.max_bytes", int64(m.Budget.MaxBytes)).
		Int("budget.max_wall_time_ns", int64(m.Budget.MaxWallTime)).
		Int("created_at_unix_nano", m.CreatedAt.UTC().UnixNano()).
		String("created_by", m.CreatedBy)

	raw, err := w.Bytes()
	if err != nil {
		return nil, newError(op, ErrInvalidManifest, "encode manifest: %v", err)
	}
	return raw, nil
}

// Digest returns the manifest's content digest. Two manifests with the same
// meaning digest identically on every machine.
func (m OnboardingManifest) Digest() (string, error) {
	raw, err := m.canonical()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func splitDigest(digest string) (algorithm, hexPart string, err error) {
	const op = "onboarding.splitDigest"
	algorithm, hexPart, found := strings.Cut(digest, ":")
	if !found || algorithm == "" || hexPart == "" {
		return "", "", newError(op, ErrInvalidManifest, "digest %q is not algorithm:hex", digest)
	}
	return algorithm, hexPart, nil
}

// SignedManifest is an [OnboardingManifest] together with the digest and
// ed25519 signature recorded at signing time. It carries no private key
// material.
type SignedManifest struct {
	Manifest    OnboardingManifest
	Digest      string
	SignerKeyID string
	Signature   string
}

// SignManifest validates m, computes its digest, and signs that digest with
// priv under keyID.
func SignManifest(m OnboardingManifest, keyID string, priv ed25519.PrivateKey) (SignedManifest, error) {
	const op = "onboarding.SignManifest"
	if err := m.Validate(); err != nil {
		return SignedManifest{}, err
	}
	if strings.TrimSpace(keyID) == "" {
		return SignedManifest{}, newError(op, ErrInvalidManifest, "signer has no key id")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return SignedManifest{}, newError(op, ErrSignatureInvalid, "private key has the wrong size")
	}
	digest, err := m.Digest()
	if err != nil {
		return SignedManifest{}, err
	}
	_, hexPart, err := splitDigest(digest)
	if err != nil {
		return SignedManifest{}, err
	}
	digestBytes, err := hex.DecodeString(hexPart)
	if err != nil {
		return SignedManifest{}, newError(op, ErrSignatureInvalid, "digest is not hex")
	}
	sig := ed25519.Sign(priv, digestBytes)
	return SignedManifest{
		Manifest:    m,
		Digest:      digest,
		SignerKeyID: keyID,
		Signature:   hex.EncodeToString(sig),
	}, nil
}

// VerifyStatus is the outcome of [VerifyManifest].
type VerifyStatus string

// The declared verify outcomes.
const (
	// VerifyValid reports that the manifest content matches its recorded
	// digest and the signature verifies against the supplied public key.
	VerifyValid VerifyStatus = "VALID"
	// VerifyTampered reports that the manifest content no longer matches the
	// digest recorded at signing time.
	VerifyTampered VerifyStatus = "TAMPERED"
	// VerifyInvalidSignature reports that the signature does not verify
	// against the recorded digest and the supplied public key.
	VerifyInvalidSignature VerifyStatus = "INVALID_SIGNATURE"
	// VerifyInvalidManifest reports that the manifest itself fails
	// structural validation, independent of any signature question.
	VerifyInvalidManifest VerifyStatus = "INVALID_MANIFEST"
)

// VerifyManifest recomputes sm.Manifest's canonical digest and checks it
// against sm.Digest, then checks sm.Signature against sm.Digest under pub.
//
// A mismatch in the first check is [VerifyTampered]: the manifest content
// changed since it was signed. A mismatch in the second, with matching
// digests, is [VerifyInvalidSignature]: the wrong key, a corrupted signature,
// a forged one, or an absent one (the zero-value [SignedManifest] an unsigned
// manifest produces). A structurally invalid manifest never reaches either
// check and is reported as [VerifyInvalidManifest].
func VerifyManifest(sm SignedManifest, pub ed25519.PublicKey) (VerifyStatus, error) {
	const op = "onboarding.VerifyManifest"
	if err := sm.Manifest.Validate(); err != nil {
		return VerifyInvalidManifest, err
	}
	recomputed, err := sm.Manifest.Digest()
	if err != nil {
		return VerifyInvalidManifest, err
	}
	if !strings.EqualFold(recomputed, sm.Digest) {
		return VerifyTampered, newError(op, ErrManifestTampered,
			"recorded digest %s does not match recomputed %s", sm.Digest, recomputed)
	}
	_, hexPart, err := splitDigest(recomputed)
	if err != nil {
		return VerifyInvalidManifest, err
	}
	digestBytes, err := hex.DecodeString(hexPart)
	if err != nil {
		return VerifyInvalidManifest, newError(op, ErrManifestTampered, "recorded digest is not hex")
	}
	sigBytes, err := hex.DecodeString(sm.Signature)
	if err != nil {
		return VerifyInvalidSignature, newError(op, ErrSignatureInvalid, "signature is not hex")
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, digestBytes, sigBytes) {
		return VerifyInvalidSignature, newError(op, ErrSignatureInvalid, "ed25519 verification failed")
	}
	return VerifyValid, nil
}
