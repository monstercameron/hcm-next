package contact

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const digestRevisionSchemaVersion = 1

// Version reports the digest-backed contact contract version.
func Version() int { return digestRevisionSchemaVersion }

var (
	ErrInvalidContactRevision  = errors.New("contact: invalid digest-backed endpoint revision")
	ErrContactLineage          = errors.New("contact: endpoint revision lineage is invalid")
	ErrChallengeBudget         = errors.New("contact: challenge attempt budget is exhausted")
	ErrChallengeNotFound       = errors.New("contact: challenge not found")
	ErrNoVerifiedEndpoint      = errors.New("contact: no verified endpoint is eligible as primary")
	ErrAmbiguousPrimary        = errors.New("contact: primary endpoint selection is ambiguous")
	ErrInvalidReissue          = errors.New("contact: invalid challenge reissue")
	ErrExternalContactMismatch = errors.New("contact: external contact observation disagrees")
	ErrContactRepairRequired   = errors.New("contact: bounded contact repair required")
)

func digestString(value string) bool {
	return strings.HasPrefix(value, canonicalbytes.DigestAlgorithm+":") && len(value) > len(canonicalbytes.DigestAlgorithm)+1
}

// ContactEndpointRevision is the privacy-preserving endpoint representation.
// The normalized value is used only while constructing NormalizedValueDigest;
// this record stores the digest and a presentation hint, never the value.
type ContactEndpointRevision struct {
	Subject               values.EntityRef
	EndpointID            string
	Revision              uint64
	SupersedesRevision    uint64
	Kind                  EndpointType
	Purpose               string
	Priority              int
	Source                string
	NormalizedValueDigest string
	DisplayHint           string
	Verification          VerificationState
	CanonicalDigest       string
}

// NormalizedEndpointRevision is a descriptive alias for callers that want the
// normalization boundary visible in their type name.
type NormalizedEndpointRevision = ContactEndpointRevision

// EndpointRevisionV2 is an additive name for the digest-backed revision.
type EndpointRevisionV2 = ContactEndpointRevision

func NewContactEndpointRevision(subject values.EntityRef, endpointID string, kind EndpointType, raw, purpose string, priority int, source string) (ContactEndpointRevision, error) {
	normalized, hint, err := normalizeContactValue(kind, raw)
	if err != nil {
		return ContactEndpointRevision{}, err
	}
	r := ContactEndpointRevision{Subject: subject, EndpointID: endpointID, Revision: 1, Kind: kind, Purpose: purpose, Priority: priority, Source: source, NormalizedValueDigest: digestNormalizedContactValue(kind, normalized), DisplayHint: hint, Verification: Unverified}
	r.CanonicalDigest = r.computedDigest()
	return r, r.Validate()
}

// NewNormalizedEndpointRevision is an alias for NewContactEndpointRevision.
func NewNormalizedEndpointRevision(subject values.EntityRef, endpointID string, kind EndpointType, raw, purpose string, priority int, source string) (ContactEndpointRevision, error) {
	return NewContactEndpointRevision(subject, endpointID, kind, raw, purpose, priority, source)
}

func digestNormalizedContactValue(kind EndpointType, normalized string) string {
	w := canonicalbytes.New("hcmnext.domains.contact.NormalizedEndpointValue", digestRevisionSchemaVersion).
		String("kind", string(kind)).String("normalized", normalized)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func normalizeContactValue(kind EndpointType, raw string) (string, string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", "", fmt.Errorf("%w: raw value has ambiguous surrounding whitespace", ErrAmbiguousNormalization)
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII {
			return "", "", fmt.Errorf("%w: value contains whitespace, control, or non-ASCII text", ErrAmbiguousNormalization)
		}
	}
	switch kind {
	case EndpointEmail:
		if strings.Count(raw, "@") != 1 {
			return "", "", fmt.Errorf("%w: email must have one at-sign", ErrInvalidEndpoint)
		}
		parts := strings.SplitN(raw, "@", 2)
		if parts[0] == "" || parts[1] == "" || !strings.Contains(parts[1], ".") {
			return "", "", fmt.Errorf("%w: email syntax", ErrInvalidEndpoint)
		}
		normalized := strings.ToLower(raw)
		return normalized, maskEndpoint(normalized, EndpointEmail), nil
	case EndpointPhone:
		if !strings.HasPrefix(raw, "+") {
			return "", "", fmt.Errorf("%w: phone must use international form", ErrInvalidEndpoint)
		}
		for _, r := range raw[1:] {
			if r < '0' || r > '9' {
				return "", "", fmt.Errorf("%w: phone must contain only international digits", ErrInvalidEndpoint)
			}
		}
		digits := raw[1:]
		if len(digits) < 7 || len(digits) > 15 || digits[0] == '0' {
			return "", "", fmt.Errorf("%w: phone digit count", ErrInvalidEndpoint)
		}
		return "+" + digits, maskEndpoint("+"+digits, EndpointPhone), nil
	default:
		return "", "", fmt.Errorf("%w: unsupported endpoint kind %q", ErrInvalidEndpoint, kind)
	}
}

