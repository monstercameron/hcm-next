package edge

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ingressPolicyVersion = 1

// Version returns the version of the hostile-ingress contract.
func Version() int { return ingressPolicyVersion }

// Explain describes the ingress contract without including request data.
func Explain() string { return "bounded request-shape WAF and connection-burst admission" }

type IngressOutcome string

const (
	IngressAllow    IngressOutcome = "ALLOW"
	IngressThrottle IngressOutcome = "THROTTLE"
	IngressReject   IngressOutcome = "REJECT"
)

type IngressReason string

const (
	ReasonAmbiguousFraming     IngressReason = "AMBIGUOUS_FRAMING"
	ReasonMalformedFraming     IngressReason = "MALFORMED_FRAMING"
	ReasonHeaderTooLarge       IngressReason = "HEADERS_TOO_LARGE"
	ReasonBodyTooLarge         IngressReason = "BODY_TOO_LARGE"
	ReasonCompressionBomb      IngressReason = "COMPRESSION_BOMB"
	ReasonForbiddenMethod      IngressReason = "FORBIDDEN_METHOD"
	ReasonForbiddenContentType IngressReason = "FORBIDDEN_CONTENT_TYPE"
	ReasonForbiddenEncoding    IngressReason = "FORBIDDEN_ENCODING"
	ReasonConnectionBurst      IngressReason = "CONNECTION_BURST"
	ReasonInvalidRequest       IngressReason = "INVALID_REQUEST"
)

// RequestShapePolicy contains only bounded edge controls. The zero value is
// not used directly; DefaultRequestShapePolicy supplies safe defaults.
type RequestShapePolicy struct {
	MaxHeaderBytes      int
	MaxHeaderCount      int
	MaxBodyBytes        int64
	MaxCompressedBytes  int64
	MaxCompressionRatio int64
	AllowedMethods      []string
	AllowedContentTypes []string
	AllowedEncodings    []string
	RetryAfter          int
}

// DefaultRequestShapePolicy is the conservative policy used by NewHandler.
func DefaultRequestShapePolicy() RequestShapePolicy {
	return RequestShapePolicy{
		MaxHeaderBytes: 64 << 10, MaxHeaderCount: 128, MaxBodyBytes: 4 << 20,
		MaxCompressedBytes: 4 << 20, MaxCompressionRatio: 100,
		AllowedMethods:      []string{http.MethodGet, http.MethodHead, http.MethodPost},
		AllowedContentTypes: []string{"application/json", "application/proto", "application/protobuf"},
		AllowedEncodings:    []string{"identity", "gzip"}, RetryAfter: 1,
	}
}

// RequestShape is the pure input to CheckRequest. Body is the bytes as
// received; for gzip it is compressed bytes.
type RequestShape struct {
	Method           string
	Headers          http.Header
	ContentLength    int64
	TransferEncoding []string
	Body             []byte
}

// IngressDecision is an audit-safe WAF decision. NormalizedBody is returned
// only for an allowed request so middleware can pass decompressed bytes to the
// protocol decoder without allowing an expansion to escape the limits.
type IngressDecision struct {
	Outcome         IngressOutcome
	Reason          IngressReason
	Rule            string
	Status          int
	RetryAfter      int
	CompressedBytes int64
	BodyBytes       int64
	NormalizedBody  []byte
}

func (d IngressDecision) Explain() string {
	return fmt.Sprintf("edge ingress outcome=%s reason=%s rule=%s status=%d retry_after=%d compressed_bytes=%d body_bytes=%d", d.Outcome, d.Reason, d.Rule, d.Status, d.RetryAfter, d.CompressedBytes, d.BodyBytes)
}

