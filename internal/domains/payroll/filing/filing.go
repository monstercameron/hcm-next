package filing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/transport"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/envelope"
)

var (
	ErrInvalidRequest = errors.New("filing: invalid request")
	ErrTransport      = errors.New("filing: transport failed")
	ErrEvidence       = errors.New("filing: evidence persistence failed")
	ErrBrokenChain    = errors.New("filing: evidence chain is broken")
	ErrNotFound       = errors.New("filing: evidence is not found")
)

// Refusal is a typed, safe validation refusal. Element is always the
// offending required element; no element value or payload is included.
type Refusal struct {
	Code    string
	Element string
	Reason  string
}

func (e Refusal) Error() string {
	return fmt.Sprintf("filing: refusal code=%s element=%s: %s", e.Code, e.Element, e.Reason)
}

func (e Refusal) Is(target error) bool {
	return target == ErrMissingElement && e.Code == "MISSING_REQUIRED_ELEMENT"
}

// FilingRequest is the source record for one filing. Elements are the
// source-to-report values used for required-element validation. Payload may be
// CSV, XML, JSON, or another partner format; it is never put into evidence.
type FilingRequest struct {
	SubmissionID   string
	Kind           ExchangeKind
	Jurisdiction   legal.Jurisdiction
	AsOf           time.Time
	Payload        []byte
	Elements       map[string]string
	CustodyContext custody.Context
	ObjectID       string
	CorrectionOf   string
}

// SubmissionRequest is the descriptive alias used by callers at the domain
// boundary.
type SubmissionRequest = FilingRequest

// ValidateRequired checks all profile elements before encryption or transport.
func ValidateRequired(profile Profile, elements map[string]string, payload []byte) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if len(payload) == 0 {
		return fmt.Errorf("%w: payload is empty", ErrInvalidPayload)
	}
	values := elements
	if len(values) == 0 {
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return fmt.Errorf("%w: elements are required for non-JSON payload", ErrInvalidPayload)
		}
		values = make(map[string]string, len(decoded))
		for key, value := range decoded {
			if text, ok := value.(string); ok {
				values[key] = text
			} else if value != nil {
				values[key] = fmt.Sprint(value)
			}
		}
	}
	for _, element := range profile.RequiredElements {
		if strings.TrimSpace(values[element]) == "" {
			return Refusal{Code: "MISSING_REQUIRED_ELEMENT", Element: element, Reason: "required before transmission"}
		}
	}
	return nil
}

// EnvelopePort is the only encryption seam used by the service. It accepts
// plaintext only for the duration of Encrypt and returns provider-neutral
// ciphertext and safe operation evidence.
type EnvelopePort interface {
	Encrypt(custody.Context, string, []byte) (envelope.Envelope, envelope.Evidence, error)
}

// EncryptionPort is a descriptive alias for EnvelopePort.
type EncryptionPort = EnvelopePort

// TransportPort is the only outbound I/O seam. A real connectivity transport
// adapter, such as transport.REST, can satisfy it; the domain never dials.
type TransportPort interface {
	Call(context.Context, transport.Request) (transport.Response, error)
}

// EvidencePort is the persistence seam for immutable submission chains.
type EvidencePort interface {
	Append(string, EvidenceChain) error
	Get(string) (EvidenceChain, error)
	Verify(string) error
}

// EvidenceKind names the four required evidence revisions.
type EvidenceKind string

const (
	PayloadEvidence        EvidenceKind = "PAYLOAD"
	SubmissionEvidence     EvidenceKind = "SUBMISSION"
	AcknowledgmentEvidence EvidenceKind = "ACKNOWLEDGMENT"
	CorrectionEvidence     EvidenceKind = "CORRECTION"
)

// EvidenceRecord is immutable, content-addressed evidence. It carries only
// digests and provider-safe metadata, never plaintext, secrets, or account
// numbers.
type EvidenceRecord struct {
	Kind               EvidenceKind
	Revision           uint64
	At                 time.Time
	PreviousDigest     string
	ProfileDigest      string
	PayloadDigest      string
	ReferenceDigest    string
	CorrectionOfDigest string
	Status             string
	Digest             string
}

// EvidenceChain is the append-only evidence history for one submission.
type EvidenceChain struct {
	Records []EvidenceRecord
}