func maskEndpoint(normalized string, kind EndpointType) string {
	if kind == EndpointEmail {
		at := strings.LastIndexByte(normalized, '@')
		local := normalized[:at]
		if len(local) <= 2 {
			return strings.Repeat("*", len(local)) + normalized[at:]
		}
		return local[:1] + strings.Repeat("*", len(local)-2) + local[len(local)-1:] + normalized[at:]
	}
	return "+" + strings.Repeat("*", len(normalized)-4) + normalized[len(normalized)-4:]
}

func (r ContactEndpointRevision) Validate() error {
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidContactRevision, err)
	}
	if strings.TrimSpace(r.EndpointID) == "" {
		return fmt.Errorf("%w: endpoint id is required", ErrInvalidContactRevision)
	}
	if r.Revision == 0 || r.SupersedesRevision >= r.Revision && r.SupersedesRevision != 0 {
		return fmt.Errorf("%w: revision lineage is invalid", ErrInvalidContactRevision)
	}
	if r.Kind != EndpointEmail && r.Kind != EndpointPhone {
		return fmt.Errorf("%w: kind is not declared", ErrInvalidContactRevision)
	}
	if strings.TrimSpace(r.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrInvalidContactRevision)
	}
	if r.Priority < 0 {
		return fmt.Errorf("%w: priority cannot be negative", ErrInvalidContactRevision)
	}
	if strings.TrimSpace(r.Source) == "" {
		return fmt.Errorf("%w: source is required", ErrInvalidContactRevision)
	}
	if !digestString(r.NormalizedValueDigest) {
		return fmt.Errorf("%w: normalized value digest is required", ErrInvalidContactRevision)
	}
	if strings.TrimSpace(r.DisplayHint) == "" {
		return fmt.Errorf("%w: display hint is required", ErrInvalidContactRevision)
	}
	if r.Verification != Unverified && r.Verification != Verified {
		return fmt.Errorf("%w: verification state is not declared", ErrInvalidContactRevision)
	}
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidContactRevision)
	}
	return nil
}

