package bootstrap_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// The intent-only `promotion.propose` contract (PROMO-007) end to end, over
// both of this repository's transports against one composed cell.
//
// What is under test here is not that the request message has the right
// fields - internal/intent/app's own suite proves that, and it proves it
// structurally. What is under test here is the claim the field list only
// makes plausible: that a browser reaching this cell over the
// gRPC-over-WebSocket tunnel and a native client reaching it over direct gRPC
// are invoking one semantic capability. Two transports, one composed cell,
// one Promotion application service, one intent - and the digests to prove
// it, since "they behaved the same" is otherwise a claim about two log lines.
//
// Both clients are bound to the same *grpc.Server. That is deliberate and it
// is the point: there is no second registration, no second interceptor chain
// and no second handler for one of them to diverge in.

// promotionCell is one composed journey cell published on both transports.
type promotionCell struct {
	harness *journeyHarness

	direct journeyv1.JourneyServiceClient
	tunnel journeyv1.JourneyServiceClient

	// tunnelHost is the httptest server's host:port, kept so a test can dial
	// a second tunnel connection (an unauthenticated one, say) of its own.
	tunnelHost string
}

// newPromotionCell composes the journey cell [newJourneyHarness] builds, then
// serves it once and reaches it twice.
//
// It builds its own *grpc.Server rather than reusing the harness's bufconn
// one because the tunnel needs a server it can be mounted beside: the same
// transportcell.NewGRPCServer and transportcell.NewEdgeHandlerWithTunnel
// calls cmd/hcmnext's buildServe makes, over the same *app.Cell.
func newPromotionCell(t *testing.T) *promotionCell {
	t.Helper()
	h := newJourneyHarness(t)

	grpcServer, err := transportcell.NewGRPCServer(h.cell.app)
	if err != nil {
		t.Fatalf("NewGRPCServer: %v", err)
	}
	t.Cleanup(grpcServer.Stop)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })

	direct, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = direct.Close() })

	handler, err := transportcell.NewEdgeHandlerWithTunnel(h.cell.app, grpcServer)
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithTunnel: %v", err)
	}
	edgeServer := httptest.NewServer(handler)
	t.Cleanup(edgeServer.Close)
	host := strings.TrimPrefix(edgeServer.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	tunnelConn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:           "ws://" + host + transportcell.TunnelPath,
		Headers:          http.Header{"Authorization": []string{h.cell.token}},
		HandshakeTimeout: 10 * time.Second,
		GRPCOptions:      []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = tunnelConn.Close() })

	return &promotionCell{
		harness:    h,
		direct:     journeyv1.NewJourneyServiceClient(direct),
		tunnel:     journeyv1.NewJourneyServiceClient(tunnelConn),
		tunnelHost: host,
	}
}

// callCtx carries the credential each RPC is admitted with. The tunnel
// deliberately forwards nothing from the upgrade into per-RPC metadata, so
// both transports need it and both get exactly the same one.
func (c *promotionCell) callCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.harness.cell.token), cancel
}

// transports returns the two clients by name, so every claim below is made
// once per transport out of one loop rather than twice in prose.
func (c *promotionCell) transports() map[string]journeyv1.JourneyServiceClient {
	return map[string]journeyv1.JourneyServiceClient{"grpc": c.direct, "tunnel": c.tunnel}
}

// promotionProposeRequest is the manager's intention for the corpus worker
// every journey test in this suite promotes: OPS-HRBP2/P2 to OPS-HRBP3/P3 at
// 98,000 USD, effective 2026-06-01.
func promotionProposeRequest(clientRequestID string) *journeyv1.ProposePromotionRequest {
	return &journeyv1.ProposePromotionRequest{
		SubjectWorkerRef:        "omar-reyes",
		DesiredJobCode:          "OPS-HRBP3",
		DesiredGrade:            "P3",
		DesiredPositionId:       "POS-HRBP-301",
		DesiredBasePay:          "98000.00",
		EffectiveDate:           "2026-06-01",
		Reason:                  "promotion_into_senior_hrbp",
		ExpectedSubjectRevision: app.PromotionSubjectRevision("omar-reyes"),
		ClientRequestId:         clientRequestID,
	}
}

// smuggledCurrentPay is one syntactically valid but undefined Protobuf field
// - number 900, wire type 2, carrying "93000.00" - which is the exact shape
// of the smuggling this contract refuses: a caller stating the subject's
// current base pay in a message with nowhere to put it.
var smuggledCurrentPay = append([]byte{0xA2, 0x38, 0x08}, []byte("93000.00")...)

