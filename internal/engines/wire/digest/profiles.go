package digest

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/canonical"
)

// The separately versioned canonicalization profiles named by the canonical
// envelope contract. Each is versioned independently; a change to any of them
// creates a new version and never edits one in place.
const (
	ProfileProposal                = "hcmnext.proposal"
	ProfileIdempotentRequest       = "hcmnext.idempotent_request"
	ProfileLedgerEvent             = "hcmnext.ledger_event"
	ProfileConfigBundle            = "hcmnext.config_bundle"
	ProfileEvidenceManifest        = "hcmnext.evidence_manifest"
	ProfileArtifactReference       = "hcmnext.artifact_reference"
	ProfileExternalPayloadEvidence = "hcmnext.external_payload_evidence"
)

// SchemaIntentsV1 is the semantic schema the built-in profiles project.
const (
	SchemaIntentsV1        = "hcmnext.intents.v1"
	SchemaIntentsV1Version = 1
)

// materiality is the platform's floor for a profile id: paths the profile must
// bind, and paths it must not. It exists because a profile that quietly drops a
// material path still produces a valid-looking digest, and an approval bound to
// that digest would then survive a change it was supposed to catch.
type materiality struct {
	required  []string
	forbidden []string
}

// materialityRules encodes the PROPOSAL split from the canonical envelope
// contract: the proposal payload and the intent/revision identity are material,
// while the control snapshots are revalidated context and must never be hashed
// into the digest an approval binds. Republishing a policy bundle or a
// classification taxonomy must not invalidate every pending approval.
//
// Profile ids without a rule here are unconstrained until their canonical
// models exist.
var materialityRules = map[string]materiality{
	ProfileProposal: {
		required: []string{
			"intent_id",
			"proposal_revision_id",
			"revision",
			"proposal.schema.schema_id",
			"proposal.schema.version",
			"proposal.schema.protobuf_full_name",
			"proposal.protobuf_wire_bytes",
		},
		forbidden: []string{
			"control_snapshots",
			"material_proposal_digest",
		},
	},
	ProfileIdempotentRequest: {
		required: []string{
			"tenant_id",
			"definition",
			"idempotency_key",
			"request.protobuf_wire_bytes",
		},
		forbidden: []string{
			"control_snapshots",
			"canonical_request_digest",
			"lifecycle",
			"created_at",
			"recorded_at",
			"last_transition_at",
			"trace_id",
			"correlation_id",
		},
	},
}

// checkMateriality enforces the floor before a profile version is published.
func checkMateriality(p canonical.Profile) error {
	rule, ok := materialityRules[p.ID]
	if !ok {
		return nil
	}
	if !p.RejectUnknownFields {
		return newError("RegisterProfile", ErrNonMaterialPath,
			"%s is a material signed profile and must reject unknown fields", p.ID)
	}
	for _, req := range rule.required {
		if !covers(p.Material, req) {
			return newError("RegisterProfile", ErrOmittedMaterialPath,
				"%s v%d does not bind %q", p.ID, p.Version, req)
		}
	}
	for _, m := range p.Material {
		for _, bad := range rule.forbidden {
			if m == bad || strings.HasPrefix(m, bad+".") {
				return newError("RegisterProfile", ErrNonMaterialPath,
					"%s v%d binds %q, which is revalidated context", p.ID, p.Version, m)
			}
		}
	}
	return nil
}

// covers reports whether the material list fully binds path: either it names
// path exactly, or it names an ancestor of path and therefore includes it as
// part of that ancestor's subtree.
func covers(material []string, path string) bool {
	for _, m := range material {
		if m == path || strings.HasPrefix(path, m+".") {
			return true
		}
	}
	return false
}

// MaterialityFloor returns the required and forbidden path lists for a profile
// id, sorted. Both are empty for an unconstrained profile id.
func MaterialityFloor(profileID string) (required, forbidden []string) {
	rule := materialityRules[profileID]
	required = append(required, rule.required...)
	forbidden = append(forbidden, rule.forbidden...)
	sort.Strings(required)
	sort.Strings(forbidden)
	return required, forbidden
}

