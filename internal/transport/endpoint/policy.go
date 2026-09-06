// Package endpoint owns the reusable edge policy compiled from the canonical
// endpoint manifest. It contains transport mechanics only: business handlers
// remain behind the supplied http.Handler and are never selected here.
package endpoint

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/transport/manifest"
)

const policyVersion = 1

// Version returns the version of the endpoint policy contract.
func Version() int { return policyVersion }

// Explain returns a stable, human-readable summary of one compiled policy.
// It is intentionally derived from typed fields rather than from request data.
func Explain(p Policy) string {
	return fmt.Sprintf("%s %s deadline=%s retry=%t/%d hedge=%t body=%d", p.Procedure, p.Method, p.Deadline, p.Retry.Allowed, p.Retry.MaxAttempts, p.Hedging.Allowed, p.Budget.MaxBodyBytes)
}

// EffectClass describes the replay safety of a method.
type EffectClass string

const (
	EffectReadOnly           EffectClass = "READ_ONLY"
	EffectIdempotentWrite    EffectClass = "IDEMPOTENT_WRITE"
	EffectNonIdempotentWrite EffectClass = "NON_IDEMPOTENT_WRITE"
)

// RetryPolicy is the transport retry contract for one method.
type RetryPolicy struct {
	Allowed        bool
	MaxAttempts    int
	RetryableCodes []int
}

// HedgingPolicy states whether concurrent speculative attempts are permitted.
type HedgingPolicy struct {
	Allowed bool
}

// ResourceBudget bounds request bytes and logical work. MaxConcurrency is
// enforced by Limiter, which is deliberately a caller-owned process resource.
type ResourceBudget struct {
	MaxBodyBytes       int64
	MaxCompressedBytes int64
	MaxWorkUnits       int64
	MaxConcurrency     int
}

// Policy is the compiled edge contract for one endpoint.
type Policy struct {
	Procedure         string
	HTTPPath          string
	Method            string
	BodyBinding       string
	ContentTypes      []string
	ContentEncodings  []string
	CacheControl      string
	SensitiveResponse bool
	AllowedHosts      []string
	AllowedOrigins    []string
	RequireCSRF       bool
	CSRFHeader        string
	CSRFCookie        string
	Deadline          time.Duration
	Retry             RetryPolicy
	Hedging           HedgingPolicy
	Effect            EffectClass
	Budget            ResourceBudget
	SecurityHeaders   http.Header
}

// Table is an immutable-by-convention procedure/path lookup table.
type Table struct {
	byProcedure map[string]Policy
	byRoute     map[string]Policy
}

// Compile converts one generated manifest row into an executable edge policy.
func Compile(def manifest.EndpointDefinition) (Policy, error) {
	if def.GRPCProcedure == "" || def.HTTPMethod == "" {
		return Policy{}, errors.New("endpoint: manifest row is missing procedure or method")
	}
	method := strings.ToUpper(strings.TrimSpace(def.HTTPMethod))
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodHead {
		return Policy{}, fmt.Errorf("endpoint: unsupported HTTP method %q", method)
	}
	if def.DeadlineBudgetMillis <= 0 {
		return Policy{}, errors.New("endpoint: manifest row has no positive deadline budget")
	}

	p := Policy{
		Procedure:         def.GRPCProcedure,
		HTTPPath:          def.HTTPPathTemplate,
		Method:            method,
		BodyBinding:       def.HTTPBodyBinding,
		ContentTypes:      []string{"application/json", "application/proto", "application/protobuf"},
		ContentEncodings:  []string{"identity", "gzip"},
		CacheControl:      "no-store",
		SensitiveResponse: true,
		RequireCSRF:       method == http.MethodPost,
		CSRFHeader:        "X-HCM-CSRF-Token",
		CSRFCookie:        "hcmnext_browser_csrf",
		Deadline:          time.Duration(def.DeadlineBudgetMillis) * time.Millisecond,
		Budget: ResourceBudget{
			MaxBodyBytes:       4 << 20,
			MaxCompressedBytes: 4 << 20,
			MaxWorkUnits:       1000,
			MaxConcurrency:     32,
		},
		SecurityHeaders: canonicalSecurityHeaders(),
	}
	if method == http.MethodGet || method == http.MethodHead {
		p.BodyBinding = ""
		p.ContentTypes = nil
	}
	if def.Disposition != manifest.DispositionServed {
		// Refused methods still receive the same edge limits. The owner decides
		// the business refusal after admission; this layer never reimplements it.
		p.CacheControl = "no-store"
	}

	switch def.IntentBehavior {
	case manifest.IntentBehaviorObserves, manifest.IntentBehaviorNonMaterial:
		p.Effect = EffectReadOnly
	case manifest.IntentBehaviorCreates, manifest.IntentBehaviorConsumes, manifest.IntentBehaviorEmits:
		p.Effect = EffectIdempotentWrite
	default:
		return Policy{}, fmt.Errorf("endpoint: invalid intent behavior %q", def.IntentBehavior)
	}
	switch def.IdempotencyClass {
	case manifest.IdempotencyReadSafe:
		p.Retry = RetryPolicy{Allowed: true, MaxAttempts: 3, RetryableCodes: []int{408, 429, 500, 502, 503, 504}}
	case manifest.IdempotencyKey:
		p.Retry = RetryPolicy{Allowed: true, MaxAttempts: 2, RetryableCodes: []int{429, 502, 503, 504}}
	case manifest.IdempotencyNone:
		p.Effect = EffectNonIdempotentWrite
		p.Retry = RetryPolicy{Allowed: false, MaxAttempts: 1}
	default:
		return Policy{}, fmt.Errorf("endpoint: invalid idempotency class %q", def.IdempotencyClass)
	}
	// Hedging is not enabled by the P1A manifest. In particular, no effect
	// bearing method may be speculatively invoked twice.
	p.Hedging = HedgingPolicy{Allowed: false}
	return p, nil
}

