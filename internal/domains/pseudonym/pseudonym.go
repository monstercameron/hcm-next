// Package pseudonym provides scoped pseudonymous subject identifiers. It
// requires a custody derivation port and never receives the underlying key.
package pseudonym

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

const schemaVersion = 1

type GenerateRequest struct {
	Subject string
	Scope   string
	Purpose string
}

// Pseudonym contains only a scoped identifier and its public derivation
// coordinates. It never contains the subject or an identity mapping.
type Pseudonym struct {
	ID         string `json:"id"`
	Value      string `json:"value"`
	Generation int    `json:"generation"`
	Tenant     string `json:"tenant"`
	Scope      string `json:"scope"`
	Purpose    string `json:"purpose"`
}

type ReidentifyRequest struct {
	Pseudonym      Pseudonym `json:"pseudonym"`
	RequestedBy    string    `json:"requested_by"`
	Approver       string    `json:"approver"`
	EvidenceDigest string    `json:"evidence_digest"`
}

type Evidence struct {
	Operation     string    `json:"operation"`
	Generation    int       `json:"generation"`
	RequestDigest string    `json:"request_digest"`
	At            time.Time `json:"at"`
}

type sealedMapping struct {
	Generation int
	Tenant     string
	Scope      string
	Purpose    string
	Nonce      []byte
	Ciphertext []byte
}

type Service struct {
	deriver custody.KeyDeriver
	key     custody.Handle
	clock   func() time.Time

	mu         sync.RWMutex
	generation int
	mappings   map[string]sealedMapping
}

var (
	ErrInvalidRequest         = errors.New("pseudonym: invalid request")
	ErrInvalidCustodyKey      = errors.New("pseudonym: invalid custody key")
	ErrPseudonymNotFound      = errors.New("pseudonym: identifier not found")
	ErrReidentificationDenied = errors.New("pseudonym: re-identification denied")
	ErrDerivationUnavailable  = errors.New("pseudonym: custody derivation unavailable")
)

// New creates a pseudonym service backed by an opaque custody key handle.
// The deriver is the only component that can access provider-held key state.
func New(deriver custody.KeyDeriver, key custody.Handle) (*Service, error) {
	if deriver == nil {
		return nil, ErrDerivationUnavailable
	}
	if err := key.Validate(); err != nil || key.Kind != custody.Key {
		return nil, fmt.Errorf("%w: a valid KEY handle is required", ErrInvalidCustodyKey)
	}
	return &Service{deriver: deriver, key: key, clock: func() time.Time { return time.Now().UTC() }, generation: 1, mappings: make(map[string]sealedMapping)}, nil
}

// NewPseudonymizer is a semantic alias for New.
func NewPseudonymizer(deriver custody.KeyDeriver, key custody.Handle) (*Service, error) {
	return New(deriver, key)
}

// Generate derives an identifier stable for the same subject, tenant, scope,
// purpose, and generation. A different scope or purpose uses a different PRF
// domain and therefore has no computable relation without custody access.
func (s *Service) Generate(ctx custody.Context, request GenerateRequest) (Pseudonym, error) {
	if err := s.validateContext(ctx); err != nil {
		return Pseudonym{}, err
	}
	if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.Scope) == "" || strings.TrimSpace(request.Purpose) == "" {
		return Pseudonym{}, ErrInvalidRequest
	}
	if request.Purpose != ctx.Purpose {
		return Pseudonym{}, fmt.Errorf("%w: request purpose must match context", ErrInvalidRequest)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	identifier, derived, err := s.identifier(ctx, request.Subject, request.Scope, request.Purpose, s.generation)
	if err != nil {
		return Pseudonym{}, err
	}
	if _, exists := s.mappings[identifier.ID]; !exists {
		sealed, err := seal(derived.Output, []byte(request.Subject), mappingAAD(identifier))
		if err != nil {
			return Pseudonym{}, err
		}
		sealed.Generation = identifier.Generation
		sealed.Tenant = identifier.Tenant
		sealed.Scope = identifier.Scope
		sealed.Purpose = identifier.Purpose
		s.mappings[identifier.ID] = sealed
	}
	return identifier, nil
}

// Pseudonymize is an alias for Generate.
func (s *Service) Pseudonymize(ctx custody.Context, request GenerateRequest) (Pseudonym, error) {
	return s.Generate(ctx, request)
}

