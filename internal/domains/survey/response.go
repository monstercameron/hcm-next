package survey

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DisclosureMode selects the identity boundary for responses. The response
// record contains only a scoped pseudonym or a fresh response token; it never
// contains the subject reference supplied to RecordResponse.
type DisclosureMode string

const (
	DisclosurePseudonymous DisclosureMode = "PSEUDONYMOUS"
	DisclosureAnonymous    DisclosureMode = "ANONYMOUS"
)

// FormDefinitionRef pins the exact form definition accepted by a campaign.
// Digest is optional for compatibility with the retired FORM-003 shape, but
// when present it is part of the exact match.
type FormDefinitionRef struct {
	Ref     string
	Version uint32
	Digest  string
}

func (r FormDefinitionRef) Validate() error {
	if r.Ref == "" || r.Version == 0 {
		return ErrInvalidResponseCampaign
	}
	return nil
}

func (r FormDefinitionRef) String() string {
	return fmt.Sprintf("%s@%d#%s", r.Ref, r.Version, r.Digest)
}

// ResponseCampaign adds response-specific policy to an already validated
// campaign revision. It is a value object: closing a campaign is recorded by
// ResponseStore, while Closed permits callers to pass a known closed snapshot.
type ResponseCampaign struct {
	Campaign          CampaignRevision
	FormDefinition    FormDefinitionRef
	Disclosure        DisclosureMode
	AllowRevision     bool
	MinimumCohortSize int
	Closed            bool
}

// MinimumCohort returns the effective suppression threshold. The campaign's
// anonymity threshold remains the source of truth unless this response
// policy explicitly raises it.
func (c ResponseCampaign) MinimumCohort() int {
	minimum := c.Campaign.AnonymityThreshold
	if c.MinimumCohortSize > minimum {
		minimum = c.MinimumCohortSize
	}
	return minimum
}

func (c ResponseCampaign) Validate() error {
	if err := c.Campaign.Validate(); err != nil {
		return fmt.Errorf("%w: campaign: %v", ErrInvalidResponseCampaign, err)
	}
	if err := c.FormDefinition.Validate(); err != nil {
		return fmt.Errorf("%w: form definition: %v", ErrInvalidResponseCampaign, err)
	}
	if c.Disclosure != DisclosurePseudonymous && c.Disclosure != DisclosureAnonymous {
		return fmt.Errorf("%w: disclosure mode %q is not declared", ErrInvalidResponseCampaign, c.Disclosure)
	}
	if c.MinimumCohortSize < 0 || c.MinimumCohort() < 5 {
		return ErrAnonymityThresholdTooLow
	}
	return nil
}

// PseudonymPort is the narrow survey dependency on the pseudonym engine. The
// subject is an input to this port only; implementations must return a value
// scoped by the supplied campaign scope and purpose, and the survey store
// never retains the subject.
type PseudonymPort interface {
	Pseudonymize(subject, scope, purpose string) (string, error)
}

// PseudonymPortFunc adapts a function to PseudonymPort, which is useful for a
// process-local fake without importing the pseudonym engine into this domain.
type PseudonymPortFunc func(subject, scope, purpose string) (string, error)

func (f PseudonymPortFunc) Pseudonymize(subject, scope, purpose string) (string, error) {
	return f(subject, scope, purpose)
}

// FormValidator validates answers against the exact form definition pinned by
// the campaign and returns validation evidence for the response digest.
type FormValidator interface {
	Validate(form FormDefinitionRef, answers []ResponseAnswer) (string, error)
}

// FormValidatorFunc adapts a pure validation function to FormValidator.
type FormValidatorFunc func(form FormDefinitionRef, answers []ResponseAnswer) (string, error)

func (f FormValidatorFunc) Validate(form FormDefinitionRef, answers []ResponseAnswer) (string, error) {
	return f(form, answers)
}

// ResponseAnswer is the typed answer payload accepted by the form validator.
// It has no actor, subject, or delivery identity field.
type ResponseAnswer struct {
	QuestionID string
	Value      string
}

// ResponseInput is the transient submission input. SubjectRef is consumed by
// the pseudonym port for pseudonymous campaigns and is refused for anonymous
// campaigns; it is never copied into ResponseRecord.
type ResponseInput struct {
	SubjectRef     string
	FormDefinition FormDefinitionRef
	Answers        []ResponseAnswer
	SubmittedAt    values.Instant
}

