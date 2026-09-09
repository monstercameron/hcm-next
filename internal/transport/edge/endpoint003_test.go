package edge_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// forbiddenInProjection is the set of markers that must never appear in
// anything a caller receives: stack traces, SQL, storage driver text, source
// paths, policy source and secrets.
var forbiddenInProjection = []string{
	"goroutine ",
	"panic:",
	"pq:",
	"select ",
	"insert into",
	"internal/data",
	".go:",
	"password",
	"private key",
}

// assertNoLeak fails the test if payload carries anything unowned.
func assertNoLeak(t *testing.T, label string, payload string) {
	t.Helper()
	lower := strings.ToLower(payload)
	for _, marker := range forbiddenInProjection {
		if strings.Contains(lower, marker) {
			t.Errorf("%s leaked %q: %s", label, marker, payload)
		}
	}
}

// withUnknownField returns a clone of msg carrying an unrecognized Protobuf
// field, which is how a "material unknown field" is presented to a server that
// was compiled against the current descriptor.
func withUnknownField(msg proto.Message, fieldNumber protowire.Number) proto.Message {
	out := proto.Clone(msg)
	raw := protowire.AppendTag(nil, fieldNumber, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 1)
	out.ProtoReflect().SetUnknown(protoreflect.RawFields(raw))
	return out
}

