package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// TestTodo_ADMIN_008_Security proves GetWorkflowInstance inherits the exact
// same admission and route-policy boundary every other AdminService method
// does (it is already covered by TestTodo_ADMIN_001's "every method requires
// the operator role" table), plus the two conditions specific to composing a
// database-backed method behind an optional Dependencies port: an operator
// gets a typed UNAVAILABLE, never a panic or a raw driver error, when the
// workflow executor is not configured, and a syntactically invalid instance
// id is refused before any query runs.
func TestTodo_ADMIN_008_Security(t *testing.T) {
	conn, cleanup := startTestServer(t, admin.Dependencies{})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("an operator caller gets UNAVAILABLE, not a panic, when the workflow executor is unconfigured", func(t *testing.T) {
		_, err := client.GetWorkflowInstance(withToken(ctx, fixtureOperatorToken), &adminv1.GetWorkflowInstanceRequest{
			InstanceId: uuid.New().String(),
		})
		owned := assertOwnedCode(t, err, envelope.CodeUnavailable)
		if owned.EvidenceRef().ID == "" {
			t.Fatal("an unavailable-port result must still carry the authentication evidence reference")
		}
	})

	t.Run("a malformed instance id is refused as INVALID_ARGUMENT before any query runs", func(t *testing.T) {
		_, err := client.GetWorkflowInstance(withToken(ctx, fixtureOperatorToken), &adminv1.GetWorkflowInstanceRequest{
			InstanceId: "not-a-uuid",
		})
		assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	})

	t.Run("an unauthenticated caller is rejected before the operator check even runs", func(t *testing.T) {
		_, err := client.GetWorkflowInstance(ctx, &adminv1.GetWorkflowInstanceRequest{InstanceId: uuid.New().String()})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	t.Run("an ordinary-user credential is refused PERMISSION_DENIED, not UNAVAILABLE", func(t *testing.T) {
		// The route-policy gate (internal/operations/admin.RequireOperator)
		// must run, and refuse, before this method ever looks at whether its
		// own optional port is configured: an unconfigured Dependencies field
		// must never be what tells a non-operator caller "you were close."
		_, err := client.GetWorkflowInstance(withToken(ctx, fixtureOrdinaryToken), &adminv1.GetWorkflowInstanceRequest{
			InstanceId: uuid.New().String(),
		})
		assertOwnedCode(t, err, envelope.CodePermissionDenied)
	})
}