// ResponseRecord is an append-only response artifact. ResponseKey is either a
// campaign-scoped pseudonym or a random anonymous token. There is deliberately
// no SubjectRef field and no campaign sample/member field.
type ResponseRecord struct {
	CampaignID       string
	CampaignRevision uint64
	FormDefinition   FormDefinitionRef
	ResponseKey      string
	ResponseRevision int
	Answers          []ResponseAnswer
	ValidationDigest string
	SubmittedAt      values.Instant
	Digest           string
}

// Aggregate is a cohort-safe read model. It contains no response key and no
// subject reference; callers receive it only after the minimum cohort check.
type Aggregate struct {
	CampaignID       string
	CampaignRevision uint64
	FormDefinition   FormDefinitionRef
	Count            int
	Breakdown        map[string]map[string]int
	Digest           string
}

var (
	ErrInvalidResponseCampaign = errors.New("survey: invalid response campaign")
	ErrInvalidResponse         = errors.New("survey: invalid response")
	ErrFormVersionMismatch     = errors.New("survey: response form definition does not match campaign")
	ErrFormValidationFailed    = errors.New("survey: response form validation failed")
	ErrPseudonymRequired       = errors.New("survey: pseudonymous response requires a subject reference and pseudonym port")
	ErrSubjectLinkageRefused   = errors.New("survey: anonymous response cannot carry a subject reference")
	ErrDuplicateResponse       = errors.New("survey: response already recorded for pseudonym")
	ErrCampaignClosed          = errors.New("survey: campaign is closed")
	ErrAggregateCohortTooSmall = errors.New("survey: aggregate cohort is below the campaign minimum")
	ErrResponseTokenFailed     = errors.New("survey: could not mint anonymous response token")
)

const responsePurpose = "survey-response"

// ResponseStore is a concurrency-safe append-only domain store. It is an
// in-memory seam for the pure response rules; durable persistence belongs to a
// later adapter and must preserve the same no-subject record shape.
type ResponseStore struct {
	mu      sync.RWMutex
	records map[string][]ResponseRecord
	closed  map[string]bool
}

func NewResponseStore() *ResponseStore {
	return &ResponseStore{
		records: make(map[string][]ResponseRecord),
		closed:  make(map[string]bool),
	}
}

// RecordResponse validates and appends one response. A second response for a
// pseudonym is refused unless revisions are enabled, in which case a new
// ResponseRevision is appended and the old record remains unchanged.
func (s *ResponseStore) RecordResponse(campaign ResponseCampaign, input ResponseInput, pseudonyms PseudonymPort, forms FormValidator) (ResponseRecord, error) {
	if s == nil {
		return ResponseRecord{}, ErrInvalidResponse
	}
	if err := campaign.Validate(); err != nil {
		return ResponseRecord{}, err
	}
	if campaign.Closed || s.isClosed(campaign) {
		return ResponseRecord{}, ErrCampaignClosed
	}
	if err := input.SubmittedAt.Validate(); err != nil {
		return ResponseRecord{}, fmt.Errorf("%w: submitted at: %v", ErrInvalidResponse, err)
	}
	if err := input.FormDefinition.Validate(); err != nil || !sameFormDefinition(input.FormDefinition, campaign.FormDefinition) {
		return ResponseRecord{}, ErrFormVersionMismatch
	}
	if forms == nil {
		return ResponseRecord{}, fmt.Errorf("%w: validator is required", ErrFormValidationFailed)
	}
	answers := cloneAnswers(input.Answers)
	validationDigest, err := forms.Validate(campaign.FormDefinition, answers)
	if err != nil {
		return ResponseRecord{}, fmt.Errorf("%w: %w", ErrFormValidationFailed, err)
	}
	if validationDigest == "" {
		return ResponseRecord{}, ErrFormValidationFailed
	}

	var key string
	switch campaign.Disclosure {
	case DisclosurePseudonymous:
		if input.SubjectRef == "" || pseudonyms == nil {
			return ResponseRecord{}, ErrPseudonymRequired
		}
		key, err = pseudonyms.Pseudonymize(input.SubjectRef, campaignScope(campaign), responsePurpose)
		if err != nil || key == "" {
			return ResponseRecord{}, fmt.Errorf("%w: %v", ErrPseudonymRequired, err)
		}
	case DisclosureAnonymous:
		if input.SubjectRef != "" {
			return ResponseRecord{}, ErrSubjectLinkageRefused
		}
		key, err = randomResponseToken()
		if err != nil {
			return ResponseRecord{}, err
		}
	default:
		return ResponseRecord{}, ErrInvalidResponseCampaign
	}

	storeKey := responseStoreKey(campaign)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed[storeKey] {
		return ResponseRecord{}, ErrCampaignClosed
	}
	revision := 1
	if campaign.Disclosure == DisclosurePseudonymous {
		for _, prior := range s.records[storeKey] {
			if prior.ResponseKey != key {
				continue
			}
			if !campaign.AllowRevision {
				return ResponseRecord{}, ErrDuplicateResponse
			}
			if prior.ResponseRevision >= revision {
				revision = prior.ResponseRevision + 1
			}
		}
	}
	record := ResponseRecord{
		CampaignID:       campaign.Campaign.CampaignID,
		CampaignRevision: campaign.Campaign.Revision,
		FormDefinition:   campaign.FormDefinition,
		ResponseKey:      key,
		ResponseRevision: revision,
		Answers:          answers,
		ValidationDigest: validationDigest,
		SubmittedAt:      input.SubmittedAt,
	}
	record.Digest = responseRecordDigest(record)
	if record.Digest == "" {
		return ResponseRecord{}, ErrInvalidResponse
	}
	s.records[storeKey] = append(s.records[storeKey], cloneResponseRecord(record))
	return cloneResponseRecord(record), nil
}

