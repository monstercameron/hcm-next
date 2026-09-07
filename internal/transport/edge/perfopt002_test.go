package edge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/edge"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/transport/transporttest"
	"github.com/monstercameron/hcm-next/internal/trust"
)

type countingVerifier struct {
	inner     trust.Verifier
	calls     atomic.Int64
	lastToken atomic.Value
}

func (v *countingVerifier) Verify(ctx context.Context, credential trust.Credential) (*trust.Principal, error) {
	v.calls.Add(1)
	v.lastToken.Store(credential.Token)
	return v.inner.Verify(ctx, credential)
}

func newPerfoptEdge(t testing.TB) (http.Handler, *countingVerifier, *transporttest.IntentHandler, *trust.HMACVerifier, string, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	baseVerifier, err := transporttest.NewVerifier(clock)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(baseVerifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	verifier := &countingVerifier{inner: baseVerifier}
	intent := &transporttest.IntentHandler{}
	h, err := edge.NewHandler(edge.Options{
		Config: transporttest.Config(verifier, clock, "req-perfopt-002", nil),
		Intent: intent,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, verifier, intent, baseVerifier, token, now
}

func newPerfoptJSONRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, edge.ProcedureGetIntent,
		strings.NewReader(`{"intentId":"`+transporttest.KnownIntentID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(transport.AuthorizationMetadataKey, token)
	return req
}

func TestTodo_PERFOPT_002(t *testing.T) {
	h, verifier, _, _, token, _ := newPerfoptEdge(t)
	req := newPerfoptJSONRequest(token)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := verifier.calls.Load(); got != 1 {
		t.Fatalf("Verify calls = %d, want 1", got)
	}
}

func TestTodo_PERFOPT_002_Security(t *testing.T) {
	t.Run("authorization tampering is reverified", func(t *testing.T) {
		now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
		baseVerifier, err := transporttest.NewVerifier(func() time.Time { return now })
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		token, err := transporttest.BearerToken(baseVerifier, transporttest.DefaultClaims(now))
		if err != nil {
			t.Fatalf("BearerToken: %v", err)
		}
		verifier := &countingVerifier{inner: baseVerifier}
		cfg := transporttest.Config(verifier, func() time.Time { return now }, "req-tamper", nil)
		req := newPerfoptJSONRequest(token)
		ctx, _, _, admitErr := transport.WithPreAdmission(req.Context(), cfg,
			transport.MapMetadata(req.Header), edge.ProcedureGetIntent)
		if admitErr != nil {
			t.Fatalf("WithPreAdmission: %v", admitErr)
		}
		req = req.WithContext(ctx)
		req.Header.Set(transport.AuthorizationMetadataKey, "Bearer altered")

		_, _, admitErr = transport.Admit(req.Context(), cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata(req.Header),
			Method:   edge.ProcedureGetIntent,
			Kind:     transport.KindHTTPEdge,
			Message:  &intentsv1.GetIntentRequest{},
		})
		if admitErr == nil || admitErr.Code() != envelope.CodeUnauthenticated {
			t.Fatalf("tampered request error = %v, want unauthenticated", admitErr)
		}
		if got := verifier.calls.Load(); got != 2 {
			t.Fatalf("Verify calls = %d, want 2", got)
		}
		if got := verifier.lastToken.Load(); got != "altered" {
			t.Fatalf("last verified token = %v, want altered", got)
		}
	})

	t.Run("sequential requests keep principals isolated", func(t *testing.T) {
		h, verifier, intent, baseVerifier, tokenOne, now := newPerfoptEdge(t)
		claims := transporttest.DefaultClaims(now)
		claims.Subject = "user-two"
		tokenTwo, err := transporttest.BearerToken(baseVerifier, claims)
		if err != nil {
			t.Fatalf("BearerToken: %v", err)
		}
		for _, token := range []string{tokenOne, tokenTwo} {
			res := httptest.NewRecorder()
			h.ServeHTTP(res, newPerfoptJSONRequest(token))
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
			}
		}
		calls := intent.Calls()
		if len(calls) != 2 || calls[0].SubjectID == calls[1].SubjectID {
			t.Fatalf("principal subjects = %+v, want two distinct subjects", calls)
		}
		if got := verifier.calls.Load(); got != 2 {
			t.Fatalf("Verify calls = %d, want 2", got)
		}
	})

	t.Run("non-json request still authenticates in interceptor", func(t *testing.T) {
		h, verifier, _, _, token, _ := newPerfoptEdge(t)
		req := httptest.NewRequest(http.MethodPost, edge.ProcedureGetIntent, nil)
		req.Header.Set("Content-Type", "application/proto")
		req.Header.Set(transport.AuthorizationMetadataKey, token)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if got := verifier.calls.Load(); got != 1 {
			t.Fatalf("Verify calls = %d, want 1", got)
		}
	})
}

func TestTodo_PERFOPT_002_Integration(t *testing.T) {
	h, verifier, intent, baseVerifier, token, now := newPerfoptEdge(t)
	claims := transporttest.DefaultClaims(now)
	principal, err := baseVerifier.Verify(context.Background(), trust.Credential{
		Scheme:   "Bearer",
		Token:    strings.TrimPrefix(token, "Bearer "),
		Audience: transporttest.Audience,
	})
	if err != nil {
		t.Fatalf("Verify expected principal: %v", err)
	}
	if principal.Subject() != claims.Subject {
		t.Fatalf("expected principal subject = %q, got %q", claims.Subject, principal.Subject())
	}

	res := httptest.NewRecorder()
	h.ServeHTTP(res, newPerfoptJSONRequest(token))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	calls := intent.Calls()
	if len(calls) != 1 {
		t.Fatalf("invocation count = %d, want 1", len(calls))
	}
	if calls[0].PrincipalFingerprint != principal.Fingerprint() {
		t.Fatalf("invocation principal fingerprint = %q, middleware principal fingerprint = %q", calls[0].PrincipalFingerprint, principal.Fingerprint())
	}
	if got := verifier.calls.Load(); got != 1 {
		t.Fatalf("Verify calls = %d, want 1", got)
	}
}

func BenchmarkTodo_PERFOPT_002(b *testing.B) {
	h, _, _, _, token, _ := newPerfoptEdge(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, newPerfoptJSONRequest(token))
		if res.Code != http.StatusOK {
			b.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
		}
	}
}