// CheckRequest evaluates request shape and returns a typed decision. It does
// not authenticate, authorize, call domain code, or mutate a caller-owned
// request.
func CheckRequest(shape RequestShape, policy RequestShapePolicy) IngressDecision {
	policy = normalizeIngressPolicy(policy)
	base := IngressDecision{Outcome: IngressAllow, Status: http.StatusOK, NormalizedBody: append([]byte(nil), shape.Body...)}
	if shape.ContentLength < -1 {
		return rejectIngress(ReasonMalformedFraming, "edge.framing.content_length")
	}
	if !allowedFold(policy.AllowedMethods, shape.Method) {
		return rejectIngressStatus(ReasonForbiddenMethod, "edge.method.allowlist", http.StatusMethodNotAllowed)
	}
	if headerBytes(shape.Headers) > policy.MaxHeaderBytes || len(shape.Headers) > policy.MaxHeaderCount {
		return rejectIngressStatus(ReasonHeaderTooLarge, "edge.headers.budget", http.StatusRequestHeaderFieldsTooLarge)
	}
	if ambiguousFraming(shape) {
		return rejectIngress(ReasonAmbiguousFraming, "edge.framing.single_message_length")
	}
	if contentLengthMismatch(shape) {
		return rejectIngress(ReasonMalformedFraming, "edge.framing.content_length_consistency")
	}
	if malformedTransferEncoding(shape) {
		return rejectIngress(ReasonMalformedFraming, "edge.framing.transfer_encoding")
	}
	if hasBody(shape) && (shape.Method == http.MethodGet || shape.Method == http.MethodHead) {
		return rejectIngressStatus(ReasonForbiddenContentType, "edge.body.read_methods", http.StatusUnsupportedMediaType)
	}
	encoding, ok := singleToken(strings.Join(shape.Headers.Values("Content-Encoding"), ","))
	if !ok {
		return rejectIngress(ReasonForbiddenEncoding, "edge.encoding.single_encoding")
	}
	if encoding == "" {
		encoding = "identity"
	}
	if !allowedFold(policy.AllowedEncodings, encoding) {
		return rejectIngress(ReasonForbiddenEncoding, "edge.encoding.allowlist")
	}
	if int64(len(shape.Body)) > policy.MaxCompressedBytes {
		return rejectIngressStatus(ReasonBodyTooLarge, "edge.body.compressed_budget", http.StatusRequestEntityTooLarge)
	}
	if !hasBody(shape) {
		return base
	}
	contentType, err := mediaType(strings.Join(shape.Headers.Values("Content-Type"), ","))
	if err != nil || !allowedFold(policy.AllowedContentTypes, contentType) {
		return rejectIngressStatus(ReasonForbiddenContentType, "edge.content_type.allowlist", http.StatusUnsupportedMediaType)
	}

	base.CompressedBytes = int64(len(shape.Body))
	if encoding == "gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(shape.Body))
		if err != nil {
			return rejectIngressStatus(ReasonMalformedFraming, "edge.gzip.header", http.StatusBadRequest)
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, policy.MaxBodyBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return rejectIngressStatus(ReasonCompressionBomb, "edge.gzip.expansion_budget", http.StatusRequestEntityTooLarge)
		}
		base.NormalizedBody = body
	} else {
		base.NormalizedBody = append([]byte(nil), shape.Body...)
	}
	base.BodyBytes = int64(len(base.NormalizedBody))
	if base.BodyBytes > policy.MaxBodyBytes || (base.CompressedBytes > 0 && base.BodyBytes > base.CompressedBytes*policy.MaxCompressionRatio) {
		return rejectIngressStatus(ReasonCompressionBomb, "edge.body.expansion_budget", http.StatusRequestEntityTooLarge)
	}
	return base
}