func (r ContactEndpointRevision) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.contact.ContactEndpointRevision", digestRevisionSchemaVersion).
		Value("subject", r.Subject).String("endpoint_id", r.EndpointID).Int("revision", int64(r.Revision)).Int("supersedes_revision", int64(r.SupersedesRevision)).
		String("kind", string(r.Kind)).String("purpose", r.Purpose).Int("priority", int64(r.Priority)).String("source", r.Source).
		String("normalized_value_digest", r.NormalizedValueDigest).String("display_hint", r.DisplayHint).String("verification", string(r.Verification)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r ContactEndpointRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r ContactEndpointRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Successor returns an immutable child revision and refuses an identity or
// lineage change. Verification is a normal successor, never an in-place edit.
func (r ContactEndpointRevision) Successor(next ContactEndpointRevision) (ContactEndpointRevision, error) {
	if err := r.Validate(); err != nil {
		return ContactEndpointRevision{}, err
	}
	if next.Subject != r.Subject || next.EndpointID != r.EndpointID || next.Revision != r.Revision+1 || next.SupersedesRevision != r.Revision {
		return ContactEndpointRevision{}, ErrContactLineage
	}
	if next.Kind != r.Kind || next.Purpose != r.Purpose {
		return ContactEndpointRevision{}, fmt.Errorf("%w: endpoint kind and purpose are immutable", ErrContactLineage)
	}
	next.CanonicalDigest = next.computedDigest()
	return next, next.Validate()
}

func (r ContactEndpointRevision) MarkVerified() (ContactEndpointRevision, error) {
	next := r
	next.Verification = Verified
	next.Revision++
	next.SupersedesRevision = r.Revision
	return r.Successor(next)
}

// CorrectEndpointRevision creates a successor without laundering verification
// evidence. A correction may retain existing evidence when the protected
// endpoint value is unchanged, or deliberately downgrade it when the value is
// changed; it can never turn an unverified value into a verified one.
func CorrectEndpointRevision(current, replacement ContactEndpointRevision) (ContactEndpointRevision, error) {
	if err := current.Validate(); err != nil {
		return ContactEndpointRevision{}, err
	}
	if replacement.Verification == Verified && current.Verification != Verified {
		return ContactEndpointRevision{}, fmt.Errorf("%w: correction cannot add verification", ErrContactLineage)
	}
	if replacement.NormalizedValueDigest != current.NormalizedValueDigest && replacement.Verification == Verified {
		return ContactEndpointRevision{}, fmt.Errorf("%w: corrected endpoint value requires fresh verification", ErrContactLineage)
	}
	return current.Successor(replacement)
}

// SelectPrimaryEndpoint chooses the single highest-priority verified endpoint.
// It refuses ties and never promotes an unverified revision.
func SelectPrimaryEndpoint(revisions []ContactEndpointRevision) (ContactEndpointRevision, error) {
	return selectPrimaryEndpoint(revisions, "")
}

// SelectPrimaryEndpointForPurpose chooses a primary only from the requested
// purpose. The latest revision for each endpoint is authoritative, so an old
// verified revision cannot compete with its successor. Lower priority values
// win; equal priorities across distinct endpoints remain ambiguous.
func SelectPrimaryEndpointForPurpose(revisions []ContactEndpointRevision, purpose string) (ContactEndpointRevision, error) {
	if strings.TrimSpace(purpose) == "" {
		return ContactEndpointRevision{}, fmt.Errorf("%w: purpose is required", ErrInvalidPurpose)
	}
	return selectPrimaryEndpoint(revisions, purpose)
}

func selectPrimaryEndpoint(revisions []ContactEndpointRevision, purpose string) (ContactEndpointRevision, error) {
	if len(revisions) == 0 {
		return ContactEndpointRevision{}, ErrNoVerifiedEndpoint
	}
	latest := make(map[string]ContactEndpointRevision)
	var subject values.EntityRef
	for _, revision := range revisions {
		if err := revision.Validate(); err != nil {
			return ContactEndpointRevision{}, err
		}
		if purpose != "" && revision.Purpose != purpose {
			continue
		}
		if subject == (values.EntityRef{}) {
			subject = revision.Subject
		} else if revision.Subject != subject {
			return ContactEndpointRevision{}, fmt.Errorf("%w: primary candidates cross subject or tenant scope", ErrContactLineage)
		}
		prior, ok := latest[revision.EndpointID]
		if ok && revision.Revision == prior.Revision && revision.CanonicalDigest != prior.CanonicalDigest {
			return ContactEndpointRevision{}, fmt.Errorf("%w: conflicting endpoint revisions", ErrContactLineage)
		}
		if !ok || revision.Revision > prior.Revision {
			latest[revision.EndpointID] = revision
		}
	}
	var selected ContactEndpointRevision
	ambiguous := false
	for _, revision := range latest {
		if revision.Verification != Verified {
			continue
		}
		if selected.EndpointID == "" || revision.Priority < selected.Priority {
			selected = revision
			ambiguous = false
			continue
		}
		if revision.Priority == selected.Priority {
			ambiguous = true
		}
	}
	if selected.EndpointID == "" {
		return ContactEndpointRevision{}, ErrNoVerifiedEndpoint
	}
	if ambiguous {
		return ContactEndpointRevision{}, ErrAmbiguousPrimary
	}
	return selected, nil
}

// ContactChallengeStatus is the terminal state of a digest-backed challenge.
type ContactChallengeStatus string

const (
	ContactChallengeIssued    ContactChallengeStatus = "ISSUED"
	ContactChallengeVerified  ContactChallengeStatus = "VERIFIED"
	ContactChallengeExpired   ContactChallengeStatus = "EXPIRED"
	ContactChallengeExhausted ContactChallengeStatus = "EXHAUSTED"
	ContactChallengeRevoked   ContactChallengeStatus = "REVOKED"
)

func (s ContactChallengeStatus) Valid() bool {
	switch s {
	case ContactChallengeIssued, ContactChallengeVerified, ContactChallengeExpired, ContactChallengeExhausted:
		return true
	case ContactChallengeRevoked:
		return true
	default:
		return false
	}
}

type ContactChallengeEventKind string

const (
	ChallengeEventIssued    ContactChallengeEventKind = "ISSUED"
	ChallengeEventAnswered  ContactChallengeEventKind = "ANSWERED"
	ChallengeEventVerified  ContactChallengeEventKind = "VERIFIED"
	ChallengeEventExpired   ContactChallengeEventKind = "EXPIRED"
	ChallengeEventExhausted ContactChallengeEventKind = "EXHAUSTED"
	ChallengeEventRevoked   ContactChallengeEventKind = "REVOKED"
)

// ContactChallengeEvent is a digested audit event. AnswerDigest is a
// scope-bound digest of the transient answer; the answer itself is not stored.
type ContactChallengeEvent struct {
	Kind         ContactChallengeEventKind
	At           time.Time
	Attempt      int
	AnswerDigest string
	Digest       string
}

func (e ContactChallengeEvent) Validate() error {
	if e.Kind != ChallengeEventIssued && e.Kind != ChallengeEventAnswered && e.Kind != ChallengeEventVerified && e.Kind != ChallengeEventExpired && e.Kind != ChallengeEventExhausted && e.Kind != ChallengeEventRevoked {
		return errors.New("contact: challenge event kind is not declared")
	}
	if e.At.IsZero() || e.Attempt < 0 {
		return errors.New("contact: challenge event time and attempt are required")
	}
	if e.Kind == ChallengeEventAnswered && !digestString(e.AnswerDigest) {
		return errors.New("contact: answered event requires answer digest")
	}
	if !digestString(e.Digest) || e.Digest != e.computedDigest() {
		return errors.New("contact: challenge event digest mismatch")
	}
	return nil
}

func (e ContactChallengeEvent) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.contact.ContactChallengeEvent", digestRevisionSchemaVersion).
		String("kind", string(e.Kind)).String("at", e.At.UTC().Format(time.RFC3339Nano)).Int("attempt", int64(e.Attempt)).String("answer_digest", e.AnswerDigest).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e ContactChallengeEvent) computedDigest() string { return canonicalbytes.Digest(e.body()) }

