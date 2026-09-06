package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidPublication  = errors.New("mapping: invalid publication")
	ErrInvalidActivation   = errors.New("mapping: invalid activation evidence")
	ErrPublicationConflict = errors.New("mapping: publication conflict")
	ErrUnpublishedVersion  = errors.New("mapping: version is unpublished")
	ErrSupersededVersion   = errors.New("mapping: version is superseded")
	ErrNoActiveVersion     = errors.New("mapping: no active version")
	ErrAlreadyActive       = errors.New("mapping: version is already active")
)

// PublicationEvidence is the approval and provenance required to publish one
// compiled MappingProfileVersion. Author and Approver are deliberately
// separate identities: a person cannot publish their own mapping approval.
// PublishedAt is caller supplied when a timestamp is available; this package
// never reads a clock.
type PublicationEvidence struct {
	Author            string
	Approver          string
	Reason            string
	EvidenceRef       string
	PublishedAt       time.Time
	EffectiveInterval EffectiveInterval
}

// PublicationRequest is the descriptive name for PublicationEvidence used by
// the method and function forms of Publish.
type PublicationRequest = PublicationEvidence

// ActivationEvidence identifies the approver who authorized an activation.
// ActivatedBy is accepted as a compatibility spelling for Approver. If both
// are supplied they must identify the same principal.
type ActivationEvidence struct {
	Approver    string
	ActivatedBy string
	Reason      string
	EvidenceRef string
	At          time.Time
	ActivatedAt time.Time
}

// EffectiveInterval is the explicit validity interval for a published
// mapping. A zero endpoint is open-ended; when both endpoints are present,
// From must precede To. The interval is copied into the publication digest so
// changing it requires a new immutable publication.
type EffectiveInterval struct {
	From time.Time
	To   time.Time
}

// PublicationRecord is an immutable publication of one compiled profile
// version. The registry returns deep copies of the compiled value and never
// mutates a record after it is published. Superseded is registry state: it
// says that a later activation displaced this publication, while the
// publication record and its approval evidence remain intact for rollback.
type PublicationRecord struct {
	MappingID string
	Version   int
	Compiled  MappingProfileVersion

	Author            string
	Approver          string
	Reason            string
	EvidenceRef       string
	PublishedAt       time.Time
	EffectiveInterval EffectiveInterval
	Superseded        bool

	publicationDigest string
}

// Digest returns the digest of the publication record, including the
// compiled profile digest and its approval evidence.
func (p PublicationRecord) Digest() string { return p.publicationDigest }

// Explain gives a bounded, audit-friendly description without embedding the
// profile payload or approval reason in logs and operator summaries.
func (p PublicationRecord) Explain() string {
	return fmt.Sprintf("mapping publication %s@%d by %s approved by %s (superseded=%t; %s)",
		p.MappingID, p.Version, p.Author, p.Approver, p.Superseded, p.publicationDigest)
}

// ActivationEvent is one append-only activation. PreviousVersion and
// PreviousPublicationDigest are the lineage edge to the version active just
// before this event. Rollback is an explicit activation of an older published
// version; it never removes a publication or an earlier event.
type ActivationEvent struct {
	Sequence uint64

	MappingID         string
	Version           int
	PublicationDigest string

	PreviousVersion           int
	PreviousPublicationDigest string

	Approver    string
	Reason      string
	EvidenceRef string
	At          time.Time
	Rollback    bool

	eventDigest string
}

// Digest returns the content digest of this activation event.
func (e ActivationEvent) Digest() string { return e.eventDigest }

// Explain describes the activation and its lineage in one deterministic line.
func (e ActivationEvent) Explain() string {
	kind := "activation"
	if e.Rollback {
		kind = "rollback"
	}
	return fmt.Sprintf("mapping %s %s@%d after %s@%d by %s (%s)",
		kind, e.MappingID, e.Version, e.MappingID, e.PreviousVersion, e.Approver, e.eventDigest)
}