func normalizeIngressPolicy(policy RequestShapePolicy) RequestShapePolicy {
	defaults := DefaultRequestShapePolicy()
	if policy.MaxHeaderBytes <= 0 {
		policy.MaxHeaderBytes = defaults.MaxHeaderBytes
	}
	if policy.MaxHeaderCount <= 0 {
		policy.MaxHeaderCount = defaults.MaxHeaderCount
	}
	if policy.MaxBodyBytes <= 0 {
		policy.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if policy.MaxCompressedBytes <= 0 {
		policy.MaxCompressedBytes = defaults.MaxCompressedBytes
	}
	if policy.MaxCompressionRatio <= 0 {
		policy.MaxCompressionRatio = defaults.MaxCompressionRatio
	}
	if len(policy.AllowedMethods) == 0 {
		policy.AllowedMethods = defaults.AllowedMethods
	}
	if len(policy.AllowedContentTypes) == 0 {
		policy.AllowedContentTypes = defaults.AllowedContentTypes
	}
	if len(policy.AllowedEncodings) == 0 {
		policy.AllowedEncodings = defaults.AllowedEncodings
	}
	if policy.RetryAfter <= 0 {
		policy.RetryAfter = defaults.RetryAfter
	}
	return policy
}

func rejectIngress(reason IngressReason, rule string) IngressDecision {
	status := http.StatusBadRequest
	if reason == ReasonConnectionBurst {
		status = http.StatusTooManyRequests
	}
	return rejectIngressStatus(reason, rule, status)
}

func rejectIngressStatus(reason IngressReason, rule string, status int) IngressDecision {
	outcome := IngressReject
	if reason == ReasonConnectionBurst {
		outcome = IngressThrottle
	}
	return IngressDecision{Outcome: outcome, Reason: reason, Rule: rule, Status: status}
}

func hasBody(shape RequestShape) bool { return len(shape.Body) > 0 || shape.ContentLength > 0 }

func headerBytes(header http.Header) int {
	total := 0
	for key, values := range header {
		total += len(key)
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}

func ambiguousFraming(shape RequestShape) bool {
	lengths := shape.Headers.Values("Content-Length")
	if len(lengths) > 1 {
		return true
	}
	transfer := shape.Headers.Values("Transfer-Encoding")
	return len(lengths) > 0 && len(transfer) > 0
}

func contentLengthMismatch(shape RequestShape) bool {
	values := shape.Headers.Values("Content-Length")
	if len(values) == 0 || shape.ContentLength < 0 {
		return false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 64)
	return err != nil || value < 0 || value != shape.ContentLength
}

func malformedTransferEncoding(shape RequestShape) bool {
	values := append([]string(nil), shape.TransferEncoding...)
	if len(values) == 0 {
		values = shape.Headers.Values("Transfer-Encoding")
	}
	if len(values) == 0 {
		return false
	}
	joined := strings.Join(values, ",")
	parts := strings.Split(joined, ",")
	if len(parts) != 1 || !strings.EqualFold(strings.TrimSpace(parts[0]), "chunked") {
		return true
	}
	return false
}

func singleToken(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 1 {
		return "", false
	}
	value := strings.ToLower(strings.TrimSpace(parts[0]))
	return value, value != ""
}

func mediaType(raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", errors.New("edge: invalid media type")
	}
	value, _, err := mime.ParseMediaType(raw)
	return strings.ToLower(value), err
}

func allowedFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

// IngressMiddleware enforces the request-shape decision before domain
// handlers. A rejected or throttled request never reaches next.
func IngressMiddleware(next http.Handler, policy RequestShapePolicy, gate *BurstGate) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	policy = normalizeIngressPolicy(policy)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader := r.Body
		if reader == nil {
			reader = http.NoBody
		}
		body, err := io.ReadAll(io.LimitReader(reader, policy.MaxCompressedBytes+1))
		if err != nil {
			writeIngressDecision(w, rejectIngress(ReasonMalformedFraming, "edge.body.read"))
			return
		}
		shape := RequestShape{Method: r.Method, Headers: r.Header, ContentLength: r.ContentLength, TransferEncoding: r.TransferEncoding, Body: body}
		decision := CheckRequest(shape, policy)
		if decision.Outcome == IngressAllow && gate != nil {
			key := r.RemoteAddr
			if key == "" {
				key = "unknown"
			}
			burst := gate.Check(key, time.Now().UTC())
			if burst.Outcome != IngressAllow {
				decision = burst
			}
		}
		if decision.Outcome != IngressAllow {
			writeIngressDecision(w, decision)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(decision.NormalizedBody))
		r.ContentLength = int64(len(decision.NormalizedBody))
		r.Header.Del("Content-Encoding")
		next.ServeHTTP(w, r)
	})
}

func writeIngressDecision(w http.ResponseWriter, decision IngressDecision) {
	if decision.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(decision.RetryAfter))
	}
	w.Header().Set("X-HCM-Edge-Decision", string(decision.Outcome))
	http.Error(w, string(decision.Reason), decision.Status)
}

// BurstGate is a bounded in-process connection/request burst counter. It is
// an edge safety valve, not a durable tenant quota; durable tenant accounting
// belongs to internal/operations/admission.
type BurstGate struct {
	mu      sync.Mutex
	window  time.Duration
	max     int
	retry   int
	entries map[string]burstEntry
}

type burstEntry struct {
	started time.Time
	count   int
}

// NewBurstGate creates a deterministic-window burst gate.
func NewBurstGate(window time.Duration, maxRequests, retryAfter int) (*BurstGate, error) {
	if window <= 0 || maxRequests <= 0 || retryAfter <= 0 {
		return nil, errors.New("edge: invalid burst gate")
	}
	return &BurstGate{window: window, max: maxRequests, retry: retryAfter, entries: make(map[string]burstEntry)}, nil
}

// Check records one request for key and returns a typed allow/throttle result.
func (g *BurstGate) Check(key string, now time.Time) IngressDecision {
	if g == nil {
		return IngressDecision{Outcome: IngressAllow, Status: http.StatusOK}
	}
	if strings.TrimSpace(key) == "" || now.IsZero() {
		return rejectIngress(ReasonInvalidRequest, "edge.burst.key")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.entries == nil {
		g.entries = make(map[string]burstEntry)
	}
	entry := g.entries[key]
	if entry.started.IsZero() || now.Before(entry.started) || now.Sub(entry.started) >= g.window {
		entry = burstEntry{started: now}
	}
	entry.count++
	g.entries[key] = entry
	if entry.count > g.max {
		return IngressDecision{Outcome: IngressThrottle, Reason: ReasonConnectionBurst, Rule: "edge.connection.burst", Status: http.StatusTooManyRequests, RetryAfter: g.retry}
	}
	return IngressDecision{Outcome: IngressAllow, Status: http.StatusOK}
}
