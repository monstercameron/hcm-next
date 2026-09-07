package secrets

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var resolveNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// recordingPort is a custody port stand-in. It is deliberately incapable of
// returning a value: the port has no operation that could.
type recordingPort struct {
	calls    int
	lastCtx  custody.Context
	lastOp   custody.Operation
	lastObj  custody.Handle
	lease    custody.Lease
	err      error
	override func(custody.Lease) custody.Lease
}

func (p *recordingPort) IssueLease(ctx custody.Context, object custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	p.calls++
	p.lastCtx, p.lastObj, p.lastOp = ctx, object, op
	if p.err != nil {
		return custody.Lease{}, p.err
	}
	lease := custody.Lease{
		ID: "lease-1", Handle: object, Operation: op,
		ExpiresAt: resolveNow.Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext),
	}
	if p.override != nil {
		lease = p.override(lease)
	}
	p.lease = lease
	return lease, nil
}

func resolveContext() custody.RequestContext {
	return custody.RequestContext{
		Workload: "payroll-connector", Tenant: "tenant-a", Region: "us-east",
		Purpose: "payroll_disbursement", Destination: "bank-api",
	}
}

func resolvePolicy(t testing.TB) *AccessPolicy {
	t.Helper()
	p, err := NewAccessPolicy(AccessGrant{
		GrantID: "grant-1", SecretID: "sec_123", Workload: "payroll-connector",
		Tenant: "tenant-a", Region: "us-east", Purpose: "payroll_disbursement",
		Destination: "bank-api", Operations: []custody.Operation{custody.LeaseOperation, custody.Sign},
	})
	if err != nil {
		t.Fatalf("NewAccessPolicy: %v", err)
	}
	return p
}

func newResolver(t testing.TB, port Port) *Resolver {
	t.Helper()
	r, err := NewResolver(resolvePolicy(t), port, func() time.Time { return resolveNow })
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return r
}

// TestTodo_TRUST_015_ConsumersResolveReferencesAtUseTime is the GREEN clause:
// a consumer holds a reference, resolves it at the moment of use against an
// access policy, and receives a bounded authorization plus evidence that
// carries only the version and the reference.
func TestTodo_TRUST_015_ConsumersResolveReferencesAtUseTime(t *testing.T) {
	port := &recordingPort{}
	res, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, 5*time.Minute)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ReferenceID != "sec_123" || res.Version != "v3" || res.GrantID != "grant-1" {
		t.Fatalf("resolution did not carry the reference and version: %+v", res)
	}
	if res.LeaseID == "" || !res.ExpiresAt.After(resolveNow) || res.Operation != custody.Sign {
		t.Fatalf("resolution is not a bounded authorization: %+v", res)
	}
	if res.ContextDigest != custody.ContextDigest(resolveContext()) {
		t.Fatal("resolution is not bound to the request context")
	}

	// The port saw the bound request, and only the bound request.
	if port.calls != 1 || port.lastOp != custody.Sign || port.lastObj.Version != "v3" || port.lastObj.Kind != custody.Secret {
		t.Fatalf("the custody port was called %d times with %+v", port.calls, port.lastObj)
	}
	if port.lastCtx.Purpose != "payroll_disbursement" || port.lastCtx.Destination != "bank-api" {
		t.Fatalf("the custody port lost the purpose or destination: %+v", port.lastCtx)
	}

	// Evidence records version and reference, never a value, and survives the
	// redaction checker unchanged.
	evidence := res.Evidence()
	if evidence["reference"] != "sec_123" || evidence["version"] != "v3" || evidence["kind"] != string(APICredential) {
		t.Fatalf("evidence lost the reference or version: %#v", evidence)
	}
	if err := Check(evidence); err != nil {
		t.Fatalf("resolution evidence failed redaction: %v", err)
	}
	if err := Check(res); err != nil {
		t.Fatalf("the resolution itself failed redaction: %v", err)
	}
	if err := ResolutionCarriesNoMaterial(); err != nil {
		t.Fatalf("the resolution type can carry material: %v", err)
	}
	if strings.Contains(res.String(), "kv/tenant-a/payroll") {
		t.Fatalf("the log line echoed the provider locator: %s", res.String())
	}
}

