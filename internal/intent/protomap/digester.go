package protomap

import (
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/digest"
)

// Digester implements the intent kernel's digest port over a digest registry.
//
// It exists because the canonical models are the generated Protobuf messages,
// and the kernel packages hold no wire types. The kernel asks for a digest; this
// type projects the kernel value onto its canonical model and asks the registry
// to mint one.
type Digester struct {
	registry *digest.Registry
}

// NewDigester returns a digester over reg. A nil registry is an error rather
// than a lazily-created default: which profiles are published is a deployment
// decision, not something a digest call should quietly make.
func NewDigester(reg *digest.Registry) (*Digester, error) {
	if reg == nil {
		return nil, lossy("digester requires a digest registry")
	}
	return &Digester{registry: reg}, nil
}

// NewDefaultDigester returns a digester over a registry publishing the built-in
// PROPOSAL and IDEMPOTENT_REQUEST profile versions.
func NewDefaultDigester() (*Digester, error) {
	reg, err := digest.NewDefaultRegistry()
	if err != nil {
		return nil, err
	}
	return NewDigester(reg)
}

// Registry returns the underlying digest registry, so that a caller can verify
// a reference it was handed rather than trusting it.
func (d *Digester) Registry() *digest.Registry { return d.registry }

// RequestDigest computes the IDEMPOTENT_REQUEST digest of an instance.
//
// The profile binds tenant and organization scope, the definition, the purpose,
// the subjects as a set, the requested effective time, the typed request
// payload, the idempotency key and the execution mode. It deliberately excludes
// correlation and trace ids, lifecycle state, timestamps, control snapshots and
// the instance's own digest: two identical requests differing only in trace id
// are the same request.
func (d *Digester) RequestDigest(i intent.Instance) (digest.Reference, error) {
	msg, err := InstanceToProto(i)
	if err != nil {
		return digest.Reference{}, err
	}
	// The instance's own digest field must not participate in its own digest.
	msg.CanonicalRequestDigest = nil
	ref, _, err := d.registry.Compute(msg, digest.ProfileIdempotentRequest)
	if err != nil {
		return digest.Reference{}, err
	}
	return ref, nil
}

// ProposalDigest computes the PROPOSAL material digest of a revision.
//
// Control snapshots are encoded onto the message as evidence and are excluded
// from the digest by the profile's material list. That exclusion is the whole
// materiality rule: republishing a policy bundle, a classification taxonomy or
// a reference dataset changes the snapshots and therefore triggers
// revalidation, but it does not change this digest and so does not invalidate
// approvals bound to it.
func (d *Digester) ProposalDigest(p intent.ProposalRevision) (digest.Reference, error) {
	msg, err := ProposalToProto(p)
	if err != nil {
		return digest.Reference{}, err
	}
	msg.MaterialProposalDigest = nil
	ref, _, err := d.registry.Compute(msg, digest.ProfileProposal)
	if err != nil {
		return digest.Reference{}, err
	}
	return ref, nil
}

// VerifyRequestDigest recomputes an instance's request digest and compares it
// with the reference the instance carries.
func (d *Digester) VerifyRequestDigest(i intent.Instance) error {
	msg, err := InstanceToProto(i)
	if err != nil {
		return err
	}
	msg.CanonicalRequestDigest = nil
	return d.registry.Verify(msg, i.CanonicalRequestDigest)
}

// VerifyProposalDigest recomputes a revision's material digest and compares it
// with the reference the revision carries.
func (d *Digester) VerifyProposalDigest(p intent.ProposalRevision) error {
	msg, err := ProposalToProto(p)
	if err != nil {
		return err
	}
	msg.MaterialProposalDigest = nil
	return d.registry.Verify(msg, p.MaterialDigest)
}

// compile-time assertion that the port is satisfied.
var _ intent.Digester = (*Digester)(nil)