// CloseCampaign makes subsequent writes refuse for the campaign revision.
func (s *ResponseStore) CloseCampaign(campaign ResponseCampaign) error {
	if s == nil {
		return ErrInvalidResponse
	}
	if err := campaign.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.closed[responseStoreKey(campaign)] = true
	s.mu.Unlock()
	return nil
}

// Records returns a defensive copy of the append-only response history. It is
// intended for evidence and tests; aggregate consumers should use Aggregate.
func (s *ResponseStore) Records(campaign ResponseCampaign) []ResponseRecord {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	prior := s.records[responseStoreKey(campaign)]
	result := make([]ResponseRecord, len(prior))
	for i, record := range prior {
		result[i] = cloneResponseRecord(record)
	}
	return result
}

// Aggregate returns a breakdown only when the response count reaches the
// campaign's effective minimum cohort. No response key is copied into it.
func (s *ResponseStore) Aggregate(campaign ResponseCampaign) (Aggregate, error) {
	if s == nil {
		return Aggregate{}, ErrInvalidResponse
	}
	if err := campaign.Validate(); err != nil {
		return Aggregate{}, err
	}
	s.mu.RLock()
	prior := s.records[responseStoreKey(campaign)]
	cohort := aggregateCohort(campaign, prior)
	if len(cohort) < campaign.MinimumCohort() {
		s.mu.RUnlock()
		return Aggregate{}, ErrAggregateCohortTooSmall
	}
	breakdown := make(map[string]map[string]int)
	for _, record := range cohort {
		for _, answer := range record.Answers {
			if breakdown[answer.QuestionID] == nil {
				breakdown[answer.QuestionID] = make(map[string]int)
			}
			breakdown[answer.QuestionID][answer.Value]++
		}
	}
	s.mu.RUnlock()
	result := Aggregate{
		CampaignID:       campaign.Campaign.CampaignID,
		CampaignRevision: campaign.Campaign.Revision,
		FormDefinition:   campaign.FormDefinition,
		Count:            len(cohort),
		Breakdown:        breakdown,
	}
	result.Digest = aggregateDigest(result)
	return result, nil
}

// ExplainResponses is the package-level response contract. It is intentionally
// value-free so diagnostics cannot become an identity side channel.
func ExplainResponses() string {
	return "Survey responses bind an exact form version, use campaign-scoped pseudonyms or fresh anonymous tokens, append revisions without overwrite, refuse closed campaigns, and suppress aggregates below the minimum cohort."
}

// Explain returns a value-free response-record explanation.
func (r ResponseRecord) Explain() string { return ExplainResponses() }