// TestTodo_TRUST_015_ResolutionIsAuthorizedPerUse proves the resolution is
// authorized, not merely shaped: every dimension of the grant is load bearing
// and a miss on any of them is refused before the custody port is reached.
func TestTodo_TRUST_015_ResolutionIsAuthorizedPerUse(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*custody.RequestContext, *SecretReference, *custody.Operation)
		want   error
	}{
		{"another workload", func(c *custody.RequestContext, _ *SecretReference, _ *custody.Operation) { c.Workload = "hr-connector" }, ErrNotAuthorized},
		{"another purpose", func(c *custody.RequestContext, _ *SecretReference, _ *custody.Operation) { c.Purpose = "analytics" }, ErrNotAuthorized},
		{"another destination", func(c *custody.RequestContext, _ *SecretReference, _ *custody.Operation) {
			c.Destination = "attacker-api"
		}, ErrNotAuthorized},
		{"another tenant", func(c *custody.RequestContext, r *SecretReference, _ *custody.Operation) {
			c.Tenant, r.Tenant = "tenant-b", "tenant-b"
		}, ErrNotAuthorized},
		{"another region", func(c *custody.RequestContext, r *SecretReference, _ *custody.Operation) {
			c.Region, r.Region = "eu-west", "eu-west"
		}, ErrNotAuthorized},
		{"another secret", func(_ *custody.RequestContext, r *SecretReference, _ *custody.Operation) { r.ID = "sec_999" }, ErrNotAuthorized},
		{"an ungranted operation", func(_ *custody.RequestContext, _ *SecretReference, o *custody.Operation) { *o = custody.Decrypt }, ErrNotAuthorized},
		{"an administration operation", func(_ *custody.RequestContext, _ *SecretReference, o *custody.Operation) { *o = custody.Rotate }, ErrNotAuthorized},
		{"a tenant the context does not match", func(c *custody.RequestContext, _ *SecretReference, _ *custody.Operation) { c.Tenant = "tenant-b" }, ErrNotAuthorized},
		{"a disabled version", func(_ *custody.RequestContext, r *SecretReference, _ *custody.Operation) { r.State = Disabled }, ErrUnusableVersion},
		{"a destroyed version", func(_ *custody.RequestContext, r *SecretReference, _ *custody.Operation) { r.State = Destroyed }, ErrUnusableVersion},
		{"a version that does not exist yet", func(_ *custody.RequestContext, r *SecretReference, _ *custody.Operation) { r.State = Requested }, ErrUnusableVersion},
		{"a reference that is not reference-shaped", func(_ *custody.RequestContext, r *SecretReference, _ *custody.Operation) {
			r.ProviderPath = "kv?password=leaked"
		}, ErrInvalidReference},
		{"a context missing its purpose", func(c *custody.RequestContext, _ *SecretReference, _ *custody.Operation) { c.Purpose = "" }, custody.ErrInvalidContext},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &recordingPort{}
			rctx, ref, op := resolveContext(), validReference(), custody.Sign
			tc.mutate(&rctx, &ref, &op)
			if _, err := newResolver(t, port).Resolve(rctx, ref, op, time.Minute); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if port.calls != 0 {
				t.Fatalf("an unauthorized use reached the custody port %d times", port.calls)
			}
		})
	}

	// Rotation overlap stays usable so a consumer is not broken mid-campaign.
	for _, state := range []State{Active, Rotating, ActiveNew} {
		ref := validReference()
		ref.State = state
		if _, err := newResolver(t, &recordingPort{}).Resolve(resolveContext(), ref, custody.Sign, time.Minute); err != nil {
			t.Fatalf("state %s was refused: %v", state, err)
		}
	}
}

