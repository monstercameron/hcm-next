package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Build errors. All are matchable with errors.Is; each names the exact
// contract that was broken rather than folding every failure into one.
var (
	// ErrRequestInvalid is returned for a malformed [Request]. It is reserved
	// for contract failures the caller must fix; a business problem with the
	// promotion is not this package's concern at all.
	ErrRequestInvalid = errors.New("promotion/snapshot: request is invalid")
	// ErrReadFailed wraps a failure returned by one of the domain read ports.
	// A port that fails is distinct from a port that answers "no record":
	// only the second is a business fact.
	ErrReadFailed = errors.New("promotion/snapshot: a domain read failed")
	// ErrInputUnavailable is returned when a required or activated-conditional
	// input could not be established. It is always wrapped in an [InputError]
	// naming the exact input.
	ErrInputUnavailable = errors.New("promotion/snapshot: a required input is not available")
)

// Availability is what this snapshot is able to say about one input's value.
//
// It is four-valued on purpose, and the four are not interchangeable.
// DISCLOSED carries a value. WITHHELD means authorization refused it: the
// input is present in the snapshot, with its descriptors, and with no value at
// all -- a withheld input must never reach a proposal as a value, and must
// never be dropped, because a dropped input and an unrequested one look the
// same downstream. ABSENT means the record answered and had nothing to say.
// UNKNOWN means the read could not establish either.
type Availability string

// Availability states.
const (
	// AvailabilityDisclosed means the authorized read returned a value.
	AvailabilityDisclosed Availability = "DISCLOSED"
	// AvailabilityWithheld means authorization refused the input.
	AvailabilityWithheld Availability = "WITHHELD"
	// AvailabilityAbsent means the record holds no such fact at the
	// coordinate.
	AvailabilityAbsent Availability = "ABSENT"
	// AvailabilityUnknown means presence itself could not be established.
	AvailabilityUnknown Availability = "UNKNOWN"
)

// String returns the wire token.
func (a Availability) String() string { return string(a) }

// presence maps an availability onto the snapshot engine's three-valued
// presence lattice. WITHHELD and UNKNOWN both map to UNKNOWN because neither
// establishes presence, and a withheld input must not be reported as a known
// absence -- that would leak the denial as a fact about the record.
func (a Availability) presence() enginesnapshot.InputPresence {
	switch a {
	case AvailabilityDisclosed:
		return enginesnapshot.PresencePresent
	case AvailabilityAbsent:
		return enginesnapshot.PresenceAbsent
	default:
		return enginesnapshot.PresenceUnknown
	}
}

// Input is one bound business input: its descriptors, what the snapshot may
// say about it, and -- only when DISCLOSED -- its exact canonical text.
//
// CanonicalText is a canonical rendering rather than a typed value because
// eight inputs of eight different shapes have to be digested by one encoding.
// It is empty for every non-DISCLOSED availability, which is what keeps a
// WITHHELD input from ever presenting as a value.
type Input struct {
	// Name is the semantic input name, one of the declared constants.
	Name string
	// Entry is the input's descriptor set: owner, tenant, authority class,
	// source, effective-at/known-at, revision, head, watermark, freshness,
	// classification, provenance and reference version.
	Entry enginesnapshot.InputEntry
	// Availability is what this snapshot may say about the value.
	Availability Availability
	// Reason is the policy or diagnosis token for a non-DISCLOSED input, and
	// is empty for a DISCLOSED one. It never contains a value.
	Reason string
	// CanonicalText is the exact material rendering of the value, and is empty
	// unless Availability is DISCLOSED.
	CanonicalText string
	// Subject is the entity the input is a fact about: the worker for the
	// people, org and rewards inputs, the target position for the position
	// inputs, the budget scope for the budget input.
	Subject intent.SubjectReference
	// ResourceKey addresses the input within its owning domain.
	ResourceKey values.ResourceKey
}

// materialText is the assertion text one input contributes to the material
// encoding: the availability token, then the canonical text. The availability
// is prefixed rather than left implicit so that a WITHHELD input and an input
// whose disclosed value happened to be the string "WITHHELD" can never encode
// identically, and so that a change in disclosure outcome alone always changes
// the digest.
func (i Input) materialText() string {
	return string(i.Availability) + ":" + i.CanonicalText
}

// observation projects the input onto the presence-bearing descriptor
// SNAPSHOT-003 evaluates. Only a PRESENT observation may carry a value, so the
// value is written only for a disclosed input.
func (i Input) observation() enginesnapshot.SnapshotInput {
	obs := enginesnapshot.SnapshotInput{Name: i.Name, Presence: i.Availability.presence()}
	if obs.Presence == enginesnapshot.PresencePresent {
		obs.Value = i.CanonicalText
	}
	return obs
}