// Explain is the package-level explanation entry point for an activation
// event. It mirrors the Explain-shaped symbols used by governed engines.
func Explain(event ActivationEvent) string { return event.Explain() }

// PublicationRegistry is a kernel-pure, concurrent in-memory publication
// registry. It is a state machine suitable for tests and for a caller-owned
// adapter; it performs no database or network I/O.
type PublicationRegistry struct {
	mu sync.RWMutex

	publications     map[string]map[int]PublicationRecord
	publicationOrder map[string][]int
	superseded       map[string]map[int]bool
	active           map[string]activeVersion
	history          map[string][]ActivationEvent
	nextSequence     uint64
}

type activeVersion struct {
	version int
	digest  string
}

// NewPublicationRegistry returns an empty concurrent registry.
func NewPublicationRegistry() *PublicationRegistry {
	return &PublicationRegistry{
		publications:     make(map[string]map[int]PublicationRecord),
		publicationOrder: make(map[string][]int),
		superseded:       make(map[string]map[int]bool),
		active:           make(map[string]activeVersion),
		history:          make(map[string][]ActivationEvent),
	}
}

// NewRegistry is a concise alias for NewPublicationRegistry.
func NewRegistry() *PublicationRegistry { return NewPublicationRegistry() }

// Publish records a compiled profile only after validating its distinct
// author/approver approval, reason, and evidence reference. Repeating the
// exact same publication is idempotent; a different record at the same
// mapping/version is refused because published versions are immutable.
func (r *PublicationRegistry) Publish(version MappingProfileVersion, evidence PublicationEvidence) (PublicationRecord, error) {
	if r == nil {
		return PublicationRecord{}, fmt.Errorf("%w: nil registry", ErrInvalidPublication)
	}
	author, approver, err := publicationActors(evidence)
	if err != nil {
		return PublicationRecord{}, err
	}
	profile := version.Profile()
	if profile.MappingID == "" || version.Version() < 1 || version.Digest() == "" {
		return PublicationRecord{}, fmt.Errorf("%w: compiled profile must have mapping id, positive version, and digest", ErrInvalidPublication)
	}

	record := PublicationRecord{
		MappingID:         profile.MappingID,
		Version:           version.Version(),
		Compiled:          cloneMappingProfileVersion(version),
		Author:            author,
		Approver:          approver,
		Reason:            strings.TrimSpace(evidence.Reason),
		EvidenceRef:       strings.TrimSpace(evidence.EvidenceRef),
		PublishedAt:       canonicalPublicationTime(evidence.PublishedAt),
		EffectiveInterval: canonicalEffectiveInterval(evidence.EffectiveInterval),
	}
	if record.Reason == "" || record.EvidenceRef == "" {
		return PublicationRecord{}, fmt.Errorf("%w: reason and evidence ref are required", ErrInvalidPublication)
	}
	if err := validateEffectiveInterval(record.EffectiveInterval); err != nil {
		return PublicationRecord{}, err
	}
	record.publicationDigest = publicationDigest(record)

	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.publications[record.MappingID]
	if versions == nil {
		versions = make(map[int]PublicationRecord)
		r.publications[record.MappingID] = versions
	}
	if existing, found := versions[record.Version]; found {
		if existing.publicationDigest == record.publicationDigest {
			return clonePublicationRecord(existing), nil
		}
		return PublicationRecord{}, fmt.Errorf("%w: %s@%d already has immutable publication evidence", ErrPublicationConflict, record.MappingID, record.Version)
	}
	versions[record.Version] = clonePublicationRecord(record)
	r.publicationOrder[record.MappingID] = append(r.publicationOrder[record.MappingID], record.Version)
	return clonePublicationRecord(record), nil
}

// Publish is also available as a function to match the platform registry's
// publish-then-activate style.
func Publish(registry *PublicationRegistry, version MappingProfileVersion, evidence PublicationEvidence) (PublicationRecord, error) {
	if registry == nil {
		return PublicationRecord{}, fmt.Errorf("%w: nil registry", ErrInvalidPublication)
	}
	return registry.Publish(version, evidence)
}