// TestTodo_TRUST_015_ProviderBehaviourStaysBehindTheCustodyPort is the
// REFACTOR clause: the resolver talks to one narrow port, refuses a lease the
// provider widened or redirected, and has no way to receive a value.
func TestTodo_TRUST_015_ProviderBehaviourStaysBehindTheCustodyPort(t *testing.T) {
	t.Run("a provider that redirects or widens the lease is refused", func(t *testing.T) {
		cases := []struct {
			name     string
			override func(custody.Lease) custody.Lease
		}{
			{"another secret", func(l custody.Lease) custody.Lease { l.Handle.ID = "sec_999"; return l }},
			{"another version", func(l custody.Lease) custody.Lease { l.Handle.Version = "v9"; return l }},
			{"another operation", func(l custody.Lease) custody.Lease { l.Operation = custody.Decrypt; return l }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				port := &recordingPort{override: tc.override}
				if _, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute); !errors.Is(err, ErrNotAuthorized) {
					t.Fatalf("error = %v, want ErrNotAuthorized", err)
				}
			})
		}
	})

	t.Run("an already-expired lease is refused", func(t *testing.T) {
		port := &recordingPort{override: func(l custody.Lease) custody.Lease {
			l.ExpiresAt = resolveNow.Add(-time.Second)
			return l
		}}
		if _, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute); !errors.Is(err, custody.ErrExpired) {
			t.Fatalf("error = %v, want custody.ErrExpired", err)
		}
	})

	t.Run("a provider failure surfaces as itself and yields no resolution", func(t *testing.T) {
		port := &recordingPort{err: custody.ErrDenied}
		res, err := newResolver(t, port).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute)
		if !errors.Is(err, custody.ErrDenied) {
			t.Fatalf("error = %v, want custody.ErrDenied", err)
		}
		if res != (Resolution{}) {
			t.Fatalf("a failed resolution returned data: %+v", res)
		}
	})

	t.Run("a real custody provider satisfies the port", func(t *testing.T) {
		// The port is the custody interface narrowed to one method, so any
		// custody.Provider is usable without this package knowing the vault.
		var provider custody.Provider
		var _ Port = portOf(provider)
	})

	t.Run("the resolver cannot be built without a policy or a port", func(t *testing.T) {
		if _, err := NewResolver(nil, &recordingPort{}, nil); !errors.Is(err, ErrInvalidResolver) {
			t.Fatalf("a resolver was built without a policy: %v", err)
		}
		if _, err := NewResolver(resolvePolicy(t), nil, nil); !errors.Is(err, ErrInvalidResolver) {
			t.Fatalf("a resolver was built without a custody port: %v", err)
		}
		if _, err := newResolver(t, &recordingPort{}).Resolve(resolveContext(), validReference(), custody.Sign, 0); !errors.Is(err, ErrInvalidResolver) {
			t.Fatalf("a lease with no duration was accepted: %v", err)
		}
	})
}

// portOf narrows a custody.Provider to the resolver's port. It exists to make
// the assignability a compile-time fact in the test above.
func portOf(p custody.Provider) Port {
	if p == nil {
		return nil
	}
	return p
}

