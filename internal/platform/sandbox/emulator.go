package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// EmulatorScenario is the closed fault vocabulary for the connector sandbox
// emulator. It models transport-facing behavior without opening a socket or
// depending on a production credential, destination, or database.
type EmulatorScenario string

const (
	EmulatorPage             EmulatorScenario = "PAGE"
	EmulatorRateLimited      EmulatorScenario = "RATE_LIMITED"
	EmulatorTimeoutAfterSend EmulatorScenario = "TIMEOUT_AFTER_SEND"
	EmulatorPartial          EmulatorScenario = "PARTIAL"
	EmulatorSchemaDrift      EmulatorScenario = "SCHEMA_DRIFT"
	EmulatorObservation      EmulatorScenario = "OBSERVATION"
)

// ErrEmulatorScriptExhausted means a test issued more requests than its
// resettable script declared.
var ErrEmulatorScriptExhausted = errors.New("sandbox emulator: script exhausted")

// ErrEmulatorTimeoutAfterSend is an intentionally ambiguous outcome: the
// request was recorded as sent, but the caller receives no provider response.
var ErrEmulatorTimeoutAfterSend = errors.New("sandbox emulator: timeout after send")

// EmulatorStep is one deterministic provider response or fault. Body bytes
// and response headers are copied at construction and never exposed through
// the call log, so a fixture can contain only the data the test deliberately
// supplied.
type EmulatorStep struct {
	Scenario      EmulatorScenario
	Status        int
	Body          []byte
	Headers       http.Header
	SchemaVersion string
}

// EmulatorCall records only bounded request metadata and the scripted result.
type EmulatorCall struct {
	Ordinal  int
	Scenario EmulatorScenario
	Method   string
	URL      string
	Sent     bool
	Status   int
	At       time.Time
}

// EmulatorReset is the evidence returned by a reset.
type EmulatorReset struct {
	Generation uint64
	StepDigest string
	Steps      int
	ResetAt    time.Time
}

// Emulator is a resettable, in-process HTTP provider fixture. It implements
// [interface{ Do(*http.Request) (*http.Response, error)}] structurally, so it
// can be passed directly as internal/connectivity/transport.Client.HTTP.
type Emulator struct {
	baseline []EmulatorStep
	now      func() time.Time

	mu         sync.Mutex
	steps      []EmulatorStep
	position   int
	generation uint64
	calls      []EmulatorCall
}

// NewEmulator validates and freezes a script. A script is intentionally
// bounded so a test cannot accidentally turn the emulator into an unbounded
// payload or memory source.
func NewEmulator(steps []EmulatorStep) (*Emulator, error) {
	return NewEmulatorWithClock(steps, nil)
}

// NewEmulatorWithClock is [NewEmulator] with an injectable clock for stable
// reset evidence.
func NewEmulatorWithClock(steps []EmulatorStep, now func() time.Time) (*Emulator, error) {
	if len(steps) == 0 || len(steps) > 256 {
		return nil, errors.New("sandbox emulator: script must contain 1..256 steps")
	}
	copySteps := make([]EmulatorStep, len(steps))
	for i, step := range steps {
		if err := validateStep(step); err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		copySteps[i] = cloneStep(step)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	e := &Emulator{baseline: copySteps, now: now}
	e.steps = cloneSteps(copySteps)
	return e, nil
}

func validateStep(step EmulatorStep) error {
	switch step.Scenario {
	case EmulatorPage, EmulatorRateLimited, EmulatorTimeoutAfterSend,
		EmulatorPartial, EmulatorSchemaDrift, EmulatorObservation:
	default:
		return fmt.Errorf("unknown scenario %q", step.Scenario)
	}
	if len(step.Body) > 1<<20 {
		return errors.New("body exceeds 1 MiB fixture bound")
	}
	if step.Scenario != EmulatorTimeoutAfterSend {
		status := step.Status
		if status == 0 {
			status = http.StatusOK
		}
		if status < 100 || status > 599 {
			return fmt.Errorf("status %d is invalid", status)
		}
	}
	return nil
}

func cloneStep(step EmulatorStep) EmulatorStep {
	step.Body = append([]byte(nil), step.Body...)
	step.Headers = step.Headers.Clone()
	return step
}

func cloneSteps(steps []EmulatorStep) []EmulatorStep {
	out := make([]EmulatorStep, len(steps))
	for i, step := range steps {
		out[i] = cloneStep(step)
	}
	return out
}

// Reset restores the exact original script, clears call evidence, and
// increments the generation. The digest remains stable across every reset.
func (e *Emulator) Reset() EmulatorReset {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.position = 0
	e.steps = cloneSteps(e.baseline)
	e.calls = nil
	e.generation++
	return EmulatorReset{
		Generation: e.generation,
		StepDigest: digestSteps(e.baseline),
		Steps:      len(e.baseline),
		ResetAt:    e.now().UTC(),
	}
}

// StepDigest returns the immutable baseline digest.
func (e *Emulator) StepDigest() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return digestSteps(e.baseline)
}