// ContactVerificationChallenge contains only the scope-bound token digest and
// digested events. It is an immutable-by-value challenge stream.
type ContactVerificationChallenge struct {
	ChallengeID            string
	Subject                values.EntityRef
	EndpointID             string
	EndpointRevisionDigest string
	NormalizedValueDigest  string
	Purpose                string
	IssuedAt               time.Time
	ExpiresAt              time.Time
	AttemptBudget          int
	Attempts               int
	TokenDigest            string
	Status                 ContactChallengeStatus
	Events                 []ContactChallengeEvent
	CanonicalDigest        string
}

func IssueContactChallenge(challengeID string, subject values.EntityRef, endpoint ContactEndpointRevision, purpose, token string, now time.Time, ttl time.Duration, attemptBudget int) (ContactVerificationChallenge, string, error) {
	if err := endpoint.Validate(); err != nil {
		return ContactVerificationChallenge{}, "", err
	}
	if token == "" {
		return ContactVerificationChallenge{}, "", fmt.Errorf("%w: token is required", ErrChallengeTokenRequired)
	}
	if subject != endpoint.Subject || purpose != endpoint.Purpose {
		return ContactVerificationChallenge{}, "", fmt.Errorf("%w: challenge subject and purpose must match endpoint", ErrInvalidContactRevision)
	}
	c := ContactVerificationChallenge{ChallengeID: challengeID, Subject: subject, EndpointID: endpoint.EndpointID, EndpointRevisionDigest: endpoint.CanonicalDigest, NormalizedValueDigest: endpoint.NormalizedValueDigest, Purpose: purpose, IssuedAt: now.UTC(), ExpiresAt: now.UTC().Add(ttl), AttemptBudget: attemptBudget, TokenDigest: challengeTokenDigest(subject, endpoint.CanonicalDigest, purpose, token), Status: ContactChallengeIssued}
	c.Events = []ContactChallengeEvent{{Kind: ChallengeEventIssued, At: now.UTC(), Attempt: 0}}
	c.Events[0].Digest = c.Events[0].computedDigest()
	c.CanonicalDigest = c.computedDigest()
	if err := c.Validate(); err != nil {
		return ContactVerificationChallenge{}, "", err
	}
	return c, token, nil
}

func NewContactVerificationChallenge(challengeID string, subject values.EntityRef, endpoint ContactEndpointRevision, purpose, token string, now time.Time, ttl time.Duration, attemptBudget int) (ContactVerificationChallenge, error) {
	c, _, err := IssueContactChallenge(challengeID, subject, endpoint, purpose, token, now, ttl, attemptBudget)
	return c, err
}

