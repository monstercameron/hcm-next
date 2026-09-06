// Package defaultactivation implements ALIGN-048. It admits only versioned,
// already-admitted default features and refuses an activation that would turn
// a shipped-but-disabled definition into product authority.
package defaultactivation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// ContractExplain describes the policy without including feature or manifest values.
func ContractExplain() string {
	return "default feature activation requires an admitted versioned manifest"
}

// Disposition is the ProductSliceDefinition default disposition vocabulary.
type Disposition string

const (
	CoreRequired        Disposition = "CORE_REQUIRED"
	DomainPackDefault   Disposition = "DOMAIN_PACK_DEFAULT"
	AvailableNotEnabled Disposition = "AVAILABLE_NOT_ENABLED"
	CustomerDefined     Disposition = "CUSTOMER_DEFINED"
	Deferred            Disposition = "DEFERRED"
	Prohibited          Disposition = "PROHIBITED"
)

var dispositions = map[Disposition]bool{
	CoreRequired: true, DomainPackDefault: true, AvailableNotEnabled: true,
	CustomerDefined: true, Deferred: true, Prohibited: true,
}

// Feature is the compiled, cross-lens identity of a default feature. The
// references are identifiers only; this checker never becomes an owner of
// business, storage, or capability definitions.
type Feature struct {
	ID                 string
	Version            string
	Owner              string
	BusinessOwner      string
	DefaultDisposition Disposition
	CapabilityRefs     []string
	QueryContracts     []string
	CommandContracts   []string
	Tables             []string
}

// Manifest is the signed/admitted release boundary supplied by the registry.
type Manifest struct {
	ID                 string
	Version            string
	Digest             string
	AdmittedFeatureIDs []string
}

// Request asks whether Feature may be activated as part of the default
// product. It carries no tenant data and performs no persistence.
type Request struct {
	Feature  Feature
	Manifest Manifest
}

// Decision is the bounded evidence returned by Admit.
type Decision struct {
	Allowed         bool
	Reason          string
	FeatureID       string
	FeatureVersion  string
	ManifestID      string
	ManifestVersion string
	ManifestDigest  string
	EvidenceDigest  string
}

var (
	ErrInvalidInput = errors.New("defaultactivation: invalid input")
	ErrUnadmitted   = errors.New("defaultactivation: feature is not admitted for default activation")
)

// Admit validates the complete default-activation boundary. A feature that is
// merely available, customer-defined, deferred, or prohibited can be shipped
// for later use but cannot be silently enabled by this path.
func Admit(req Request) (Decision, error) {
	if err := validateFeature(req.Feature); err != nil {
		return Decision{}, err
	}
	if err := validateManifest(req.Manifest); err != nil {
		return Decision{}, err
	}
	d := Decision{
		FeatureID: req.Feature.ID, FeatureVersion: req.Feature.Version,
		ManifestID: req.Manifest.ID, ManifestVersion: req.Manifest.Version,
		ManifestDigest: req.Manifest.Digest,
	}
	if req.Feature.DefaultDisposition != CoreRequired && req.Feature.DefaultDisposition != DomainPackDefault {
		d.Reason = "DEFAULT_DISPOSITION_NOT_ADMITTED"
		d.EvidenceDigest = digestDecision(d, req.Feature.DefaultDisposition)
		return d, fmt.Errorf("%w: %s", ErrUnadmitted, req.Feature.DefaultDisposition)
	}
	if !contains(req.Manifest.AdmittedFeatureIDs, req.Feature.ID) {
		d.Reason = "FEATURE_NOT_IN_ADMITTED_MANIFEST"
		d.EvidenceDigest = digestDecision(d, req.Feature.DefaultDisposition)
		return d, fmt.Errorf("%w: feature %q is absent from manifest %q", ErrUnadmitted, req.Feature.ID, req.Manifest.ID)
	}
	d.Allowed = true
	d.Reason = "ADMITTED_DEFAULT_FEATURE"
	d.EvidenceDigest = digestDecision(d, req.Feature.DefaultDisposition)
	return d, nil
}

// Validate checks the same request without producing an activation decision.
func Validate(req Request) error {
	_, err := Admit(req)
	return err
}

func validateFeature(f Feature) error {
	for field, value := range map[string]string{
		"id": f.ID, "version": f.Version, "owner": f.Owner, "business_owner": f.BusinessOwner,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: feature %s is required", ErrInvalidInput, field)
		}
	}
	if !dispositions[f.DefaultDisposition] {
		return fmt.Errorf("%w: feature disposition %q is not declared", ErrInvalidInput, f.DefaultDisposition)
	}
	for name, refs := range map[string][]string{
		"capability_refs": f.CapabilityRefs, "query_contracts": f.QueryContracts, "tables": f.Tables,
	} {
		if len(refs) == 0 {
			return fmt.Errorf("%w: feature %s is empty", ErrInvalidInput, name)
		}
		if err := validateRefs(name, refs); err != nil {
			return err
		}
	}
	if err := validateRefs("command_contracts", f.CommandContracts); err != nil {
		return err
	}
	return nil
}

func validateManifest(m Manifest) error {
	for field, value := range map[string]string{"id": m.ID, "version": m.Version, "digest": m.Digest} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: manifest %s is required", ErrInvalidInput, field)
		}
	}
	return validateRefs("admitted_feature_ids", m.AdmittedFeatureIDs)
}

func validateRefs(name string, refs []string) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return fmt.Errorf("%w: %s contains an empty reference", ErrInvalidInput, name)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: %s contains duplicate reference %q", ErrInvalidInput, name, ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func contains(values []string, want string) bool { return slices.Contains(values, want) }

func digestDecision(d Decision, disposition Disposition) string {
	parts := []string{fmt.Sprint(schemaVersion), d.FeatureID, d.FeatureVersion, d.ManifestID, d.ManifestVersion, d.ManifestDigest, string(disposition), d.Reason}
	sort.Strings(parts[1:])
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns only decision metadata and stable reason tokens.
func Explain(d Decision) string {
	return fmt.Sprintf("default activation allowed=%t reason=%s evidence=%s", d.Allowed, d.Reason, d.EvidenceDigest)
}
