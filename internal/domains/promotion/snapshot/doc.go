// Package snapshot builds the immutable Promotion input snapshot (PROMO-001):
// the exact, bound set of authoritative reads one ProposePromotion request was
// formed against, at one as-of business date and one known-at knowledge
// cut-off.
//
// Semantic owner: People domain (composite ChangeRequest, promote_worker).
// Phase: P1A.
//
// What it is for. A promotion proposal is only as trustworthy as the facts it
// was computed from. If the proposal records "Omar earns 93,000 and the target
// position has a free head" without recording *which revision of which source*
// said so, at which watermark, under whose authority, then nothing downstream
// can tell a stale read from a current one, or a value the caller was
// authorized to see from one it was not. This package produces the artifact
// that makes those questions answerable: one [PromotionInputSnapshot] binding
// eight named business inputs, each as an InputEntry from
// internal/engines/snapshot carrying its own owner, authority class, source
// reference, effective-at/known-at coordinate, revision, head, watermark,
// freshness, classification, provenance and reference/config version.
//
// Four properties are deliberate.
//
// First, every input is read through the authorized read its owning domain
// already exposes -- people.ExplainWorkerState, org.ResolveManagerRelationships,
// position.CalculateCapacity, rewards.ReadAuthorizedCompensation,
// rewards.EvaluateCompensationPayBandPosition and a budget observation port --
// never through a private path of this package's own. There is no constructor
// that accepts a caller-supplied value for any input, which is what makes
// "the snapshot cannot be told what the baseline is" a property of the type
// rather than a rule somebody has to remember.
//
// Second, disclosure survives into the artifact. An input the caller may not
// see is present as an entry whose [Input.Availability] is WITHHELD and whose
// canonical text is empty. It is never dropped (which would make a denied read
// indistinguishable from an unrequested one) and never carries a value
// (which would defeat the denial). ABSENT and UNKNOWN are likewise distinct
// from each other and from WITHHELD.
//
// Third, completeness is three-valued, and it is the snapshot engine's own
// verdict (SNAPSHOT-003), not a Boolean this package invents. A required input
// that is MISSING or UNKNOWN refuses the whole build with an [InputError]
// naming that input; an optional input that is absent is reported as absent
// and never coerced to a default; a conditional input is evaluated only when
// its condition is actually satisfied.
//
// Fourth, the digest is the intent's own material encoding. [PromotionInputSnapshot.Digest]
// is sha256 over the material payload of [PromotionInputSnapshot.MaterialInputs],
// an intent.ProposalRevision projection carrying exactly the material inputs a
// promotion proposal binds: tenant, subjects, effective time, the per-input
// current-state assertions and the per-input source baselines. Identical
// authoritative inputs therefore produce an identical digest, and any material
// change -- a different value, a different revision, a different disclosure
// outcome -- produces a new one, without this package inventing a second
// definition of "material" that could drift from the kernel's.
//
// This package writes nothing, reserves nothing and calls nothing outbound.
package snapshot