// ProposalProfileV1 is the built-in PROPOSAL profile over
// hcmnext.intents.v1.ProposalRevision.
//
// Material: the typed planned domain writes carried by the proposal payload,
// the intent and proposal revision identity, the supersession link, and the
// payload's own attachment digest. Excluded: control_snapshots (revalidated
// context), the revision's own material_proposal_digest (self-reference),
// created_by and created_at (provenance evidence), invalidator_refs (lifecycle
// bookkeeping), and the payload digest's canonical bytes artifact reference,
// which is a storage location rather than content identity.
func ProposalProfileV1() canonical.Profile {
	return canonical.Profile{
		ID:            ProfileProposal,
		Version:       1,
		SchemaID:      SchemaIntentsV1,
		SchemaVersion: SchemaIntentsV1Version,
		MessageName:   "hcmnext.intents.v1.ProposalRevision",
		Material: []string{
			"proposal_revision_id",
			"intent_id",
			"revision",
			"proposal.schema.schema_id",
			"proposal.schema.version",
			"proposal.schema.protobuf_full_name",
			"proposal.schema.descriptor_digest",
			"proposal.protobuf_wire_bytes",
			"proposal.canonical_digest.profile_id",
			"proposal.canonical_digest.profile_version",
			"proposal.canonical_digest.schema_id",
			"proposal.canonical_digest.schema_version",
			"proposal.canonical_digest.algorithm_id",
			"proposal.canonical_digest.canonical_length",
			"proposal.canonical_digest.digest",
			"proposal.canonical_digest.scope_binding_digest",
			"supersedes_proposal_revision_id",
		},
		Verbatim: []string{
			// Digest hex and descriptor digests are opaque identifiers, not
			// human text; NFC folding must never touch them.
			"proposal.schema.descriptor_digest",
			"proposal.canonical_digest.digest",
			"proposal.canonical_digest.scope_binding_digest",
		},
		RejectUnknownFields: true,
	}
}

// ProposalScope binds a proposal digest to its intent and proposal revision.
// ProposalRevision does not carry the tenant; a caller that needs tenant
// binding supplies it through Options.Scope and VerifyWithScope.
func ProposalScope() ScopeSpec {
	return ScopeSpec{
		IntentPath:           "intent_id",
		ProposalRevisionPath: "proposal_revision_id",
	}
}

// IdempotentRequestProfileV1 is the built-in IDEMPOTENT_REQUEST profile over
// hcmnext.intents.v1.IntentInstance.
//
// Material: tenant and organization scope, the intent definition being
// invoked, the declared purpose, the subjects (a set — their order carries no
// meaning), the requested effective time, the typed request payload, the
// idempotency key, and the execution mode. Excluded: correlation and trace
// identifiers, lifecycle state, timestamps, control snapshots, and the
// request's own canonical digest.
func IdempotentRequestProfileV1() canonical.Profile {
	return canonical.Profile{
		ID:            ProfileIdempotentRequest,
		Version:       1,
		SchemaID:      SchemaIntentsV1,
		SchemaVersion: SchemaIntentsV1Version,
		MessageName:   "hcmnext.intents.v1.IntentInstance",
		Material: []string{
			"tenant_id",
			"organization_scope_id",
			"definition",
			"purpose",
			"subjects",
			"requested_effective_at",
			"request.schema.schema_id",
			"request.schema.version",
			"request.schema.protobuf_full_name",
			"request.protobuf_wire_bytes",
			"idempotency_key",
			"execution_mode",
		},
		Sets:                []string{"subjects"},
		Verbatim:            []string{"idempotency_key"},
		RejectUnknownFields: true,
	}
}

// IdempotentRequestScope binds an idempotent request digest to its tenant and
// intent.
func IdempotentRequestScope() ScopeSpec {
	return ScopeSpec{TenantPath: "tenant_id", IntentPath: "intent_id"}
}

// NewDefaultRegistry returns a registry publishing sha256 and the built-in
// profile versions. Each caller gets its own registry; there is no shared
// mutable global to fight over.
func NewDefaultRegistry() (*Registry, error) {
	r := NewRegistry()
	if err := r.RegisterProfile(ProposalProfileV1(), ProposalScope()); err != nil {
		return nil, err
	}
	if err := r.RegisterProfile(IdempotentRequestProfileV1(), IdempotentRequestScope()); err != nil {
		return nil, err
	}
	return r, nil
}
