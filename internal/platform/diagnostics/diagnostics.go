// Package diagnostics owns the admission and redaction policy for operational
// inspection. It deliberately does not expose an HTTP listener or mutate
// application state.
package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Surface string

const contractVersion = 1

// Version identifies the stable diagnostic admission contract.
func Version() int { return contractVersion }

// Explain describes the package boundary without exposing runtime endpoints.
func Explain() string { return "default-off scoped expiring diagnostic admission and redaction" }

// Compatibility aliases keep the policy vocabulary explicit at integration
// boundaries without introducing another registry or adapter.
type DiagnosticSurface = Surface

const (
	SurfacePprof     Surface = "pprof"
	SurfaceExpvar    Surface = "expvar"
	SurfaceMetrics   Surface = "metrics"
	SurfaceDebug     Surface = "debug"
	SurfaceConfig    Surface = "config"
	SurfaceGoroutine Surface = "goroutine"
	SurfaceHeap      Surface = "heap"
	SurfaceTrace     Surface = "trace"
)

type Classification string

const (
	ClassificationPublic     Classification = "PUBLIC"
	ClassificationProtected  Classification = "PROTECTED"
	ClassificationRestricted Classification = "RESTRICTED"
)

type Budget struct {
	MaxDuration time.Duration
	MaxBytes    int64
	MaxCPU      time.Duration
}

type Manifest struct {
	Enabled  bool
	Surfaces map[Surface]bool
	Budget   Budget
}

type DiagnosticManifest = Manifest

// DefaultManifest is intentionally disabled and has no enabled surfaces.
func DefaultManifest() Manifest {
	return Manifest{Enabled: false, Surfaces: map[Surface]bool{}, Budget: Budget{MaxDuration: 5 * time.Minute, MaxBytes: 8 << 20, MaxCPU: 30 * time.Second}}
}

type Workload struct {
	ID            string
	Authenticated bool
}

type Request struct {
	Workload Workload
	Tenant   string
	Purpose  string
	Surface  Surface
	Duration time.Duration
	Budget   Budget
	Now      time.Time
}

type DiagnosticRequest = Request

type EventKind string

const (
	EventAccess EventKind = "ACCESS"
	EventDrop   EventKind = "DROP"
)

type Event struct {
	Kind     EventKind
	At       time.Time
	Workload string
	Tenant   string
	Purpose  string
	Surface  Surface
	Artifact string
	Reason   string
}

type DiagnosticEvent = Event

type Artifact struct {
	ID             string
	Tenant         string
	Surface        Surface
	Classification Classification
	Bytes          []byte
	Redacted       bool
	CreatedAt      time.Time
}

type ProtectedArtifact = Artifact

type Session struct {
	m         *Manager
	request   Request
	expiresAt time.Time
	budget    Budget
	used      int64
	usedCPU   time.Duration
	closed    bool
	mu        sync.Mutex
}

type Manager struct {
	mu       sync.RWMutex
	manifest Manifest
	clock    func() time.Time
	events   []Event
}

var (
	ErrDisabled       = errors.New("diagnostics: disabled")
	ErrUnauthorized   = errors.New("diagnostics: workload is not authenticated")
	ErrInvalidScope   = errors.New("diagnostics: tenant and purpose are required")
	ErrSurfaceDenied  = errors.New("diagnostics: surface is not enabled")
	ErrDurationDenied = errors.New("diagnostics: duration exceeds policy")
	ErrBudgetDenied   = errors.New("diagnostics: budget exceeds policy")
	ErrExpired        = errors.New("diagnostics: session expired")
	ErrClosed         = errors.New("diagnostics: session closed")
	ErrCaptureBounds  = errors.New("diagnostics: capture exceeds budget")
)

func NewManager(manifest Manifest) *Manager {
	if manifest.Surfaces == nil {
		manifest.Surfaces = map[Surface]bool{}
	}
	return &Manager{manifest: cloneManifest(manifest), clock: time.Now}
}

// NewManagerWithClock is useful for deterministic expiry tests and embedded
// runtimes that already have a trusted clock.
func NewManagerWithClock(manifest Manifest, clock func() time.Time) *Manager {
	m := NewManager(manifest)
	if clock != nil {
		m.clock = clock
	}
	return m
}

// SetClock is intended for controlled runtimes and tests; production callers
// should use NewManagerWithClock with their trusted time source.
func (m *Manager) SetClock(clock func() time.Time) {
	if m == nil || clock == nil {
		return
	}
	m.mu.Lock()
	m.clock = clock
	m.mu.Unlock()
}

func (m *Manager) Manifest() Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneManifest(m.manifest)
}

// Authorize creates a short-lived session. It has no business-side effects.
func (m *Manager) Authorize(r Request) (*Session, error) {
	if m == nil {
		return nil, ErrDisabled
	}
	m.mu.RLock()
	manifest := cloneManifest(m.manifest)
	now := m.clock()
	m.mu.RUnlock()
	if !manifest.Enabled {
		return nil, ErrDisabled
	}
	if !r.Workload.Authenticated || strings.TrimSpace(r.Workload.ID) == "" {
		return nil, ErrUnauthorized
	}
	if strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.Purpose) == "" {
		return nil, ErrInvalidScope
	}
	if !manifest.Surfaces[r.Surface] {
		return nil, ErrSurfaceDenied
	}
	if r.Duration <= 0 || r.Duration > manifest.Budget.MaxDuration {
		return nil, ErrDurationDenied
	}
	if r.Budget.MaxBytes <= 0 || r.Budget.MaxBytes > manifest.Budget.MaxBytes || r.Budget.MaxCPU <= 0 || r.Budget.MaxCPU > manifest.Budget.MaxCPU {
		return nil, ErrBudgetDenied
	}
	if r.Now.IsZero() {
		r.Now = now
	}
	s := &Session{m: m, request: r, expiresAt: r.Now.Add(r.Duration), budget: r.Budget}
	m.record(Event{Kind: EventAccess, At: r.Now, Workload: r.Workload.ID, Tenant: r.Tenant, Purpose: r.Purpose, Surface: r.Surface, Reason: "authorized"})
	return s, nil
}

