// Package bootstrap implements secret-zero workload bootstrap. A workload
// proves its identity with mTLS, then receives only a bounded reference lease
// from the custody port. There is deliberately no bootstrap secret field,
// environment fallback, or plaintext credential path in this package.
package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

const MaxBootstrapLeaseTTL = 5 * time.Minute

var (
	ErrInvalidBootstrap = errors.New("bootstrap: invalid secret-zero bootstrap request")
	ErrBootstrapDenied  = errors.New("bootstrap: workload bootstrap denied")
	ErrNotReady         = errors.New("bootstrap: workload is not ready")
)

// Request names the one reference-only lease a workload needs to become
// ready. The custody Handle is an opaque reference; it contains no secret.
type Request struct {
	Handle      custody.Handle
	Tenant      string
	Region      string
	Purpose     string
	Destination string
	Operation   custody.Operation
	TTL         time.Duration
}

// Evidence is safe to persist: it records coordinates and outcome only.
type Evidence struct {
	Workload    string
	Tenant      string
	Destination string
	Operation   custody.Operation
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Outcome     string
	Reason      string
}

// Result is ready only when the peer was verified and custody returned a
// lease whose scope and lifetime are no wider than the request.
type Result struct {
	Identity workload.MTLSIdentity
	Lease    custody.Lease
	Ready    bool
	Evidence Evidence
}

// Bootstrapper composes mTLS attestation and the provider-neutral custody
// lease port. It does not retain a secret or a lease between calls.
type Bootstrapper struct {
	verifier *workload.MTLSVerifier
	custody  custody.Provider
	now      func() time.Time
}

func New(verifier *workload.MTLSVerifier, provider custody.Provider, now func() time.Time) (*Bootstrapper, error) {
	if verifier == nil || provider == nil {
		return nil, ErrInvalidBootstrap
	}
	if now == nil {
		now = time.Now
	}
	return &Bootstrapper{verifier: verifier, custody: provider, now: now}, nil
}

// Acquire verifies the peer certificate and asks custody for a bounded lease.
// Any verification or custody failure returns Ready=false and no lease.
func (b *Bootstrapper) Acquire(ctx context.Context, peer tls.ConnectionState, req Request) (Result, error) {
	result := Result{Evidence: Evidence{Tenant: req.Tenant, Destination: req.Destination, Operation: req.Operation, Outcome: "denied"}}
	if b == nil || b.verifier == nil || b.custody == nil {
		return result, ErrNotReady
	}
	if err := validateRequest(req); err != nil {
		result.Evidence.Reason = err.Error()
		return result, err
	}
	identity, err := b.verifier.VerifyConnection(peer)
	if err != nil {
		result.Evidence.Reason = err.Error()
		return result, fmt.Errorf("%w: identity: %v", ErrBootstrapDenied, err)
	}
	result.Identity = identity
	if identity.Tenant() != req.Tenant || req.Handle.Tenant != req.Tenant {
		result.Evidence.Reason = "tenant_scope_mismatch"
		return result, fmt.Errorf("%w: tenant scope mismatch", ErrBootstrapDenied)
	}
	now := b.now().UTC()
	lease, err := b.custody.IssueLease(custody.Context{RequestContext: custody.RequestContext{Workload: identity.Subject(), Tenant: req.Tenant, Region: req.Region, Purpose: req.Purpose, Destination: req.Destination}}, req.Handle, req.Operation, req.TTL)
	if err != nil {
		result.Evidence.Reason = err.Error()
		return result, fmt.Errorf("%w: custody: %v", ErrBootstrapDenied, err)
	}
	if err := lease.Validate(now); err != nil || lease.Handle != req.Handle || lease.Operation != req.Operation || lease.ExpiresAt.After(now.Add(req.TTL)) {
		result.Evidence.Reason = "custody_returned_unbounded_lease"
		return result, fmt.Errorf("%w: custody returned an invalid or widened lease", ErrBootstrapDenied)
	}
	result.Lease = lease
	result.Ready = true
	result.Evidence = Evidence{Workload: identity.Subject(), Tenant: req.Tenant, Destination: req.Destination, Operation: req.Operation, IssuedAt: now, ExpiresAt: lease.ExpiresAt.UTC(), Outcome: "granted"}
	return result, nil
}

func validateRequest(req Request) error {
	if err := req.Handle.Validate(); err != nil {
		return fmt.Errorf("%w: handle: %v", ErrInvalidBootstrap, err)
	}
	if req.Tenant == "" || strings.TrimSpace(req.Tenant) != req.Tenant || req.Region == "" || strings.TrimSpace(req.Region) != req.Region || req.Purpose == "" || strings.TrimSpace(req.Purpose) != req.Purpose || req.Destination == "" || strings.TrimSpace(req.Destination) != req.Destination {
		return fmt.Errorf("%w: tenant, region, purpose, and destination are required", ErrInvalidBootstrap)
	}
	if req.Operation == "" {
		return fmt.Errorf("%w: operation is required", ErrInvalidBootstrap)
	}
	if req.TTL <= 0 || req.TTL > MaxBootstrapLeaseTTL {
		return fmt.Errorf("%w: lease TTL must be positive and at most %s", ErrInvalidBootstrap, MaxBootstrapLeaseTTL)
	}
	return nil
}

// Explain is intentionally static and secret-free for diagnostics.
func Explain() string {
	return "secret-zero bootstrap: verified mTLS identity to bounded custody reference lease"
}
