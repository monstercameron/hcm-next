// Package refdata owns the immutable reference-dataset release and
// tenant-adoption lifecycle. It is deliberately kernel-pure: persistence,
// authorization and source retrieval are supplied by callers as evidence.
package refdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const schemaVersion = 1

// Version returns the reference-data contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidRelease      = errors.New("refdata: invalid release")
	ErrInvalidMember       = errors.New("refdata: invalid member")
	ErrInvalidSource       = errors.New("refdata: invalid source")
	ErrReleaseNotValidated = errors.New("refdata: release is not validated")
	ErrReleaseNotPublished = errors.New("refdata: release is not published")
	ErrReleaseWithdrawn    = errors.New("refdata: release is withdrawn")
	ErrInvalidAdoption     = errors.New("refdata: invalid adoption")
	ErrUnknownMember       = errors.New("refdata: override names an unknown member")
	ErrMandatoryOverride   = errors.New("refdata: mandatory member cannot be weakened")
	ErrDuplicateAdoption   = errors.New("refdata: adoption already exists")
	ErrRollbackUnavailable = errors.New("refdata: rollback target is unavailable")
)

// ReleaseState is the governed lifecycle of one immutable release.
type ReleaseState string

const (
	StateDraft     ReleaseState = "DRAFT"
	StateValidated ReleaseState = "VALIDATED"
	StatePublished ReleaseState = "PUBLISHED"
	StateWithdrawn ReleaseState = "WITHDRAWN"
)

func (s ReleaseState) valid() bool {
	return s == StateDraft || s == StateValidated || s == StatePublished || s == StateWithdrawn
}

// SourceEvidence binds a release to its authoritative source, license and
// retrieval evidence. A source version is not inferred from a URL.
type SourceEvidence struct {
	SourceRef     string    `json:"source_ref"`
	SourceVersion string    `json:"source_version"`
	SourceDigest  string    `json:"source_digest"`
	SignatureRef  string    `json:"signature_ref"`
	LicenseRef    string    `json:"license_ref"`
	Coverage      string    `json:"coverage"`
	RetrievedAt   time.Time `json:"retrieved_at"`
}

