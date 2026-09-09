package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

type wideningBootstrapProvider struct{ bootstrapProvider }

func (p *wideningBootstrapProvider) IssueLease(ctx custody.Context, handle custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "widened", Handle: handle, Operation: op, ExpiresAt: p.now.Add(ttl + time.Second), ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func TestNew_RejectsMissingDependenciesAndExplainIsStable(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, false)
	if _, err := New(nil, b.custody, nil); !errors.Is(err, ErrInvalidBootstrap) {
		t.Fatalf("nil verifier error = %v", err)
	}
	if _, err := New(b.verifier, nil, nil); !errors.Is(err, ErrInvalidBootstrap) {
		t.Fatalf("nil provider error = %v", err)
	}
	if got := Explain(); got == "" || got != "secret-zero bootstrap: verified mTLS identity to bounded custody reference lease" {
		t.Fatalf("Explain() = %q", got)
	}
	var nilBootstrap *Bootstrapper
	result, err := nilBootstrap.Acquire(context.Background(), peer, Request{Handle: handle})
	if !errors.Is(err, ErrNotReady) || result.Ready || result.Lease.ID != "" {
		t.Fatalf("nil Bootstrapper.Acquire = %+v, %v", result, err)
	}
}

func TestValidateRequest_RejectsMissingCoordinatesAndTTLBoundaries(t *testing.T) {
	valid := Request{Handle: custody.Handle{ID: "h", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "us-east"}, Tenant: "tenant-a", Region: "us-east", Purpose: "read", Destination: "provider", Operation: custody.LeaseOperation, TTL: time.Minute}
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"invalid handle", func(r *Request) { r.Handle = custody.Handle{} }},
		{"blank tenant", func(r *Request) { r.Tenant = "" }},
		{"padded region", func(r *Request) { r.Region = " us-east" }},
		{"blank purpose", func(r *Request) { r.Purpose = "" }},
		{"padded destination", func(r *Request) { r.Destination = "provider " }},
		{"missing operation", func(r *Request) { r.Operation = "" }},
		{"zero ttl", func(r *Request) { r.TTL = 0 }},
		{"ttl too large", func(r *Request) { r.TTL = MaxBootstrapLeaseTTL + time.Nanosecond }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)
			if err := validateRequest(req); !errors.Is(err, ErrInvalidBootstrap) {
				t.Fatalf("validateRequest = %v, want ErrInvalidBootstrap", err)
			}
		})
	}
}

func TestAcquire_RejectsTenantMismatchAndWidenedCustodyLease(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, false)
	valid := Request{Handle: handle, Tenant: "tenant-a", Region: "us-east", Purpose: "read", Destination: "provider", Operation: custody.LeaseOperation, TTL: time.Minute}
	valid.Handle.Tenant = "tenant-b"
	result, err := b.Acquire(context.Background(), peer, valid)
	if !errors.Is(err, ErrBootstrapDenied) || result.Ready || result.Evidence.Reason != "tenant_scope_mismatch" {
		t.Fatalf("tenant mismatch = %+v, %v", result, err)
	}
	b.custody = &wideningBootstrapProvider{bootstrapProvider{now: now}}
	valid.Handle = handle
	result, err = b.Acquire(context.Background(), peer, valid)
	if !errors.Is(err, ErrBootstrapDenied) || result.Ready || result.Lease.ID != "" || result.Evidence.Reason != "custody_returned_unbounded_lease" {
		t.Fatalf("widened lease = %+v, %v", result, err)
	}
}