// Open is a concise adapter spelling for Authorize.
func (m *Manager) Open(r Request) (*Session, error) { return m.Authorize(r) }

func (s *Session) Capture(input []byte) (Artifact, error) {
	return s.capture(input, time.Nanosecond)
}

// CaptureWithCPU records the bounded CPU allowance consumed by a capture.
// Adapters that can measure CPU should use this method; Capture uses a minimal
// unit so callers cannot bypass the configured CPU ceiling accidentally.
func (s *Session) CaptureWithCPU(input []byte, cpu time.Duration) (Artifact, error) {
	return s.capture(input, cpu)
}

func (s *Session) capture(input []byte, cpu time.Duration) (Artifact, error) {
	if s == nil {
		return Artifact{}, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.m.currentTime()
	if s.closed {
		return Artifact{}, ErrClosed
	}
	if !now.Before(s.expiresAt) {
		s.closed = true
		s.drop(now, "expired")
		return Artifact{}, ErrExpired
	}
	if cpu <= 0 || cpu > s.budget.MaxCPU-s.usedCPU {
		s.drop(now, "cpu_budget")
		return Artifact{}, ErrCaptureBounds
	}
	if int64(len(input)) > s.budget.MaxBytes-s.used {
		s.drop(now, "byte_budget")
		return Artifact{}, ErrCaptureBounds
	}
	out, classified := Redact(input)
	s.used += int64(len(input))
	s.usedCPU += cpu
	d := sha256.Sum256(out)
	a := Artifact{ID: hex.EncodeToString(d[:]), Tenant: s.request.Tenant, Surface: s.request.Surface, Classification: classified, Bytes: out, Redacted: !equalBytes(input, out), CreatedAt: now}
	s.m.record(Event{Kind: EventAccess, At: now, Workload: s.request.Workload.ID, Tenant: s.request.Tenant, Purpose: s.request.Purpose, Surface: s.request.Surface, Artifact: a.ID, Reason: "capture"})
	return a, nil
}

func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.drop(s.m.currentTime(), "closed")
	}
}
func (s *Session) ExpiresAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.expiresAt
}

func (s *Session) drop(at time.Time, reason string) {
	s.m.record(Event{Kind: EventDrop, At: at, Workload: s.request.Workload.ID, Tenant: s.request.Tenant, Purpose: s.request.Purpose, Surface: s.request.Surface, Reason: reason})
}
func (m *Manager) Events() []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Event(nil), m.events...)
}
func (m *Manager) record(e Event) { m.mu.Lock(); m.events = append(m.events, e); m.mu.Unlock() }

func (m *Manager) currentTime() time.Time {
	m.mu.RLock()
	clock := m.clock
	m.mu.RUnlock()
	if clock == nil {
		return time.Now()
	}
	return clock()
}

var sensitive = regexp.MustCompile(`(?i)^(password|passwd|secret|token|credential|authorization|cookie|private[_ -]?key|ssn|sql|query|payload)([_-].*)?$`)
var sqlWord = regexp.MustCompile(`(?i)^(select|insert|update|delete|drop|alter|truncate|with)$`)

// Redact removes sensitive diagnostic content before it becomes an artifact.
// It is deterministic and intentionally conservative: a SQL-looking payload
// is replaced in full, while ordinary structured fields retain their keys.
func Redact(in []byte) ([]byte, Classification) {
	if len(in) == 0 {
		return nil, ClassificationPublic
	}
	parts := strings.Fields(string(in))
	if len(parts) > 0 && sqlWord.MatchString(strings.Trim(parts[0], "`\"'(),")) {
		return []byte("[REDACTED]"), ClassificationRestricted
	}
	changed := false
	restricted := false
	for i, p := range parts {
		key := strings.Trim(strings.TrimSpace(strings.SplitN(p, "=", 2)[0]), "`\"'(),")
		if sensitive.MatchString(key) || sensitive.MatchString(strings.Trim(p, "`\"'(),")) || sqlWord.MatchString(strings.Trim(p, "`\"'(),")) {
			if key == "" {
				key = "value"
			}
			parts[i] = key + "=[REDACTED]"
			changed = true
			restricted = true
		}
	}
	out := []byte(strings.Join(parts, " "))
	if restricted {
		return out, ClassificationRestricted
	}
	if changed {
		return out, ClassificationProtected
	}
	return out, ClassificationProtected
}

// ExplainArtifact is safe diagnostic presentation: it includes identity and
// classification but never the protected bytes.
func ExplainArtifact(a Artifact) string {
	return fmt.Sprintf("artifact=%s tenant=%s surface=%s class=%s redacted=%t bytes=%d", a.ID, a.Tenant, a.Surface, a.Classification, a.Redacted, len(a.Bytes))
}
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func cloneManifest(in Manifest) Manifest {
	out := in
	out.Surfaces = make(map[Surface]bool, len(in.Surfaces))
	for k, v := range in.Surfaces {
		out.Surfaces[k] = v
	}
	return out
}

// Surfaces returns a stable list useful to conformance checks.
func Surfaces() []Surface {
	out := []Surface{SurfacePprof, SurfaceExpvar, SurfaceMetrics, SurfaceDebug, SurfaceConfig, SurfaceGoroutine, SurfaceHeap, SurfaceTrace}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