// InputError is the typed refusal a missing or unestablished required input
// produces. It names the input and its verdict, and deliberately carries no
// value -- a refusal that quoted the input it could not disclose would be a
// disclosure.
type InputError struct {
	// InputName is the exact input that refused the build.
	InputName string
	// Verdict is the completeness verdict for that input: MISSING or UNKNOWN.
	Verdict enginesnapshot.CompletenessVerdict
	// Availability is what the snapshot was able to say about the input.
	Availability Availability
	// Detail is a safe, value-free explanation.
	Detail string
}

// Error implements error.
func (e *InputError) Error() string {
	return fmt.Sprintf("promotion/snapshot: required input %s is %s (availability %s): %s",
		e.InputName, e.Verdict, e.Availability, e.Detail)
}

// Unwrap makes every InputError match ErrInputUnavailable.
func (e *InputError) Unwrap() error { return ErrInputUnavailable }

// InputNameOf returns the exact input named by a build refusal, or "" for any
// other error.
func InputNameOf(err error) string {
	var e *InputError
	if errors.As(err, &e) {
		return e.InputName
	}
	return ""
}

// PromotionInputSnapshot is the immutable Promotion input snapshot: every
// material read one ProposePromotion request was formed against, bound to one
// tenant, one effective date, one known-at horizon and one digest.
//
// It is immutable by construction rather than by convention: Build is the only
// constructor, every field it fills is derived from an authorized domain read,
// and the accessors below return copies of the slices they expose so a caller
// holding the snapshot cannot alter the one another caller holds.
type PromotionInputSnapshot struct {
	// Tenant is the single tenant every input belongs to.
	Tenant values.TenantId
	// Subject is the worker being promoted.
	Subject values.EntityRef
	// TargetPosition is the position the promotion moves them into.
	TargetPosition values.EntityRef
	// EffectiveOn is the business date every input is asserted as of.
	EffectiveOn values.LocalDate
	// KnownAt is the knowledge cut-off every input was resolved under.
	KnownAt values.KnownAt
	// EffectiveTime is the open business interval starting at EffectiveOn that
	// the material projection binds as the proposal's effective time.
	EffectiveTime values.EffectiveInterval

	// Reads is the source-neutral engine snapshot binding every input's
	// descriptors under one consistency requirement and one read digest.
	Reads enginesnapshot.ReadSnapshot
	// Completeness is SNAPSHOT-003's three-valued verdict over Reads and
	// [Specification].
	Completeness enginesnapshot.CompletenessResult

	// inputs are the bound inputs in declaration order. They are unexported so
	// the only way to obtain them is [PromotionInputSnapshot.Inputs], which
	// copies.
	inputs []Input

	// Digest is "sha256:<hex>" over the material encoding of
	// [PromotionInputSnapshot.MaterialInputs].
	Digest string
}

// Inputs returns the bound inputs in declaration order. The slice is a copy:
// a caller that mutates it does not mutate the snapshot.
func (s PromotionInputSnapshot) Inputs() []Input {
	return append([]Input(nil), s.inputs...)
}

// Lookup returns the bound input for a name, if the snapshot carries one.
func (s PromotionInputSnapshot) Lookup(name string) (Input, bool) {
	for _, in := range s.inputs {
		if in.Name == name {
			return in, true
		}
	}
	return Input{}, false
}

// Disclosed returns the exact canonical text of one input, and whether it was
// disclosed at all. A withheld, absent or unknown input returns ("", false):
// there is deliberately no accessor that would hand back a non-disclosed
// input's text, because there is none to hand back.
func (s PromotionInputSnapshot) Disclosed(name string) (string, bool) {
	in, ok := s.Lookup(name)
	if !ok || in.Availability != AvailabilityDisclosed {
		return "", false
	}
	return in.CanonicalText, true
}

// Explain reports what the snapshot is made of, in declaration order: the
// subject, the target position, the coordinate it was taken at, and one line
// per input naming its owner, authority class, watermark, availability and
// completeness verdict. It carries no input value, so it is safe to place in a
// refusal, an evidence record or a log.
func (s PromotionInputSnapshot) Explain() string {
	verdicts := make(map[string]enginesnapshot.CompletenessVerdict, len(s.Completeness.Dispositions))
	for _, d := range s.Completeness.Dispositions {
		verdicts[d.Name] = d.Verdict
	}
	var b strings.Builder
	fmt.Fprintf(&b, "promotion input snapshot %s for tenant %s: subject %s into position %s, effective %s, known at %s, overall %s",
		s.Digest, s.Tenant, s.Subject, s.TargetPosition, s.EffectiveOn, s.KnownAt, s.Completeness.Overall)
	for _, in := range s.inputs {
		fmt.Fprintf(&b, "\n- %s (owner %s, authority %s, watermark %s): %s/%s",
			in.Name, in.Entry.Owner, in.Entry.Authority, in.Entry.Watermark, in.Availability, verdicts[in.Name])
		if in.Reason != "" {
			fmt.Fprintf(&b, " [%s]", in.Reason)
		}
	}
	return b.String()
}

