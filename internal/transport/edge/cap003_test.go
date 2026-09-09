package edge_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// TestTodo_CAP_003_Integration is the CAP-003 integration test.
//
// RED: gRPC, the HTTP edge and the Go client returning a different semantic
// code, retryability, field path or correlation/evidence reference for the
// same failure.
//
// GREEN: the canonical envelope means the same thing on every channel. The
// assertion is deliberately not "the HTTP status matched": it compares the
// owned condition, the retry classification, the field violations, the
// correlation identifier and the evidence reference, and only then the
// projected status.
func TestTodo_CAP_003_Integration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	cases := []struct {
		name      string
		grpc      func() error
		edge      func() error
		wantCode  envelope.Code
		wantHTTP  int
		retryable bool
	}{
		{
			name: "hidden or absent resource",
			grpc: func() error {
				_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}))
				return err
			},
			wantCode: envelope.CodeNotFound,
			wantHTTP: http.StatusNotFound,
		},
		{
			name: "stale revision",
			grpc: func() error {
				req := submitRequest()
				req.ExpectedInstanceVersion = 1
				_, err := h.grpcIntent.SubmitIntent(h.grpcContext(ctx), req)
				return err
			},
			edge: func() error {
				req := submitRequest()
				req.ExpectedInstanceVersion = 1
				_, err := h.edgeIntent.SubmitIntent(ctx, edgeRequest(h, req))
				return err
			},
			wantCode: envelope.CodeFailedPrecondition,
			wantHTTP: http.StatusPreconditionFailed,
		},
		{
			name: "prohibited purpose",
			grpc: func() error {
				_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{
					IntentId: transporttest.KnownIntentID,
					Scope:    &commonv1.ScopeContext{Purpose: transporttest.PurposeUnauthorized},
				})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{
					IntentId: transporttest.KnownIntentID,
					Scope:    &commonv1.ScopeContext{Purpose: transporttest.PurposeUnauthorized},
				}))
				return err
			},
			wantCode: envelope.CodePermissionDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name: "missing authentication",
			grpc: func() error {
				_, err := h.grpcIntent.GetIntent(h.grpcContextWithoutCredential(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.GetIntent(ctx, edgeRequestWithoutCredential(&intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}))
				return err
			},
			wantCode: envelope.CodeUnauthenticated,
			wantHTTP: http.StatusUnauthorized,
		},
		{
			name: "structurally invalid request",
			grpc: func() error {
				_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{}))
				return err
			},
			wantCode: envelope.CodeInvalidArgument,
			wantHTTP: http.StatusBadRequest,
		},
		{
			name: "dependency failure",
			grpc: func() error {
				_, err := h.grpcIntent.CancelIntent(h.grpcContext(ctx), cancelRequest(transporttest.RawFaultReasonRef))
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, cancelRequest(transporttest.RawFaultReasonRef)))
				return err
			},
			wantCode:  envelope.CodeUnavailable,
			wantHTTP:  http.StatusServiceUnavailable,
			retryable: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grpcErr, edgeErr := tc.grpc(), tc.edge()
			if grpcErr == nil || edgeErr == nil {
				t.Fatalf("expected a failure: grpc=%v edge=%v", grpcErr, edgeErr)
			}

			viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
			if viaGRPC.Code() != tc.wantCode {
				t.Errorf("gRPC owned code = %v, want %v", viaGRPC.Code(), tc.wantCode)
			}
			if viaEdge.Code() != tc.wantCode {
				t.Errorf("edge owned code = %v, want %v", viaEdge.Code(), tc.wantCode)
			}
			if viaGRPC.Retryable() != tc.retryable {
				t.Errorf("retryable = %v, want %v", viaGRPC.Retryable(), tc.retryable)
			}
			if viaGRPC.HTTPStatus() != tc.wantHTTP {
				t.Errorf("projected HTTP status = %d, want %d", viaGRPC.HTTPStatus(), tc.wantHTTP)
			}
			if viaGRPC.CorrelationID() != fixedRequestID {
				t.Errorf("correlation = %q, want the request identifier", viaGRPC.CorrelationID())
			}
			assertOwnedParity(t, tc.name, viaGRPC, viaEdge)
			assertNoLeak(t, tc.name+" grpc", grpcErr.Error())
			assertNoLeak(t, tc.name+" edge", edgeErr.Error())
		})
	}

	t.Run("an authenticated failure carries an evidence reference", func(t *testing.T) {
		_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID})
		owned := ownedFromGRPC(t, err)
		if owned.EvidenceRef().ID == "" {
			t.Error("an authenticated failure must carry an evidence reference")
		}
	})

	t.Run("structured logging records the same outcome on both transports", func(t *testing.T) {
		h.mu.Lock()
		h.records = nil
		h.mu.Unlock()

		req := &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}
		if _, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(req)); err == nil {
			t.Fatal("expected a failure")
		}
		if _, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(req))); err == nil {
			t.Fatal("expected a failure")
		}

		byTransport := map[transport.Kind]transport.LogRecord{}
		for _, record := range h.logRecords() {
			if strings.HasSuffix(record.Method, "GetIntent") {
				byTransport[record.Transport] = record
			}
		}
		grpcRecord, okGRPC := byTransport[transport.KindGRPC]
		edgeRecord, okEdge := byTransport[transport.KindHTTPEdge]
		if !okGRPC || !okEdge {
			t.Fatalf("missing log records: grpc=%v edge=%v", okGRPC, okEdge)
		}
		if grpcRecord.Code != edgeRecord.Code || grpcRecord.ReasonRef != edgeRecord.ReasonRef {
			t.Errorf("log outcomes differ: grpc=%+v edge=%+v", grpcRecord, edgeRecord)
		}
		if grpcRecord.RequestID != edgeRecord.RequestID {
			t.Errorf("log correlation differs: grpc=%q edge=%q", grpcRecord.RequestID, edgeRecord.RequestID)
		}
		if grpcRecord.EvidenceID != edgeRecord.EvidenceID {
			t.Errorf("log evidence differs: grpc=%q edge=%q", grpcRecord.EvidenceID, edgeRecord.EvidenceID)
		}
		for _, record := range []transport.LogRecord{grpcRecord, edgeRecord} {
			assertNoLeak(t, "log record", record.Method+" "+record.ReasonRef+" "+record.SubjectID+" "+record.TenantID)
			if strings.Contains(h.token, record.SubjectID) && record.SubjectID != "" {
				t.Error("a log record carries credential material")
			}
		}
	})
}

