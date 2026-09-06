// Package backends describes the portable, protocol-level telemetry stack.
// It does not import Prometheus, Loki, Tempo or Grafana SDKs: deployment and
// composition roots can bind these contracts to compatible OSS services.
package backends

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

func Explain() string { return "portable OSS metrics logs traces and dashboard backend contract" }

type Kind string

const (
	KindMetrics    Kind = "PROMETHEUS"
	KindLogs       Kind = "LOKI"
	KindTraces     Kind = "TEMPO"
	KindDashboards Kind = "GRAFANA"
)

// Spec is the deployment evidence for one backend.
type Spec struct {
	Kind             Kind
	Protocol         string
	License          string
	OpenSource       bool
	Private          bool
	TenantScoped     bool
	Retention        time.Duration
	RestoreSupported bool
	CostBudgetBytes  int64
}

// Signal is metadata only; payloads are redacted and content addressed before
// they reach a backend.
type Signal struct {
	ID          string
	TenantToken string
	Kind        Kind
	Region      string
	Digest      string
	ObservedAt  time.Time
	RetainUntil time.Time
	Bytes       int64
}

type Snapshot struct {
	Signals []Signal
}

type Qualification struct {
	Complete  bool
	Missing   []Kind
	Unsafe    []string
	LicenseOK bool
	CostOK    bool
	RestoreOK bool
}

type Stack struct {
	mu      sync.RWMutex
	specs   map[Kind]Spec
	signals map[string]Signal
}

var (
	ErrInvalidSpec   = errors.New("telemetry backends: invalid backend specification")
	ErrInvalidSignal = errors.New("telemetry backends: invalid signal")
	ErrOverBudget    = errors.New("telemetry backends: cost budget exceeded")
	ErrExpired       = errors.New("telemetry backends: signal is outside retention")
)

// DefaultSpecs is the portable four-service baseline for Gate A.
func DefaultSpecs() []Spec {
	return []Spec{
		{Kind: KindMetrics, Protocol: "prometheus-remote-write", License: "Apache-2.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 7 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 1 << 30},
		{Kind: KindLogs, Protocol: "otlp-http", License: "AGPL-3.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 30 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 2 << 30},
		{Kind: KindTraces, Protocol: "otlp-http", License: "Apache-2.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 14 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 2 << 30},
		{Kind: KindDashboards, Protocol: "grafana-http-api", License: "AGPL-3.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 365 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 1 << 30},
	}
}

func New(specs []Spec) (*Stack, Qualification, error) {
	stack := &Stack{specs: make(map[Kind]Spec), signals: make(map[string]Signal)}
	for _, spec := range specs {
		if _, ok := stack.specs[spec.Kind]; ok {
			return nil, Qualification{}, fmt.Errorf("%w: duplicate kind %s", ErrInvalidSpec, spec.Kind)
		}
		stack.specs[spec.Kind] = spec
	}
	qualification := stack.Qualify()
	if !qualification.Complete {
		return nil, qualification, ErrInvalidSpec
	}
	return stack, qualification, nil
}

func Default() (*Stack, Qualification, error) { return New(DefaultSpecs()) }

func (s *Stack) Qualify() Qualification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := Qualification{LicenseOK: true, CostOK: true, RestoreOK: true}
	for _, kind := range []Kind{KindMetrics, KindLogs, KindTraces, KindDashboards} {
		spec, ok := s.specs[kind]
		if !ok {
			q.Missing = append(q.Missing, kind)
			continue
		}
		if !spec.OpenSource || strings.TrimSpace(spec.License) == "" {
			q.LicenseOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":license")
		}
		if !spec.Private || !spec.TenantScoped || spec.Retention <= 0 {
			q.Unsafe = append(q.Unsafe, string(kind)+":isolation-or-retention")
		}
		if !spec.RestoreSupported {
			q.RestoreOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":restore")
		}
		if spec.CostBudgetBytes <= 0 {
			q.CostOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":cost")
		}
	}
	q.Complete = len(q.Missing) == 0 && len(q.Unsafe) == 0 && q.LicenseOK && q.CostOK && q.RestoreOK
	return q
}

func (s *Stack) Ingest(signal Signal) error {
	if strings.TrimSpace(signal.ID) == "" || strings.TrimSpace(signal.TenantToken) == "" || strings.ContainsAny(signal.TenantToken, " \t\r\n") || signal.Kind == "" || strings.TrimSpace(signal.Region) == "" || strings.TrimSpace(signal.Digest) == "" || signal.ObservedAt.IsZero() || signal.RetainUntil.IsZero() || signal.Bytes < 0 {
		return ErrInvalidSignal
	}
	if len(signal.TenantToken) > 128 {
		return ErrInvalidSignal
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.specs[signal.Kind]
	if !ok || !spec.Private || !spec.TenantScoped {
		return ErrInvalidSpec
	}
	if signal.RetainUntil.Before(signal.ObservedAt) {
		return ErrExpired
	}
	var used int64
	for _, existing := range s.signals {
		if existing.Kind == signal.Kind {
			used += existing.Bytes
		}
	}
	if used+signal.Bytes > spec.CostBudgetBytes {
		return ErrOverBudget
	}
	if _, exists := s.signals[signal.ID]; exists {
		return nil
	}
	s.signals[signal.ID] = signal
	return nil
}

// Query always requires one exact tenant token and never supports a wildcard.
func (s *Stack) Query(tenantToken string, kind Kind, now time.Time) []Signal {
	if strings.TrimSpace(tenantToken) == "" || strings.ContainsAny(tenantToken, " \t\r\n") {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Signal, 0)
	for _, signal := range s.signals {
		if signal.TenantToken == tenantToken && signal.Kind == kind && (now.IsZero() || now.Before(signal.RetainUntil)) {
			result = append(result, signal)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *Stack) ExportSnapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := Snapshot{Signals: make([]Signal, 0, len(s.signals))}
	for _, signal := range s.signals {
		result.Signals = append(result.Signals, signal)
	}
	sort.Slice(result.Signals, func(i, j int) bool { return result.Signals[i].ID < result.Signals[j].ID })
	return result
}

func (s *Stack) Restore(snapshot Snapshot) error {
	newSignals := make(map[string]Signal, len(snapshot.Signals))
	for _, signal := range snapshot.Signals {
		if err := s.Ingest(signal); err != nil {
			return err
		}
		newSignals[signal.ID] = signal
	}
	s.mu.Lock()
	s.signals = newSignals
	s.mu.Unlock()
	return nil
}

func (q Qualification) Explain() string {
	unsafe := append([]string(nil), q.Unsafe...)
	sort.Strings(unsafe)
	return fmt.Sprintf("telemetry stack complete=%t license=%t cost=%t restore=%t missing=%d unsafe=%d", q.Complete, q.LicenseOK, q.CostOK, q.RestoreOK, len(q.Missing), len(unsafe))
}