// TestEndpointValidationAndErrorProjectionParity is the ENDPOINT-003 primary
// test.
//
// RED: an unknown JSON field, a duplicate key, invalid UTF-8, an ambiguous
// numeric value, an unknown enum, a material unknown Protobuf field or an
// invalid local constraint must not be accepted; HTTP and gRPC must not
// disagree on the owned code, the field path, retryability or the zero-effect
// result; and no error may leak a stack, SQL, policy source, secret or
// restricted value.
//
// GREEN: strict bounded decoding plus structural validation returns one owned
// typed error projected through the declared gRPC status and HTTP status with
// safe details, and every rejected vector reaches no handler at all.
func TestEndpointValidationAndErrorProjectionParity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("strict JSON decoding rejects laxer encodings", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			body      string
			fieldPath string
			rule      string
		}{
			{
				name:      "unknown field",
				body:      `{"intentId":"` + transporttest.KnownIntentID + `","escalate":true}`,
				fieldPath: "escalate",
				rule:      "strict_decoding.unknown_field",
			},
			{
				name:      "duplicate key",
				body:      `{"intentId":"a","intentId":"` + transporttest.KnownIntentID + `"}`,
				fieldPath: "intentId",
				rule:      "strict_decoding.duplicate_key",
			},
			{
				name:      "ambiguous numeric value",
				body:      `{"intentId":"` + transporttest.KnownIntentID + `","freshness":123456789012345678901}`,
				fieldPath: "freshness",
				rule:      "strict_decoding.ambiguous_number",
			},
			{
				name:      "unknown enum value",
				body:      `{"intentId":"` + transporttest.KnownIntentID + `","freshness":"CONSISTENCY_FRESHNESS_HINT_WHENEVER"}`,
				fieldPath: "(request)",
				rule:      "strict_decoding.malformed_json",
			},
			{
				name:      "trailing content",
				body:      `{"intentId":"` + transporttest.KnownIntentID + `"} {"intentId":"x"}`,
				fieldPath: "(request)",
				rule:      "strict_decoding.malformed_json",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.intent.Reset()
				status, payload := h.postJSON(t, edge.ProcedureGetIntent, tc.body, nil)

				if status != http.StatusBadRequest {
					t.Errorf("HTTP status = %d, want 400", status)
				}
				if calls := h.intent.Calls(); len(calls) != 0 {
					t.Errorf("a rejected vector reached the handler %d times", len(calls))
				}

				wire := decodeWireError(t, payload)
				if wire.Code != "invalid_argument" {
					t.Errorf("wire code = %q, want invalid_argument", wire.Code)
				}
				assertNoLeak(t, tc.name, string(payload))

				detail := decodeErrorDetail(t, wire)
				if detail.GetCode() != commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
					t.Errorf("detail code = %v", detail.GetCode())
				}
				if detail.GetRetryable() {
					t.Error("a malformed request must not be classified retryable")
				}
				if !hasViolation(detail, tc.fieldPath, tc.rule) {
					t.Errorf("violations = %v, want one at %q from rule %q",
						detail.GetFieldViolations(), tc.fieldPath, tc.rule)
				}
			})
		}
	})

	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		h.intent.Reset()
		status, payload := h.postJSON(t, edge.ProcedureGetIntent, "{\"intentId\":\"\xff\xfe\"}", nil)
		if status != http.StatusBadRequest {
			t.Errorf("HTTP status = %d, want 400", status)
		}
		if calls := h.intent.Calls(); len(calls) != 0 {
			t.Errorf("a rejected vector reached the handler %d times", len(calls))
		}
		detail := decodeErrorDetail(t, decodeWireError(t, payload))
		if !hasViolation(detail, "(request)", "strict_decoding.invalid_utf8") {
			t.Errorf("violations = %v, want the invalid UTF-8 rule", detail.GetFieldViolations())
		}
	})

	t.Run("a material unknown Protobuf field is rejected on both transports", func(t *testing.T) {
		h.intent.Reset()
		base := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}

		grpcMsg, _ := withUnknownField(base, 9999).(*intentsv1.GetIntentRequest)
		_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), grpcMsg)
		if grpcErr == nil {
			t.Fatal("gRPC accepted a material unknown field")
		}
		edgeMsg, _ := withUnknownField(base, 9999).(*intentsv1.GetIntentRequest)
		_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, edgeMsg))
		if edgeErr == nil {
			t.Fatal("the edge accepted a material unknown field")
		}

		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeInvalidArgument {
			t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
		}
		if got := viaGRPC.Violations(); len(got) == 0 || got[0].RuleRef != "strict_decoding.unknown_field" {
			t.Errorf("violations = %+v, want the unknown-field rule", got)
		}
		assertOwnedParity(t, "material unknown Protobuf field", viaGRPC, viaEdge)
		if calls := h.intent.Calls(); len(calls) != 0 {
			t.Errorf("a rejected vector reached the handler %d times", len(calls))
		}
	})

	t.Run("structural validation agrees across transports", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			grpc      func() error
			edge      func() error
			fieldPath string
			rule      string
		}{
			{
				name: "missing required identifier",
				grpc: func() error {
					_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{})
					return err
				},
				edge: func() error {
					_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{}))
					return err
				},
				fieldPath: "intent_id",
				rule:      "structural_validation.required_field",
			},
			{
				name: "missing nested required identifier",
				grpc: func() error {
					_, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), &intentsv1.CreateIntentRequest{IdempotencyKey: "k"})
					return err
				},
				edge: func() error {
					_, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, &intentsv1.CreateIntentRequest{IdempotencyKey: "k"}))
					return err
				},
				fieldPath: "definition.intent_type_id",
				rule:      "structural_validation.required_field",
			},
			{
				name: "page size beyond the bound",
				grpc: func() error {
					_, err := h.grpcIntent.ListIntents(h.grpcContext(ctx),
						&intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 100000}})
					return err
				},
				edge: func() error {
					_, err := h.edgeIntent.ListIntents(ctx, edgeRequest(h,
						&intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 100000}}))
					return err
				},
				fieldPath: "page.page_size",
				rule:      "structural_validation.page_size",
			},
			{
				name: "string beyond the transport bound",
				grpc: func() error {
					_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx),
						&intentsv1.GetIntentRequest{IntentId: strings.Repeat("x", 5000)})
					return err
				},
				edge: func() error {
					_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h,
						&intentsv1.GetIntentRequest{IntentId: strings.Repeat("x", 5000)}))
					return err
				},
				fieldPath: "intent_id",
				rule:      "structural_validation.string_length",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.intent.Reset()
				grpcErr, edgeErr := tc.grpc(), tc.edge()
				if grpcErr == nil || edgeErr == nil {
					t.Fatalf("an invalid request was accepted: grpc=%v edge=%v", grpcErr, edgeErr)
				}

				viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
				if viaGRPC.Code() != envelope.CodeInvalidArgument {
					t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
				}
				if !hasOwnedViolation(viaGRPC, tc.fieldPath, tc.rule) {
					t.Errorf("violations = %+v, want one at %q from rule %q", viaGRPC.Violations(), tc.fieldPath, tc.rule)
				}
				assertOwnedParity(t, tc.name, viaGRPC, viaEdge)
				if calls := h.intent.Calls(); len(calls) != 0 {
					t.Errorf("a rejected vector reached the handler %d times", len(calls))
				}
			})
		}
	})
}

