package readiness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidResolution = errors.New("readiness: invalid resolution request")
	ErrEvidenceReader    = errors.New("readiness: evidence reader failed")
	ErrEvidenceMismatch  = errors.New("readiness: evidence does not match the pinned query")
)

// ResolutionStatus is the requirement-level outcome produced by evidence
// resolution. It is intentionally distinct from ReadinessStatus: resolving
// evidence as SATISFIED does not itself authorize a domain's READY decision.
type ResolutionStatus string

const (
	ResolutionSatisfied   ResolutionStatus = "SATISFIED"
	ResolutionUnsatisfied ResolutionStatus = "UNSATISFIED"
	ResolutionConditional ResolutionStatus = "CONDITIONAL"
	ResolutionUnknown     ResolutionStatus = "UNKNOWN"
)

func (s ResolutionStatus) Valid() bool {
	return s == ResolutionSatisfied || s == ResolutionUnsatisfied || s == ResolutionConditional || s == ResolutionUnknown
}

func (s ResolutionStatus) String() string { return string(s) }

// EvidenceAccess is decided by the authorization boundary before evidence
// reaches this engine. Unauthorized descriptors are never copied to results.
type EvidenceAccess string

const (
	EvidenceAuthorized EvidenceAccess = "AUTHORIZED"
	EvidenceDenied     EvidenceAccess = "DENIED"
	EvidenceWithheld   EvidenceAccess = "WITHHELD"
)

func (a EvidenceAccess) Valid() bool {
	return a == EvidenceAuthorized || a == EvidenceDenied || a == EvidenceWithheld
}

// EvidenceTrust records source trust without importing raw document/content
// custody. Untrusted and quarantined descriptors can never satisfy a rule.
type EvidenceTrust string

const (
	EvidenceTrusted     EvidenceTrust = "TRUSTED"
	EvidenceUntrusted   EvidenceTrust = "UNTRUSTED"
	EvidenceQuarantined EvidenceTrust = "QUARANTINED"
)

func (t EvidenceTrust) Valid() bool {
	return t == EvidenceTrusted || t == EvidenceUntrusted || t == EvidenceQuarantined
}

// EvidenceState controls the requirement-level result once all authorization,
// trust, freshness and pinned-version checks pass.
type EvidenceState string

const (
	EvidenceSatisfied   EvidenceState = "SATISFIED"
	EvidenceConditional EvidenceState = "CONDITIONAL"
)

func (s EvidenceState) Valid() bool { return s == EvidenceSatisfied || s == EvidenceConditional }

// EvidenceClassification is a label reference, not content. The resolver
// carries it into an authorized reference so downstream consumers can apply
// their own policy without seeing the artifact.
type EvidenceClassification string

const (
	ClassificationPublic       EvidenceClassification = "PUBLIC"
	ClassificationInternal     EvidenceClassification = "INTERNAL"
	ClassificationConfidential EvidenceClassification = "CONFIDENTIAL"
	ClassificationRestricted   EvidenceClassification = "RESTRICTED"
)

func (c EvidenceClassification) Valid() bool {
	return c == ClassificationPublic || c == ClassificationInternal || c == ClassificationConfidential || c == ClassificationRestricted
}

// Provenance is the minimal source-attribution descriptor. It intentionally
// contains no raw document or medical content.
type Provenance struct {
	Source      string
	EvidenceRef string
	RecordedAt  values.RecordedAt
}

func (p Provenance) Validate() error {
	if strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.EvidenceRef) == "" || p.RecordedAt.Canonical() == nil {
		return fmt.Errorf("%w: provenance requires source, evidence ref and recorded-at", ErrInvalidResolution)
	}
	return nil
}

func (p Provenance) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.engines.readiness.Provenance", schemaVersion).
		String("source", p.Source).String("evidence_ref", p.EvidenceRef).Value("recorded_at", p.RecordedAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// EvidenceDescriptor is the only evidence shape the engine accepts: a
// typed, bitemporal, source-attributed descriptor with authorization and
// trust state. There is deliberately no value/content field.
type EvidenceDescriptor struct {
	Tenant          values.TenantId
	Subject         values.EntityRef
	RequirementID   string
	EvidenceRef     string
	Kind            EvidenceKind
	Effective       values.EffectiveInterval
	KnownAt         values.KnownAt
	Revision        values.RevisionToken
	ObservedAt      values.Instant
	FreshUntil      values.Instant
	SourceAuthority string
	Provenance      Provenance
	Classification  EvidenceClassification
	Access          EvidenceAccess
	Trust           EvidenceTrust
	State           EvidenceState
}

func (e EvidenceDescriptor) Validate() error {
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: evidence tenant: %v", ErrInvalidResolution, err)
	}
	if err := e.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: evidence subject: %v", ErrInvalidResolution, err)
	}
	for label, value := range map[string]string{"requirement": e.RequirementID, "evidence_ref": e.EvidenceRef, "source_authority": e.SourceAuthority} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: evidence %s is required", ErrInvalidResolution, label)
		}
	}
	if !e.Kind.Valid() || !e.Access.Valid() || !e.Trust.Valid() || !e.State.Valid() || !e.Classification.Valid() {
		return fmt.Errorf("%w: evidence descriptor has an invalid closed field", ErrInvalidResolution)
	}
	if err := e.Effective.Validate(); err != nil || e.Effective.Kind() != values.IntervalKindInstant {
		return fmt.Errorf("%w: evidence effective interval must be a valid instant interval", ErrInvalidResolution)
	}
	if e.KnownAt.Canonical() == nil || e.ObservedAt.Validate() != nil || e.FreshUntil.Validate() != nil {
		return fmt.Errorf("%w: evidence bitemporal/freshness fields are invalid", ErrInvalidResolution)
	}
	if err := e.Revision.Validate(); err != nil || !e.Revision.IsSpecified() {
		return fmt.Errorf("%w: evidence revision is required", ErrInvalidResolution)
	}
	if err := e.Provenance.Validate(); err != nil {
		return err
	}
	if e.FreshUntil.Compare(e.ObservedAt) < 0 {
		return fmt.Errorf("%w: evidence freshness ends before observation", ErrInvalidResolution)
	}
	return nil
}