// CompileManifest compiles and indexes a canonical endpoint manifest.
func CompileManifest(m *manifest.EndpointManifest) (Table, error) {
	if m == nil {
		return Table{}, errors.New("endpoint: nil endpoint manifest")
	}
	t := Table{byProcedure: make(map[string]Policy, len(m.Endpoints)), byRoute: make(map[string]Policy, len(m.Endpoints))}
	for _, def := range m.Endpoints {
		p, err := Compile(def)
		if err != nil {
			return Table{}, err
		}
		if _, exists := t.byProcedure[p.Procedure]; exists {
			return Table{}, fmt.Errorf("endpoint: duplicate procedure %q", p.Procedure)
		}
		t.byProcedure[p.Procedure] = p
		if p.HTTPPath != "" {
			key := routeKey(p.Method, p.HTTPPath)
			if _, exists := t.byRoute[key]; exists {
				return Table{}, fmt.Errorf("endpoint: duplicate HTTP route %q", key)
			}
			t.byRoute[key] = p
		}
	}
	return t, nil
}

// DefaultTable compiles the live generated endpoint manifest.
func DefaultTable() (Table, error) {
	m, err := manifest.Build()
	if err != nil {
		return Table{}, err
	}
	return CompileManifest(m)
}

// Lookup resolves a procedure or an exact method/path pair.
func (t Table) Lookup(method, path string) (Policy, bool) {
	if p, ok := t.byProcedure[path]; ok {
		return p, strings.EqualFold(p.Method, method)
	}
	p, ok := t.byRoute[routeKey(strings.ToUpper(method), path)]
	return p, ok
}

func (t Table) routePathKnown(path string) bool {
	for _, p := range t.byRoute {
		if p.HTTPPath == path {
			return true
		}
	}
	return false
}

func routeKey(method, path string) string { return strings.ToUpper(method) + " " + path }