// Member is one canonical reference value. Retired members remain in the
// release so historical pins can be replayed, but a release cannot carry two
// meanings for the same code.
type Member struct {
	Code       string            `json:"code"`
	Label      string            `json:"label"`
	Mandatory  bool              `json:"mandatory"`
	Retired    bool              `json:"retired"`
	ValidFrom  time.Time         `json:"valid_from"`
	ValidTo    time.Time         `json:"valid_to,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Digest     string            `json:"digest"`
}

// NewMember creates a content-addressed member. Attributes are copied and
// sorted by the canonical encoder, so construction order cannot change its
// identity.
func NewMember(code, label string, mandatory, retired bool, validFrom, validTo time.Time, attributes map[string]string) (Member, error) {
	m := Member{Code: code, Label: label, Mandatory: mandatory, Retired: retired, ValidFrom: validFrom.UTC(), ValidTo: validTo.UTC(), Attributes: cloneMap(attributes)}
	if err := m.validate(); err != nil {
		return Member{}, err
	}
	m.Digest = memberDigest(m)
	return m, nil
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m Member) validate() error {
	if strings.TrimSpace(m.Code) == "" || m.Code != strings.TrimSpace(m.Code) || strings.TrimSpace(m.Label) == "" || m.Label != strings.TrimSpace(m.Label) {
		return fmt.Errorf("%w: code and label are required", ErrInvalidMember)
	}
	if m.ValidFrom.IsZero() || (!m.ValidTo.IsZero() && !m.ValidFrom.Before(m.ValidTo)) {
		return fmt.Errorf("%w: member validity interval is invalid", ErrInvalidMember)
	}
	return nil
}

// Validate checks a member independently of a release.
func (m Member) Validate() error { return m.validate() }

func memberDigest(m Member) string {
	canonical := struct {
		Code       string            `json:"code"`
		Label      string            `json:"label"`
		Mandatory  bool              `json:"mandatory"`
		Retired    bool              `json:"retired"`
		ValidFrom  string            `json:"valid_from"`
		ValidTo    string            `json:"valid_to,omitempty"`
		Attributes map[string]string `json:"attributes,omitempty"`
	}{m.Code, m.Label, m.Mandatory, m.Retired, m.ValidFrom.UTC().Format(time.RFC3339Nano), formatTime(m.ValidTo), m.Attributes}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Release is an immutable source release. State transitions do not change
// Digest; a consumer can therefore retain a stable content pin while the
// publication state evolves.
type Release struct {
	DatasetID       string         `json:"dataset_id"`
	Version         string         `json:"version"`
	State           ReleaseState   `json:"state"`
	Source          SourceEvidence `json:"source"`
	SchemaDigest    string         `json:"schema_digest"`
	Applicability   []string       `json:"applicability"`
	Members         []Member       `json:"members"`
	ConsumerRefs    []string       `json:"consumer_refs"`
	AffectedIntents []string       `json:"affected_intents"`
	EffectiveFrom   time.Time      `json:"effective_from"`
	EffectiveTo     time.Time      `json:"effective_to,omitempty"`
	KnownAt         time.Time      `json:"known_at"`
	Digest          string         `json:"digest"`
}

// NewRelease seals and validates a draft release. Missing member digests are
// filled from the member content; supplied digests must already match.
func NewRelease(r Release) (Release, error) {
	if r.State == "" {
		r.State = StateDraft
	}
	r = cloneRelease(r)
	for i := range r.Members {
		if r.Members[i].Digest == "" {
			r.Members[i].Digest = memberDigest(r.Members[i])
		}
	}
	sort.Slice(r.Members, func(i, j int) bool { return r.Members[i].Code < r.Members[j].Code })
	if err := r.validate(false); err != nil {
		return Release{}, err
	}
	r.Digest = releaseDigest(r)
	return r, nil
}

// SealRelease is the explicit-name alias for NewRelease.
func SealRelease(r Release) (Release, error) { return NewRelease(r) }

func cloneRelease(r Release) Release {
	r.Applicability = append([]string(nil), r.Applicability...)
	r.ConsumerRefs = append([]string(nil), r.ConsumerRefs...)
	r.AffectedIntents = append([]string(nil), r.AffectedIntents...)
	r.Members = append([]Member(nil), r.Members...)
	for i := range r.Members {
		r.Members[i].Attributes = cloneMap(r.Members[i].Attributes)
	}
	return r
}

func normalizeSet(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func (r Release) validate(checkDigest bool) error {
	if strings.TrimSpace(r.DatasetID) == "" || strings.TrimSpace(r.Version) == "" || r.DatasetID != strings.TrimSpace(r.DatasetID) || r.Version != strings.TrimSpace(r.Version) {
		return fmt.Errorf("%w: dataset id and version are required", ErrInvalidRelease)
	}
	if !r.State.valid() {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidRelease, r.State)
	}
	if strings.TrimSpace(r.Source.SourceRef) == "" || strings.TrimSpace(r.Source.SourceVersion) == "" || !validDigest(r.Source.SourceDigest) || strings.TrimSpace(r.Source.SignatureRef) == "" || strings.TrimSpace(r.Source.LicenseRef) == "" || strings.TrimSpace(r.Source.Coverage) == "" {
		return fmt.Errorf("%w: source, signature, license and coverage evidence are required", ErrInvalidSource)
	}
	if r.Source.RetrievedAt.IsZero() || r.EffectiveFrom.IsZero() || r.KnownAt.IsZero() || (!r.EffectiveTo.IsZero() && !r.EffectiveFrom.Before(r.EffectiveTo)) {
		return fmt.Errorf("%w: source retrieval and effective-known intervals are required", ErrInvalidRelease)
	}
	if !validDigest(r.SchemaDigest) || len(r.Members) == 0 {
		return fmt.Errorf("%w: schema digest and at least one member are required", ErrInvalidRelease)
	}
	seen := make(map[string]bool, len(r.Members))
	for _, m := range r.Members {
		if err := m.validate(); err != nil {
			return err
		}
		if seen[m.Code] {
			return fmt.Errorf("%w: duplicate member %q", ErrInvalidRelease, m.Code)
		}
		seen[m.Code] = true
		if m.Digest == "" || m.Digest != memberDigest(m) {
			return fmt.Errorf("%w: member %q digest does not match content", ErrInvalidRelease, m.Code)
		}
	}
	if len(normalizeSet(r.Applicability)) == 0 || len(normalizeSet(r.ConsumerRefs)) == 0 || len(normalizeSet(r.AffectedIntents)) == 0 {
		return fmt.Errorf("%w: applicability, consumers and affected intents are required", ErrInvalidRelease)
	}
	if checkDigest && (r.Digest == "" || r.Digest != releaseDigest(r)) {
		return fmt.Errorf("%w: release digest does not match content", ErrInvalidRelease)
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Validate verifies the complete sealed release, including its content
// digest. Use NewRelease to seal a newly authored draft.
func (r Release) Validate() error { return r.validate(true) }

func releaseDigest(r Release) string {
	canonical := cloneRelease(r)
	canonical.State = ""
	canonical.Digest = ""
	canonical.Applicability = normalizeSet(canonical.Applicability)
	canonical.ConsumerRefs = normalizeSet(canonical.ConsumerRefs)
	canonical.AffectedIntents = normalizeSet(canonical.AffectedIntents)
	sort.Slice(canonical.Members, func(i, j int) bool { return canonical.Members[i].Code < canonical.Members[j].Code })
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Validation is evidence that the exact release digest passed completeness
// and conflict checks.
type Validation struct {
	DatasetID string
	Version   string
	Digest    string
	CheckedAt time.Time
}

// ValidateRelease performs source, schema, member completeness and digest
// validation without changing the release.
func ValidateRelease(r Release, checkedAt time.Time) (Validation, error) {
	if err := r.validate(true); err != nil {
		return Validation{}, err
	}
	if r.State == StateWithdrawn {
		return Validation{}, ErrReleaseWithdrawn
	}
	if checkedAt.IsZero() {
		return Validation{}, fmt.Errorf("%w: validation time is required", ErrInvalidRelease)
	}
	return Validation{DatasetID: r.DatasetID, Version: r.Version, Digest: r.Digest, CheckedAt: checkedAt.UTC()}, nil
}

// PublishRelease creates a published state revision only when the validation
// still names the exact immutable content.
func PublishRelease(r Release, v Validation, publishedAt time.Time) (Release, error) {
	if v.DatasetID != r.DatasetID || v.Version != r.Version || v.Digest == "" || v.Digest != r.Digest {
		return Release{}, ErrReleaseNotValidated
	}
	if _, err := ValidateRelease(r, v.CheckedAt); err != nil {
		return Release{}, err
	}
	if r.State != StateValidated && r.State != StateDraft {
		return Release{}, ErrReleaseNotValidated
	}
	if publishedAt.IsZero() {
		return Release{}, fmt.Errorf("%w: publication time is required", ErrInvalidRelease)
	}
	out := cloneRelease(r)
	out.State = StatePublished
	return out, nil
}

// MarkValidated returns the lifecycle state consumed by PublishRelease.
func MarkValidated(r Release, v Validation) (Release, error) {
	if v.DatasetID != r.DatasetID || v.Version != r.Version || v.Digest != r.Digest {
		return Release{}, ErrReleaseNotValidated
	}
	out := cloneRelease(r)
	out.State = StateValidated
	return out, nil
}

// WithdrawRelease records a terminal source withdrawal. It preserves the
// content digest so existing execution pins remain explainable, while future
// adoption and rollback reject the withdrawn release.
func WithdrawRelease(r Release, at time.Time) (Release, error) {
	if r.State != StatePublished || at.IsZero() {
		return Release{}, ErrReleaseNotPublished
	}
	out := cloneRelease(r)
	out.State = StateWithdrawn
	return out, nil
}

// Override is a tenant-local, governed deviation from a release member. It
// cannot remove or blank a mandatory member.
type Override struct {
	Code       string `json:"code"`
	Value      string `json:"value"`
	Disable    bool   `json:"disable"`
	Reason     string `json:"reason"`
	ApprovedBy string `json:"approved_by"`
}

// AdoptionRequest carries the rollout gate and impact evidence required to
// move one tenant to a published release.
type AdoptionRequest struct {
	TenantID      string
	EffectiveAt   time.Time
	Actor         string
	RolloutDigest string
	Overrides     []Override
	ConsumerRefs  []string
	ImpactRefs    []string
	Rollback      bool
}

type AdoptionEventKind string

const (
	EventAdopted  AdoptionEventKind = "ADOPTED"
	EventRollback AdoptionEventKind = "ROLLED_BACK"
)

// Adoption is one append-only tenant event and its effective pin.
type Adoption struct {
	TenantID        string            `json:"tenant_id"`
	DatasetID       string            `json:"dataset_id"`
	Version         string            `json:"version"`
	ReleaseDigest   string            `json:"release_digest"`
	Event           AdoptionEventKind `json:"event"`
	PreviousVersion string            `json:"previous_version,omitempty"`
	EffectiveAt     time.Time         `json:"effective_at"`
	AdoptedAt       time.Time         `json:"adopted_at"`
	Actor           string            `json:"actor"`
	RolloutDigest   string            `json:"rollout_digest"`
	Overrides       []Override        `json:"overrides,omitempty"`
	ConsumerRefs    []string          `json:"consumer_refs"`
	ImpactRefs      []string          `json:"impact_refs"`
	Digest          string            `json:"digest"`
}

// ExecutionPin is the compact immutable reference a running execution keeps.
type ExecutionPin struct {
	DatasetID     string
	Version       string
	ReleaseDigest string
	EffectiveAt   time.Time
}

// Pin returns a copy-safe execution pin from an adoption event.
func Pin(a Adoption) ExecutionPin {
	return ExecutionPin{DatasetID: a.DatasetID, Version: a.Version, ReleaseDigest: a.ReleaseDigest, EffectiveAt: a.EffectiveAt.UTC()}
}

// Adopt validates and creates one tenant adoption event. current may be nil
// for the first adoption.
func Adopt(current *Adoption, release Release, req AdoptionRequest, adoptedAt time.Time) (Adoption, error) {
	if release.State != StatePublished {
		if release.State == StateWithdrawn {
			return Adoption{}, ErrReleaseWithdrawn
		}
		return Adoption{}, ErrReleaseNotPublished
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.RolloutDigest) == "" || req.EffectiveAt.IsZero() || adoptedAt.IsZero() {
		return Adoption{}, fmt.Errorf("%w: tenant, actor, rollout gate and times are required", ErrInvalidAdoption)
	}
	if req.EffectiveAt.Before(release.EffectiveFrom) || (!release.EffectiveTo.IsZero() && !req.EffectiveAt.Before(release.EffectiveTo)) {
		return Adoption{}, fmt.Errorf("%w: effective date is outside release interval", ErrInvalidAdoption)
	}
	if current != nil {
		if current.DatasetID != release.DatasetID || current.TenantID != req.TenantID {
			return Adoption{}, fmt.Errorf("%w: current adoption does not match release or tenant", ErrInvalidAdoption)
		}
		if current.Version == release.Version && current.ReleaseDigest == release.Digest {
			return Adoption{}, ErrDuplicateAdoption
		}
	}
	memberByCode := make(map[string]Member, len(release.Members))
	for _, member := range release.Members {
		memberByCode[member.Code] = member
	}
	seen := make(map[string]bool, len(req.Overrides))
	overrides := append([]Override(nil), req.Overrides...)
	sort.Slice(overrides, func(i, j int) bool { return overrides[i].Code < overrides[j].Code })
	for _, override := range overrides {
		member, ok := memberByCode[override.Code]
		if !ok {
			return Adoption{}, fmt.Errorf("%w: %s", ErrUnknownMember, override.Code)
		}
		if seen[override.Code] || strings.TrimSpace(override.Reason) == "" || strings.TrimSpace(override.ApprovedBy) == "" {
			return Adoption{}, fmt.Errorf("%w: duplicate or ungoverned override %q", ErrInvalidAdoption, override.Code)
		}
		seen[override.Code] = true
		if member.Mandatory && (override.Disable || strings.TrimSpace(override.Value) == "") {
			return Adoption{}, fmt.Errorf("%w: %s", ErrMandatoryOverride, override.Code)
		}
	}
	consumerRefs := normalizeSet(req.ConsumerRefs)
	if len(consumerRefs) == 0 {
		consumerRefs = normalizeSet(release.ConsumerRefs)
	}
	impactRefs := normalizeSet(req.ImpactRefs)
	if len(impactRefs) == 0 {
		impactRefs = normalizeSet(release.AffectedIntents)
	}
	if len(consumerRefs) == 0 || len(impactRefs) == 0 {
		return Adoption{}, fmt.Errorf("%w: consumer and impact evidence is required", ErrInvalidAdoption)
	}
	event := EventAdopted
	previous := ""
	if current != nil {
		previous = current.Version
	}
	if req.Rollback {
		event = EventRollback
		if current == nil || current.Version == "" {
			return Adoption{}, ErrRollbackUnavailable
		}
	}
	out := Adoption{TenantID: req.TenantID, DatasetID: release.DatasetID, Version: release.Version, ReleaseDigest: release.Digest, Event: event, PreviousVersion: previous, EffectiveAt: req.EffectiveAt.UTC(), AdoptedAt: adoptedAt.UTC(), Actor: req.Actor, RolloutDigest: req.RolloutDigest, Overrides: overrides, ConsumerRefs: consumerRefs, ImpactRefs: impactRefs}
	out.Digest = adoptionDigest(out)
	return out, nil
}

// Rollback creates a new adoption event for target; it never mutates current.
func Rollback(current Adoption, target Release, actor string, effectiveAt, adoptedAt time.Time) (Adoption, error) {
	return Adopt(&current, target, AdoptionRequest{TenantID: current.TenantID, EffectiveAt: effectiveAt, Actor: actor, RolloutDigest: digestText("rollback:" + target.Digest), ConsumerRefs: target.ConsumerRefs, ImpactRefs: target.AffectedIntents, Rollback: true}, adoptedAt)
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func adoptionDigest(a Adoption) string {
	copy := a
	copy.Digest = ""
	b, _ := json.Marshal(copy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ValidateAdoption verifies the immutable event and its release binding.
func ValidateAdoption(a Adoption, r Release) error {
	if a.DatasetID != r.DatasetID || a.Version != r.Version || a.ReleaseDigest != r.Digest || a.Digest == "" || a.Digest != adoptionDigest(a) {
		return ErrInvalidAdoption
	}
	if a.Event != EventAdopted && a.Event != EventRollback {
		return ErrInvalidAdoption
	}
	return nil
}

// Explain returns an audit-safe summary with source and impact pins, not
// member values or tenant override payloads.
func Explain(r Release) string {
	return fmt.Sprintf("reference release %s@%s digest=%s state=%s source=%s/%s consumers=%d impacts=%d", r.DatasetID, r.Version, r.Digest, r.State, r.Source.SourceRef, r.Source.SourceVersion, len(r.ConsumerRefs), len(r.AffectedIntents))
}

// Explain returns the same audit-safe summary as the package-level helper.
func (r Release) Explain() string { return Explain(r) }

// Validate checks an adoption event against the exact source release it
// claims to have adopted.
func (a Adoption) Validate(r Release) error { return ValidateAdoption(a, r) }