// TestTodo_ENDPOINT_003_Golden pins the projection of each owned condition
// onto the wire, end to end, through a live edge. It is the assertion that
// catches the connect-go default that maps FAILED_PRECONDITION to 400 instead
// of the contract's 412.
func TestTodo_ENDPOINT_003_Golden(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct {
		name       string
		procedure  string
		body       string
		httpStatus int
		wireCode   string
		ownedCode  commonv1.ErrorCode
		retryable  bool
	}{
		{
			name:       "structurally invalid request",
			procedure:  edge.ProcedureGetIntent,
			body:       `{"unknownField":true}`,
			httpStatus: http.StatusBadRequest,
			wireCode:   "invalid_argument",
			ownedCode:  commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
		},
		{
			name:       "hidden or absent resource",
			procedure:  edge.ProcedureGetIntent,
			body:       `{"intentId":"` + transporttest.MissingIntentID + `"}`,
			httpStatus: http.StatusNotFound,
			wireCode:   "not_found",
			ownedCode:  commonv1.ErrorCode_ERROR_CODE_NOT_FOUND,
		},
		{
			name:       "unmet business precondition",
			procedure:  edge.ProcedureSubmitIntent,
			body:       `{"idempotencyKey":"k","intentId":"` + transporttest.KnownIntentID + `","proposalRevisionId":"r","expectedInstanceVersion":"1"}`,
			httpStatus: http.StatusPreconditionFailed,
			wireCode:   "failed_precondition",
			ownedCode:  commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION,
		},
		{
			name:       "prohibited purpose",
			procedure:  edge.ProcedureGetIntent,
			body:       `{"intentId":"` + transporttest.KnownIntentID + `","scope":{"purpose":"` + transporttest.PurposeUnauthorized + `"}}`,
			httpStatus: http.StatusForbidden,
			wireCode:   "permission_denied",
			ownedCode:  commonv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED,
		},
		{
			name:       "dependency failure before a committed result",
			procedure:  edge.ProcedureCancelIntent,
			body:       `{"idempotencyKey":"k","intentId":"` + transporttest.KnownIntentID + `","expectedInstanceVersion":"7","reasonRef":"` + transporttest.RawFaultReasonRef + `"}`,
			httpStatus: http.StatusServiceUnavailable,
			wireCode:   "unavailable",
			ownedCode:  commonv1.ErrorCode_ERROR_CODE_UNAVAILABLE,
			retryable:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, payload := h.postJSON(t, tc.procedure, tc.body, nil)
			if status != tc.httpStatus {
				t.Errorf("HTTP status = %d, want %d (body %s)", status, tc.httpStatus, payload)
			}
			wire := decodeWireError(t, payload)
			if wire.Code != tc.wireCode {
				t.Errorf("wire code = %q, want %q", wire.Code, tc.wireCode)
			}
			detail := decodeErrorDetail(t, wire)
			if detail.GetCode() != tc.ownedCode {
				t.Errorf("owned code = %v, want %v", detail.GetCode(), tc.ownedCode)
			}
			if detail.GetRetryable() != tc.retryable {
				t.Errorf("retryable = %v, want %v", detail.GetRetryable(), tc.retryable)
			}
			if detail.GetCorrelationId() != fixedRequestID {
				t.Errorf("correlation = %q, want the request identifier", detail.GetCorrelationId())
			}
			assertNoLeak(t, tc.name, string(payload))
		})
	}
}

// TestTodo_ENDPOINT_003_Property asserts the invariant over a generated set of
// malformed bodies: a body the strict screen rejects never reaches a handler
// and never produces a 5xx.
func TestTodo_ENDPOINT_003_Property(t *testing.T) {
	h := newHarness(t)

	bodies := []string{
		`{`,
		`}`,
		``,
		`null`,
		`[]`,
		`"a string"`,
		`{"a":`,
		`{"intentId":}`,
		`{"intentId":"a","intentId":"b"}`,
		`{"intentId":1}`,
		`{"intentId":"a","unknown":{"nested":true}}`,
		`{"scope":{"tenantId":"a","tenantId":"b"}}`,
		`{"freshness":99999999999999999999999}`,
		`{"intentId":"` + strings.Repeat("x", 9000) + `"}`,
		`{"scope":{"unknown":1}}`,
	}

	for _, body := range bodies {
		t.Run(shorten(body), func(t *testing.T) {
			h.intent.Reset()
			status, payload := h.postJSON(t, edge.ProcedureGetIntent, body, nil)
			if status >= 500 {
				t.Fatalf("status %d for body %q: a malformed request must never be a server failure (%s)", status, body, payload)
			}
			if status < 400 {
				t.Fatalf("status %d for body %q: a malformed request must be rejected", status, body)
			}
			if calls := h.intent.Calls(); len(calls) != 0 {
				t.Errorf("body %q reached the handler", body)
			}
			assertNoLeak(t, "property", string(payload))
		})
	}
}