func challengeTokenDigest(subject values.EntityRef, endpointRevisionDigest, purpose, token string) string {
	w := canonicalbytes.New("hcmnext.domains.contact.ContactChallengeToken", digestRevisionSchemaVersion).
		Value("subject", subject).String("endpoint_revision_digest", endpointRevisionDigest).String("purpose", purpose).String("token", token)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func (c ContactVerificationChallenge) Validate() error {
	if strings.TrimSpace(c.ChallengeID) == "" {
		return fmt.Errorf("%w: challenge id is required", ErrInvalidContactRevision)
	}
	if err := c.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidContactRevision, err)
	}
	if strings.TrimSpace(c.EndpointID) == "" || !digestString(c.EndpointRevisionDigest) || !digestString(c.NormalizedValueDigest) || strings.TrimSpace(c.Purpose) == "" {
		return fmt.Errorf("%w: endpoint, revision digest, and purpose are required", ErrInvalidContactRevision)
	}
	if c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() || !c.IssuedAt.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: challenge expiry must follow issue time", ErrInvalidContactRevision)
	}
	if c.AttemptBudget <= 0 || c.Attempts < 0 || c.Attempts > c.AttemptBudget {
		return fmt.Errorf("%w: challenge attempt budget is invalid", ErrInvalidContactRevision)
	}
	if !digestString(c.TokenDigest) || !c.Status.Valid() || len(c.Events) == 0 {
		return fmt.Errorf("%w: challenge digest, status, and issued event are required", ErrInvalidContactRevision)
	}
	for _, event := range c.Events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("%w: event: %v", ErrInvalidContactRevision, err)
		}
	}
	if c.CanonicalDigest == "" || c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: challenge canonical digest mismatch", ErrInvalidContactRevision)
	}
	return nil
}

func (c ContactVerificationChallenge) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.contact.ContactVerificationChallenge", digestRevisionSchemaVersion).
		String("challenge_id", c.ChallengeID).Value("subject", c.Subject).String("endpoint_id", c.EndpointID).
		String("endpoint_revision_digest", c.EndpointRevisionDigest).String("normalized_value_digest", c.NormalizedValueDigest).String("purpose", c.Purpose).
		String("issued_at", c.IssuedAt.UTC().Format(time.RFC3339Nano)).String("expires_at", c.ExpiresAt.UTC().Format(time.RFC3339Nano)).
		Int("attempt_budget", int64(c.AttemptBudget)).Int("attempts", int64(c.Attempts)).String("token_digest", c.TokenDigest).String("status", string(c.Status)).Count("events", len(c.Events))
	for _, event := range c.Events {
		w.String("event", event.Digest)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (c ContactVerificationChallenge) computedDigest() string { return canonicalbytes.Digest(c.body()) }
func (c ContactVerificationChallenge) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	return c.body()
}

func (c ContactVerificationChallenge) appendEvent(kind ContactChallengeEventKind, now time.Time, attempt int, answerDigest string) ContactVerificationChallenge {
	next := c
	next.Events = append([]ContactChallengeEvent(nil), c.Events...)
	event := ContactChallengeEvent{Kind: kind, At: now.UTC(), Attempt: attempt, AnswerDigest: answerDigest}
	event.Digest = event.computedDigest()
	next.Events = append(next.Events, event)
	next.CanonicalDigest = next.computedDigest()
	return next
}

// Respond records one answer and returns the next immutable challenge state.
// Wrong answers are retained as digests and consume the bounded budget.
func (c ContactVerificationChallenge) Respond(token string, now time.Time) (ContactVerificationChallenge, ContactChallengeStatus, error) {
	if err := c.Validate(); err != nil {
		return ContactVerificationChallenge{}, "", err
	}
	if c.Status != ContactChallengeIssued {
		return c, c.Status, nil
	}
	if now.IsZero() || !now.Before(c.ExpiresAt) {
		next := c.appendEvent(ChallengeEventExpired, now, c.Attempts, "")
		next.Status = ContactChallengeExpired
		next.CanonicalDigest = next.computedDigest()
		return next, next.Status, next.Validate()
	}
	if token == "" {
		return c, c.Status, fmt.Errorf("%w: token is required", ErrChallengeTokenRequired)
	}
	nextAttempts := c.Attempts + 1
	answerDigest := challengeTokenDigest(c.Subject, c.EndpointRevisionDigest, c.Purpose, token)
	next := c.appendEvent(ChallengeEventAnswered, now, nextAttempts, answerDigest)
	next.Attempts = nextAttempts
	if answerDigest == c.TokenDigest {
		next.Status = ContactChallengeVerified
		next = next.appendEvent(ChallengeEventVerified, now, nextAttempts, "")
	} else if nextAttempts >= c.AttemptBudget {
		next.Status = ContactChallengeExhausted
		next = next.appendEvent(ChallengeEventExhausted, now, nextAttempts, "")
	}
	next.CanonicalDigest = next.computedDigest()
	return next, next.Status, next.Validate()
}

