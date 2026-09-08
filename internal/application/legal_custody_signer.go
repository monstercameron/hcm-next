package application

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"reflect"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

// Errors returned by the application-owned legal signer composition boundary.
var (
	ErrLegalCustodySignerProvider  = errors.New("application: legal custody signer provider is required")
	ErrLegalCustodySignerScope     = errors.New("application: legal custody signer scope does not match handle")
	ErrLegalCustodySignerKind      = errors.New("application: legal custody signer requires a key handle")
	ErrLegalCustodySignerHandle    = errors.New("application: legal custody signer returned a different handle")
	ErrLegalCustodySignerAlgorithm = errors.New("application: legal custody signer returned an unsupported algorithm")
	ErrLegalCustodySignerResult    = errors.New("application: legal custody signer returned an empty signature")
)

// legalCustodySigner is deliberately fixed at composition time. It accepts no
// caller-supplied context or handle, so every provider request uses the exact
// configured RequestContext and Handle. The upstream
// legal producer remains responsible for binding the signed bytes to its
// trusted semantic context.
type legalCustodySigner struct {
	provider custody.Provider
	ctx      custody.Context
	handle   custody.Handle
}

var _ legal.SignerPort = (*legalCustodySigner)(nil)

// NewLegalCustodySigner composes the legal signer with a custody provider.
// The provider remains responsible for authenticating and authorizing the
// operation. publicKey is trusted configuration used by legal.NewPortSigner
// for offline verification; no private key or provider material is exported.
func NewLegalCustodySigner(provider custody.Provider, request custody.RequestContext, handle custody.Handle, publicKey ed25519.PublicKey) (*legal.Signer, error) {
	port, err := newLegalCustodySignerPort(provider, request, handle, publicKey)
	if err != nil {
		return nil, err
	}
	return legal.NewPortSigner(publicKey, port)
}

func newLegalCustodySignerPort(provider custody.Provider, request custody.RequestContext, handle custody.Handle, publicKey ed25519.PublicKey) (legal.SignerPort, error) {
	if provider == nil || isNilLegalCustodyProvider(provider) {
		return nil, ErrLegalCustodySignerProvider
	}
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("%w: request context: %v", custody.ErrInvalidContext, err)
	}
	if err := handle.Validate(); err != nil {
		return nil, fmt.Errorf("%w: custody handle: %v", custody.ErrInvalidHandle, err)
	}
	if handle.Kind != custody.Key {
		return nil, ErrLegalCustodySignerKind
	}
	if request.Tenant != handle.Tenant || request.Region != handle.Region {
		return nil, ErrLegalCustodySignerScope
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: trusted public key has %d bytes", legal.ErrSignerKey, len(publicKey))
	}
	return &legalCustodySigner{provider: provider, ctx: custody.Context{RequestContext: request}, handle: handle}, nil
}

type legalCustodyProviderError struct{ cause error }

func (legalCustodyProviderError) Error() string   { return "application: legal custody signing failed" }
func (e legalCustodyProviderError) Unwrap() error { return e.cause }

func isNilLegalCustodyProvider(provider custody.Provider) bool {
	v := reflect.ValueOf(provider)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (s *legalCustodySigner) Sign(message []byte) ([]byte, error) {
	if s == nil || s.provider == nil {
		return nil, ErrLegalCustodySignerProvider
	}
	// Copy the digest before crossing the adapter boundary and copy the result
	// before returning it, preventing provider/caller slice aliasing.
	signature, _, err := s.provider.Sign(s.ctx, s.handle, append([]byte(nil), message...))
	if err != nil {
		return nil, legalCustodyProviderError{cause: err}
	}
	if signature.Handle != s.handle {
		return nil, ErrLegalCustodySignerHandle
	}
	if signature.Algorithm != "Ed25519" {
		return nil, ErrLegalCustodySignerAlgorithm
	}
	if len(signature.Data) != ed25519.SignatureSize {
		return nil, ErrLegalCustodySignerResult
	}
	return append([]byte(nil), signature.Data...), nil
}
