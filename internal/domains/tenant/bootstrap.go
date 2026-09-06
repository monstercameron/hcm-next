package tenant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidBootstrapManifest reports that a [BootstrapManifest] is missing a
// required field or otherwise cannot be digested at all. It is always wrapped
// with the offending detail; callers match it with errors.Is.
var ErrInvalidBootstrapManifest = errors.New("tenant: invalid bootstrap manifest")

// ErrBootstrapRejected reports that a structurally valid manifest was refused
// against the tenant's currently applied bootstrap state (TENANT-002 RED): a
// different manifest identity claiming the same tenant, a stale revision, or
// the same revision number carrying different content. It is always wrapped
// with the offending detail; callers match it with errors.Is.
var ErrBootstrapRejected = errors.New("TENANT_BOOTSTRAP_REJECTED")

// BootstrapManifest is the caller-declared, revisioned statement of what one
// pilot tenant's bootstrap establishes.
//
// It is deliberately narrower than the full multi-plane ProvisioningRun the
// TENANT-002 spec eventually wants (identity, admin, keys, products,
// policies, schemas, recovery contacts, audit and health all independently
// VERIFIED) -- those planes do not exist yet and each depends on its own
// undelivered todo (TOOL-014, TRUST-019). What this type and
// [ResolveBootstrap] deliver is the slice TENANT-002 needs regardless of how
// many planes eventually compose it: a tenant's bootstrap is named by a
// caller-assigned revision number, replaying the identical revision is a
// no-op, and changing what a revision means without bumping the number is
// refused rather than silently absorbed.
type BootstrapManifest struct {
	// ManifestID is the caller-supplied identity of this manifest. A tenant
	// bootstrapped from one manifest identity can never be re-bootstrapped
	// from a different one through the same ledger entry: see
	// [ResolveBootstrap].
	ManifestID string
	// Tenant is the tenant slug this manifest bootstraps.
	Tenant string
	// Cell, Region, ResidencyProfile and IsolationTier are the logical
	// placement facts this bootstrap establishes, the same dimensions
	// [Placement] signs for tenant relocation.
	Cell             string
	Region           string
	ResidencyProfile string
	IsolationTier    string
	// Revision is the caller-declared version of this manifest's content. It
	// is the only thing that may legitimately change what "the applied
	// manifest" means for this tenant; see [ResolveBootstrap].
	Revision uint64
	// OwnerRef identifies the human or role accountable for this bootstrap.
	OwnerRef string
	// CreatedAt is the drafting instant, supplied rather than read from the
	// wall clock so a manifest's digest is a pure function of its inputs.
	CreatedAt time.Time
	// CreatedBy identifies who drafted the manifest.
	CreatedBy string
}

// Validate reports the first reason m cannot be digested or applied.
func (m BootstrapManifest) Validate() error {
	for _, f := range []struct {
		name  string
		value string
	}{
		{"manifest_id", m.ManifestID},
		{"tenant", m.Tenant},
		{"cell", m.Cell},
		{"region", m.Region},
		{"residency_profile", m.ResidencyProfile},
		{"isolation_tier", m.IsolationTier},
		{"owner_ref", m.OwnerRef},
		{"created_by", m.CreatedBy},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidBootstrapManifest, f.name)
		}
	}
	if m.Revision == 0 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidBootstrapManifest)
	}
	if m.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidBootstrapManifest)
	}
	return nil
}

// canonical returns the exact bytes [BootstrapManifest.Digest] hashes. It is a
// plain, deterministic JSON encoding of every field that names what this
// bootstrap means; the field order is fixed by the struct tags below rather
// than by Go's field order, so a struct field reorder can never silently
// change the digest.
type canonicalBootstrapManifest struct {
	ManifestID       string `json:"manifest_id"`
	Tenant           string `json:"tenant"`
	Cell             string `json:"cell"`
	Region           string `json:"region"`
	ResidencyProfile string `json:"residency_profile"`
	IsolationTier    string `json:"isolation_tier"`
	Revision         uint64 `json:"revision"`
	OwnerRef         string `json:"owner_ref"`
	CreatedAtUnixNS  int64  `json:"created_at_unix_nano"`
	CreatedBy        string `json:"created_by"`
}

func (m BootstrapManifest) canonical() ([]byte, error) {
	return json.Marshal(canonicalBootstrapManifest{
		ManifestID:       m.ManifestID,
		Tenant:           m.Tenant,
		Cell:             m.Cell,
		Region:           m.Region,
		ResidencyProfile: m.ResidencyProfile,
		IsolationTier:    m.IsolationTier,
		Revision:         m.Revision,
		OwnerRef:         m.OwnerRef,
		CreatedAtUnixNS:  m.CreatedAt.UTC().UnixNano(),
		CreatedBy:        m.CreatedBy,
	})
}