func (c EvidenceChain) clone() EvidenceChain {
	c.Records = append([]EvidenceRecord(nil), c.Records...)
	return c
}

// Append returns a new chain and never mutates c.
func (c EvidenceChain) Append(record EvidenceRecord) (EvidenceChain, error) {
	if !validEvidenceKind(record.Kind) || record.At.IsZero() {
		return EvidenceChain{}, fmt.Errorf("%w: evidence kind and time are required", ErrBrokenChain)
	}
	next := c.clone()
	record.Revision = uint64(len(next.Records) + 1)
	if len(next.Records) > 0 {
		record.PreviousDigest = next.Records[len(next.Records)-1].Digest
	}
	record.Digest = digestRecord(record)
	next.Records = append(next.Records, record)
	return next, nil
}

// Verify checks the hash chain and required evidence ordering. It refuses a
// chain that could not prove payload, submission, acknowledgment, and (when
// present) correction continuity.
func (c EvidenceChain) Verify() error {
	if len(c.Records) < 3 {
		return fmt.Errorf("%w: payload, submission and acknowledgment are required", ErrBrokenChain)
	}
	previous := ""
	for i, record := range c.Records {
		if record.Revision != uint64(i+1) || record.PreviousDigest != previous || record.Digest == "" || record.Digest != digestRecord(record) {
			return fmt.Errorf("%w: revision %d digest linkage", ErrBrokenChain, i+1)
		}
		if !validEvidenceKind(record.Kind) || record.At.IsZero() || record.ProfileDigest == "" {
			return fmt.Errorf("%w: revision %d metadata", ErrBrokenChain, i+1)
		}
		previous = record.Digest
	}
	if c.Records[0].Kind != PayloadEvidence || c.Records[1].Kind != SubmissionEvidence || c.Records[2].Kind != AcknowledgmentEvidence {
		return fmt.Errorf("%w: first three revisions must be payload, submission, acknowledgment", ErrBrokenChain)
	}
	if c.Records[0].PayloadDigest == "" || c.Records[1].PayloadDigest != c.Records[0].PayloadDigest || c.Records[2].PayloadDigest != c.Records[0].PayloadDigest {
		return fmt.Errorf("%w: payload digest is not carried through acknowledgment", ErrBrokenChain)
	}
	for i := 3; i < len(c.Records); i++ {
		if c.Records[i].Kind != CorrectionEvidence || c.Records[i].CorrectionOfDigest == "" {
			return fmt.Errorf("%w: revision %d correction linkage", ErrBrokenChain, i+1)
		}
	}
	return nil
}

func validEvidenceKind(kind EvidenceKind) bool {
	switch kind {
	case PayloadEvidence, SubmissionEvidence, AcknowledgmentEvidence, CorrectionEvidence:
		return true
	default:
		return false
	}
}

func digestRecord(record EvidenceRecord) string {
	copyRecord := record
	copyRecord.Digest = ""
	raw, _ := json.Marshal(copyRecord)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// MemoryEvidenceStore preserves the same append-only and verification
// semantics a durable adapter must implement. It is intentionally pure and
// contains no database or provider dependency.
type MemoryEvidenceStore struct {
	mu     sync.RWMutex
	chains map[string]EvidenceChain
}

func NewMemoryEvidenceStore() *MemoryEvidenceStore {
	return &MemoryEvidenceStore{chains: make(map[string]EvidenceChain)}
}

func (s *MemoryEvidenceStore) Append(id string, chain EvidenceChain) error {
	if s == nil || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: submission id is required", ErrEvidence)
	}
	if err := chain.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.chains[id]; ok {
		if !sameChain(existing, chain) {
			return fmt.Errorf("%w: immutable submission revision differs", ErrEvidence)
		}
		return nil
	}
	s.chains[id] = chain.clone()
	return nil
}