// FuzzTodo_ENDPOINT_003 fuzzes the raw request body at the edge. Whatever
// arrives, the edge answers with a client error, never invokes a handler with
// a body it did not fully accept, and never leaks unowned text.
func FuzzTodo_ENDPOINT_003(f *testing.F) {
	h := newHarness(f)

	f.Add(`{"intentId":"` + transporttest.KnownIntentID + `"}`)
	f.Add(`{"intentId":"a","intentId":"b"}`)
	f.Add(`{"unknown":true}`)
	f.Add(`{`)
	f.Add("\xff\xfe")
	f.Add(`{"freshness":1e400}`)
	f.Add(`{"scope":{"tenantId":"victim-corp"}}`)

	f.Fuzz(func(t *testing.T, body string) {
		status, payload := h.postJSON(t, edge.ProcedureGetIntent, body, nil)
		if status >= 500 {
			t.Fatalf("status %d for body %q (%s)", status, body, payload)
		}
		if status == http.StatusOK {
			return
		}
		lower := strings.ToLower(string(payload))
		for _, marker := range forbiddenInProjection {
			if strings.Contains(lower, marker) {
				t.Fatalf("error body leaked %q for input %q: %s", marker, body, payload)
			}
		}
	})
}

// TestTodo_ENDPOINT_003_Integration drives the same logical defect through
// both transports and requires one owned answer.
func TestTodo_ENDPOINT_003_Integration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.intent.Reset()
	request := &intentsv1.SubmitIntentRequest{
		IdempotencyKey:          "idem-stale",
		IntentId:                transporttest.KnownIntentID,
		ProposalRevisionId:      "revision-1",
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion + 99,
	}

	_, grpcErr := h.grpcIntent.SubmitIntent(h.grpcContext(ctx), clone(request))
	_, edgeErr := h.edgeIntent.SubmitIntent(ctx, edgeRequest(h, clone(request)))
	if grpcErr == nil || edgeErr == nil {
		t.Fatalf("a stale revision was accepted: grpc=%v edge=%v", grpcErr, edgeErr)
	}

	viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
	if viaGRPC.Code() != envelope.CodeFailedPrecondition {
		t.Errorf("owned code = %v, want FAILED_PRECONDITION", viaGRPC.Code())
	}
	if viaGRPC.HTTPStatus() != http.StatusPreconditionFailed {
		t.Errorf("projected HTTP status = %d, want 412", viaGRPC.HTTPStatus())
	}
	assertOwnedParity(t, "stale revision", viaGRPC, viaEdge)

	// The handler is a semantic owner, so it does get to run here; what must
	// be identical is what the caller learns from it.
	if calls := h.intent.Calls(); len(calls) != 2 {
		t.Errorf("recorded %d handler calls, want 2", len(calls))
	}
}

// TestTodo_ENDPOINT_003_Security proves that no rejection path discloses
// unowned text, on either transport, for any of the fixture's failure modes.
func TestTodo_ENDPOINT_003_Security(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("a raw provider failure is never projected", func(t *testing.T) {
		request := cancelRequest(transporttest.RawFaultReasonRef)

		_, grpcErr := h.grpcIntent.CancelIntent(h.grpcContext(ctx), clone(request))
		_, edgeErr := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, clone(request)))
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("the fault scenario succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}

		assertNoLeak(t, "grpc status", grpcErr.Error())
		assertNoLeak(t, "edge error", edgeErr.Error())

		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeUnavailable {
			t.Errorf("owned code = %v, want UNAVAILABLE", viaGRPC.Code())
		}
		assertNoLeak(t, "owned message", viaGRPC.Message())
		assertOwnedParity(t, "raw provider failure", viaGRPC, viaEdge)
	})

	t.Run("every failure body is safe", func(t *testing.T) {
		for _, body := range []string{
			`{"unknownField":1}`,
			`{"intentId":"` + transporttest.MissingIntentID + `"}`,
			`{"intentId":"a","intentId":"b"}`,
			`{"intentId":"` + transporttest.KnownIntentID + `","scope":{"tenantId":"victim-corp"}}`,
		} {
			_, payload := h.postJSON(t, edge.ProcedureGetIntent, body, nil)
			assertNoLeak(t, "failure body", string(payload))
		}
	})
}