// TestTodo_CAP_003_Fault is the CAP-003 fault test. It drives the failure
// modes that are not the caller's fault -- an unowned dependency error and an
// expired deadline -- and requires the same owned projection and the same
// redaction on both channels.
func TestTodo_CAP_003_Fault(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("an unowned dependency failure becomes one owned condition", func(t *testing.T) {
		grpcErr := func() error {
			_, err := h.grpcIntent.CancelIntent(h.grpcContext(ctx), cancelRequest(transporttest.RawFaultReasonRef))
			return err
		}()
		edgeErr := func() error {
			_, err := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, cancelRequest(transporttest.RawFaultReasonRef)))
			return err
		}()
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("the fault scenario succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}

		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeUnavailable {
			t.Errorf("owned code = %v, want UNAVAILABLE", viaGRPC.Code())
		}
		if !viaGRPC.Retryable() {
			t.Error("UNAVAILABLE must be classified retryable")
		}
		assertOwnedParity(t, "unowned dependency failure", viaGRPC, viaEdge)

		// The provider text exists; it simply never crosses the edge.
		for _, rendered := range []string{grpcErr.Error(), edgeErr.Error(), viaGRPC.Message(), viaEdge.Message()} {
			if strings.Contains(rendered, transporttest.RawFaultDiagnostic) {
				t.Errorf("the raw provider diagnostic crossed the edge: %s", rendered)
			}
			assertNoLeak(t, "fault projection", rendered)
		}
	})

	t.Run("an expired deadline is one owned condition on both transports", func(t *testing.T) {
		request := &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID}

		grpcCtx, cancelGRPC := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancelGRPC()
		_, grpcErr := h.grpcIntent.SimulateIntent(h.grpcContext(grpcCtx), clone(request))

		edgeCtx, cancelEdge := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancelEdge()
		_, edgeErr := h.edgeIntent.SimulateIntent(edgeCtx, edgeRequest(h, clone(request)))

		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("a blocked call with an expired deadline succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeDeadlineExceeded {
			t.Errorf("gRPC owned code = %v, want DEADLINE_EXCEEDED", viaGRPC.Code())
		}
		if viaEdge.Code() != envelope.CodeDeadlineExceeded {
			t.Errorf("edge owned code = %v, want DEADLINE_EXCEEDED", viaEdge.Code())
		}
		if viaGRPC.HTTPStatus() != http.StatusGatewayTimeout {
			t.Errorf("projected HTTP status = %d, want 504", viaGRPC.HTTPStatus())
		}
	})

	t.Run("the server caps an unbounded request", func(t *testing.T) {
		// The fixture's configured cap is short, so a request that arrives
		// with no deadline still ends. An uncapped server would hang here.
		done := make(chan error, 1)
		go func() {
			_, err := h.grpcIntent.SimulateIntent(h.grpcContext(ctx),
				&intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID})
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a blocked call returned success")
			}
		case <-time.After(6 * time.Second):
			t.Fatal("a request with no caller deadline never ended, so the server cap is not applied")
		}
	})
}
