// Package modeling owns the executable boundary for unresolved workflow
// modeling questions. It records who owns a question, when it must be
// decided, what evidence is required, and the safe behavior before a decision
// exists. It is deliberately a value-only package: it does not publish a
// workflow or infer a domain answer.
package modeling

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidDecision = errors.New("workflow modeling: invalid decision")
	ErrDuplicateID     = errors.New("workflow modeling: duplicate decision id")
	ErrUnsafeDefault   = errors.New("workflow modeling: unsafe or missing safe default")
)

// Status is the lifecycle of an unresolved modeling question.
type Status string

const (
	StatusOpenOwned Status = "OPEN_OWNED"
	StatusDecided   Status = "DECIDED"
	StatusDeferred  Status = "DEFERRED"
)

func (s Status) Valid() bool {
	return s == StatusOpenOwned || s == StatusDecided || s == StatusDeferred
}

// QuestionKind identifies the boundary that owns the uncertainty.
type QuestionKind string

const (
	QuestionOwnership         QuestionKind = "OWNERSHIP"
	QuestionPrecedence        QuestionKind = "PRECEDENCE"
	QuestionComposition       QuestionKind = "COMPOSITION"
	QuestionBoundary          QuestionKind = "BOUNDARY"
	QuestionReadiness         QuestionKind = "READINESS"
	QuestionTiming            QuestionKind = "TIMING"
	QuestionExternalAuthority QuestionKind = "EXTERNAL_AUTHORITY"
)

func (k QuestionKind) Valid() bool {
	switch k {
	case QuestionOwnership, QuestionPrecedence, QuestionComposition,
		QuestionBoundary, QuestionReadiness, QuestionTiming,
		QuestionExternalAuthority:
		return true
	default:
		return false
	}
}

// SafeDefault is the only behavior allowed while the question is unresolved.
type SafeDefault string

const (
	DefaultBlock           SafeDefault = "BLOCK"
	DefaultRouteHuman      SafeDefault = "ROUTE_HUMAN"
	DefaultDeferCapability SafeDefault = "DEFER_CAPABILITY"
)

func (d SafeDefault) Valid() bool {
	return d == DefaultBlock || d == DefaultRouteHuman || d == DefaultDeferCapability
}

// Alternative preserves the rejected or pending choices for historical
// explanation. The descriptions are design evidence, not runtime authority.
type Alternative struct {
	ID          string
	Description string
	Consequence string
}

// Decision is one immutable, revisioned modeling question. Revision 1 is a
// valid initial owned record; changing any material field requires a new
// revision in the source registry.
type Decision struct {
	ID                  string
	Kind                QuestionKind
	AffectedIntents     []string
	AffectedWorkflows   []string
	AccountableOwner    string
	DecisionDeadline    time.Time
	SafeDefault         SafeDefault
	Alternatives        []Alternative
	Consequences        []string
	Authority           string
	SourceEvidence      []string
	InvalidationTrigger string
	LinkedTodo          string
	LinkedTests         []string
	Status              Status
	Revision            uint64
}

// Boundary is the executable, non-authoritative result used before a domain
// decision is available.
type Boundary struct {
	DecisionID string
	Revision   uint64
	Action     SafeDefault
	Write      bool
}