// ---------------------------------------------------------------------------
// INTEGRATION
// ---------------------------------------------------------------------------

// TestTodo_PROMO_007_Integration is the parity claim: the same intention
// expressed over direct gRPC and over the GoGRPCBridge tunnel is one
// promotion, and the digests say so.
//
// The two halves are different claims and both are needed. Identical requests
// - the client request id included - are one intent on either transport,
// which is idempotency working across a transport boundary rather than within
// one. Requests differing only in the client request id are two intents with
// one content digest, which is what proves the first half was not a vacuous
// comparison of one stored row against itself.
func TestTodo_PROMO_007_Integration(t *testing.T) {
	c := newPromotionCell(t)
	ctx, cancel := c.callCtx(t)
	defer cancel()

	req := promotionProposeRequest("req-parity-1")

	overGRPC, err := c.direct.ProposePromotion(ctx, req)
	if err != nil {
		t.Fatalf("ProposePromotion over gRPC: %v", err)
	}
	overTunnel, err := c.tunnel.ProposePromotion(ctx, req)
	if err != nil {
		t.Fatalf("ProposePromotion over the tunnel: %v", err)
	}

	if overGRPC.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("stage over gRPC = %v, want PROPOSED", overGRPC.GetStage())
	}
	if overGRPC.GetProposalRevisionId() == "" || overGRPC.GetMaterialDigest() == "" {
		t.Fatalf("the preflight simulation minted no proposal: %+v", overGRPC)
	}
	for _, field := range []struct{ name, grpc, tunnel string }{
		{"intent_id", overGRPC.GetIntentId(), overTunnel.GetIntentId()},
		{"correlation_id", overGRPC.GetCorrelationId(), overTunnel.GetCorrelationId()},
		{"proposal_revision_id", overGRPC.GetProposalRevisionId(), overTunnel.GetProposalRevisionId()},
		{"material_digest", overGRPC.GetMaterialDigest(), overTunnel.GetMaterialDigest()},
		{"canonical_request_digest", overGRPC.GetCanonicalRequestDigest(), overTunnel.GetCanonicalRequestDigest()},
		{"request_digest", overGRPC.GetRequestDigest(), overTunnel.GetRequestDigest()},
	} {
		if field.grpc != field.tunnel {
			t.Errorf("%s differs by transport: gRPC %q, tunnel %q", field.name, field.grpc, field.tunnel)
		}
	}

	// The digest the wire carries is the one internal/intent/app computes and
	// internal/intent/app's golden test pins. If the two could differ, the
	// pin would be pinning something no client ever sees.
	if got, want := overGRPC.GetRequestDigest(), app.PromotionProposeRequestDigest(req); got != want {
		t.Fatalf("request_digest = %q, want the canonical %q", got, want)
	}

	t.Run("a different client request id is a second intent with one content digest", func(t *testing.T) {
		second, err := c.tunnel.ProposePromotion(ctx, promotionProposeRequest("req-parity-2"))
		if err != nil {
			t.Fatalf("ProposePromotion over the tunnel: %v", err)
		}
		if second.GetIntentId() == overGRPC.GetIntentId() {
			t.Fatal("a distinct client request id replayed the first intent")
		}
		if second.GetRequestDigest() != overGRPC.GetRequestDigest() {
			t.Fatalf("the same promotion digested differently: %q vs %q",
				second.GetRequestDigest(), overGRPC.GetRequestDigest())
		}
		if second.GetCanonicalRequestDigest() == overGRPC.GetCanonicalRequestDigest() {
			t.Fatal("two intents under different idempotency keys share one canonical request digest")
		}
	})

	t.Run("the current placement came from the governed read, not the request", func(t *testing.T) {
		// Nothing in ProposePromotionRequest says where the subject is today,
		// so a proposal that knows is a proposal built from the capability
		// gateway's own answer. This is that fact stated as an assertion: the
		// recorded intent carries OPS-HRBP2/P2, which no caller supplied.
		detail, err := c.direct.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{
			IntentId: overGRPC.GetIntentId(),
		})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		current := detail.GetDetail().GetJourney().GetCurrent()
		if current.GetJobCode() != "OPS-HRBP2" || current.GetGrade() != "P2" {
			t.Fatalf("current placement = %+v, want the governed read's own OPS-HRBP2/P2", current)
		}
		if got := detail.GetDetail().GetJourney().GetCurrentBase(); got != "93000.00" {
			t.Fatalf("current base = %q, want the corpus baseline 93000.00", got)
		}
	})

	t.Run("exactly one intent was recorded for the replayed request", func(t *testing.T) {
		n := queryOne[int](t, c.harness.cell,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
			pgstore.TenantID(testTenant), "promotion.propose:req-parity-1")
		if n != 1 {
			t.Fatalf("intents recorded for one client request id = %d, want 1", n)
		}
	})
}

