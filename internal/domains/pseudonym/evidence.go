package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// RevelationState is explicit on every evidence view. A missing disclosure
// event is therefore represented as not_revealed rather than as an omitted
// or ambiguous value.
type RevelationState string

const (
	RevelationNotRevealed RevelationState = "not_revealed"
	RevelationRevealed    RevelationState = "revealed"
)

var (
	ErrEvidenceInvalid    = errors.New("pseudonym: invalid confidential evidence")
	ErrEvidenceNotFound   = errors.New("pseudonym: confidential evidence not found")
	ErrEvidenceAppendOnly = errors.New("pseudonym: confidential evidence is append-only")
)

// EvidenceInput contains a digest and custody reference rather than ordinary
// actor identity or raw confidential content.
type EvidenceInput struct {
	Pseudonym     Pseudonym
	ContentDigest string
	IntakeAt      time.Time
	CustodyRef    string
	Revelation    RevelationState
}

// EvidenceCorrectionRequest appends a successor to one evidence record. The
// corrected record remains intact and is named by CorrectionOf.
type EvidenceCorrectionRequest struct {
	RecordID          string
	CorrectedRecordID string
	Reason            string
	ContentDigest     string
	CustodyRef        string
	IntakeAt          time.Time
}

// EvidenceRecord is an append-only confidential evidence fact keyed by the
// pseudonym generation. It contains no subject identity and no raw content.
type EvidenceRecord struct {
	ID               string          `json:"id"`
	PseudonymID      string          `json:"pseudonym_id"`
	Tenant           string          `json:"tenant"`
	Scope            string          `json:"scope"`
	Generation       int             `json:"generation"`
	ContentDigest    string          `json:"content_digest"`
	IntakeAt         time.Time       `json:"intake_at"`
	RecordedAt       time.Time       `json:"recorded_at"`
	CustodyRef       string          `json:"custody_ref"`
	Revelation       RevelationState `json:"revelation"`
	CorrectionOf     string          `json:"correction_of,omitempty"`
	Corrects         string          `json:"corrects,omitempty"`
	CorrectionReason string          `json:"correction_reason,omitempty"`
	Digest           string          `json:"digest"`
}

// ConfidentialEvidenceRecord is a semantic alias for callers that want the
// confidentiality boundary visible in their type names.
type ConfidentialEvidenceRecord = EvidenceRecord

// RevelationEvent is appended separately from an evidence fact. Recording a
// revelation never rewrites the evidence record under its pseudonym.
type RevelationEvent struct {
	ID                    string          `json:"id"`
	EvidenceID            string          `json:"evidence_id"`
	RevelationEvidenceRef string          `json:"revelation_evidence_ref"`
	State                 RevelationState `json:"state"`
	At                    time.Time       `json:"at"`
	Until                 time.Time       `json:"until"`
	Digest                string          `json:"digest"`
}

type evidenceKey struct {
	tenant     string
	scope      string
	generation int
}

// ConfidentialEvidenceStore is an append-only in-memory evidence boundary.
// The maps index records, but all externally visible writes append a new
// record; no update or delete operation exists for an evidence fact.
type ConfidentialEvidenceStore struct {
	clock func() time.Time

	mu       sync.RWMutex
	sequence uint64
	records  map[string]EvidenceRecord
	byKey    map[evidenceKey][]string
	children map[string][]string
	events   []RevelationEvent
}

// NewConfidentialEvidenceStore creates an empty append-only store.
func NewConfidentialEvidenceStore(clock func() time.Time) *ConfidentialEvidenceStore {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &ConfidentialEvidenceStore{clock: clock, records: make(map[string]EvidenceRecord), byKey: make(map[evidenceKey][]string), children: make(map[string][]string)}
}

