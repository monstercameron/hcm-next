// Package observability contains backend-neutral observability semantics.
//
// Event names and their meanings are an operational API. This package keeps
// that API typed and versioned; presentation messages and encoders belong to
// consumers. In particular, errors are represented only by a bounded code,
// type and retryability bit: raw error strings and stacks must not enter logs.
package observability

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const SchemaVersion = 1

type Severity string

const (
	SeverityDebug Severity = "DEBUG"
	SeverityInfo  Severity = "INFO"
	SeverityWarn  Severity = "WARN"
	SeverityError Severity = "ERROR"
)

type Outcome string

const (
	OutcomeSuccess   Outcome = "SUCCESS"
	OutcomeFailure   Outcome = "FAILURE"
	OutcomePartial   Outcome = "PARTIAL"
	OutcomeUnknown   Outcome = "UNKNOWN"
	OutcomeDenied    Outcome = "DENIED"
	OutcomeCancelled Outcome = "CANCELLED"
	OutcomeDegraded  Outcome = "DEGRADED"
)

func (o Outcome) Valid() bool {
	switch o {
	case OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeUnknown,
		OutcomeDenied, OutcomeCancelled, OutcomeDegraded:
		return true
	default:
		return false
	}
}

// ErrorFields is safe operational error metadata. Message and stack are
// deliberately absent; protected forensic references are owned elsewhere.
type ErrorFields struct {
	Code      string
	Type      string
	Retryable bool
}

func (e ErrorFields) Valid() bool {
	return tokenRE.MatchString(e.Code) && tokenRE.MatchString(e.Type)
}

// Definition describes one stable operation event. Every outcome is mapped
// explicitly, preventing a new outcome from silently inheriting SUCCESS.
type Definition struct {
	Name      string
	Version   int
	Templates map[Outcome]string
	Severity  map[Outcome]Severity
}

type Event struct {
	Name     string
	Version  int
	Severity Severity
	Outcome  Outcome
	Template string
	Error    *ErrorFields
	// Flattened fields make adapters straightforward while Error remains a
	// convenient grouped view. They are empty/false when Error is nil.
	ErrorCode string
	ErrorType string
	Retryable bool
}

type Registry struct{ definitions map[string]Definition }

// SemanticRegistry and LogEvent are descriptive aliases used by adapters.
type SemanticRegistry = Registry
type LogEvent = Event
type LogDefinition = Definition

var (
	ErrUnknownOperation  = errors.New("observability: unknown operation")
	ErrInvalidDefinition = errors.New("observability: invalid event definition")
	ErrInvalidOutcome    = errors.New("observability: invalid outcome")
	ErrInvalidError      = errors.New("observability: invalid error fields")
)

var tokenRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)
var eventRE = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

func NewRegistry(defs ...Definition) (*Registry, error) {
	r := &Registry{definitions: make(map[string]Definition, len(defs))}
	for _, d := range defs {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) Register(d Definition) error {
	if r == nil || !eventRE.MatchString(d.Name) || d.Version <= 0 || len(d.Templates) != len(outcomes()) || len(d.Severity) != len(outcomes()) {
		return ErrInvalidDefinition
	}
	if r.definitions == nil {
		r.definitions = make(map[string]Definition)
	}
	for _, o := range outcomes() {
		if strings.TrimSpace(d.Templates[o]) == "" || !validSeverity(d.Severity[o]) {
			return ErrInvalidDefinition
		}
	}
	if _, exists := r.definitions[d.Name]; exists {
		return fmt.Errorf("%w: duplicate %q", ErrInvalidDefinition, d.Name)
	}
	d.Templates = cloneTemplates(d.Templates)
	d.Severity = cloneSeverity(d.Severity)
	r.definitions[d.Name] = d
	return nil
}

// Resolve maps an operation result without changing its business outcome.
func (r *Registry) Resolve(operation string, outcome Outcome, errFields *ErrorFields) (Event, error) {
	if r == nil {
		return Event{}, ErrUnknownOperation
	}
	d, ok := r.definitions[operation]
	if !ok {
		return Event{}, fmt.Errorf("%w: %s", ErrUnknownOperation, operation)
	}
	if !outcome.Valid() {
		return Event{}, ErrInvalidOutcome
	}
	if outcome == OutcomeFailure && errFields == nil {
		return Event{}, fmt.Errorf("%w: failure requires fields", ErrInvalidError)
	}
	if errFields != nil && !errFields.Valid() {
		return Event{}, ErrInvalidError
	}
	var ef *ErrorFields
	if errFields != nil {
		c := *errFields
		ef = &c
	}
	var code, typ string
	var retryable bool
	if ef != nil {
		code, typ, retryable = ef.Code, ef.Type, ef.Retryable
	}
	return Event{Name: d.Name, Version: d.Version, Severity: d.Severity[outcome], Outcome: outcome, Template: d.Templates[outcome], Error: ef, ErrorCode: code, ErrorType: typ, Retryable: retryable}, nil
}

// NewSemanticRegistry is the explicit constructor name for callers that do
// not need to know this registry is log-oriented.
func NewSemanticRegistry(defs ...Definition) (*Registry, error) { return NewRegistry(defs...) }

// Map is a concise adapter-facing spelling of Resolve.
func (r *Registry) Map(operation string, outcome Outcome, errFields *ErrorFields) (Event, error) {
	return r.Resolve(operation, outcome, errFields)
}

func outcomes() []Outcome {
	return []Outcome{OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeUnknown, OutcomeDenied, OutcomeCancelled, OutcomeDegraded}
}
func validSeverity(s Severity) bool {
	return s == SeverityDebug || s == SeverityInfo || s == SeverityWarn || s == SeverityError
}
func cloneTemplates(in map[Outcome]string) map[Outcome]string {
	out := make(map[Outcome]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneSeverity(in map[Outcome]Severity) map[Outcome]Severity {
	out := make(map[Outcome]Severity, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Operations returns sorted registered names for deterministic conformance checks.
func (r *Registry) Operations() []string {
	out := make([]string, 0, len(r.definitions))
	for k := range r.definitions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DefaultSeverity is the published baseline; callers may override it only in
// a versioned Definition. Denials are WARN, never an ERROR/system failure.
func DefaultSeverity(o Outcome) Severity {
	switch o {
	case OutcomeSuccess, OutcomeCancelled:
		return SeverityInfo
	case OutcomeFailure:
		return SeverityError
	case OutcomePartial, OutcomeUnknown, OutcomeDenied, OutcomeDegraded:
		return SeverityWarn
	default:
		return ""
	}
}