// MaterialInputs returns the material-input projection of the snapshot: an
// intent.ProposalRevision carrying exactly the inputs a promotion proposal
// binds as material, and nothing else.
//
// It is a ProposalRevision rather than a shape of this package's own so that
// "material" has one definition in the system. The kernel decides what is
// material by which fields its material encoding covers; projecting onto that
// type means this snapshot's digest is computed by the kernel's own encoder
// over the kernel's own material field list, and cannot drift from it.
//
// The projection deliberately fills only the five material coordinates a read
// snapshot can honestly assert: tenant, subjects, effective time, the
// current-state assertion per input and the source baseline per input.
// Proposed state, writes, effects, children, reservations, approvals,
// obligations and cost are what a *simulation* produces (PROMO-002 onward);
// asserting them here would make the snapshot claim to know the plan.
func (s PromotionInputSnapshot) MaterialInputs() intent.ProposalRevision {
	assertions := make([]intent.StateAssertion, 0, len(s.inputs))
	baselines := make([]intent.SourceBaseline, 0, len(s.inputs))
	for _, in := range s.inputs {
		assertions = append(assertions, intent.StateAssertion{
			Subject:       in.Subject,
			ResourceKey:   in.ResourceKey,
			FieldPath:     in.Name,
			CanonicalText: in.materialText(),
		})
		baselines = append(baselines, intent.SourceBaseline{
			StreamID:         in.Entry.Watermark.Stream(),
			ExpectedRevision: in.Entry.Watermark,
		})
	}
	return intent.ProposalRevision{
		Tenant:          s.Tenant,
		Subjects:        s.subjects(),
		EffectiveTime:   s.EffectiveTime,
		CurrentState:    assertions,
		SourceBaselines: baselines,
	}
}

// subjects returns the deduplicated subject set the material projection binds,
// sorted so the set is construction-order independent even before the kernel's
// own set encoding sorts it.
func (s PromotionInputSnapshot) subjects() []intent.SubjectReference {
	seen := make(map[intent.SubjectReference]struct{}, len(s.inputs))
	out := make([]intent.SubjectReference, 0, len(s.inputs))
	for _, in := range s.inputs {
		if _, dup := seen[in.Subject]; dup {
			continue
		}
		seen[in.Subject] = struct{}{}
		out = append(out, in.Subject)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AuthorityDomain != out[j].AuthorityDomain {
			return out[i].AuthorityDomain < out[j].AuthorityDomain
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	return out
}

// computeDigest returns "sha256:<hex>" over the material payload of the
// projection. It is the kernel's own material encoding, so two snapshots over
// byte-identical authoritative inputs always share it and any material change
// always changes it.
func computeDigest(projection intent.ProposalRevision) string {
	sum := sha256.Sum256(projection.MaterialPayload().WireBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BaselineSnapshot projects the Promotion input snapshot onto the kernel's
// intent.BaselineSnapshot -- the exact input state intent.Preflight evaluates
// and the value ProposePromotion records on the instance.
//
// The mapping is one-for-one and lossless in the direction the kernel cares
// about. SnapshotID is this snapshot's digest, so the baseline the kernel
// preflighted against is identified by the same bytes the proposal's material
// digest was computed from. Revisions binds each input name to the watermark
// it was read at, which is what makes a stale-baseline check possible per
// input rather than per request. ForbiddenFields lists exactly the inputs
// authorization withheld, so the kernel refuses a proposal that tried to
// assert one. PresentInputs lists the inputs that actually resolved to a
// value, and NegativeStates records one entry per input that did not: REDACTED
// for a withheld one, UNAVAILABLE for an absent one, UNKNOWN for an
// unestablished one, which is how a non-complete snapshot reaches the kernel
// as a stated negative rather than as a silent gap.
func (s PromotionInputSnapshot) BaselineSnapshot() intent.BaselineSnapshot {
	out := intent.BaselineSnapshot{
		SnapshotID: s.Digest,
		ObservedAt: s.KnownAt.Instant(),
		Revisions:  make(map[string]values.RevisionToken, len(s.inputs)),
	}
	subjects := s.subjects()
	out.KnownSubjects = append(out.KnownSubjects, subjects...)
	for _, in := range s.inputs {
		out.Revisions[in.Name] = in.Entry.Watermark
		switch in.Availability {
		case AvailabilityDisclosed:
			out.PresentInputs = append(out.PresentInputs, in.Name)
		case AvailabilityWithheld:
			out.ForbiddenFields = append(out.ForbiddenFields, in.Name)
			out.NegativeStates = append(out.NegativeStates, intent.NegativeRedacted)
		case AvailabilityAbsent:
			out.NegativeStates = append(out.NegativeStates, intent.NegativeUnavailable)
		default:
			out.NegativeStates = append(out.NegativeStates, intent.NegativeUnknown)
		}
	}
	return out
}