// TestTodo_TRUST_015_AccessPolicyHasNoWildcards proves an underspecified
// grant is rejected at construction rather than read as "anything".
func TestTodo_TRUST_015_AccessPolicyHasNoWildcards(t *testing.T) {
	base := AccessGrant{
		GrantID: "g", SecretID: "sec_123", Workload: "w", Tenant: "t", Region: "r",
		Purpose: "p", Destination: "d", Operations: []custody.Operation{custody.Sign},
	}
	blanks := []struct {
		name   string
		mutate func(*AccessGrant)
	}{
		{"grant id", func(g *AccessGrant) { g.GrantID = "" }},
		{"secret id", func(g *AccessGrant) { g.SecretID = "" }},
		{"workload", func(g *AccessGrant) { g.Workload = "" }},
		{"tenant", func(g *AccessGrant) { g.Tenant = "" }},
		{"region", func(g *AccessGrant) { g.Region = "" }},
		{"purpose", func(g *AccessGrant) { g.Purpose = "" }},
		{"destination", func(g *AccessGrant) { g.Destination = "" }},
		{"operations", func(g *AccessGrant) { g.Operations = nil }},
		{"padded workload", func(g *AccessGrant) { g.Workload = " w " }},
		{"an administration operation", func(g *AccessGrant) { g.Operations = []custody.Operation{custody.Revoke} }},
		{"an unknown operation", func(g *AccessGrant) { g.Operations = []custody.Operation{custody.Operation("exfiltrate")} }},
	}
	for _, tc := range blanks {
		t.Run(tc.name, func(t *testing.T) {
			g := base
			tc.mutate(&g)
			if _, err := NewAccessPolicy(g); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("grant with no %s was accepted: %v", tc.name, err)
			}
		})
	}
	if _, err := NewAccessPolicy(base, base); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("a duplicated grant id was accepted: %v", err)
	}
	// A nil policy authorizes nothing.
	var nilPolicy *AccessPolicy
	if _, ok := nilPolicy.Authorize(validReference(), resolveContext(), custody.Sign); ok {
		t.Fatal("a nil access policy authorized a use")
	}
	// The frozen policy does not alias the caller's operation slice.
	ops := []custody.Operation{custody.Sign}
	g := base
	g.Operations = ops
	policy, err := NewAccessPolicy(g)
	if err != nil {
		t.Fatalf("NewAccessPolicy: %v", err)
	}
	ops[0] = custody.Decrypt
	ref := validReference()
	rctx := custody.RequestContext{Workload: "w", Tenant: "t", Region: "r", Purpose: "p", Destination: "d"}
	ref.Tenant, ref.Region = "t", "r"
	if _, ok := policy.Authorize(ref, rctx, custody.Decrypt); ok {
		t.Fatal("mutating the caller's slice widened a frozen grant")
	}
	if _, ok := policy.Authorize(ref, rctx, custody.Sign); !ok {
		t.Fatal("the frozen grant lost its own operation")
	}
}

// FuzzTodo_TRUST_015_Resolve treats the request context and the reference as
// untrusted. Resolution may refuse anything, but it must never panic and
// never produce a resolution that the policy did not authorize.
func FuzzTodo_TRUST_015_Resolve(f *testing.F) {
	f.Add("payroll-connector", "payroll_disbursement", "bank-api", "sec_123", "v3")
	f.Add("", "", "", "", "")
	f.Add("payroll-connector\n", "payroll_disbursement", "bank-api", "sec_123", "v3")

	f.Fuzz(func(t *testing.T, workload, purpose, destination, secretID, version string) {
		for _, s := range []string{workload, purpose, destination, secretID, version} {
			if len(s) > 256 {
				return
			}
		}
		port := &recordingPort{}
		rctx := resolveContext()
		rctx.Workload, rctx.Purpose, rctx.Destination = workload, purpose, destination
		ref := validReference()
		ref.ID, ref.Version = secretID, version

		res, err := newResolver(t, port).Resolve(rctx, ref, custody.Sign, time.Minute)
		if err != nil {
			return
		}
		if res.GrantID != "grant-1" || res.ReferenceID != "sec_123" {
			t.Fatalf("an unauthorized resolution succeeded: %+v", res)
		}
		if workload != "payroll-connector" || purpose != "payroll_disbursement" || destination != "bank-api" {
			t.Fatalf("a mismatched context resolved: %q/%q/%q", workload, purpose, destination)
		}
		if err := Check(res); err != nil {
			t.Fatalf("a successful resolution failed redaction: %v", err)
		}
	})
}

// TestTodo_TRUST_015_ResolutionEvidenceNeverCarriesAValue is the security
// property stated as its own test: no arrangement of a resolution, its
// evidence or its log line discloses material, and a resolution built around
// a value-bearing field is caught by the checker.
func TestTodo_TRUST_015_ResolutionEvidenceNeverCarriesAValue(t *testing.T) {
	res, err := newResolver(t, &recordingPort{}).Resolve(resolveContext(), validReference(), custody.Sign, time.Minute)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rendered := fmt.Sprintf("%s %v %+v", res.String(), res.Evidence(), res)
	for _, forbidden := range []string{"sk-test", "-----BEGIN", "password="} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("a rendered resolution disclosed %q", forbidden)
		}
	}
	// A hypothetical consumer that attached a value alongside the resolution
	// is caught before it can be persisted.
	snapshot := map[string]any{"resolution": res, "api_key": "sk-test-abcdefghijklmnop"}
	if err := Check(snapshot); err == nil {
		t.Fatal("a snapshot pairing a resolution with a raw value was accepted")
	}
}
