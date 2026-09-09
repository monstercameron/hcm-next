package cryptoagile

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// CustodyKeySource adapts internal/trust/custody.Provider - the key custody
// port - into [SignerPort] and [VerifierPort] by mapping each suite id to a
// custody [custody.Handle] under one fixed [custody.RequestContext]. It
// never asks custody for raw key material: custody's port deliberately has
// no such operation (see custody's own package doc), and this adapter only
// ever forwards custody's own opaque signature bytes.
type CustodyKeySource struct {
	provider custody.Provider
	context  custody.Context
	handles  map[string]custody.Handle
}

// NewCustodyKeySource validates reqCtx and every handle in handles (keyed by
// suite id) and returns a key source backed by provider.
func NewCustodyKeySource(provider custody.Provider, reqCtx custody.RequestContext, handles map[string]custody.Handle) (*CustodyKeySource, error) {
	if provider == nil {
		return nil, fmt.Errorf("cryptoagile: custody key source needs a non-nil Provider")
	}
	if err := reqCtx.Validate(); err != nil {
		return nil, fmt.Errorf("cryptoagile: custody key source request context: %w", err)
	}
	copied := make(map[string]custody.Handle, len(handles))
	for id, h := range handles {
		if err := h.Validate(); err != nil {
			return nil, fmt.Errorf("cryptoagile: custody handle for suite %q: %w", id, err)
		}
		copied[id] = h
	}
	return &CustodyKeySource{
		provider: provider,
		context:  custody.Context{RequestContext: reqCtx},
		handles:  copied,
	}, nil
}

func (c *CustodyKeySource) handleFor(suiteID string) (custody.Handle, error) {
	h, ok := c.handles[suiteID]
	if !ok {
		return custody.Handle{}, fmt.Errorf("cryptoagile: no custody handle mapped for suite %q", suiteID)
	}
	return h, nil
}

// Sign implements [SignerPort] by delegating to custody's Sign operation.
func (c *CustodyKeySource) Sign(suiteID string, message []byte) ([]byte, error) {
	h, err := c.handleFor(suiteID)
	if err != nil {
		return nil, err
	}
	sig, _, err := c.provider.Sign(c.context, h, message)
	if err != nil {
		return nil, fmt.Errorf("cryptoagile: custody sign for suite %q: %w", suiteID, err)
	}
	return sig.Data, nil
}

// Verify implements [VerifierPort] by delegating to custody's Verify
// operation.
func (c *CustodyKeySource) Verify(suiteID string, message, signature []byte) (bool, error) {
	h, err := c.handleFor(suiteID)
	if err != nil {
		return false, err
	}
	sig := custody.Signature{Handle: h, Data: signature}
	ok, _, err := c.provider.Verify(c.context, h, message, sig)
	if err != nil {
		return false, fmt.Errorf("cryptoagile: custody verify for suite %q: %w", suiteID, err)
	}
	return ok, nil
}
