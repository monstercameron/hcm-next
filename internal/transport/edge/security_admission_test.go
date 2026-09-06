package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func securityPolicy() RequestShapePolicy {
	return RequestShapePolicy{MaxHeaderBytes: 128, MaxHeaderCount: 8, MaxBodyBytes: 32, MaxCompressedBytes: 32, MaxCompressionRatio: 10, RetryAfter: 3}
}

func TestTodo_EDGE_002(t *testing.T) {
	policy := securityPolicy()
	valid := RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)}
	if got := CheckRequest(valid, policy); got.Outcome != IngressAllow {
		t.Fatalf("valid request = %+v", got)
	}
	cases := []struct {
		name   string
		shape  RequestShape
		reason IngressReason
	}{
		{"smuggling", RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Length": {"1", "2"}, "Content-Type": {"application/json"}}, Body: []byte("x")}, ReasonAmbiguousFraming},
		{"encoding", RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip, br"}}, Body: []byte("x")}, ReasonForbiddenEncoding},
		{"method", RequestShape{Method: http.MethodTrace}, ReasonForbiddenMethod},
		{"type", RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("x")}, ReasonForbiddenContentType},
		{"body", RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(strings.Repeat("x", 33))}, ReasonBodyTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckRequest(tc.shape, policy)
			if got.Outcome != IngressReject || got.Reason != tc.reason {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestTodo_EDGE_002_Fault(t *testing.T) {
	policy := securityPolicy()
	got := CheckRequest(RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}, "Content-Length": {"1"}}}, policy)
	// A gzip stream with an invalid header is rejected before any handler can run.
	got = CheckRequest(RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}}, Body: []byte("not gzip")}, policy)
	if got.Reason != ReasonMalformedFraming || got.Status != http.StatusBadRequest {
		t.Fatalf("fault decision = %+v", got)
	}
}

func TestTodo_EDGE_002_Security(t *testing.T) {
	policy := securityPolicy()
	got := CheckRequest(RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Length": {"1"}, "Transfer-Encoding": {"chunked"}, "Content-Type": {"application/json"}}, Body: []byte("x")}, policy)
	if got.Reason != ReasonAmbiguousFraming || got.Outcome != IngressReject {
		t.Fatalf("smuggling decision = %+v", got)
	}
	got = CheckRequest(RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}, "Transfer-Encoding": {"gzip"}}, Body: []byte("x")}, policy)
	if got.Reason != ReasonMalformedFraming {
		t.Fatalf("transfer decision = %+v", got)
	}
}

func FuzzTodo_EDGE_002(f *testing.F) {
	f.Add("application/json", "identity", "{}")
	f.Add("text/plain", "br", "payload")
	f.Fuzz(func(t *testing.T, contentType, encoding, body string) {
		decision := CheckRequest(RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {contentType}, "Content-Encoding": {encoding}}, Body: []byte(body)}, securityPolicy())
		if decision.Outcome == IngressAllow && decision.Status != http.StatusOK {
			t.Fatalf("allowed decision has failure status: %+v", decision)
		}
	})
}

func TestTodo_EDGE_002_Mutation(t *testing.T) {
	policy := securityPolicy()
	called := false
	h := IngressMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), policy, nil)
	req, _ := http.NewRequest(http.MethodPost, "http://edge.test/v1", strings.NewReader(strings.Repeat("x", 33)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called || rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("rejected request reached handler: called=%t status=%d", called, rec.Code)
	}
}

func BenchmarkTodo_EDGE_002(b *testing.B) {
	policy := securityPolicy()
	shape := RequestShape{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)}
	for i := 0; i < b.N; i++ {
		_ = CheckRequest(shape, policy)
	}
}