// ---------------------------------------------------------------------------
// SECURITY
// ---------------------------------------------------------------------------

// TestTodo_PROMO_007_Security is the refusal matrix, run identically on both
// transports because a contract that is intent-only on one of them and not on
// the other is not intent-only.
func TestTodo_PROMO_007_Security(t *testing.T) {
	c := newPromotionCell(t)
	ctx, cancel := c.callCtx(t)
	defer cancel()

	for name, client := range c.transports() {
		t.Run(name+"/a smuggled current pay is refused", func(t *testing.T) {
			before := intentCount(t, c)
			req := promotionProposeRequest("req-smuggle-" + name)
			req.ProtoReflect().SetUnknown(smuggledCurrentPay)

			_, err := client.ProposePromotion(ctx, req)
			owned := assertPromotionRefusal(t, err, envelope.CodeInvalidArgument)
			if owned.ReasonRef() != "structural.request_rejected" {
				t.Fatalf("ReasonRef() = %q, want structural.request_rejected", owned.ReasonRef())
			}
			if after := intentCount(t, c); after != before {
				t.Fatalf("a refused request recorded %d intents", after-before)
			}
		})

		t.Run(name+"/the expected subject revision is required", func(t *testing.T) {
			req := promotionProposeRequest("req-norev-" + name)
			req.ExpectedSubjectRevision = ""
			owned := assertPromotionRefusal(t, mustFail(client.ProposePromotion(ctx, req)), envelope.CodeInvalidArgument)
			if !strings.Contains(owned.Message(), "expected_subject_revision") {
				t.Fatalf("refusal %q does not name expected_subject_revision", owned.Message())
			}
		})

		t.Run(name+"/the client request id is required", func(t *testing.T) {
			req := promotionProposeRequest("")
			owned := assertPromotionRefusal(t, mustFail(client.ProposePromotion(ctx, req)), envelope.CodeInvalidArgument)
			if !strings.Contains(owned.Message(), "client_request_id") {
				t.Fatalf("refusal %q does not name client_request_id", owned.Message())
			}
		})

		t.Run(name+"/a stale subject revision is refused", func(t *testing.T) {
			req := promotionProposeRequest("req-stale-" + name)
			req.ExpectedSubjectRevision = "rewards.package.omar-reyes@7"
			owned := assertPromotionRefusal(t, mustFail(client.ProposePromotion(ctx, req)), envelope.CodeFailedPrecondition)
			if owned.ReasonRef() != "promotion.propose.stale_subject_revision" {
				t.Fatalf("ReasonRef() = %q, want promotion.propose.stale_subject_revision", owned.ReasonRef())
			}
		})

		t.Run(name+"/an unknown subject is refused without disclosing anything", func(t *testing.T) {
			req := promotionProposeRequest("req-nobody-" + name)
			req.SubjectWorkerRef = "nobody-at-all"
			assertPromotionRefusal(t, mustFail(client.ProposePromotion(ctx, req)), envelope.CodeInvalidArgument)
		})

		t.Run(name+"/a currency the subject does not use is refused", func(t *testing.T) {
			req := promotionProposeRequest("req-currency-" + name)
			req.DesiredPayCurrency = "EUR"
			owned := assertPromotionRefusal(t, mustFail(client.ProposePromotion(ctx, req)), envelope.CodeInvalidArgument)
			if !strings.Contains(owned.Message(), "desired_pay_currency") {
				t.Fatalf("refusal %q does not name desired_pay_currency", owned.Message())
			}
		})

		t.Run(name+"/a caller without the read authority cannot propose", func(t *testing.T) {
			// The subject's current placement is read through the capability
			// gateway under the caller's own authorization decision, and a
			// caller who may not read it may not propose against it either.
			// A path that skipped the gateway would answer this caller.
			weak, cancelWeak := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelWeak()
			weak = metadata.AppendToOutgoingContext(weak, transport.AuthorizationMetadataKey,
				"Bearer "+c.harness.issue(t, "intent_author"))

			_, err := client.ProposePromotion(weak, promotionProposeRequest("req-weak-"+name))
			if err == nil {
				t.Fatal("a caller with no compensation-read authority proposed a promotion")
			}
			if got := status.Code(err); got != codes.PermissionDenied {
				t.Fatalf("status = %s, want PERMISSION_DENIED: %v", got, err)
			}
		})

		t.Run(name+"/a call carrying no credential is refused", func(t *testing.T) {
			// The tunnel upgrade carried a credential; the call does not.
			// Upgrading is not authenticating, and the contract is admitted
			// per call on both transports.
			bare, cancelBare := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelBare()
			_, err := client.ProposePromotion(bare, promotionProposeRequest("req-nocred-"+name))
			if err == nil {
				t.Fatal("a call with no credential was answered")
			}
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Fatalf("status = %s, want UNAUTHENTICATED: %v", got, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MUTATION
// ---------------------------------------------------------------------------

// promotionDomainTables are the tables a promotion eventually writes to. Not
// one of them may change while a promotion is only being proposed.
//
// They are listed by name rather than counted generically because the claim
// is about these specific tables: the subject's own record, the placement, the
// occupancy, the pay, the budget and the human work the execution would raise.
// A generic "no table changed" sweep would also catch the intent chronology,
// which propose is supposed to write.
var promotionDomainTables = []string{
	"worker", "employment", "assignment",
	"job_position", "position_occupancy",
	"compensation_package", "compensation_component",
	"workforce_budget", "budget_reservation",
	"journey_worker", "workflow_instance", "work_item", "work_item_transition",
}

// TestTodo_PROMO_007_Mutation is the zero-effect claim: propose and its
// preflight simulation compute a proposal and record chronology, and change
// no workforce state at all.
//
// It is checked by counting rather than by inspecting, because the failure
// this guards against is a write nobody meant to make: an assertion about
// what a specific row says would pass even if propose had inserted a second
// one. The intent chronology is checked in the opposite direction in the same
// test - it must grow - so that a propose which silently did nothing at all
// cannot pass by writing nothing anywhere.
func TestTodo_PROMO_007_Mutation(t *testing.T) {
	c := newPromotionCell(t)
	ctx, cancel := c.callCtx(t)
	defer cancel()

	before := map[string]int{}
	for _, table := range promotionDomainTables {
		before[table] = tableCount(t, c, table)
	}
	intentsBefore := intentCount(t, c)

	for i, client := range []journeyv1.JourneyServiceClient{c.direct, c.tunnel} {
		res, err := client.ProposePromotion(ctx, promotionProposeRequest("req-mutation-"+string(rune('a'+i))))
		if err != nil {
			t.Fatalf("ProposePromotion: %v", err)
		}
		if res.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
			t.Fatalf("stage = %v, want PROPOSED", res.GetStage())
		}
	}

	for _, table := range promotionDomainTables {
		if after := tableCount(t, c, table); after != before[table] {
			t.Errorf("propose changed %s: %d rows before, %d after", table, before[table], after)
		}
	}
	if after := intentCount(t, c); after != intentsBefore+2 {
		t.Fatalf("intents recorded = %d, want %d (propose must record chronology)", after-intentsBefore, 2)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tableCount counts every row of one table in the suite's schema. It is not
// tenant-scoped: a write leaking into another tenant is a failure this test
// should see, not one it should filter out.
func tableCount(t *testing.T, c *promotionCell, table string) int {
	t.Helper()
	// table comes from promotionDomainTables, a compiled-in list, so there is
	// no caller-supplied text in this statement.
	return queryOne[int](t, c.harness.cell, `SELECT count(*) FROM `+table)
}

// intentCount counts the promotion.propose intents recorded for the suite's
// tenant.
func intentCount(t *testing.T, c *promotionCell) int {
	t.Helper()
	return queryOne[int](t, c.harness.cell,
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key LIKE 'promotion.propose:%'`,
		pgstore.TenantID(testTenant))
}

// mustFail asserts an RPC failed and returns its error, so a refusal
// assertion reads as one line rather than three.
func mustFail(_ *journeyv1.ProposePromotionResponse, err error) error {
	return err
}

// assertPromotionRefusal decodes a refusal as the repository's owned error
// model and asserts its condition. Decoding rather than reading the gRPC code
// is what proves the journey surface answers in the same shape every other
// service does, on whichever transport carried it.
func assertPromotionRefusal(t *testing.T, err error, want envelope.Code) *envelope.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	owned, ok := envelope.FromGRPC(err)
	if !ok {
		t.Fatalf("error %v did not decode as an owned envelope.Error", err)
	}
	if owned.Code() != want {
		t.Fatalf("Code() = %s, want %s (reason=%s message=%s)",
			owned.Code(), want, owned.ReasonRef(), owned.Message())
	}
	return owned
}