// HTTPMiddleware enforces policy before calling next and applies canonical
// response headers after it. Unknown paths are left to the owner's mux so a
// 404 remains an owner's route decision rather than a policy oracle.
func HTTPMiddleware(t Table, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, known := t.Lookup(r.Method, r.URL.Path)
		if !known {
			if _, procedureExists := t.byProcedure[r.URL.Path]; procedureExists {
				writePolicyError(w, http.StatusMethodNotAllowed)
				return
			}
			if t.routePathKnown(r.URL.Path) {
				writePolicyError(w, http.StatusMethodNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		writer := &policyResponseWriter{ResponseWriter: w, policy: p}
		if err := admitRequest(writer, r, p); err != nil {
			writePolicyError(writer, err.status)
			return
		}
		ctx, cancel := p.Context(r.Context())
		defer cancel()
		next.ServeHTTP(writer, r.WithContext(ctx))
	})
}

type policyError struct{ status int }

func admitRequest(w http.ResponseWriter, r *http.Request, p Policy) *policyError {
	if !validHost(r.Host, p.AllowedHosts) {
		return &policyError{http.StatusForbidden}
	}
	if err := checkOrigin(r, p); err != nil {
		return err
	}
	if r.Method != p.Method {
		return &policyError{http.StatusMethodNotAllowed}
	}
	if !acceptableResponse(r.Header.Get("Accept")) {
		return &policyError{http.StatusNotAcceptable}
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead || p.BodyBinding == "" {
		if hasBody, _ := requestHasBody(r, 1); hasBody {
			return &policyError{http.StatusUnsupportedMediaType}
		}
		return nil
	}
	media, err := mediaType(r.Header.Get("Content-Type"))
	if err != nil || !containsFold(p.ContentTypes, media) {
		return &policyError{http.StatusUnsupportedMediaType}
	}
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	if encoding == "" {
		encoding = "identity"
	}
	if !containsFold(p.ContentEncodings, encoding) {
		return &policyError{http.StatusUnsupportedMediaType}
	}
	if r.ContentLength > p.Budget.MaxCompressedBytes {
		return &policyError{http.StatusRequestEntityTooLarge}
	}
	body, ok := readBoundedBody(r, p.Budget, encoding)
	if !ok {
		return &policyError{http.StatusRequestEntityTooLarge}
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	r.Header.Del("Content-Encoding")
	return nil
}

func requestHasBody(r *http.Request, limit int64) (bool, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return r.ContentLength > 0, nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		return false, err
	}
	return len(b) > 0, nil
}

func readBoundedBody(r *http.Request, b ResourceBudget, encoding string) ([]byte, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true
	}
	compressed, err := io.ReadAll(io.LimitReader(r.Body, b.MaxCompressedBytes+1))
	if err != nil || int64(len(compressed)) > b.MaxCompressedBytes {
		return nil, false
	}
	if encoding == "gzip" {
		zr, err := gzip.NewReader(strings.NewReader(string(compressed)))
		if err != nil {
			return nil, false
		}
		decompressed, err := io.ReadAll(io.LimitReader(zr, b.MaxBodyBytes+1))
		_ = zr.Close()
		if err != nil || int64(len(decompressed)) > b.MaxBodyBytes {
			return nil, false
		}
		return decompressed, true
	}
	if int64(len(compressed)) > b.MaxBodyBytes {
		return nil, false
	}
	return compressed, true
}

func mediaType(raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", errors.New("endpoint: invalid content type")
	}
	mt, _, err := mime.ParseMediaType(raw)
	return strings.ToLower(mt), err
}

func checkOrigin(r *http.Request, p Policy) *policyError {
	if isUnsafe(r.Method) && (strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Purpose")), "prefetch") || strings.EqualFold(strings.TrimSpace(r.Header.Get("Purpose")), "prefetch")) {
		return &policyError{http.StatusForbidden}
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if strings.ContainsAny(origin, "\r\n") || origin == "null" {
		return &policyError{http.StatusForbidden}
	}
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return &policyError{http.StatusForbidden}
		}
		canonical := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
		allowed := p.AllowedOrigins
		if len(allowed) == 0 {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			allowed = []string{scheme + "://" + strings.ToLower(r.Host)}
		}
		matched := false
		for _, value := range allowed {
			if strings.EqualFold(strings.TrimSpace(value), canonical) {
				matched = true
				break
			}
		}
		if !matched {
			return &policyError{http.StatusForbidden}
		}
	}
	if p.RequireCSRF && isUnsafe(r.Method) && (origin != "" || r.Header.Get("Cookie") != "") {
		cookie, err := r.Cookie(p.CSRFCookie)
		if err != nil || cookie.Value == "" || r.Header.Get(p.CSRFHeader) != cookie.Value {
			return &policyError{http.StatusForbidden}
		}
	}
	return nil
}

func validHost(host string, allowed []string) bool {
	if strings.ContainsAny(host, "\r\n") {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if strings.EqualFold(strings.TrimSpace(value), host) {
			return true
		}
	}
	return false
}