// Verify checks the record's immutable digest without consulting the store.
func (r ResponseRecord) Verify() error {
	if r.CampaignID == "" || r.CampaignRevision == 0 || r.ResponseKey == "" || r.ResponseRevision < 1 || r.ValidationDigest == "" {
		return ErrInvalidResponse
	}
	if err := r.FormDefinition.Validate(); err != nil {
		return ErrInvalidResponse
	}
	if err := r.SubmittedAt.Validate(); err != nil {
		return ErrInvalidResponse
	}
	if r.Digest == "" || responseRecordDigest(r) != r.Digest {
		return ErrInvalidResponse
	}
	return nil
}

// Explain returns a value-free aggregate explanation.
func (a Aggregate) Explain() string { return ExplainResponses() }

func sameFormDefinition(left, right FormDefinitionRef) bool {
	return left.Ref == right.Ref && left.Version == right.Version && left.Digest == right.Digest
}

func aggregateCohort(campaign ResponseCampaign, records []ResponseRecord) []ResponseRecord {
	if campaign.Disclosure == DisclosureAnonymous {
		return records
	}
	latest := make(map[string]ResponseRecord, len(records))
	for _, record := range records {
		prior, ok := latest[record.ResponseKey]
		if !ok || record.ResponseRevision > prior.ResponseRevision {
			latest[record.ResponseKey] = record
		}
	}
	cohort := make([]ResponseRecord, 0, len(latest))
	for _, record := range records {
		if latest[record.ResponseKey].Digest == record.Digest {
			cohort = append(cohort, record)
		}
	}
	return cohort
}

func campaignScope(campaign ResponseCampaign) string {
	return fmt.Sprintf("survey-campaign:%s:%d", campaign.Campaign.CampaignID, campaign.Campaign.Revision)
}

func responseStoreKey(campaign ResponseCampaign) string {
	return fmt.Sprintf("%s\x00%d", campaign.Campaign.CampaignID, campaign.Campaign.Revision)
}

func (s *ResponseStore) isClosed(campaign ResponseCampaign) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed[responseStoreKey(campaign)]
}

func randomResponseToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("%w: %v", ErrResponseTokenFailed, err)
	}
	return "response:v1:" + hex.EncodeToString(raw), nil
}

func cloneAnswers(answers []ResponseAnswer) []ResponseAnswer {
	return append([]ResponseAnswer(nil), answers...)
}

func cloneResponseRecord(record ResponseRecord) ResponseRecord {
	record.Answers = cloneAnswers(record.Answers)
	return record
}

func responseRecordDigest(record ResponseRecord) string {
	w := canonicalbytes.New("hcmnext.domains.survey.ResponseRecord", 1).
		String("campaign_id", record.CampaignID).
		Int("campaign_revision", int64(record.CampaignRevision)).
		String("form_definition", record.FormDefinition.String()).
		String("response_key", record.ResponseKey).
		Int("response_revision", int64(record.ResponseRevision)).
		String("validation_digest", record.ValidationDigest).
		Value("submitted_at", record.SubmittedAt).
		Count("answers", len(record.Answers))
	for i, answer := range record.Answers {
		w.String(fmt.Sprintf("answer_%d_question", i), answer.QuestionID)
		w.String(fmt.Sprintf("answer_%d_value", i), answer.Value)
	}
	digest, _ := w.Digest()
	return digest
}

func aggregateDigest(aggregate Aggregate) string {
	w := canonicalbytes.New("hcmnext.domains.survey.Aggregate", 1).
		String("campaign_id", aggregate.CampaignID).
		Int("campaign_revision", int64(aggregate.CampaignRevision)).
		String("form_definition", aggregate.FormDefinition.String()).
		Int("count", int64(aggregate.Count))
	for _, questionID := range sortedKeys(aggregate.Breakdown) {
		w.String("question_id", questionID)
		for _, value := range sortedKeys(aggregate.Breakdown[questionID]) {
			w.String("value", value).Int("occurrences", int64(aggregate.Breakdown[questionID][value]))
		}
	}
	digest, _ := w.Digest()
	return digest
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	// The canonical bytes profile sorts map-like keys at the caller boundary;
	// use a small insertion sort here to keep this package dependency-free.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
