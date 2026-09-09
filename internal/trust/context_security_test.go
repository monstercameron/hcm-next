package trust

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func contextPrincipal(t *testing.T) *Principal {
	t.Helper()
	p, err := NewPrincipal(PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "subject-1", SubjectKind: SubjectKindHuman,
		OrganizationScopeID: "org-a", Purposes: []string{"z-purpose", "a-purpose"},
		AuthenticationMethod: AuthenticationMethodBearerToken, Assurance: AssuranceHigh,
		SessionRef: "session-1", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0),
		CredentialDigest: "cred:sha256:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWithPrincipal_FromContext_RoundTripAndNilIsNoOp(t *testing.T) {
	p := contextPrincipal(t)
	base := context.Background()
	if got := WithPrincipal(base, nil); got != base {
		t.Fatal("nil principal changed the context")
	}
	ctx := WithPrincipal(base, p)
	got, ok := FromContext(ctx)
	if !ok || got != p {
		t.Fatalf("FromContext = (%p, %v), want (%p, true)", got, ok, p)
	}
	got, err := MustFromContext(ctx)
	if err != nil || got != p {
		t.Fatalf("MustFromContext = (%p, %v), want (%p, nil)", got, err, p)
	}
	if _, ok := FromContext(base); ok {
		t.Fatal("empty context unexpectedly carried a principal")
	}
}

func TestMustFromContext_MissingReturnsSentinel(t *testing.T) {
	got, err := MustFromContext(context.Background())
	if got != nil || !errors.Is(err, ErrNoPrincipal) {
		t.Fatalf("MustFromContext = (%p, %v), want nil/ErrNoPrincipal", got, err)
	}
}