// Rotate starts a new pseudonym generation. Existing mappings remain as
// custody-encrypted blobs so an evidenced request can re-identify an older
// generation without storing a plaintext escrow map.
func (s *Service) Rotate(ctx custody.Context) (int, error) {
	if err := s.validateContext(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	return s.generation, nil
}

func (s *Service) CurrentGeneration(ctx custody.Context) (int, error) {
	if err := s.validateContext(ctx); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation, nil
}

// Reidentify decrypts exactly one mapping only after an evidenced request
// with a distinct approver. The returned evidence contains no subject.
func (s *Service) Reidentify(ctx custody.Context, request ReidentifyRequest) (string, Evidence, error) {
	if err := s.validateContext(ctx); err != nil {
		return "", Evidence{}, err
	}
	id := request.Pseudonym.ID
	if id == "" {
		id = request.Pseudonym.Value
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.Approver) == "" || request.RequestedBy == request.Approver || strings.TrimSpace(request.EvidenceDigest) == "" {
		return "", Evidence{}, ErrReidentificationDenied
	}
	s.mu.RLock()
	mapping, ok := s.mappings[id]
	s.mu.RUnlock()
	if !ok {
		return "", Evidence{}, ErrPseudonymNotFound
	}
	derived, err := s.derive(ctx, mapping.Tenant, mapping.Scope, mapping.Purpose, mapping.Generation)
	if err != nil {
		return "", Evidence{}, err
	}
	subject, err := open(derived.Output, mapping.Nonce, mapping.Ciphertext, mappingAAD(Pseudonym{ID: id, Generation: mapping.Generation, Tenant: mapping.Tenant, Scope: mapping.Scope, Purpose: mapping.Purpose}))
	if err != nil {
		return "", Evidence{}, ErrReidentificationDenied
	}
	requestDigest := sha256.Sum256([]byte(strings.Join([]string{id, request.RequestedBy, request.Approver, request.EvidenceDigest}, "\x00")))
	evidence := Evidence{Operation: "reidentify", Generation: mapping.Generation, RequestDigest: hex.EncodeToString(requestDigest[:]), At: s.clock().UTC()}
	return string(subject), evidence, nil
}

func (s *Service) validateContext(ctx custody.Context) error {
	if s == nil || s.deriver == nil {
		return ErrDerivationUnavailable
	}
	if err := ctx.Validate(); err != nil {
		return err
	}
	if ctx.Tenant != s.key.Tenant || ctx.Region != s.key.Region {
		return fmt.Errorf("%w: custody key scope does not match context", ErrInvalidRequest)
	}
	return nil
}

func (s *Service) identifier(ctx custody.Context, subject, scope, purpose string, generation int) (Pseudonym, custody.DerivedValue, error) {
	derived, err := s.derive(ctx, ctx.Tenant, scope, purpose, generation)
	if err != nil {
		return Pseudonym{}, custody.DerivedValue{}, err
	}
	mac := hmac.New(sha256.New, derived.Output)
	_, _ = mac.Write([]byte(subject))
	digest := mac.Sum(nil)
	id := fmt.Sprintf("psn:v%d:g%d:%s", schemaVersion, generation, hex.EncodeToString(digest))
	return Pseudonym{ID: id, Value: id, Generation: generation, Tenant: ctx.Tenant, Scope: scope, Purpose: purpose}, derived, nil
}

func (s *Service) derive(ctx custody.Context, tenant, scope, purpose string, generation int) (custody.DerivedValue, error) {
	label := []byte(strings.Join([]string{"hcm-next/pseudonym", tenant, scope, purpose, fmt.Sprint(generation)}, "\x00"))
	derived, _, err := s.deriver.Derive(ctx, s.key, label)
	if err != nil || !hmacOutputUsable(derived.Output) {
		return custody.DerivedValue{}, fmt.Errorf("%w: %v", ErrDerivationUnavailable, err)
	}
	return derived, nil
}

func hmacOutputUsable(output []byte) bool { return len(output) >= sha256.Size }

func mappingAAD(pseudonym Pseudonym) []byte {
	return []byte(strings.Join([]string{pseudonym.ID, pseudonym.Tenant, pseudonym.Scope, pseudonym.Purpose, fmt.Sprint(pseudonym.Generation)}, "\x00"))
}

func seal(derived, plaintext, aad []byte) (sealedMapping, error) {
	key := sha256.Sum256(append([]byte("hcm-next/pseudonym/mapping\x00"), derived...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return sealedMapping{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return sealedMapping{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return sealedMapping{}, err
	}
	return sealedMapping{Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, plaintext, aad)}, nil
}

func open(derived, nonce, ciphertext, aad []byte) ([]byte, error) {
	key := sha256.Sum256(append([]byte("hcm-next/pseudonym/mapping\x00"), derived...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrReidentificationDenied
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

// Version is the pseudonym contract version.
func Version() int { return schemaVersion }

// Explain describes scope separation, custody derivation, and escrow policy.
func Explain() string {
	return "Scoped pseudonyms use custody-backed HMAC derivation; mappings remain encrypted under that custody key and re-identification requires evidenced distinct approval."
}