// Digest returns the manifest's content digest: the lowercase hex sha256 of
// its canonical encoding. Two manifests with the same meaning digest
// identically on every machine; changing any field -- including Revision --
// changes the digest.
func (m BootstrapManifest) Digest() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	b, err := m.canonical()
	if err != nil {
		return "", fmt.Errorf("%w: encode manifest: %v", ErrInvalidBootstrapManifest, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// BootstrapRecord is the latest manifest revision actually applied for a
// tenant, as durably recorded by internal/data/tenancy's bootstrap ledger.
// A nil *BootstrapRecord means the tenant has never been bootstrapped through
// that ledger.
type BootstrapRecord struct {
	ManifestID string
	Revision   uint64
	Digest     string
	AppliedAt  time.Time
}

// BootstrapDecision is the three-valued outcome [ResolveBootstrap] returns.
type BootstrapDecision string

const (
	// BootstrapApply reports that the incoming manifest should be recorded as
	// the tenant's new current bootstrap state: either the tenant has never
	// been bootstrapped, or the manifest declares a newer revision than the
	// one currently applied.
	BootstrapApply BootstrapDecision = "APPLY"
	// BootstrapNoop reports that the incoming manifest exactly restates the
	// revision already applied (same revision, same digest): nothing should
	// be written.
	BootstrapNoop BootstrapDecision = "NOOP"
	// BootstrapRejected reports that the incoming manifest conflicts with
	// what is already applied and must not be written.
	BootstrapRejected BootstrapDecision = "REJECTED"
)

// BootstrapOutcome is the full result of resolving one manifest against a
// tenant's current bootstrap state.
type BootstrapOutcome struct {
	Decision BootstrapDecision
	// Digest is the incoming manifest's own content digest, computed
	// regardless of Decision, so a caller can log or persist it even for a
	// rejection.
	Digest string
	// Reason explains a REJECTED decision. Empty for APPLY and NOOP.
	Reason string
}

// ResolveBootstrap decides what should happen with incoming against current,
// the tenant's most recently applied manifest revision (nil when the tenant
// has never been bootstrapped).
//
// TENANT-002 GREEN, in this narrowed slice: a first bootstrap always applies;
// replaying the exact same revision with the exact same content is a no-op
// that changes nothing; declaring the same revision number with different
// content is refused (the manifest changed without declaring a new revision);
// declaring a revision older than the one already applied is refused (a stale
// manifest); declaring a strictly newer revision applies, whatever its
// content -- a new revision is exactly how a manifest is permitted to change.
// A manifest naming a different ManifestID than the tenant's currently
// applied one is refused outright: this ledger tracks one manifest identity's
// revision history per tenant, not an arbitrary swap of identities.
func ResolveBootstrap(current *BootstrapRecord, incoming BootstrapManifest) (BootstrapOutcome, error) {
	digest, err := incoming.Digest()
	if err != nil {
		return BootstrapOutcome{}, err
	}

	if current == nil {
		return BootstrapOutcome{Decision: BootstrapApply, Digest: digest}, nil
	}

	if current.ManifestID != incoming.ManifestID {
		err := fmt.Errorf(
			"%w: tenant %q is already bootstrapped from manifest %q; %q names a different manifest",
			ErrBootstrapRejected, incoming.Tenant, current.ManifestID, incoming.ManifestID)
		return BootstrapOutcome{Decision: BootstrapRejected, Digest: digest, Reason: err.Error()}, err
	}

	switch {
	case incoming.Revision < current.Revision:
		err := fmt.Errorf(
			"%w: manifest %q revision %d is stale; revision %d is already applied",
			ErrBootstrapRejected, incoming.ManifestID, incoming.Revision, current.Revision)
		return BootstrapOutcome{Decision: BootstrapRejected, Digest: digest, Reason: err.Error()}, err

	case incoming.Revision == current.Revision:
		if strings.EqualFold(digest, current.Digest) {
			return BootstrapOutcome{Decision: BootstrapNoop, Digest: digest}, nil
		}
		err := fmt.Errorf(
			"%w: manifest %q revision %d is already applied with digest %s; declare a new revision to change it",
			ErrBootstrapRejected, incoming.ManifestID, incoming.Revision, current.Digest)
		return BootstrapOutcome{Decision: BootstrapRejected, Digest: digest, Reason: err.Error()}, err

	default: // incoming.Revision > current.Revision
		return BootstrapOutcome{Decision: BootstrapApply, Digest: digest}, nil
	}
}
