package admin_test

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	admin "github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
)

// TestTodo_SVC_011_Integration starts the admin server on a loopback
// listener and drives ListIntents through it end to end, proving the
// admin-local wire shape and the reused transport.IntentHandler port stay
// in agreement (SVC-011's REFACTOR: "CLI and GWC/HTTP use the same API
// contracts and authorization decisions"). SVC-011's other required cases -
// PRIMARY, GOLDEN, SECURITY and MUTATION - live in
// internal/operations/admin as pure, network-free tests over the policy
// package this transport adapter calls into (RequireOperator, ParseFields,
// ParseSections, the authorization-decision constructors): that package,
// not this one, is where SVC-011's "CLI contains business/store logic" and
// "operator methods inherit ordinary-user authority" RED conditions are
// structurally impossible to reintroduce without touching a different
// file's worth of code, which is what a pure-package test can assert
// without a network round trip.
func TestTodo_SVC_011_Integration(t *testing.T) {
	handler := &fakeIntentHandler{}
	conn, cleanup := startTestServer(t, admin.Dependencies{Intent: handler})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.ListIntents(withToken(ctx, fixtureOperatorToken), &adminv1.ListIntentsRequest{}); err != nil {
		t.Fatalf("ListIntents: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("reused intent handler called %d times, want exactly 1", handler.calls)
	}
}
