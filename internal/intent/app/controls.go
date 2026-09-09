package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Controls is the pinned control context this cell creates and simulates
// intents in.
//
// Every digest is computed from the artifact it names, not invented: the
// capability digest is over the published capability records, the legal and
// entitlement digests are over the references those records declare, and the
// policy and taxonomy digests are over the definition catalog's own policy
// references and classification floors. A control the build does not have is
// therefore impossible to pin, which is the property that matters: a proposal
// that claims a pinned control context has one that can be recomputed.
//
// Controls are revalidated context, never material (internal/intent doc.go):
// republishing a bundle triggers revalidation, it does not invalidate every
// pending approval.
type Controls struct {
	Snapshots intent.ControlSnapshots

	// SourceAuthorityDigest and RiskContextDigest are recorded on the instance
	// beside the control snapshots.
	SourceAuthorityDigest string
	RiskContextDigest     string
}

// digestOf hashes a labelled, ordered set of references.
func digestOf(label string, refs []string) string {
	sorted := slices.Clone(refs)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	h := sha256.New()
	fmt.Fprintf(h, "%s|%d;", label, len(sorted))
	for _, ref := range sorted {
		fmt.Fprintf(h, "%d:%s;", len(ref), ref)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewControls computes the control context from the two registries that make
// up this build's published surface.
func NewControls(defs *intent.Registry, caps *capability.Registry) Controls {
	var (
		capabilityRefs []string
		legalRefs      []string
		entitlementRef []string
		sloRefs        []string
	)
	for _, rec := range caps.List() {
		capabilityRefs = append(capabilityRefs, rec.Definition.Key().String()+"="+rec.Digest)
		legalRefs = append(legalRefs, rec.Definition.LegalBasisRef)
		entitlementRef = append(entitlementRef, rec.Definition.EntitlementRef)
		sloRefs = append(sloRefs, rec.Definition.SLOClassRef)
	}

	var (
		policyRefs         []string
		classificationRefs []string
		schemaRefs         []string
	)
	for _, def := range defs.Definitions() {
		policyRefs = append(policyRefs, def.NegativeStatePolicyRef)
		policyRefs = append(policyRefs, def.GovernanceRequirements...)
		classificationRefs = append(classificationRefs, def.DataClassificationFloor)
		schemaRefs = append(schemaRefs, def.InputSchema.String(), def.ResultSchema.String())
	}

	capabilityDigest := digestOf("capability_registry", capabilityRefs)
	policyDigest := digestOf("policy_bundle", policyRefs)
	classificationDigest := digestOf("classification_taxonomy", classificationRefs)

	return Controls{
		Snapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest:     capabilityDigest,
			PolicyBundleDigest:           policyDigest,
			LegalContextDigest:           digestOf("legal_context", legalRefs),
			EntitlementDigest:            digestOf("entitlement", entitlementRef),
			ReferenceDataDigest:          digestOf("reference_data", schemaRefs),
			ClassificationTaxonomyDigest: classificationDigest,
			ClassificationLabelSetDigest: classificationDigest,
			DLPDecisionDigest:            digestOf("dlp_decision", classificationRefs),
		},
		SourceAuthorityDigest: digestOf("source_authority", capabilityRefs),
		RiskContextDigest:     digestOf("risk_context", sloRefs),
	}
}
