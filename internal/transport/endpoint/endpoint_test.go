package endpoint

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

const (
	testProcedure = "/example.v1.Service/Apply"
	testRoute     = "/v1/example"
)

func testDefinition(method, body string, idempotency manifest.IdempotencyClass) manifest.EndpointDefinition {
	return manifest.EndpointDefinition{
		GRPCProcedure:        testProcedure,
		HTTPPathTemplate:     testRoute,
		HTTPMethod:           method,
		HTTPBodyBinding:      body,
		IntentBehavior:       manifest.IntentBehaviorConsumes,
		IdempotencyClass:     idempotency,
		DeadlineBudgetMillis: 250,
	}
}

func testTable(t *testing.T, def manifest.EndpointDefinition) Table {
	t.Helper()
	m := &manifest.EndpointManifest{SchemaVersion: 1, Endpoints: []manifest.EndpointDefinition{def}}
	table, err := CompileManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestHTTPEndpointMediaCacheOriginAndBodyPolicy(t *testing.T) {
	p, err := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	if err != nil {
		t.Fatal(err)
	}
	p.AllowedHosts = []string{"api.example"}
	p.AllowedOrigins = []string{"https://app.example"}
	table, err := CompileManifest(&manifest.EndpointManifest{Endpoints: []manifest.EndpointDefinition{
		{GRPCProcedure: p.Procedure, HTTPPathTemplate: p.HTTPPath, HTTPMethod: p.Method, HTTPBodyBinding: p.BodyBinding, IntentBehavior: manifest.IntentBehaviorConsumes, IdempotencyClass: manifest.IdempotencyKey, DeadlineBudgetMillis: 250},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// The compiler's defaults are deliberate; the boundary adds deployment
	// origin/host configuration without changing the generated row.
	compiled, ok := table.byProcedure[p.Procedure]
	if !ok {
		t.Fatal("compiled procedure missing")
	}
	compiled.AllowedHosts = p.AllowedHosts
	compiled.AllowedOrigins = p.AllowedOrigins
	table.byProcedure[p.Procedure] = compiled

	called := 0
	h := HTTPMiddleware(table, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.Header().Set("X-Content-Type-Options", "handler-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	newRequest := func(method, body string) *http.Request {
		req := httptest.NewRequest(method, "http://api.example"+testProcedure, strings.NewReader(body))
		req.Host = "api.example"
		req.Header.Set("Origin", "https://app.example")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: p.CSRFCookie, Value: "csrf"})
		req.Header.Set(p.CSRFHeader, "csrf")
		return req
	}

	cases := []struct {
		name    string
		request func() *http.Request
		status  int
	}{
		{"wrong content type", func() *http.Request {
			r := newRequest(http.MethodPost, "{}")
			r.Header.Set("Content-Type", "text/plain")
			return r
		}, http.StatusUnsupportedMediaType},
		{"wrong origin", func() *http.Request {
			r := newRequest(http.MethodPost, "{}")
			r.Header.Set("Origin", "https://evil.example")
			return r
		}, http.StatusForbidden},
		{"unacceptable response", func() *http.Request {
			r := newRequest(http.MethodPost, "{}")
			r.Header.Set("Accept", "text/html")
			return r
		}, http.StatusNotAcceptable},
		{"body over limit", func() *http.Request {
			r := newRequest(http.MethodPost, strings.Repeat("x", int(p.Budget.MaxBodyBytes)+1))
			return r
		}, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.request())
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
	if called != 0 {
		t.Fatalf("rejected requests reached handler %d times", called)
	}
	getTable := testTable(t, testDefinition(http.MethodGet, "", manifest.IdempotencyReadSafe))
	getHandler := HTTPMiddleware(getTable, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("GET body reached handler") }))
	getRequest := httptest.NewRequest(http.MethodGet, "http://api.example"+testProcedure, strings.NewReader("body"))
	getRecord := httptest.NewRecorder()
	getHandler.ServeHTTP(getRecord, getRequest)
	if getRecord.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("GET body status=%d, want %d", getRecord.Code, http.StatusUnsupportedMediaType)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newRequest(http.MethodPost, `{"ok":true}`))
	if rec.Code != http.StatusOK || called != 1 {
		t.Fatalf("valid request status=%d called=%d", rec.Code, called)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("security header was not canonicalized: %q", got)
	}
}

func TestTodo_ENDPOINT_006_Property(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		body := ""
		if method == http.MethodPost {
			body = "*"
		}
		p, err := Compile(testDefinition(method, body, manifest.IdempotencyReadSafe))
		if err != nil {
			t.Fatal(err)
		}
		if p.Deadline <= 0 || p.Budget.MaxBodyBytes <= 0 || p.CacheControl != "no-store" || p.Hedging.Allowed {
			t.Fatalf("unsafe compiled policy: %+v", p)
		}
	}
}