func isUnsafe(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func acceptableResponse(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*/*" {
		return true
	}
	for _, item := range strings.Split(raw, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil || strings.EqualFold(params["q"], "0") {
			continue
		}
		switch strings.ToLower(media) {
		case "application/json", "application/proto", "application/protobuf":
			return true
		}
	}
	return false
}

func canonicalSecurityHeaders() http.Header {
	return http.Header{
		"X-Content-Type-Options":  {"nosniff"},
		"Referrer-Policy":         {"no-referrer"},
		"X-Frame-Options":         {"DENY"},
		"Content-Security-Policy": {"default-src 'none'"},
		"Permissions-Policy":      {"geolocation=(), microphone=(), camera=()"},
	}
}

type policyResponseWriter struct {
	http.ResponseWriter
	policy  Policy
	written bool
}

func (w *policyResponseWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	if status >= 300 && status < 400 {
		location := w.Header().Get("Location")
		if location != "" && !sameOriginRedirect(location) {
			w.Header().Del("Location")
			status = http.StatusForbidden
		}
	}
	w.applyHeaders()
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *policyResponseWriter) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *policyResponseWriter) applyHeaders() {
	for key, values := range w.policy.SecurityHeaders {
		w.Header()[key] = append([]string(nil), values...)
	}
	if w.policy.CacheControl != "" {
		w.Header().Set("Cache-Control", w.policy.CacheControl)
	}
	w.Header().Set("Vary", "Origin, Accept-Encoding")
}

func (w *policyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func sameOriginRedirect(raw string) bool {
	if strings.Contains(raw, "\\") || strings.HasPrefix(raw, "//") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && !u.IsAbs() && u.Host == "" && u.User == nil && (strings.HasPrefix(raw, "/") || !strings.Contains(raw, ":"))
}

func writePolicyError(w http.ResponseWriter, status int) {
	if status == 0 {
		status = http.StatusBadRequest
	}
	http.Error(w, http.StatusText(status), status)
}

// Context applies the server cap while preserving an earlier caller deadline.
func (p Policy) Context(parent context.Context) (context.Context, context.CancelFunc) {
	if p.Deadline <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, p.Deadline)
}

// CanRetry answers whether another transport attempt is permitted. It never
// permits retries after the configured attempt count or for unknown statuses.
func (p Policy) CanRetry(attempt, status int) bool {
	if !p.Retry.Allowed || attempt < 1 || attempt >= p.Retry.MaxAttempts {
		return false
	}
	for _, code := range p.Retry.RetryableCodes {
		if code == status {
			return true
		}
	}
	return false
}

// Limiter is a bounded per-policy concurrency gate.
type Limiter struct{ slots chan struct{} }

// NewLimiter returns a limiter using the policy's maximum concurrency.
func (p Policy) NewLimiter() *Limiter {
	max := p.Budget.MaxConcurrency
	if max <= 0 {
		max = 1
	}
	return &Limiter{slots: make(chan struct{}, max)}
}

// Acquire reserves one work slot until Release is called or ctx ends.
func (l *Limiter) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns one work slot.
func (l *Limiter) Release() {
	select {
	case <-l.slots:
	default:
	}
}

// WorkBudget is a concurrency-safe logical work counter.
type WorkBudget struct {
	mu        sync.Mutex
	remaining int64
}

// NewWorkBudget returns a fresh budget for one logical operation.
func (p Policy) NewWorkBudget() *WorkBudget { return &WorkBudget{remaining: p.Budget.MaxWorkUnits} }

// Consume reserves units, returning false when the operation exceeds budget.
func (b *WorkBudget) Consume(units int64) bool {
	if units < 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if units > b.remaining {
		return false
	}
	b.remaining -= units
	return true
}

// Remaining reports the unused logical work budget.
func (b *WorkBudget) Remaining() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining
}

// RequestDigest is the stable request identity used by the parity harness.
func RequestDigest(method, path string, body []byte) string {
	sum := sha256.Sum256(append([]byte(strings.ToUpper(method)+" "+path+"\n"), body...))
	return hex.EncodeToString(sum[:])
}

// SortedHeaderNames returns deterministic header names for diagnostics.
func SortedHeaderNames(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
