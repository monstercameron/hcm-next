package admin_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// leakyWorkerFacts returns a raw failure carrying provider-shaped text
// (a fake SQL error), so [TestTodo_ADMIN_001_Security] can prove that text
// never reaches the wire.
type leakyWorkerFacts struct{}

const leakySecret = `pq: password authentication failed for user "hcmnext" select * from workers`

func (leakyWorkerFacts) WorkerFactsAt(context.Context, people.FactQuery) (people.FactSet, error) {
	return people.FactSet{}, errFmt(leakySecret)
}

// errFmt avoids importing "errors" or "fmt" a second time just for one
// sentinel; a plain string error is enough to carry the unsafe text.
type errFmt string

func (e errFmt) Error() string { return string(e) }

// TestTodo_ADMIN_001_Security proves three of SVC-011/ADMIN-001's RED
// conditions at once: (1) a request that tries to select its own trusted
// context (tenant, scope, purpose, roles - the reserved metadata names
// internal/trust.ReservedMetadataKeys enumerates) is rejected before any
// admin method runs, on the identical shared admission path every other
// service on the process uses; (2) a capability failure's raw, unsafe
// diagnostic text never crosses the wire, even wrapped inside an owned
// error; (3) AdminService exposes exactly its five declared read-only
// methods and nothing that could mutate workforce data.
func TestTodo_ADMIN_001_Security(t *testing.T) {
	conn, cleanup := startTestServer(t, admin.Dependencies{WorkerFacts: leakyWorkerFacts{}})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("a caller-selected trusted-context header is rejected before authentication evidence exists", func(t *testing.T) {
		reserved := metadata.AppendToOutgoingContext(withToken(ctx, fixtureOperatorToken),
			"x-hcm-roles", admin.OperatorRole)
		_, err := client.GetReleaseManifest(reserved, &adminv1.GetReleaseManifestRequest{})
		owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
		found := false
		for _, v := range owned.Violations() {
			if strings.HasPrefix(v.FieldPath, "metadata.x-hcm-roles") {
				found = true
			}
		}
		if !found {
			t.Fatalf("violations = %+v, want one naming metadata.x-hcm-roles", owned.Violations())
		}
	})

	t.Run("every reserved trusted-context key is screened, not just one", func(t *testing.T) {
		for _, key := range trust.ReservedMetadataKeys()[:5] {
			key := key
			t.Run(key, func(t *testing.T) {
				reserved := metadata.AppendToOutgoingContext(withToken(ctx, fixtureOperatorToken), key, "smuggled-value")
				_, err := client.GetReleaseManifest(reserved, &adminv1.GetReleaseManifestRequest{})
				assertOwnedCode(t, err, envelope.CodeInvalidArgument)
			})
		}
	})

	t.Run("a raw provider-shaped diagnostic never crosses the wire", func(t *testing.T) {
		_, err := client.GetWorkerState(withToken(ctx, fixtureOperatorToken), &adminv1.GetWorkerStateRequest{
			WorkerId: "11111111-1111-4111-8111-111111111111",
			Fields:   []string{"worker.worker_number"},
		})
		if err == nil {
			t.Fatal("expected the leaky reader to fail the call")
		}
		if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "select ") || strings.Contains(err.Error(), "pq:") {
			t.Fatalf("the client-visible error leaked unsafe diagnostic text: %v", err)
		}
	})

	t.Run("AdminService publishes exactly its five declared read-only methods", func(t *testing.T) {
		desc := adminv1.AdminService_ServiceDesc
		if len(desc.Methods) != 5 {
			t.Fatalf("service publishes %d methods, want exactly 5: %v", len(desc.Methods), methodNames(desc))
		}
		wantMutating := []string{"Create", "Submit", "Cancel", "Supersede", "Update", "Delete", "Execute", "Approve", "Reject"}
		for _, m := range desc.Methods {
			for _, prefix := range wantMutating {
				if strings.HasPrefix(m.MethodName, prefix) {
					t.Fatalf("method %s looks mutating (prefix %q); AdminService may only read, simulate or explain", m.MethodName, prefix)
				}
			}
		}
	})
}

func methodNames(desc grpc.ServiceDesc) []string {
	names := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		names = append(names, m.MethodName)
	}
	return names
}