// EvidenceReference is the authorized, content-free evidence returned by a
// successful resolution.
type EvidenceReference struct {
	EvidenceRef     string
	Kind            EvidenceKind
	FreshUntil      values.Instant
	Revision        values.RevisionToken
	SourceAuthority string
	Provenance      Provenance
	Classification  EvidenceClassification
}

func (e EvidenceReference) Canonical() []byte {
	if strings.TrimSpace(e.EvidenceRef) == "" || !e.Kind.Valid() || strings.TrimSpace(e.SourceAuthority) == "" || e.FreshUntil.Validate() != nil || e.Revision.Validate() != nil || !e.Revision.IsSpecified() || e.Provenance.Validate() != nil || !e.Classification.Valid() {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.engines.readiness.EvidenceReference", schemaVersion).
		String("evidence_ref", e.EvidenceRef).String("kind", e.Kind.String()).Value("fresh_until", e.FreshUntil).
		Value("revision", e.Revision).String("source_authority", e.SourceAuthority).Value("provenance", e.Provenance).
		String("classification", string(e.Classification)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// EvidenceQuery pins every read to one tenant, subject, requirement and
// known-at horizon. A reader must apply authorization before returning rows.
type EvidenceQuery struct {
	Tenant        values.TenantId
	Subject       values.EntityRef
	RequirementID string
	AsOf          values.Instant
	KnownAt       values.KnownAt
}

func (q EvidenceQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: query tenant: %v", ErrInvalidResolution, err)
	}
	if err := q.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: query subject: %v", ErrInvalidResolution, err)
	}
	if strings.TrimSpace(q.RequirementID) == "" || q.AsOf.Validate() != nil || q.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: query requirement, as-of and known-at are required", ErrInvalidResolution)
	}
	return nil
}

// EvidenceReader is the authorization-aware read port. It must return only
// typed descriptors for the exact query and never raw document content.
type EvidenceReader interface {
	Read(ctx context.Context, query EvidenceQuery) ([]EvidenceDescriptor, error)
}

// ResolutionRequest binds a requirement to one pinned subject/time context.
type ResolutionRequest struct {
	Requirement ReadinessRequirement
	Tenant      values.TenantId
	Subject     values.EntityRef
	AsOf        values.Instant
	KnownAt     values.KnownAt
}

func (r ResolutionRequest) Validate() error {
	if err := r.Requirement.Validate(); err != nil {
		return err
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidResolution, err)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidResolution, err)
	}
	if err := r.AsOf.Validate(); err != nil || r.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: as-of and known-at are required", ErrInvalidResolution)
	}
	inside, err := r.Requirement.Effective.ContainsInstant(r.AsOf)
	if err != nil || !inside {
		return fmt.Errorf("%w: as-of is outside requirement effective interval", ErrInvalidResolution)
	}
	return nil
}

// Resolution is the deterministic result of resolving one requirement. Only
// authorized, trusted, fresh, pinned evidence appears in Evidence.
type Resolution struct {
	RequirementID string
	Revision      uint64
	Status        ResolutionStatus
	Evidence      []EvidenceReference
	Reasons       []string
	KnownAt       values.KnownAt
	AsOf          values.Instant
	Digest        string
}

func (r Resolution) Validate() error {
	if strings.TrimSpace(r.RequirementID) == "" || r.Revision == 0 || !r.Status.Valid() || r.AsOf.Validate() != nil || r.KnownAt.Canonical() == nil || strings.TrimSpace(r.Digest) == "" {
		return fmt.Errorf("%w: malformed resolution", ErrInvalidResolution)
	}
	for _, evidence := range r.Evidence {
		if evidence.Canonical() == nil {
			return fmt.Errorf("%w: malformed authorized evidence reference", ErrInvalidResolution)
		}
	}
	if (r.Status == ResolutionSatisfied || r.Status == ResolutionConditional) && len(r.Evidence) == 0 {
		return fmt.Errorf("%w: positive resolution has no authorized evidence", ErrInvalidResolution)
	}
	if (r.Status == ResolutionUnsatisfied || r.Status == ResolutionUnknown) && len(r.Reasons) == 0 {
		return fmt.Errorf("%w: non-positive resolution has no safe reason", ErrInvalidResolution)
	}
	return nil
}