// Validate enforces the WF-DISC-003 GREEN contract. In particular, no prose
// record can omit its owner, deadline, evidence, safe behavior, or executable
// test link.
func (d Decision) Validate() error {
	if strings.TrimSpace(d.ID) == "" || !d.Kind.Valid() || !d.Status.Valid() || d.Revision == 0 {
		return fmt.Errorf("%w: id, kind, status and positive revision are required", ErrInvalidDecision)
	}
	if len(d.AffectedIntents) == 0 || len(d.AffectedWorkflows) == 0 {
		return fmt.Errorf("%w: affected intents and workflows are required", ErrInvalidDecision)
	}
	if strings.TrimSpace(d.AccountableOwner) == "" || d.DecisionDeadline.IsZero() {
		return fmt.Errorf("%w: accountable owner and decision deadline are required", ErrInvalidDecision)
	}
	if !d.SafeDefault.Valid() {
		return fmt.Errorf("%w: %q", ErrUnsafeDefault, d.SafeDefault)
	}
	if strings.TrimSpace(d.Authority) == "" || strings.TrimSpace(d.InvalidationTrigger) == "" || strings.TrimSpace(d.LinkedTodo) == "" {
		return fmt.Errorf("%w: authority, invalidation trigger and linked todo are required", ErrInvalidDecision)
	}
	if len(d.SourceEvidence) == 0 || len(d.LinkedTests) == 0 || len(d.Alternatives) == 0 || len(d.Consequences) == 0 {
		return fmt.Errorf("%w: alternatives, consequences, source evidence and tests are required", ErrInvalidDecision)
	}
	seen := map[string]bool{}
	for _, value := range append(append([]string{}, d.AffectedIntents...), d.AffectedWorkflows...) {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: affected reference is empty", ErrInvalidDecision)
		}
	}
	for _, a := range d.Alternatives {
		if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Description) == "" || strings.TrimSpace(a.Consequence) == "" || seen[a.ID] {
			return fmt.Errorf("%w: alternatives must have unique id, description and consequence", ErrInvalidDecision)
		}
		seen[a.ID] = true
	}
	for _, value := range append(append(append([]string{}, d.Consequences...), d.SourceEvidence...), d.LinkedTests...) {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: evidence, consequence and test values cannot be empty", ErrInvalidDecision)
		}
	}
	return nil
}

// SafeBoundary returns the fail-closed behavior for an accepted decision
// record. No safe default grants a write.
func (d Decision) SafeBoundary() (Boundary, error) {
	if err := d.Validate(); err != nil {
		return Boundary{}, err
	}
	return Boundary{DecisionID: d.ID, Revision: d.Revision, Action: d.SafeDefault, Write: false}, nil
}

// ValidateRegistry verifies uniqueness and validates every decision without
// mutating caller-owned slices.
func ValidateRegistry(decisions []Decision) error {
	seen := make(map[string]struct{}, len(decisions))
	for _, d := range decisions {
		if err := d.Validate(); err != nil {
			return err
		}
		if _, exists := seen[d.ID]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateID, d.ID)
		}
		seen[d.ID] = struct{}{}
	}
	return nil
}

// Digest returns a stable SHA-256 identity for a valid decision revision.
func (d Decision) Digest() string {
	if d.Validate() != nil {
		return ""
	}
	values := func(input []string) []string {
		out := append([]string(nil), input...)
		sort.Strings(out)
		return out
	}
	var b strings.Builder
	part := func(value string) { fmt.Fprintf(&b, "%d:%s;", len(value), value) }
	part(d.ID)
	part(string(d.Kind))
	part(d.DecisionDeadline.UTC().Format(time.RFC3339Nano))
	part(string(d.SafeDefault))
	part(string(d.Status))
	fold := append(values(d.AffectedIntents), values(d.AffectedWorkflows)...)
	for _, value := range fold {
		part(value)
	}
	for _, a := range d.Alternatives {
		part(a.ID)
		part(a.Description)
		part(a.Consequence)
	}
	for _, list := range [][]string{d.Consequences, d.SourceEvidence, d.LinkedTests} {
		for _, value := range values(list) {
			part(value)
		}
	}
	for _, value := range []string{d.AccountableOwner, d.Authority, d.InvalidationTrigger, d.LinkedTodo} {
		part(value)
	}
	part(fmt.Sprint(d.Revision))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Explain is bounded and names the decision boundary without exposing source
// evidence payloads or any business data.
func (d Decision) Explain() string {
	return fmt.Sprintf("workflow decision %s revision %d status=%s kind=%s safe-default=%s write=false", d.ID, d.Revision, d.Status, d.Kind, d.SafeDefault)
}