func TestTodo_EDGE_003(t *testing.T) {
	quota, err := CheckQuota(QuotaRequest{Key: QuotaKey{TenantID: "tenant-a", PrincipalID: "principal-a", Route: "route-a", Epoch: 3}, Criticality: "P2", Cost: 2}, QuotaSnapshot{KeyEpoch: 3, TenantConsumed: 8, PrincipalConsumed: 1, RouteConsumed: 1}, QuotaPolicy{TenantLimit: 10, PrincipalLimit: 10, RouteLimit: 10, RetryAfter: 5})
	if err != nil || quota.Outcome != IngressAllow || quota.Reason != "WITHIN_FAIR_SHARE" || quota.KeyFingerprint == "" {
		t.Fatalf("fair quota = %+v err=%v", quota, err)
	}
	quota, err = CheckQuota(QuotaRequest{Key: QuotaKey{TenantID: "tenant-a", PrincipalID: "principal-a", Route: "route-a", Epoch: 3}, Criticality: "P0", Cost: 2}, QuotaSnapshot{KeyEpoch: 3, TenantConsumed: 10, ReservedCritical: 2}, QuotaPolicy{TenantLimit: 10, PrincipalLimit: 10, RouteLimit: 10, RetryAfter: 5})
	if err != nil || quota.Outcome != IngressAllow || quota.Scope != "critical-reservation" {
		t.Fatalf("critical reservation = %+v err=%v", quota, err)
	}
	gate, err := NewBurstGate(time.Minute, 2, 7)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if got := gate.Check("tenant-a/principal-a/route-a", at); got.Outcome != IngressAllow {
			t.Fatalf("request %d = %+v", i, got)
		}
	}
	got := gate.Check("tenant-a/principal-a/route-a", at)
	if got.Outcome != IngressThrottle || got.Reason != ReasonConnectionBurst || got.RetryAfter != 7 {
		t.Fatalf("burst = %+v", got)
	}
	if next := gate.Check("tenant-b/principal-b/route-a", at); next.Outcome != IngressAllow {
		t.Fatalf("tenant fairness = %+v", next)
	}
}

func TestTodo_EDGE_003_Fault(t *testing.T) {
	gate, err := NewBurstGate(time.Minute, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := gate.Check("", time.Now()); got.Reason != ReasonInvalidRequest {
		t.Fatalf("invalid key = %+v", got)
	}
}

func TestTodo_EDGE_003_Security(t *testing.T) {
	gate, err := NewBurstGate(time.Minute, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if gate.Check("tenant-a/principal-a/route-a", at).Outcome != IngressAllow {
		t.Fatal("first request denied")
	}
	if gate.Check("tenant-a/principal-a/route-b", at).Outcome != IngressAllow {
		t.Fatal("route quota was not scoped")
	}
	if gate.Check("tenant-a/principal-b/route-a", at).Outcome != IngressAllow {
		t.Fatal("principal quota was not scoped")
	}
	if gate.Check("tenant-a/principal-a/route-a", at).Outcome != IngressThrottle {
		t.Fatal("quota reset/spoof was accepted")
	}
	if _, err := CheckQuota(QuotaRequest{Key: QuotaKey{TenantID: "tenant-a", PrincipalID: "principal-a", Route: "route-a", Epoch: 4}, Criticality: "P2", Cost: 1}, QuotaSnapshot{KeyEpoch: 3}, QuotaPolicy{TenantLimit: 10, PrincipalLimit: 10, RouteLimit: 10, RetryAfter: 1}); err == nil {
		t.Fatal("stale quota epoch was accepted")
	}
}

func FuzzTodo_EDGE_003(f *testing.F) {
	f.Add("tenant-a", "principal-a", "route-a")
	f.Fuzz(func(t *testing.T, tenant, principal, route string) {
		gate, err := NewBurstGate(time.Minute, 2, 1)
		if err != nil {
			t.Fatal(err)
		}
		key := tenant + "/" + principal + "/" + route
		at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		for i := 0; i < 3; i++ {
			got := gate.Check(key, at)
			if i < 2 && got.Outcome != IngressAllow {
				t.Fatalf("request %d = %+v", i, got)
			}
		}
	})
}