func (s *MemoryEvidenceStore) Get(id string) (EvidenceChain, error) {
	if s == nil {
		return EvidenceChain{}, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	chain, ok := s.chains[id]
	if !ok {
		return EvidenceChain{}, ErrNotFound
	}
	return chain.clone(), nil
}

// Verify validates this chain and any correction parent by digest.
func (s *MemoryEvidenceStore) Verify(id string) error {
	chain, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := chain.Verify(); err != nil {
		return err
	}
	if chain.Records[0].CorrectionOfDigest == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, parent := range s.chains {
		if len(parent.Records) > 0 && parent.Records[len(parent.Records)-1].Digest == chain.Records[0].CorrectionOfDigest {
			return parent.Verify()
		}
	}
	return fmt.Errorf("%w: correction parent digest is absent", ErrBrokenChain)
}

func sameChain(a, b EvidenceChain) bool { return string(mustJSON(a)) == string(mustJSON(b)) }
func mustJSON(value any) []byte         { raw, _ := json.Marshal(value); return raw }

// PreparedFiling is the encrypted wire representation ready for transport.
type PreparedFiling struct {
	Profile             Profile
	Envelope            envelope.Envelope
	WireBody            []byte
	PayloadDigest       string
	SourcePayloadDigest string
	ProfileDigest       string
	CorrectionOfDigest  string
}

// Submission is the safe result of a successful delivery.
type Submission struct {
	ID             string
	Kind           ExchangeKind
	Profile        Profile
	PayloadDigest  string
	Evidence       EvidenceChain
	Acknowledgment Acknowledgment
}

// Acknowledgment contains only digests and a status, not a remote response
// body or a provider's identifier.
type Acknowledgment struct {
	StatusCode     int
	ResponseDigest string
	At             time.Time
}

// Explanation is deliberately audit-safe: it has no submission ID,
// jurisdiction identifier, external receipt, plaintext, or account number.
type Explanation struct {
	Kind                 ExchangeKind
	SchemaVersion        string
	RequiredElementCount int
	PayloadDigest        string
	EvidenceRecordCount  int
	Acknowledged         bool
	Correction           bool
}

// Explain returns safe facts about a submission.
func (s Submission) Explain() (Explanation, error) {
	if err := s.Evidence.Verify(); err != nil {
		return Explanation{}, err
	}
	return Explanation{Kind: s.Kind, SchemaVersion: s.Profile.SchemaVersion, RequiredElementCount: len(s.Profile.RequiredElements), PayloadDigest: s.PayloadDigest, EvidenceRecordCount: len(s.Evidence.Records), Acknowledged: s.Acknowledgment.StatusCode >= 200 && s.Acknowledgment.StatusCode < 300, Correction: s.Evidence.Records[0].CorrectionOfDigest != ""}, nil
}

// ExplainSubmission is the package-level spelling.
func ExplainSubmission(s Submission) (Explanation, error) { return s.Explain() }

// Service composes profile resolution, required-element validation,
// end-to-end encryption, transport, and evidence persistence.
type Service struct {
	profiles  *Registry
	envelope  EnvelopePort
	transport TransportPort
	evidence  EvidencePort
	clock     func() time.Time
}

// ServiceOption configures non-business execution details.
type ServiceOption func(*Service)

// WithClock makes evidence timestamps deterministic in tests.
func WithClock(clock func() time.Time) ServiceOption {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func NewService(profiles *Registry, encryptor EnvelopePort, sender TransportPort, evidence EvidencePort, options ...ServiceOption) (*Service, error) {
	if profiles == nil || encryptor == nil || sender == nil || evidence == nil {
		return nil, fmt.Errorf("%w: profile, envelope, transport and evidence ports are required", ErrInvalidRequest)
	}
	s := &Service{profiles: profiles, envelope: encryptor, transport: sender, evidence: evidence, clock: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(s)
	}
	return s, nil
}

// Prepare resolves and validates a filing, then seals its payload. No I/O is
// performed and no plaintext is retained in the returned value.
func (s *Service) Prepare(ctx context.Context, req FilingRequest) (PreparedFiling, error) {
	if s == nil || ctx == nil || strings.TrimSpace(req.SubmissionID) == "" || req.AsOf.IsZero() || req.CustodyContext.Validate() != nil {
		return PreparedFiling{}, fmt.Errorf("%w: submission, instant, context and tenant are required", ErrInvalidRequest)
	}
	if req.CustodyContext.Tenant == "" {
		return PreparedFiling{}, fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	}
	profile, err := s.profiles.Resolve(req.Kind, req.Jurisdiction, req.AsOf)
	if err != nil {
		return PreparedFiling{}, err
	}
	if err := ValidateRequired(profile, req.Elements, req.Payload); err != nil {
		return PreparedFiling{}, err
	}
	objectID := req.ObjectID
	if objectID == "" {
		objectID = "filing/" + req.SubmissionID
	}
	env, _, err := s.envelope.Encrypt(req.CustodyContext, objectID, req.Payload)
	if err != nil {
		return PreparedFiling{}, fmt.Errorf("%w: envelope encryption: %v", ErrInvalidRequest, err)
	}
	wire, err := json.Marshal(env)
	if err != nil {
		return PreparedFiling{}, fmt.Errorf("%w: encode envelope: %v", ErrInvalidRequest, err)
	}
	payloadDigest := digestBytes(wire)
	sourceDigest := digestBytes(req.Payload)
	var correctionDigest string
	if req.CorrectionOf != "" {
		parent, getErr := s.evidence.Get(req.CorrectionOf)
		if getErr != nil {
			return PreparedFiling{}, fmt.Errorf("%w: correction parent: %v", ErrBrokenChain, getErr)
		}
		if getErr = parent.Verify(); getErr != nil || len(parent.Records) == 0 {
			return PreparedFiling{}, fmt.Errorf("%w: correction parent: %v", ErrBrokenChain, getErr)
		}
		correctionDigest = parent.Records[len(parent.Records)-1].Digest
	}
	return PreparedFiling{Profile: profile, Envelope: env, WireBody: wire, PayloadDigest: payloadDigest, SourcePayloadDigest: sourceDigest, ProfileDigest: profile.Digest(), CorrectionOfDigest: correctionDigest}, nil
}

// Submit performs the complete preflight/encrypt/transmit/evidence flow.
func (s *Service) Submit(ctx context.Context, req FilingRequest) (Submission, error) {
	prepared, err := s.Prepare(ctx, req)
	if err != nil {
		return Submission{}, err
	}
	request := transport.Request{Method: "POST", URL: prepared.Profile.Endpoint, Headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": req.SubmissionID, "X-Filing-Schema-Version": prepared.Profile.SchemaVersion}, Body: append([]byte(nil), prepared.WireBody...)}
	response, err := s.transport.Call(ctx, request)
	if err != nil {
		return Submission{}, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Submission{}, fmt.Errorf("%w: remote status %d", ErrTransport, response.StatusCode)
	}
	at := s.clock().UTC()
	chain := EvidenceChain{}
	for _, record := range []EvidenceRecord{
		{Kind: PayloadEvidence, At: at, ProfileDigest: prepared.ProfileDigest, PayloadDigest: prepared.PayloadDigest, CorrectionOfDigest: prepared.CorrectionOfDigest, Status: "SEALED"},
		{Kind: SubmissionEvidence, At: at, ProfileDigest: prepared.ProfileDigest, PayloadDigest: prepared.PayloadDigest, ReferenceDigest: digestBytes([]byte(req.SubmissionID)), Status: "SUBMITTED"},
		{Kind: AcknowledgmentEvidence, At: at, ProfileDigest: prepared.ProfileDigest, PayloadDigest: prepared.PayloadDigest, ReferenceDigest: digestBytes(response.Body), Status: "ACKNOWLEDGED"},
	} {
		chain, err = chain.Append(record)
		if err != nil {
			return Submission{}, err
		}
	}
	if prepared.CorrectionOfDigest != "" {
		chain, err = chain.Append(EvidenceRecord{Kind: CorrectionEvidence, At: at, ProfileDigest: prepared.ProfileDigest, PayloadDigest: prepared.PayloadDigest, CorrectionOfDigest: prepared.CorrectionOfDigest, Status: "CORRECTS"})
		if err != nil {
			return Submission{}, err
		}
	}
	if err := s.evidence.Append(req.SubmissionID, chain); err != nil {
		return Submission{}, fmt.Errorf("%w: %v", ErrEvidence, err)
	}
	return Submission{ID: req.SubmissionID, Kind: req.Kind, Profile: prepared.Profile, PayloadDigest: prepared.PayloadDigest, Evidence: chain, Acknowledgment: Acknowledgment{StatusCode: response.StatusCode, ResponseDigest: digestBytes(response.Body), At: at}}, nil
}

// Verify refuses a broken stored chain, including a missing correction parent.
func (s *Service) Verify(submissionID string) error { return s.evidence.Verify(submissionID) }

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var _ EvidencePort = (*MemoryEvidenceStore)(nil)