func publicationActors(e PublicationEvidence) (string, string, error) {
	author := strings.TrimSpace(e.Author)
	approver := strings.TrimSpace(e.Approver)
	if author == "" {
		return "", "", fmt.Errorf("%w: author is required", ErrInvalidPublication)
	}
	if approver == "" {
		return "", "", fmt.Errorf("%w: approver is required", ErrInvalidPublication)
	}
	if author == approver {
		return "", "", fmt.Errorf("%w: author and approver must be distinct", ErrInvalidPublication)
	}
	if strings.TrimSpace(e.Reason) == "" {
		return "", "", fmt.Errorf("%w: reason is required", ErrInvalidPublication)
	}
	if strings.TrimSpace(e.EvidenceRef) == "" {
		return "", "", fmt.Errorf("%w: evidence ref is required", ErrInvalidPublication)
	}
	return author, approver, nil
}

// Activate makes a published, non-superseded version active and appends one
// digested event. The registry lock makes a concurrent activation linearizable
// and leaves exactly one current active version for each mapping id.
func (r *PublicationRegistry) Activate(mappingID string, version int, evidence ActivationEvidence) (ActivationEvent, error) {
	return r.activate(mappingID, version, evidence, false)
}

// Activate is the functional form of PublicationRegistry.Activate.
func Activate(registry *PublicationRegistry, mappingID string, version int, evidence ActivationEvidence) (ActivationEvent, error) {
	if registry == nil {
		return ActivationEvent{}, fmt.Errorf("%w: nil registry", ErrInvalidActivation)
	}
	return registry.Activate(mappingID, version, evidence)
}