func TestTodo_ENDPOINT_006_Golden(t *testing.T) {
	p, err := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Explain(p), "/example.v1.Service/Apply POST deadline=250ms retry=true/2 hedge=false body=4194304"; got != want {
		t.Fatalf("explain = %q, want %q", got, want)
	}
	if got := p.SecurityHeaders.Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Fatalf("CSP = %q", got)
	}
}

func FuzzTodo_ENDPOINT_006(f *testing.F) {
	f.Add([]byte(`{"value":1}`))
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, body []byte) {
		table := testTable(t, testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
		req := httptest.NewRequest(http.MethodPost, "http://example.test"+testProcedure, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		HTTPMiddleware(table, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
		if rec.Code == 0 {
			t.Fatal("middleware did not produce a response")
		}
	})
}

func TestTodo_ENDPOINT_006_Integration(t *testing.T) {
	table := testTable(t, testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	called := false
	h := HTTPMiddleware(table, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodPost, "http://example.test"+testProcedure, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("valid grpcbridge-shaped request did not reach owner: called=%v status=%d", called, rec.Code)
	}
}

func TestTodo_ENDPOINT_006_Security(t *testing.T) {
	table := testTable(t, testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	h := HTTPMiddleware(table, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example", http.StatusFound)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://example.test"+testProcedure, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "" || rec.Code != http.StatusForbidden {
		t.Fatalf("external redirect was not blocked: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTodo_ENDPOINT_006_Browser(t *testing.T) {
	table := testTable(t, testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	h := HTTPMiddleware(table, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodPost, "http://example.test"+testProcedure, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign browser origin status=%d", rec.Code)
	}
}

func TestTodo_ENDPOINT_006_Conformance(t *testing.T) {
	table, err := DefaultTable()
	if err != nil {
		t.Fatal(err)
	}
	if len(table.byProcedure) == 0 {
		t.Fatal("default endpoint table is empty")
	}
	for procedure, p := range table.byProcedure {
		if p.Procedure != procedure || p.SecurityHeaders.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("incomplete policy for %q", procedure)
		}
	}
}

func TestEndpointMethodPolicyBoundsDeadlineRetryHedgingAndWork(t *testing.T) {
	p, err := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := p.Context(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > p.Deadline || !p.CanRetry(1, http.StatusServiceUnavailable) || p.CanRetry(2, http.StatusServiceUnavailable) || p.Hedging.Allowed {
		t.Fatalf("method policy bounds not enforced: deadline=%v retry=%v/%v hedge=%v", deadline, p.CanRetry(1, 503), p.CanRetry(2, 503), p.Hedging.Allowed)
	}
	budget := p.NewWorkBudget()
	if !budget.Consume(p.Budget.MaxWorkUnits) || budget.Consume(1) || budget.Remaining() != 0 {
		t.Fatal("work budget did not stop at the declared bound")
	}
}

func TestTodo_ENDPOINT_007_Property(t *testing.T) {
	for _, class := range []manifest.IdempotencyClass{manifest.IdempotencyReadSafe, manifest.IdempotencyKey, manifest.IdempotencyNone} {
		p, err := Compile(testDefinition(http.MethodPost, "*", class))
		if err != nil {
			t.Fatal(err)
		}
		if p.Deadline <= 0 || p.Budget.MaxWorkUnits <= 0 || p.Budget.MaxConcurrency <= 0 {
			t.Fatal("incomplete method budget")
		}
		if class == manifest.IdempotencyNone && p.Retry.Allowed {
			t.Fatal("non-idempotent operation is retryable")
		}
	}
}

func TestTodo_ENDPOINT_007_Race(t *testing.T) {
	p, _ := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	budget := p.NewWorkBudget()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = budget.Consume(1) }()
	}
	wg.Wait()
	if budget.Remaining() < 0 {
		t.Fatal("work budget became negative")
	}
}

func TestTodo_ENDPOINT_007_Integration(t *testing.T) {
	p, _ := Compile(testDefinition(http.MethodGet, "", manifest.IdempotencyReadSafe))
	ctx, cancel := p.Context(context.Background())
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if !p.CanRetry(1, http.StatusGatewayTimeout) {
		t.Fatal("read-safe timeout was not retryable")
	}
}

func TestTodo_ENDPOINT_007_Fault(t *testing.T) {
	p, _ := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limiter := p.NewLimiter()
	if err := limiter.Acquire(ctx); err == nil {
		t.Fatal("cancelled acquisition succeeded")
	}
}

func TestTodo_ENDPOINT_007_Security(t *testing.T) {
	p, _ := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyNone))
	if p.CanRetry(1, http.StatusServiceUnavailable) || p.Hedging.Allowed || p.Effect != EffectNonIdempotentWrite {
		t.Fatal("effect-bearing non-idempotent method can be replayed")
	}
}

func TestTodo_ENDPOINT_007_Conformance(t *testing.T) {
	table, err := DefaultTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range table.byProcedure {
		if p.Deadline <= 0 || p.Budget.MaxBodyBytes <= 0 || p.Hedging.Allowed {
			t.Fatal("default method policy violates endpoint bounds")
		}
	}
}

func BenchmarkTodo_ENDPOINT_007(b *testing.B) {
	p, _ := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	for i := 0; i < b.N; i++ {
		_ = p.CanRetry(1, http.StatusServiceUnavailable)
	}
}

func equalOutcome() Outcome {
	return Outcome{RequestDigest: "req", ResultDigest: "result", EvidenceDigest: "evidence", Status: "COMMITTED", SideEffects: SideEffects{Intents: 1, Events: 1, Outbox: 1}}
}

func TestEndpointHarnessDetectsTransportSemanticDivergence(t *testing.T) {
	v := Vector{Name: "create", Direct: func(context.Context) Outcome { return equalOutcome() }, GRPC: func(context.Context) Outcome { return equalOutcome() }, HTTP: func(context.Context) Outcome {
		out := equalOutcome()
		out.ResultDigest = "mutated"
		out.SideEffects.Effects = 1
		return out
	}}
	report := Compare(context.Background(), v)
	if err := report.Assert(); err == nil {
		t.Fatal("harness missed a semantic divergence")
	}
	if len(report.Mismatches) != 2 {
		t.Fatalf("mismatches = %d, want result and side effects", len(report.Mismatches))
	}
}

func TestTodo_ENDPOINT_008_Property(t *testing.T) {
	v := Vector{Name: "equal", Direct: func(context.Context) Outcome { return equalOutcome() }, GRPC: func(context.Context) Outcome { return equalOutcome() }, HTTP: func(context.Context) Outcome { return equalOutcome() }}
	if report := Compare(context.Background(), v); report.Assert() != nil {
		t.Fatal(report.Assert())
	}
}

func TestTodo_ENDPOINT_008_Golden(t *testing.T) {
	out := equalOutcome()
	report := Compare(context.Background(), Vector{Name: "golden", Direct: func(context.Context) Outcome { return out }, GRPC: func(context.Context) Outcome { return out }, HTTP: func(context.Context) Outcome { return out }})
	if len(report.Mismatches) != 0 || report.Outcomes["http"].Status != "COMMITTED" {
		t.Fatalf("unexpected golden report: %+v", report)
	}
}

func FuzzTodo_ENDPOINT_008(f *testing.F) {
	f.Add("same")
	f.Add("other")
	f.Fuzz(func(t *testing.T, digest string) {
		v := Vector{Name: "fuzz", Direct: func(context.Context) Outcome { return Outcome{RequestDigest: digest} }, GRPC: func(context.Context) Outcome { return Outcome{RequestDigest: digest} }, HTTP: func(context.Context) Outcome { return Outcome{RequestDigest: digest} }}
		if err := Compare(context.Background(), v).Assert(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_ENDPOINT_008_Race(t *testing.T) {
	v := Vector{Name: "race", Direct: func(context.Context) Outcome { return equalOutcome() }, GRPC: func(context.Context) Outcome { return equalOutcome() }, HTTP: func(context.Context) Outcome { return equalOutcome() }}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = Compare(context.Background(), v).Assert() }()
	}
	wg.Wait()
}

func TestTodo_ENDPOINT_008_Integration(t *testing.T) {
	table := testTable(t, testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	request := func(context.Context) Outcome {
		called := 0
		handler := HTTPMiddleware(table, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.Write([]byte("ok")) }))
		req := httptest.NewRequest(http.MethodPost, "http://example.test"+testProcedure, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return Outcome{RequestDigest: RequestDigest("POST", testProcedure, []byte("{}")), ResultDigest: rec.Body.String(), Status: http.StatusText(rec.Code), SideEffects: SideEffects{Work: called}}
	}
	if err := Compare(context.Background(), Vector{Name: "http", Direct: request, GRPC: request, HTTP: request}).Assert(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ENDPOINT_008_Fault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := Vector{Name: "cancelled", Direct: func(ctx context.Context) Outcome { return Outcome{ErrorDigest: ctx.Err().Error()} }, GRPC: func(ctx context.Context) Outcome { return Outcome{ErrorDigest: ctx.Err().Error()} }, HTTP: func(ctx context.Context) Outcome { return Outcome{ErrorDigest: ctx.Err().Error()} }}
	if err := Compare(ctx, v).Assert(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ENDPOINT_008_Security(t *testing.T) {
	out := equalOutcome()
	v := Vector{Name: "security", Direct: func(context.Context) Outcome { return out }, GRPC: func(context.Context) Outcome { return out }, HTTP: func(context.Context) Outcome { changed := out; changed.SideEffects.Effects++; return changed }}
	if err := Compare(context.Background(), v).Assert(); err == nil {
		t.Fatal("side-effect divergence was not reported")
	}
}

func TestTodo_ENDPOINT_008_Conformance(t *testing.T) {
	if Version() != 1 || Explain(Policy{}) == "" {
		t.Fatal("endpoint contract metadata is not available")
	}
	report := Compare(context.Background(), Vector{Name: "conformance", Direct: func(context.Context) Outcome { return equalOutcome() }, GRPC: func(context.Context) Outcome { return equalOutcome() }, HTTP: func(context.Context) Outcome { return equalOutcome() }})
	if err := report.Assert(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ENDPOINT_008_Mutation(t *testing.T) {
	out := equalOutcome()
	v := Vector{Name: "mutation", Direct: func(context.Context) Outcome { return out }, GRPC: func(context.Context) Outcome { changed := out; changed.ErrorDigest = "tampered"; return changed }, HTTP: func(context.Context) Outcome { return out }}
	if err := Compare(context.Background(), v).Assert(); err == nil {
		t.Fatal("error digest mutation escaped parity")
	}
}

func TestGzipBodyLimit(t *testing.T) {
	p, _ := Compile(testDefinition(http.MethodPost, "*", manifest.IdempotencyKey))
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write([]byte(strings.Repeat("x", int(p.Budget.MaxBodyBytes)+1)))
	_ = zw.Close()
	table := Table{byProcedure: map[string]Policy{p.Procedure: p}}
	h := HTTPMiddleware(table, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodPost, "http://example.test"+p.Procedure, &compressed)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("gzip body status=%d, want 413", rec.Code)
	}
}