// Calls returns a copy of bounded call evidence in request order.
func (e *Emulator) Calls() []EmulatorCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]EmulatorCall(nil), e.calls...)
}

// Do implements the transport client's narrow HTTP doer surface.
func (e *Emulator) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("sandbox emulator: request is required")
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.position >= len(e.steps) {
		return nil, ErrEmulatorScriptExhausted
	}
	step := e.steps[e.position]
	e.position++
	call := EmulatorCall{
		Ordinal:  len(e.calls) + 1,
		Scenario: step.Scenario,
		Method:   req.Method,
		URL:      safeRequestURL(req),
		Sent:     true,
		At:       e.now().UTC(),
	}
	if step.Scenario == EmulatorTimeoutAfterSend {
		e.calls = append(e.calls, call)
		return nil, ErrEmulatorTimeoutAfterSend
	}
	status := step.Status
	if status == 0 {
		status = http.StatusOK
		if step.Scenario == EmulatorRateLimited {
			status = http.StatusTooManyRequests
		}
	}
	call.Status = status
	e.calls = append(e.calls, call)
	body := append([]byte(nil), step.Body...)
	headers := step.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	if step.Scenario == EmulatorPartial {
		headers.Set("X-Sandbox-Partial", "true")
	}
	if step.Scenario == EmulatorSchemaDrift && step.SchemaVersion != "" {
		headers.Set("X-Sandbox-Schema-Version", step.SchemaVersion)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     headers,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func safeRequestURL(req *http.Request) string {
	if req == nil || req.URL == nil || req.URL.Hostname() == "" {
		return "<invalid>"
	}
	return req.URL.Scheme + "://" + req.URL.Hostname()
}

// ServeHTTP exposes the same script to an httptest server when a test needs
// to exercise the net/http server path. The in-process Do path remains the
// preferred path because it cannot accidentally leave a listening socket.
func (e *Emulator) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := e.Do(req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrEmulatorTimeoutAfterSend) {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func digestSteps(steps []EmulatorStep) string {
	h := sha256.New()
	for _, step := range steps {
		_, _ = io.WriteString(h, string(step.Scenario))
		_, _ = io.WriteString(h, "\x00")
		_, _ = io.WriteString(h, fmt.Sprintf("%d", step.Status))
		_, _ = io.WriteString(h, "\x00")
		_, _ = h.Write(step.Body)
		_, _ = io.WriteString(h, "\x00")
		_, _ = io.WriteString(h, step.SchemaVersion)
		_, _ = io.WriteString(h, "\x00")
		for _, pair := range sortedHeaders(step.Headers) {
			_, _ = io.WriteString(h, pair[0])
			_, _ = io.WriteString(h, "=")
			_, _ = io.WriteString(h, pair[1])
			_, _ = io.WriteString(h, "\x00")
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func sortedHeaders(headers http.Header) [][2]string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sortStrings(keys)
	out := make([][2]string, 0, len(keys))
	for _, key := range keys {
		values := append([]string(nil), headers.Values(key)...)
		out = append(out, [2]string{key, stringsJoin(values, "\x1f")})
	}
	return out
}

// Local helpers keep this fixture package's digest implementation explicit
// and avoid bringing a sorting or string utility dependency into the kernel.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func stringsJoin(values []string, sep string) string {
	var out string
	for i, value := range values {
		if i > 0 {
			out += sep
		}
		out += value
	}
	return out
}