// Resolve reads and filters only typed evidence. A denied descriptor never
// contributes its reference to the output; stale, untrusted and quarantined
// descriptors never satisfy a requirement. Unknown is used when the caller
// cannot establish authorization or pinned provenance without guessing.
func Resolve(ctx context.Context, reader EvidenceReader, req ResolutionRequest) (Resolution, error) {
	if err := req.Validate(); err != nil {
		return Resolution{}, err
	}
	if reader == nil {
		return Resolution{}, fmt.Errorf("%w: no reader configured", ErrEvidenceReader)
	}
	query := EvidenceQuery{Tenant: req.Tenant, Subject: req.Subject, RequirementID: req.Requirement.RequirementID, AsOf: req.AsOf, KnownAt: req.KnownAt}
	descriptors, err := reader.Read(ctx, query)
	if err != nil {
		return Resolution{}, fmt.Errorf("%w: %v", ErrEvidenceReader, err)
	}
	accepted := make(map[EvidenceKind]struct{}, len(req.Requirement.EvidenceKinds))
	for _, kind := range req.Requirement.EvidenceKinds {
		accepted[kind] = struct{}{}
	}
	result := Resolution{RequirementID: req.Requirement.RequirementID, Revision: req.Requirement.Revision, Status: ResolutionUnsatisfied, KnownAt: req.KnownAt, AsOf: req.AsOf}
	var denied, unpinned, conditional bool
	for _, descriptor := range descriptors {
		if err := descriptor.Validate(); err != nil {
			return Resolution{}, err
		}
		if descriptor.Tenant != req.Tenant || descriptor.Subject != req.Subject || descriptor.RequirementID != req.Requirement.RequirementID {
			return Resolution{}, fmt.Errorf("%w: descriptor identity differs from query", ErrEvidenceMismatch)
		}
		if descriptor.Access != EvidenceAuthorized {
			denied = true
			continue
		}
		if descriptor.KnownAt.Instant().Compare(req.KnownAt.Instant()) != 0 {
			unpinned = true
			continue
		}
		if descriptor.Provenance.RecordedAt.Instant().Compare(req.KnownAt.Instant()) > 0 {
			unpinned = true
			continue
		}
		if _, ok := accepted[descriptor.Kind]; !ok {
			continue
		}
		if descriptor.Trust != EvidenceTrusted {
			result.Reasons = append(result.Reasons, "evidence_not_trusted")
			continue
		}
		inside, _ := descriptor.Effective.ContainsInstant(req.AsOf)
		if !inside || descriptor.FreshUntil.Compare(req.AsOf) < 0 || descriptor.ObservedAt.Compare(req.AsOf) > 0 {
			result.Reasons = append(result.Reasons, "evidence_stale")
			continue
		}
		result.Evidence = append(result.Evidence, EvidenceReference{EvidenceRef: descriptor.EvidenceRef, Kind: descriptor.Kind, FreshUntil: descriptor.FreshUntil, Revision: descriptor.Revision, SourceAuthority: descriptor.SourceAuthority, Provenance: descriptor.Provenance, Classification: descriptor.Classification})
		conditional = conditional || descriptor.State == EvidenceConditional
	}
	if len(result.Evidence) > 0 {
		if conditional {
			result.Status = ResolutionConditional
		} else {
			result.Status = ResolutionSatisfied
		}
	} else if denied || unpinned {
		result.Status = ResolutionUnknown
		if denied {
			result.Reasons = append(result.Reasons, "evidence_not_authorized")
		}
		if unpinned {
			result.Reasons = append(result.Reasons, "evidence_not_pinned")
		}
	} else if len(result.Reasons) == 0 {
		result.Reasons = append(result.Reasons, "evidence_not_satisfied")
	}
	sort.Slice(result.Evidence, func(i, j int) bool {
		if result.Evidence[i].EvidenceRef != result.Evidence[j].EvidenceRef {
			return result.Evidence[i].EvidenceRef < result.Evidence[j].EvidenceRef
		}
		return result.Evidence[i].Kind < result.Evidence[j].Kind
	})
	sort.Strings(result.Reasons)
	result.Digest, err = resolutionDigest(req, result)
	if err != nil {
		return Resolution{}, err
	}
	return result, nil
}

func resolutionDigest(req ResolutionRequest, result Resolution) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.Resolution", schemaVersion).
		String("requirement_digest", req.Requirement.CanonicalDigest).String("tenant", string(req.Tenant)).
		Value("subject", req.Subject).Value("as_of", req.AsOf).Value("known_at", req.KnownAt).
		String("status", result.Status.String()).Count("evidence", len(result.Evidence))
	for _, evidence := range result.Evidence {
		w.Value("evidence_ref", evidence)
	}
	w.Count("reasons", len(result.Reasons))
	for _, reason := range result.Reasons {
		w.String("reason", reason)
	}
	return w.Digest()
}