// TestTodo_ENDPOINT_003_Conformance checks the projection table end to end:
// every owned condition the transports can produce maps to the HTTP status the
// contract publishes.
func TestTodo_ENDPOINT_003_Conformance(t *testing.T) {
	for _, tc := range []struct {
		code envelope.Code
		http int
	}{
		{envelope.CodeInvalidArgument, http.StatusBadRequest},
		{envelope.CodeUnauthenticated, http.StatusUnauthorized},
		{envelope.CodePermissionDenied, http.StatusForbidden},
		{envelope.CodeNotFound, http.StatusNotFound},
		{envelope.CodeAlreadyExists, http.StatusConflict},
		{envelope.CodeAborted, http.StatusConflict},
		{envelope.CodeFailedPrecondition, http.StatusPreconditionFailed},
		{envelope.CodeResourceExhausted, http.StatusTooManyRequests},
		{envelope.CodeDeadlineExceeded, http.StatusGatewayTimeout},
		{envelope.CodeUnavailable, http.StatusServiceUnavailable},
	} {
		if got := tc.code.HTTPStatus(); got != tc.http {
			t.Errorf("%v projects to HTTP %d, want %d", tc.code, got, tc.http)
		}
	}

	h := newHarness(t)
	t.Run("the live edge honors the table where connect-go disagrees", func(t *testing.T) {
		// connect-go maps FAILED_PRECONDITION to 400 by default. The contract
		// requires 412, so the edge overrides it; this is that assertion.
		status, payload := h.postJSON(t, edge.ProcedureSubmitIntent,
			`{"idempotencyKey":"k","intentId":"`+transporttest.KnownIntentID+`","proposalRevisionId":"r","expectedInstanceVersion":"1"}`, nil)
		if status != http.StatusPreconditionFailed {
			t.Fatalf("HTTP status = %d, want 412 (body %s)", status, payload)
		}
	})
}

// decodeErrorDetail extracts the canonical hcmnext.common.v1.ErrorDetail from
// a connect wire error body.
func decodeErrorDetail(t *testing.T, wire wireError) *commonv1.ErrorDetail {
	t.Helper()
	for _, d := range wire.Details {
		if !strings.HasSuffix(d.Type, "hcmnext.common.v1.ErrorDetail") {
			continue
		}
		raw, err := base64.RawStdEncoding.DecodeString(d.Value)
		if err != nil {
			raw, err = base64.StdEncoding.DecodeString(d.Value)
			if err != nil {
				t.Fatalf("decoding detail value %q: %v", d.Value, err)
			}
		}
		detail := &commonv1.ErrorDetail{}
		if err := proto.Unmarshal(raw, detail); err != nil {
			t.Fatalf("unmarshalling ErrorDetail: %v", err)
		}
		return detail
	}
	t.Fatalf("the error body carries no hcmnext.common.v1.ErrorDetail: %+v", wire)
	return nil
}

// hasViolation reports whether detail carries a violation at fieldPath from
// rule.
func hasViolation(detail *commonv1.ErrorDetail, fieldPath, rule string) bool {
	for _, v := range detail.GetFieldViolations() {
		if v.GetFieldPath() == fieldPath && v.GetRuleRef() == rule {
			return true
		}
	}
	return false
}

// hasOwnedViolation reports whether owned carries a violation at fieldPath
// from rule.
func hasOwnedViolation(owned *envelope.Error, fieldPath, rule string) bool {
	for _, v := range owned.Violations() {
		if v.FieldPath == fieldPath && v.RuleRef == rule {
			return true
		}
	}
	return false
}

// shorten trims a body for use as a subtest name.
func shorten(body string) string {
	if len(body) > 40 {
		return body[:40] + "..."
	}
	if body == "" {
		return "(empty)"
	}
	return body
}