func (r *PublicationRegistry) activate(mappingID string, version int, evidence ActivationEvidence, rollback bool) (ActivationEvent, error) {
	if r == nil {
		return ActivationEvent{}, fmt.Errorf("%w: nil registry", ErrInvalidActivation)
	}
	approver, at, err := normalizeActivationEvidence(evidence)
	if err != nil {
		return ActivationEvent{}, err
	}
	if strings.TrimSpace(mappingID) == "" || version < 1 {
		return ActivationEvent{}, fmt.Errorf("%w: mapping id and positive version are required", ErrInvalidActivation)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	publication, found := r.publications[mappingID][version]
	if !found {
		return ActivationEvent{}, fmt.Errorf("%w: %s@%d", ErrUnpublishedVersion, mappingID, version)
	}
	if !rollback && r.superseded[mappingID][version] {
		return ActivationEvent{}, fmt.Errorf("%w: %s@%d", ErrSupersededVersion, mappingID, version)
	}
	previous := r.active[mappingID]
	if !rollback && previous.version == version {
		return ActivationEvent{}, fmt.Errorf("%w: %s@%d", ErrAlreadyActive, mappingID, version)
	}
	if rollback && previous.version != 0 && previous.version != version && !r.wasPublishedBeforeLocked(mappingID, version, previous.version) {
		return ActivationEvent{}, fmt.Errorf("%w: rollback target %s@%d was not published before active %s@%d", ErrInvalidActivation, mappingID, version, mappingID, previous.version)
	}

	r.nextSequence++
	event := ActivationEvent{
		Sequence:                  r.nextSequence,
		MappingID:                 mappingID,
		Version:                   version,
		PublicationDigest:         publication.Digest(),
		PreviousVersion:           previous.version,
		PreviousPublicationDigest: previous.digest,
		Approver:                  approver,
		Reason:                    strings.TrimSpace(evidence.Reason),
		EvidenceRef:               strings.TrimSpace(evidence.EvidenceRef),
		At:                        at,
		Rollback:                  rollback,
	}
	event.eventDigest = activationDigest(event)
	if previous.version != 0 && previous.version != version {
		if r.superseded[mappingID] == nil {
			r.superseded[mappingID] = make(map[int]bool)
		}
		r.superseded[mappingID][previous.version] = true
	}
	if r.superseded[mappingID] != nil {
		r.superseded[mappingID][version] = false
	}
	r.active[mappingID] = activeVersion{version: version, digest: publication.Digest()}
	r.history[mappingID] = append(r.history[mappingID], event)
	return cloneActivationEvent(event), nil
}

// Rollback explicitly activates a prior published version and appends a
// rollback event with independent evidence. A superseded publication is
// allowed here by design; ordinary Activate remains fenced against it.
func (r *PublicationRegistry) Rollback(mappingID string, version int, evidence ActivationEvidence) (ActivationEvent, error) {
	return r.activate(mappingID, version, evidence, true)
}

// Rollback is the functional form of PublicationRegistry.Rollback.
func Rollback(registry *PublicationRegistry, mappingID string, version int, evidence ActivationEvidence) (ActivationEvent, error) {
	if registry == nil {
		return ActivationEvent{}, fmt.Errorf("%w: nil registry", ErrInvalidActivation)
	}
	return registry.Rollback(mappingID, version, evidence)
}

func normalizeActivationEvidence(e ActivationEvidence) (string, time.Time, error) {
	approver := strings.TrimSpace(e.Approver)
	activatedBy := strings.TrimSpace(e.ActivatedBy)
	if approver != "" && activatedBy != "" && approver != activatedBy {
		return "", time.Time{}, fmt.Errorf("%w: approver and activated-by differ", ErrInvalidActivation)
	}
	if approver == "" {
		approver = activatedBy
	}
	if approver == "" {
		return "", time.Time{}, fmt.Errorf("%w: approver is required", ErrInvalidActivation)
	}
	if strings.TrimSpace(e.Reason) == "" {
		return "", time.Time{}, fmt.Errorf("%w: reason is required", ErrInvalidActivation)
	}
	if strings.TrimSpace(e.EvidenceRef) == "" {
		return "", time.Time{}, fmt.Errorf("%w: evidence ref is required", ErrInvalidActivation)
	}
	at := e.At
	if at.IsZero() {
		at = e.ActivatedAt
	}
	return approver, canonicalPublicationTime(at), nil
}

// Active returns the one currently active publication for mappingID.
func (r *PublicationRegistry) Active(mappingID string) (PublicationRecord, bool) {
	if r == nil {
		return PublicationRecord{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	current, found := r.active[mappingID]
	if !found {
		return PublicationRecord{}, false
	}
	p, found := r.publications[mappingID][current.version]
	if !found {
		return PublicationRecord{}, false
	}
	return clonePublicationRecord(p), true
}

// Resolve is a descriptive alias for Active.
func (r *PublicationRegistry) Resolve(mappingID string) (PublicationRecord, error) {
	p, found := r.Active(mappingID)
	if !found {
		return PublicationRecord{}, fmt.Errorf("%w: %s", ErrNoActiveVersion, mappingID)
	}
	return p, nil
}

// History returns every activation in append order, including rollback events.
func (r *PublicationRegistry) History(mappingID string) []ActivationEvent {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	history := r.history[mappingID]
	out := make([]ActivationEvent, len(history))
	for i, event := range history {
		out[i] = cloneActivationEvent(event)
	}
	return out
}

// Publications returns all immutable publication records in publication
// order. Superseded is refreshed from registry state without changing the
// stored publication body or approval evidence.
func (r *PublicationRegistry) Publications(mappingID string) []PublicationRecord {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.publications[mappingID]
	order := r.publicationOrder[mappingID]
	out := make([]PublicationRecord, 0, len(order))
	for _, version := range order {
		if p, found := versions[version]; found {
			copy := clonePublicationRecord(p)
			copy.Superseded = r.superseded[mappingID][version]
			out = append(out, copy)
		}
	}
	return out
}

// IsActive reports whether version is the sole current active version.
func (r *PublicationRegistry) IsActive(mappingID string, version int) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active[mappingID].version == version
}

func (r *PublicationRegistry) wasPublishedBeforeLocked(mappingID string, target, current int) bool {
	seenTarget, seenCurrent := false, false
	for _, version := range r.publicationOrder[mappingID] {
		if version == target {
			seenTarget = true
			if seenCurrent {
				return false
			}
		}
		if version == current {
			seenCurrent = true
		}
	}
	return seenTarget && seenCurrent
}

type publicationIdentity struct {
	ProfileDigest     string
	MappingID         string
	Version           int
	Author            string
	Approver          string
	Reason            string
	EvidenceRef       string
	PublishedAt       time.Time
	EffectiveInterval EffectiveInterval
}

func publicationDigest(p PublicationRecord) string {
	identity := publicationIdentity{
		ProfileDigest: p.Compiled.Digest(), MappingID: p.MappingID, Version: p.Version,
		Author: p.Author, Approver: p.Approver, Reason: p.Reason,
		EvidenceRef: p.EvidenceRef, PublishedAt: p.PublishedAt,
		EffectiveInterval: p.EffectiveInterval,
	}
	return digestPublicationValue("hcmnext.connectivity.mapping.publication/v1", identity)
}

type activationIdentity struct {
	Sequence                  uint64
	MappingID                 string
	Version                   int
	PublicationDigest         string
	PreviousVersion           int
	PreviousPublicationDigest string
	Approver                  string
	Reason                    string
	EvidenceRef               string
	At                        time.Time
	Rollback                  bool
}

func activationDigest(e ActivationEvent) string {
	identity := activationIdentity{
		Sequence: e.Sequence, MappingID: e.MappingID, Version: e.Version,
		PublicationDigest: e.PublicationDigest, PreviousVersion: e.PreviousVersion,
		PreviousPublicationDigest: e.PreviousPublicationDigest, Approver: e.Approver,
		Reason: e.Reason, EvidenceRef: e.EvidenceRef, At: e.At, Rollback: e.Rollback,
	}
	return digestPublicationValue("hcmnext.connectivity.mapping.activation/v1", identity)
}

func digestPublicationValue(profile string, value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	_, _ = h.Write([]byte(profile))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(body)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func canonicalPublicationTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Microsecond)
}

func canonicalEffectiveInterval(interval EffectiveInterval) EffectiveInterval {
	return EffectiveInterval{
		From: canonicalPublicationTime(interval.From),
		To:   canonicalPublicationTime(interval.To),
	}
}

func validateEffectiveInterval(interval EffectiveInterval) error {
	if !interval.From.IsZero() && !interval.To.IsZero() && !interval.From.Before(interval.To) {
		return fmt.Errorf("%w: effective interval From must precede To", ErrInvalidPublication)
	}
	return nil
}

// Version returns the integer business version carried by a compiled profile.
func (v MappingProfileVersion) Version() int {
	version, _, _ := profileVersion(v.profile.Version)
	return version
}

func cloneMappingProfileVersion(v MappingProfileVersion) MappingProfileVersion {
	c := v
	c.profile = cloneProfile(v.profile)
	c.mappings = cloneFieldMappings(v.mappings)
	c.targetFields = append([]TargetField(nil), v.targetFields...)
	c.profile.Mappings = cloneFieldMappings(c.mappings)
	c.profile.FieldMappings = nil
	c.profile.Fields = nil
	c.profile.TargetFields = append([]TargetField(nil), c.targetFields...)
	return c
}

func cloneFieldMappings(in []FieldMapping) []FieldMapping {
	out := make([]FieldMapping, len(in))
	for i, field := range in {
		out[i] = field
	}
	return out
}

func clonePublicationRecord(p PublicationRecord) PublicationRecord {
	p.Compiled = cloneMappingProfileVersion(p.Compiled)
	return p
}

func cloneActivationEvent(e ActivationEvent) ActivationEvent { return e }