func (c ContactVerificationChallenge) Answer(token string, now time.Time) (ContactVerificationChallenge, error) {
	next, _, err := c.Respond(token, now)
	return next, err
}

type ChallengeStore interface {
	Put(ContactVerificationChallenge) error
	Get(string) (ContactVerificationChallenge, bool)
	Reissue(string, string, ContactVerificationChallenge, time.Time) (ContactVerificationChallenge, ContactVerificationChallenge, error)
}

type InMemoryChallengeStore struct {
	mu         sync.RWMutex
	challenges map[string]ContactVerificationChallenge
	reissues   map[string]challengeReissueReceipt
}

type challengeReissueReceipt struct {
	expectedDigest    string
	replacementDigest string
	revokedDigest     string
	at                time.Time
}

func NewInMemoryChallengeStore() *InMemoryChallengeStore {
	return &InMemoryChallengeStore{challenges: make(map[string]ContactVerificationChallenge), reissues: make(map[string]challengeReissueReceipt)}
}

func (s *InMemoryChallengeStore) Put(challenge ContactVerificationChallenge) error {
	if s == nil {
		return ErrInvalidContactRevision
	}
	if err := challenge.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.challenges[challenge.ChallengeID]; ok && prior.CanonicalDigest != challenge.CanonicalDigest {
		return ErrContactLineage
	}
	s.challenges[challenge.ChallengeID] = cloneChallenge(challenge)
	return nil
}

// Reissue atomically revokes the stored challenge identified by previousID and
// inserts replacement. expectedDigest prevents a stale caller from revoking a
// newer state. A retry of the already-committed operation returns the original
// pair without appending another revocation or creating another challenge.
func (s *InMemoryChallengeStore) Reissue(previousID, expectedDigest string, replacement ContactVerificationChallenge, now time.Time) (ContactVerificationChallenge, ContactVerificationChallenge, error) {
	if s == nil || strings.TrimSpace(previousID) == "" || !digestString(expectedDigest) {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, ErrInvalidReissue
	}
	if err := replacement.Validate(); err != nil {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.challenges[previousID]
	if !ok {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, ErrChallengeNotFound
	}
	if previous.CanonicalDigest != expectedDigest {
		receipt, replay := s.reissues[previousID]
		stored, exists := s.challenges[replacement.ChallengeID]
		if replay && exists && receipt.expectedDigest == expectedDigest && receipt.replacementDigest == replacement.CanonicalDigest && receipt.revokedDigest == previous.CanonicalDigest && receipt.at.Equal(now) && stored.CanonicalDigest == replacement.CanonicalDigest {
			return cloneChallenge(previous), cloneChallenge(stored), nil
		}
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, ErrContactLineage
	}
	if replacement.ChallengeID == previousID {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: replacement id must be new", ErrInvalidReissue)
	}
	if _, exists := s.challenges[replacement.ChallengeID]; exists {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: replacement id already exists", ErrInvalidReissue)
	}
	revoked, issued, err := ReissueContactChallenge(previous, replacement, now)
	if err != nil {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, err
	}
	// Both preconditions are established before either map entry changes while
	// the same lock protects the two-key commit.
	s.challenges[previousID] = cloneChallenge(revoked)
	s.challenges[replacement.ChallengeID] = cloneChallenge(issued)
	s.reissues[previousID] = challengeReissueReceipt{expectedDigest: expectedDigest, replacementDigest: issued.CanonicalDigest, revokedDigest: revoked.CanonicalDigest, at: now}
	return cloneChallenge(revoked), cloneChallenge(issued), nil
}

func (s *InMemoryChallengeStore) Get(id string) (ContactVerificationChallenge, bool) {
	if s == nil {
		return ContactVerificationChallenge{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.challenges[id]
	return cloneChallenge(c), ok
}

// SHA256Digest is a small helper for callers that already hold a protected
// normalized value. It does not place the value in any contact record.
func SHA256Digest(value []byte) string {
	sum := sha256.Sum256(value)
	return canonicalbytes.Digest(sum[:])
}
