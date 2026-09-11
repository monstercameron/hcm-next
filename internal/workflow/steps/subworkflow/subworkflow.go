// Package subworkflow implements the bounded SUBWORKFLOW step semantics
// (WF-STEP-009): a parent starts a pinned child workflow and binds its
// outcome. Expansion pins the child version, attenuates scope to the
// intersection of parent-approved, caller and child-policy scopes, bounds
// depth and fan-out at compile time, rejects recursive cycles, requires a
// separately approved expansion certificate, and leaves an explicit
// obligation with a correlation link for detached children. Cancellation
// records each child as cancelled, compensated, already complete or unable
// to cancel; parent completion records child truth verbatim and never
// overwrites it. Pure functions over explicit inputs: no clock, no ambient
// state.
package subworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel dispositions. Each Expand/Detach/PropagateCancellation/
// CompleteParent failure carries exactly one of these codes.
var (
	ErrVersionUnpinned    = errors.New("subworkflow: child version is not pinned")
	ErrVersionChanged     = errors.New("subworkflow: child version changed under its pin")
	ErrAuthorityExpansion = errors.New("subworkflow: child scope exceeds parent approval")
	ErrDepthExceeded      = errors.New("subworkflow: child depth exceeds bound")
	ErrFanoutExceeded     = errors.New("subworkflow: sibling ordinal exceeds fan-out bound")
	ErrRecursiveCycle     = errors.New("subworkflow: child recurses into its own ancestor chain")
	ErrMissingCertificate = errors.New("subworkflow: expansion certificate is required")
	ErrInvalidWaitMode    = errors.New("subworkflow: unknown wait mode")
	ErrMissingCorrelation = errors.New("subworkflow: detached child requires a correlation link")
	ErrUnknownChildState  = errors.New("subworkflow: unknown child state")
	ErrMissingChildResult = errors.New("subworkflow: mandatory child result is missing")
)

// IsCode reports whether err carries the sentinel code.
func IsCode(err, code error) bool { return errors.Is(err, code) }

// WaitMode is WAIT or DETACH_WITH_OBLIGATION.
type WaitMode string

// Wait modes.
const (
	WaitModeWait                 WaitMode = "WAIT"
	WaitModeDetachWithObligation WaitMode = "DETACH_WITH_OBLIGATION"
)

// ChildRef pins one child workflow at one version.
type ChildRef struct {
	Workflow string `json:"workflow"`
	Version  string `json:"version"`
}

// Expansion is one compile-time child-expansion request.
type Expansion struct {
	ParentRevision      string     `json:"parent_revision"`
	Child               ChildRef   `json:"child"`
	PinnedVersion       string     `json:"pinned_version"`
	ParentApprovedScope []string   `json:"parent_approved_scope"`
	CallerScope         []string   `json:"caller_scope"`
	ChildPolicyScope    []string   `json:"child_policy_scope"`
	Depth               int        `json:"depth"`
	MaxDepth            int        `json:"max_depth"`
	SiblingOrdinal      int        `json:"sibling_ordinal"`
	MaxFanout           int        `json:"max_fanout"`
	Ancestors           []ChildRef `json:"ancestors"`
	Certificate         string     `json:"certificate"`
	WaitMode            WaitMode   `json:"wait_mode"`
}