// Append adds one original evidence record and returns a defensive copy.
func (s *ConfidentialEvidenceStore) Append(input EvidenceInput) (EvidenceRecord, error) {
	if s == nil {
		return EvidenceRecord{}, ErrEvidenceInvalid
	}
	p := input.Pseudonym
	if p.ID == "" {
		p.ID = p.Value
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Scope) == "" || p.Generation <= 0 || strings.TrimSpace(input.ContentDigest) == "" || input.IntakeAt.IsZero() || strings.TrimSpace(input.CustodyRef) == "" {
		return EvidenceRecord{}, fmt.Errorf("%w: pseudonym generation, content digest, intake time and custody reference are required", ErrEvidenceInvalid)
	}
	revelation := input.Revelation
	if revelation == "" {
		revelation = RevelationNotRevealed
	}
	if revelation != RevelationNotRevealed && revelation != RevelationRevealed {
		return EvidenceRecord{}, fmt.Errorf("%w: unknown revelation state", ErrEvidenceInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	recordedAt := s.clock().UTC()
	record := EvidenceRecord{PseudonymID: p.ID, Tenant: p.Tenant, Scope: p.Scope, Generation: p.Generation, ContentDigest: input.ContentDigest, IntakeAt: input.IntakeAt.UTC(), RecordedAt: recordedAt, CustodyRef: input.CustodyRef, Revelation: revelation}
	record.ID = digestEvidenceRecord(record, s.sequence)
	record.Digest = digestEvidenceRecord(record, 0)
	s.records[record.ID] = record
	s.byKey[evidenceKey{tenant: record.Tenant, scope: record.Scope, generation: record.Generation}] = append(s.byKey[evidenceKey{tenant: record.Tenant, scope: record.Scope, generation: record.Generation}], record.ID)
	return cloneEvidenceRecord(record), nil
}

// Record is an alias for Append.
func (s *ConfidentialEvidenceStore) Record(input EvidenceInput) (EvidenceRecord, error) {
	return s.Append(input)
}

// AppendCorrection appends a new record naming the record it corrects.
func (s *ConfidentialEvidenceStore) AppendCorrection(request EvidenceCorrectionRequest) (EvidenceRecord, error) {
	if s == nil || strings.TrimSpace(request.RecordID) == "" && strings.TrimSpace(request.CorrectedRecordID) == "" || strings.TrimSpace(request.Reason) == "" {
		return EvidenceRecord{}, fmt.Errorf("%w: corrected record and reason are required", ErrEvidenceInvalid)
	}
	parentID := request.RecordID
	if parentID == "" {
		parentID = request.CorrectedRecordID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parent, ok := s.records[parentID]
	if !ok {
		return EvidenceRecord{}, ErrEvidenceNotFound
	}
	s.sequence++
	contentDigest := request.ContentDigest
	if contentDigest == "" {
		contentDigest = parent.ContentDigest
	}
	custodyRef := request.CustodyRef
	if custodyRef == "" {
		custodyRef = parent.CustodyRef
	}
	intakeAt := request.IntakeAt
	if intakeAt.IsZero() {
		intakeAt = parent.IntakeAt
	}
	record := EvidenceRecord{PseudonymID: parent.PseudonymID, Tenant: parent.Tenant, Scope: parent.Scope, Generation: parent.Generation, ContentDigest: contentDigest, IntakeAt: intakeAt.UTC(), RecordedAt: s.clock().UTC(), CustodyRef: custodyRef, Revelation: parent.Revelation, CorrectionOf: parentID, Corrects: parentID, CorrectionReason: strings.TrimSpace(request.Reason)}
	if strings.TrimSpace(record.ContentDigest) == "" || strings.TrimSpace(record.CustodyRef) == "" || record.IntakeAt.IsZero() {
		return EvidenceRecord{}, fmt.Errorf("%w: correction must retain content, intake and custody evidence", ErrEvidenceInvalid)
	}
	record.ID = digestEvidenceRecord(record, s.sequence)
	record.Digest = digestEvidenceRecord(record, 0)
	s.records[record.ID] = record
	key := evidenceKey{tenant: record.Tenant, scope: record.Scope, generation: record.Generation}
	s.byKey[key] = append(s.byKey[key], record.ID)
	s.children[parentID] = append(s.children[parentID], record.ID)
	return cloneEvidenceRecord(record), nil
}

// Correct is the compact correction command. The richer AppendCorrection
// form can replace the content or custody reference by digest.
func (s *ConfidentialEvidenceStore) Correct(recordID, reason string) (EvidenceRecord, error) {
	return s.AppendCorrection(EvidenceCorrectionRequest{RecordID: recordID, Reason: reason})
}

// Get returns one immutable snapshot of an evidence record.
func (s *ConfidentialEvidenceStore) Get(recordID string) (EvidenceRecord, bool) {
	if s == nil {
		return EvidenceRecord{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[recordID]
	return cloneEvidenceRecord(record), ok
}

// History returns the complete correction chain from its original record
// through every appended successor. Starting at any chain member returns the
// same full chain.
func (s *ConfidentialEvidenceStore) History(recordID string) ([]EvidenceRecord, error) {
	if s == nil {
		return nil, ErrEvidenceNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.records[recordID]; !ok {
		return nil, ErrEvidenceNotFound
	}
	root := recordID
	for s.records[root].CorrectionOf != "" {
		root = s.records[root].CorrectionOf
	}
	result := make([]EvidenceRecord, 0, 1)
	var visit func(string)
	visit = func(id string) {
		result = append(result, cloneEvidenceRecord(s.records[id]))
		children := append([]string(nil), s.children[id]...)
		sort.Strings(children)
		for _, child := range children {
			visit(child)
		}
	}
	visit(root)
	return result, nil
}

// CorrectionHistory is an alias for History.
func (s *ConfidentialEvidenceStore) CorrectionHistory(recordID string) ([]EvidenceRecord, error) {
	return s.History(recordID)
}

// ForGeneration returns all evidence facts keyed by one pseudonym generation.
func (s *ConfidentialEvidenceStore) ForGeneration(p Pseudonym) []EvidenceRecord {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := evidenceKey{tenant: p.Tenant, scope: p.Scope, generation: p.Generation}
	result := make([]EvidenceRecord, 0, len(s.byKey[key]))
	for _, id := range s.byKey[key] {
		result = append(result, cloneEvidenceRecord(s.records[id]))
	}
	return result
}

// RecordsForGeneration is a descriptive alias for ForGeneration.
func (s *ConfidentialEvidenceStore) RecordsForGeneration(p Pseudonym) []EvidenceRecord {
	return s.ForGeneration(p)
}

// Delete always refuses, making the append-only rule explicit to adapters
// and tests instead of relying on the absence of a convenient mutation path.
func (s *ConfidentialEvidenceStore) Delete(string) error { return ErrEvidenceAppendOnly }

// RecordRevelation appends a separate disclosure event after validating that
// the receipt refers to this exact evidence record. It never mutates record.
func (s *ConfidentialEvidenceStore) RecordRevelation(recordID string, evidence RevelationEvidence) (RevelationEvent, error) {
	if s == nil {
		return RevelationEvent{}, ErrEvidenceInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[recordID]
	if !ok {
		return RevelationEvent{}, ErrEvidenceNotFound
	}
	if err := evidence.Validate(s.clock().UTC()); err != nil || evidence.PseudonymRef != record.PseudonymID || evidence.Tenant != record.Tenant || evidence.Scope != record.Scope || evidence.Generation != record.Generation {
		return RevelationEvent{}, ErrEvidenceInvalid
	}
	s.sequence++
	event := RevelationEvent{EvidenceID: recordID, RevelationEvidenceRef: evidence.Digest, State: RevelationRevealed, At: s.clock().UTC(), Until: evidence.ExpiresAt}
	event.ID = digestRevelationEvent(event, s.sequence)
	event.Digest = digestRevelationEvent(event, 0)
	s.events = append(s.events, event)
	return event, nil
}

// RevelationStatus reports the latest explicit status without changing the
// original record. No event means the safe, explicit not_revealed state.
func (s *ConfidentialEvidenceStore) RevelationStatus(recordID string) (RevelationState, error) {
	if s == nil {
		return "", ErrEvidenceNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.records[recordID]; !ok {
		return "", ErrEvidenceNotFound
	}
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].EvidenceID == recordID {
			return s.events[i].State, nil
		}
	}
	return RevelationNotRevealed, nil
}

// RevelationEvents returns disclosure events in append order.
func (s *ConfidentialEvidenceStore) RevelationEvents() []RevelationEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RevelationEvent(nil), s.events...)
}

func cloneEvidenceRecord(record EvidenceRecord) EvidenceRecord { return record }

func digestEvidenceRecord(record EvidenceRecord, sequence uint64) string {
	canonical := strings.Join([]string{record.PseudonymID, record.Tenant, record.Scope, fmt.Sprint(record.Generation), record.ContentDigest, record.IntakeAt.UTC().Format(time.RFC3339Nano), record.RecordedAt.UTC().Format(time.RFC3339Nano), record.CustodyRef, string(record.Revelation), record.CorrectionOf, record.CorrectionReason, fmt.Sprint(sequence)}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "ev:pseudonym:" + hex.EncodeToString(sum[:])
}

func digestRevelationEvent(event RevelationEvent, sequence uint64) string {
	canonical := strings.Join([]string{event.EvidenceID, event.RevelationEvidenceRef, string(event.State), event.At.UTC().Format(time.RFC3339Nano), event.Until.UTC().Format(time.RFC3339Nano), fmt.Sprint(sequence)}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "rev:event:" + hex.EncodeToString(sum[:])
}

// ExplainEvidence describes append-only confidential evidence and separate
// disclosure status without carrying a pseudonym, actor, or content value.
func ExplainEvidence() string {
	return "Confidential evidence is keyed by pseudonym generation, retained append-only with correction links and reasons, and records revelation as a separate explicit event without rewriting prior evidence."
}

// ExplainConfidentialEvidence is an explicit alias for callers documenting
// the confidential evidence boundary.
func ExplainConfidentialEvidence() string { return ExplainEvidence() }

// Explain is the semantic explanation for this evidence boundary.
func (s *ConfidentialEvidenceStore) Explain() string { return ExplainEvidence() }
