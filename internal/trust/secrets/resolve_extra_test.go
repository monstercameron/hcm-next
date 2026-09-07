package secrets

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

func TestAccessPolicy_OrdersGrantsAndAuthorizeChecksExactDimensions(t *testing.T) {
	base := AccessGrant{SecretID: "sec_123", Workload: "w", Tenant: "t", Region: "r", Purpose: "p", Destination: "d", Operations: []custody.Operation{custody.Sign}}
	low := base
	low.GrantID = "a"
	high := base
	high.GrantID = "z"
	policy, err := NewAccessPolicy(high, low)
	if err != nil {
		t.Fatal(err)
	}
	ref := validReference()
	ref.Tenant, ref.Region = "t", "r"
	rctx := custody.RequestContext{Workload: "w", Tenant: "t", Region: "r", Purpose: "p", Destination: "d"}
	if grant, ok := policy.Authorize(ref, rctx, custody.Sign); !ok || grant != "a" {
		t.Fatalf("Authorize = %q, %v, want sorted first grant", grant, ok)
	}
	for _, mutate := range []func(*SecretReference, *custody.RequestContext, *custody.Operation){
		func(r *SecretReference, _ *custody.RequestContext, _ *custody.Operation) { r.ID = "other" },
		func(r *SecretReference, _ *custody.RequestContext, _ *custody.Operation) { r.Tenant = "other" },
		func(r *SecretReference, _ *custody.RequestContext, _ *custody.Operation) { r.Region = "other" },
		func(_ *SecretReference, c *custody.RequestContext, _ *custody.Operation) { c.Workload = "other" },
		func(_ *SecretReference, c *custody.RequestContext, _ *custody.Operation) { c.Tenant = "other" },
		func(_ *SecretReference, c *custody.RequestContext, _ *custody.Operation) { c.Region = "other" },
		func(_ *SecretReference, c *custody.RequestContext, _ *custody.Operation) { c.Purpose = "other" },
		func(_ *SecretReference, c *custody.RequestContext, _ *custody.Operation) { c.Destination = "other" },
		func(_ *SecretReference, _ *custody.RequestContext, o *custody.Operation) { *o = custody.Decrypt },
	} {
		mutatedRef, mutatedContext, op := ref, rctx, custody.Sign
		mutate(&mutatedRef, &mutatedContext, &op)
		if _, ok := policy.Authorize(mutatedRef, mutatedContext, op); ok {
			t.Fatalf("mismatched policy dimensions authorized: ref=%+v context=%+v op=%s", mutatedRef, mutatedContext, op)
		}
	}
}

func TestResolution_EvidenceStringAndResolver_RejectForgedLeases(t *testing.T) {
	port := &recordingPort{override: func(l custody.Lease) custody.Lease {
		l.ContextDigest = [32]byte{1}
		return l
	}}
	if _, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("forged context digest = %v, want ErrNotAuthorized", err)
	}
	for _, mutate := range []func(*custody.Lease){
		func(l *custody.Lease) { l.Handle.Tenant = "tenant-b" },
		func(l *custody.Lease) { l.Handle.Region = "eu-west" },
		func(l *custody.Lease) { l.Handle.Kind = custody.Key },
	} {
		port := &recordingPort{override: func(l custody.Lease) custody.Lease { mutate(&l); return l }}
		if _, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("forged lease handle = %v, want ErrNotAuthorized", err)
		}
	}
	for _, mutate := range []func(*custody.Lease){
		func(l *custody.Lease) { l.ID = "" },
		func(l *custody.Lease) { l.ExpiresAt = resolveNow },
		func(l *custody.Lease) { l.Operation = "unknown" },
		func(l *custody.Lease) { l.Handle = custody.Handle{} },
	} {
		port := &recordingPort{override: func(l custody.Lease) custody.Lease { mutate(&l); return l }}
		if _, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute); !errors.Is(err, custody.ErrInvalidLease) && !errors.Is(err, custody.ErrExpired) {
			t.Fatalf("malformed lease = %v, want custody lease error", err)
		}
	}
	res, err := newResolver(t, &recordingPort{}).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	evidence := res.Evidence()
	for _, key := range []string{"reference", "version", "kind", "provider", "grant", "operation", "lease", "expires_at", "context_digest"} {
		if evidence[key] == "" {
			t.Fatalf("Evidence missing %q: %#v", key, evidence)
		}
	}
	if evidence["reference"] != res.ReferenceID || evidence["operation"] != string(res.Operation) || res.String() == "" {
		t.Fatalf("resolution rendering = %q / %#v", res.String(), evidence)
	}
}