// ExpandedChild is one admitted child with its attenuated scope and its
// idempotency key binding parent revision, child ref/version and ordinal.
type ExpandedChild struct {
	Child          ChildRef `json:"child"`
	PinnedVersion  string   `json:"pinned_version"`
	EffectiveScope []string `json:"effective_scope"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// Expand admits one child expansion or rejects it with an exact code.
func Expand(e Expansion) (ExpandedChild, error) {
	if strings.TrimSpace(e.Child.Workflow) == "" || strings.TrimSpace(e.Child.Version) == "" {
		return ExpandedChild{}, ErrVersionUnpinned
	}
	if e.PinnedVersion != "" && e.PinnedVersion != e.Child.Version {
		return ExpandedChild{}, fmt.Errorf("%w: pinned %q, requested %q", ErrVersionChanged, e.PinnedVersion, e.Child.Version)
	}
	approved := make(map[string]bool, len(e.ParentApprovedScope))
	for _, scope := range e.ParentApprovedScope {
		approved[scope] = true
	}
	for _, scope := range e.ChildPolicyScope {
		if !approved[scope] {
			return ExpandedChild{}, fmt.Errorf("%w: %q is outside parent approval", ErrAuthorityExpansion, scope)
		}
	}
	if e.MaxDepth <= 0 || e.Depth <= 0 || e.Depth > e.MaxDepth {
		return ExpandedChild{}, fmt.Errorf("%w: depth %d with bound %d", ErrDepthExceeded, e.Depth, e.MaxDepth)
	}
	if e.MaxFanout <= 0 || e.SiblingOrdinal < 0 || e.SiblingOrdinal >= e.MaxFanout {
		return ExpandedChild{}, fmt.Errorf("%w: ordinal %d with bound %d", ErrFanoutExceeded, e.SiblingOrdinal, e.MaxFanout)
	}
	for _, ancestor := range e.Ancestors {
		if ancestor.Workflow == e.Child.Workflow {
			return ExpandedChild{}, fmt.Errorf("%w: %q recurses", ErrRecursiveCycle, e.Child.Workflow)
		}
	}
	if strings.TrimSpace(e.Certificate) == "" {
		return ExpandedChild{}, ErrMissingCertificate
	}
	if e.WaitMode != WaitModeWait && e.WaitMode != WaitModeDetachWithObligation {
		return ExpandedChild{}, fmt.Errorf("%w: %q", ErrInvalidWaitMode, e.WaitMode)
	}
	caller := make(map[string]bool, len(e.CallerScope))
	for _, scope := range e.CallerScope {
		caller[scope] = true
	}
	effective := make([]string, 0, len(e.ChildPolicyScope))
	for _, scope := range e.ChildPolicyScope {
		if caller[scope] {
			effective = append(effective, scope)
		}
	}
	sort.Strings(effective)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		e.ParentRevision, e.Child.Workflow, e.Child.Version,
		fmt.Sprintf("depth=%d", e.Depth), fmt.Sprintf("ordinal=%d", e.SiblingOrdinal),
	}, "\x00")))
	return ExpandedChild{
		Child:          e.Child,
		PinnedVersion:  e.Child.Version,
		EffectiveScope: effective,
		IdempotencyKey: "subwf:" + hex.EncodeToString(sum[:]),
	}, nil
}

// Obligation is the explicit link a detached child leaves behind.
type Obligation struct {
	Child          ChildRef `json:"child"`
	Correlation    string   `json:"correlation"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// Detach records one detached child as an explicit obligation. A detached
// child never disappears: an empty correlation link fails.
func Detach(child ExpandedChild, correlation string) (Obligation, error) {
	if strings.TrimSpace(correlation) == "" {
		return Obligation{}, ErrMissingCorrelation
	}
	return Obligation{Child: child.Child, Correlation: correlation, IdempotencyKey: child.IdempotencyKey}, nil
}

// ChildState is one cancellable child state.
type ChildState string

// Child states.
const (
	StateRunning     ChildState = "RUNNING"
	StateSucceeded   ChildState = "SUCCEEDED"
	StateCompensated ChildState = "COMPENSATED"
)

// ChildReport is one cancellation report to the parent.
type ChildReport string

// Cancellation reports.
const (
	ReportCancelled        ChildReport = "CANCELLED"
	ReportCompensated      ChildReport = "COMPENSATED"
	ReportAlreadyCompleted ChildReport = "ALREADY_COMPLETED"
	ReportCannotCancel     ChildReport = "CANNOT_CANCEL"
)

// CancellableChild is one child facing parent cancellation.
type CancellableChild struct {
	Ref         ChildRef   `json:"ref"`
	State       ChildState `json:"state"`
	Cancellable bool       `json:"cancellable"`
}

// PropagateCancellation records one child as cancelled, compensated,
// already complete or unable to cancel. Unknown states fail loudly.
func PropagateCancellation(child CancellableChild) (ChildReport, error) {
	switch child.State {
	case StateSucceeded:
		return ReportAlreadyCompleted, nil
	case StateCompensated:
		return ReportCompensated, nil
	case StateRunning:
		if child.Cancellable {
			return ReportCancelled, nil
		}
		return ReportCannotCancel, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownChildState, child.State)
	}
}

// ChildTruth is one recorded child outcome.
type ChildTruth struct {
	Child     ChildRef    `json:"child"`
	Outcome   ChildReport `json:"outcome"`
	Mandatory bool        `json:"mandatory"`
}

// ParentCompletion records child truth verbatim at parent closure.
type ParentCompletion struct {
	Children []ChildTruth `json:"children"`
}

// CompleteParent records child outcomes without overwriting them. Every
// mandatory child ref must appear with a recorded outcome; a parent that
// ignores a mandatory child result fails.
func CompleteParent(children []ChildTruth, mandatory []ChildRef) (ParentCompletion, error) {
	recorded := make(map[ChildRef]bool, len(children))
	for _, truth := range children {
		recorded[truth.Child] = truth.Outcome != ""
	}
	for _, ref := range mandatory {
		if !recorded[ref] {
			return ParentCompletion{}, fmt.Errorf("%w: %s@%s", ErrMissingChildResult, ref.Workflow, ref.Version)
		}
	}
	out := make([]ChildTruth, len(children))
	copy(out, children)
	return ParentCompletion{Children: out}, nil
}
