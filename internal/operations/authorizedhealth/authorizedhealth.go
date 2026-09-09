// Package authorizedhealth projects bounded operational evidence for an
// authenticated operator. It contains no database, transport, or mutable
// state; callers supply observations from the owning data/workflow/outbox
// packages and this package applies one fail-closed presentation boundary.
package authorizedhealth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const contractVersion = 1

// Version identifies the operator-health projection contract.
func Version() int { return contractVersion }

// Explain describes the contract without including tenant or operator data.
func Explain() string {
	return "operator-only bounded projection, workflow, outbox and reconciliation health"
}

// Freshness is the reliability of an observation, not a claim about the
// business state represented by it.
type Freshness string

const (
	FreshnessCurrent     Freshness = "CURRENT"
	FreshnessStale       Freshness = "STALE"
	FreshnessUnknown     Freshness = "UNKNOWN"
	FreshnessUnavailable Freshness = "UNAVAILABLE"
)

func (f Freshness) valid() bool {
	return f == FreshnessCurrent || f == FreshnessStale || f == FreshnessUnknown || f == FreshnessUnavailable
}

// State is the aggregate operational disposition. UNKNOWN outranks DEGRADED,
// and DEGRADED outranks HEALTHY, so missing evidence cannot look healthy.
type State string

const (
	StateHealthy  State = "HEALTHY"
	StateDegraded State = "DEGRADED"
	StateUnknown  State = "UNKNOWN"
)

var (
	ErrInvalidInput = errors.New("authorizedhealth: invalid input")
	ErrUnauthorized = errors.New("authorizedhealth: operator authorization required")
)

// ProjectionObservation is the value-only evidence needed to expose one
// critical projection's freshness and source/applied watermarks.
type ProjectionObservation struct {
	Name              string
	SchemaVersion     string
	DefinitionVersion string
	SourceSequence    int64
	AppliedSequence   int64
	ObservedAt        time.Time
	MaxAge            time.Duration
	Status            string
}

// WorkflowObservation is bounded workflow/work-item evidence. It deliberately
// carries counts and state classes, never work-item payloads.
type WorkflowObservation struct {
	Name              string
	SchemaVersion     string
	DefinitionVersion string
	ActiveRuns        int64
	FailedRuns        int64
	BlockedRuns       int64
	PendingWorkItems  int64
	OverdueWorkItems  int64
	ObservedAt        time.Time
	MaxAge            time.Duration
	Status            string
}

// OutboxObservation is bounded delivery evidence with no message body.
type OutboxObservation struct {
	SchemaVersion   string
	Pending         int64
	InFlight        int64
	Failed          int64
	Abandoned       int64
	OldestPendingAt time.Time
	ObservedAt      time.Time
	MaxAge          time.Duration
	MaxPendingAge   time.Duration
	Status          string
}

// ReconciliationObservation is the operational summary of reconciliation
// jobs, not the protected effect or provider payload.
type ReconciliationObservation struct {
	SchemaVersion  string
	Pending        int64
	Observing      int64
	Mismatch       int64
	RepairRequired int64
	ObservedAt     time.Time
	MaxAge         time.Duration
	Status         string
}

// Request is the complete caller-supplied evidence set for one tenant.
type Request struct {
	Tenant         values.TenantId
	Now            time.Time
	Projections    []ProjectionObservation
	Workflows      []WorkflowObservation
	Outbox         *OutboxObservation
	Reconciliation *ReconciliationObservation
}

type ProjectionHealth struct {
	Name              string
	SchemaVersion     string
	DefinitionVersion string
	SourceSequence    int64
	AppliedSequence   int64
	Lag               int64
	Freshness         Freshness
	State             State
	Status            string
}

type WorkflowHealth struct {
	Name              string
	SchemaVersion     string
	DefinitionVersion string
	ActiveRuns        int64
	FailedRuns        int64
	BlockedRuns       int64
	PendingWorkItems  int64
	OverdueWorkItems  int64
	Freshness         Freshness
	State             State
	Status            string
}

type OutboxHealth struct {
	SchemaVersion    string
	Pending          int64
	InFlight         int64
	Failed           int64
	Abandoned        int64
	OldestPendingAge time.Duration
	Freshness        Freshness
	State            State
	Status           string
}

type ReconciliationHealth struct {
	SchemaVersion  string
	Pending        int64
	Observing      int64
	Mismatch       int64
	RepairRequired int64
	Freshness      Freshness
	State          State
	Status         string
}

// View is safe to hand to an already-authorized operator surface. It contains
// no payloads, row identifiers, tenant enumeration, or arbitrary SQL output.
type View struct {
	PolicyVersion  string
	Purpose        string
	EvidenceID     string
	ObservedAt     time.Time
	Projections    []ProjectionHealth
	Workflows      []WorkflowHealth
	Outbox         *OutboxHealth
	Reconciliation *ReconciliationHealth
	State          State
}

// Project enforces the existing operator role before projecting any evidence.
// Tenant mismatch is deliberately returned as the same unauthorized error as
// a missing role so the boundary is noninterfering.
func Project(principal *trust.Principal, req Request) (View, error) {
	if err := admin.RequireOperator(principal); err != nil {
		return View{}, ErrUnauthorized
	}
	if principal == nil || req.Tenant == "" || req.Tenant != principal.Tenant() {
		return View{}, ErrUnauthorized
	}
	if req.Now.IsZero() {
		return View{}, fmt.Errorf("%w: observation time is required", ErrInvalidInput)
	}
	view := View{PolicyVersion: "hcmnext.admin.operator-profile/1", Purpose: "operator_diagnostics", EvidenceID: principal.EvidenceID(), ObservedAt: req.Now.UTC(), State: StateHealthy}
	for _, observation := range req.Projections {
		health, err := evaluateProjection(observation, req.Now)
		if err != nil {
			return View{}, err
		}
		view.Projections = append(view.Projections, health)
		view.State = worse(view.State, health.State)
	}
	for _, observation := range req.Workflows {
		health, err := evaluateWorkflow(observation, req.Now)
		if err != nil {
			return View{}, err
		}
		view.Workflows = append(view.Workflows, health)
		view.State = worse(view.State, health.State)
	}
	if req.Outbox != nil {
		health, err := evaluateOutbox(*req.Outbox, req.Now)
		if err != nil {
			return View{}, err
		}
		view.Outbox = &health
		view.State = worse(view.State, health.State)
	}
	if req.Reconciliation != nil {
		health, err := evaluateReconciliation(*req.Reconciliation, req.Now)
		if err != nil {
			return View{}, err
		}
		view.Reconciliation = &health
		view.State = worse(view.State, health.State)
	}
	sort.Slice(view.Projections, func(i, j int) bool { return view.Projections[i].Name < view.Projections[j].Name })
	sort.Slice(view.Workflows, func(i, j int) bool { return view.Workflows[i].Name < view.Workflows[j].Name })
	return view, nil
}

func evaluateProjection(o ProjectionObservation, now time.Time) (ProjectionHealth, error) {
	if err := validateObservationIdentity(o.Name, o.SchemaVersion, o.Status); err != nil {
		return ProjectionHealth{}, err
	}
	if o.SourceSequence < 0 || o.AppliedSequence < 0 || o.AppliedSequence > o.SourceSequence || o.MaxAge <= 0 {
		return ProjectionHealth{}, fmt.Errorf("%w: projection %q has invalid watermark or freshness bound", ErrInvalidInput, o.Name)
	}
	fresh := classifyFreshness(o.ObservedAt, now, o.MaxAge)
	state := stateForFreshness(fresh)
	lag := o.SourceSequence - o.AppliedSequence
	if o.Status != "CURRENT" || lag > 0 {
		state = worse(state, StateDegraded)
	}
	return ProjectionHealth{Name: o.Name, SchemaVersion: o.SchemaVersion, DefinitionVersion: o.DefinitionVersion, SourceSequence: o.SourceSequence, AppliedSequence: o.AppliedSequence, Lag: lag, Freshness: fresh, State: state, Status: o.Status}, nil
}

func evaluateWorkflow(o WorkflowObservation, now time.Time) (WorkflowHealth, error) {
	if err := validateObservationIdentity(o.Name, o.SchemaVersion, o.Status); err != nil {
		return WorkflowHealth{}, err
	}
	if o.ActiveRuns < 0 || o.FailedRuns < 0 || o.BlockedRuns < 0 || o.PendingWorkItems < 0 || o.OverdueWorkItems < 0 || o.MaxAge <= 0 {
		return WorkflowHealth{}, fmt.Errorf("%w: workflow %q has invalid counts or freshness bound", ErrInvalidInput, o.Name)
	}
	fresh := classifyFreshness(o.ObservedAt, now, o.MaxAge)
	state := stateForFreshness(fresh)
	if o.Status != "HEALTHY" || o.FailedRuns > 0 || o.BlockedRuns > 0 || o.OverdueWorkItems > 0 {
		state = worse(state, StateDegraded)
	}
	return WorkflowHealth{Name: o.Name, SchemaVersion: o.SchemaVersion, DefinitionVersion: o.DefinitionVersion, ActiveRuns: o.ActiveRuns, FailedRuns: o.FailedRuns, BlockedRuns: o.BlockedRuns, PendingWorkItems: o.PendingWorkItems, OverdueWorkItems: o.OverdueWorkItems, Freshness: fresh, State: state, Status: o.Status}, nil
}

func evaluateOutbox(o OutboxObservation, now time.Time) (OutboxHealth, error) {
	if o.SchemaVersion == "" || o.Status == "" || o.MaxAge <= 0 || o.MaxPendingAge <= 0 || o.Pending < 0 || o.InFlight < 0 || o.Failed < 0 || o.Abandoned < 0 {
		return OutboxHealth{}, fmt.Errorf("%w: outbox evidence is incomplete or negative", ErrInvalidInput)
	}
	fresh := classifyFreshness(o.ObservedAt, now, o.MaxAge)
	state := stateForFreshness(fresh)
	age := time.Duration(0)
	if o.Pending > 0 {
		if o.OldestPendingAt.IsZero() || o.OldestPendingAt.After(now) {
			fresh, state = FreshnessUnknown, StateUnknown
		} else {
			age = now.Sub(o.OldestPendingAt)
			if age > o.MaxPendingAge {
				state = worse(state, StateDegraded)
			}
		}
	}
	if o.Status != "HEALTHY" || o.Failed > 0 || o.Abandoned > 0 {
		state = worse(state, StateDegraded)
	}
	return OutboxHealth{SchemaVersion: o.SchemaVersion, Pending: o.Pending, InFlight: o.InFlight, Failed: o.Failed, Abandoned: o.Abandoned, OldestPendingAge: age, Freshness: fresh, State: state, Status: o.Status}, nil
}

func evaluateReconciliation(o ReconciliationObservation, now time.Time) (ReconciliationHealth, error) {
	if o.SchemaVersion == "" || o.Status == "" || o.MaxAge <= 0 || o.Pending < 0 || o.Observing < 0 || o.Mismatch < 0 || o.RepairRequired < 0 {
		return ReconciliationHealth{}, fmt.Errorf("%w: reconciliation evidence is incomplete or negative", ErrInvalidInput)
	}
	fresh := classifyFreshness(o.ObservedAt, now, o.MaxAge)
	state := stateForFreshness(fresh)
	if o.Status != "HEALTHY" || o.Mismatch > 0 || o.RepairRequired > 0 {
		state = worse(state, StateDegraded)
	}
	return ReconciliationHealth{SchemaVersion: o.SchemaVersion, Pending: o.Pending, Observing: o.Observing, Mismatch: o.Mismatch, RepairRequired: o.RepairRequired, Freshness: fresh, State: state, Status: o.Status}, nil
}

func validateObservationIdentity(name, schema, status string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(schema) == "" || strings.TrimSpace(status) == "" {
		return fmt.Errorf("%w: observation name, schema version, and status are required", ErrInvalidInput)
	}
	return nil
}

func classifyFreshness(observed, now time.Time, maxAge time.Duration) Freshness {
	if observed.IsZero() || now.Before(observed) {
		return FreshnessUnknown
	}
	if now.Sub(observed) > maxAge {
		return FreshnessStale
	}
	return FreshnessCurrent
}

func stateForFreshness(f Freshness) State {
	if f == FreshnessUnknown || f == FreshnessUnavailable {
		return StateUnknown
	}
	if f == FreshnessStale {
		return StateDegraded
	}
	return StateHealthy
}

func worse(a, b State) State {
	rank := func(s State) int {
		switch s {
		case StateHealthy:
			return 0
		case StateDegraded:
			return 1
		default:
			return 2
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// QueryEnvelope is the common authorized product-query metadata contract.
// It contains source/version/freshness evidence but no query payload.
type QueryEnvelope struct {
	Tenant              values.TenantId
	PrincipalEvidenceID string
	Purpose             string
	PolicyVersion       string
	SchemaVersion       string
	DefinitionVersion   string
	SourceWatermark     string
	Freshness           Freshness
	AuthorizationDigest string
	RowCount            int
}

type QueryRequest struct {
	Purpose           string
	PolicyVersion     string
	SchemaVersion     string
	DefinitionVersion string
	SourceWatermark   string
	Freshness         Freshness
	RowCount          int
}

// NewQueryEnvelope creates an envelope only from a validated, disclosable
// authorization decision. A denied decision returns one uniform error.
func NewQueryEnvelope(principal *trust.Principal, decision authz.Decision, req QueryRequest) (QueryEnvelope, error) {
	if principal == nil || !decision.SubjectDisclosable || decision.Scope.Effect != authz.EffectAllow {
		return QueryEnvelope{}, ErrUnauthorized
	}
	if decision.Scope.Subject.Tenant != principal.Tenant() || req.Purpose == "" || req.PolicyVersion == "" || req.SchemaVersion == "" || req.DefinitionVersion == "" || req.SourceWatermark == "" || !req.Freshness.valid() || req.RowCount < 0 {
		return QueryEnvelope{}, ErrUnauthorized
	}
	if err := decision.Validate(); err != nil {
		return QueryEnvelope{}, ErrUnauthorized
	}
	return QueryEnvelope{Tenant: principal.Tenant(), PrincipalEvidenceID: principal.EvidenceID(), Purpose: req.Purpose, PolicyVersion: req.PolicyVersion, SchemaVersion: req.SchemaVersion, DefinitionVersion: req.DefinitionVersion, SourceWatermark: req.SourceWatermark, Freshness: req.Freshness, AuthorizationDigest: decision.InputsDigest, RowCount: req.RowCount}, nil
}

// InvalidationInput is the only information permitted in an invalidation.
type InvalidationInput struct {
	ResourceKind      string
	ResourceID        string
	Projection        string
	SchemaVersion     string
	DefinitionVersion string
	SourceWatermark   string
}

// Invalidation is a bounded refetch hint, never an authoritative data event.
type Invalidation struct {
	Tenant            values.TenantId `json:"tenant"`
	ResourceKind      string          `json:"resource_kind"`
	ResourceID        string          `json:"resource_id"`
	Projection        string          `json:"projection"`
	SchemaVersion     string          `json:"schema_version"`
	DefinitionVersion string          `json:"definition_version"`
	SourceWatermark   string          `json:"source_watermark"`
}

// NewInvalidation applies the same disclosability boundary as a query.
func NewInvalidation(principal *trust.Principal, decision authz.Decision, in InvalidationInput) (Invalidation, error) {
	if principal == nil || !decision.SubjectDisclosable || decision.Scope.Effect != authz.EffectAllow || decision.Scope.Subject.Tenant != principal.Tenant() {
		return Invalidation{}, ErrUnauthorized
	}
	for field, value := range map[string]string{"resource_kind": in.ResourceKind, "resource_id": in.ResourceID, "projection": in.Projection, "schema_version": in.SchemaVersion, "definition_version": in.DefinitionVersion, "source_watermark": in.SourceWatermark} {
		if !boundedToken(value, 128) {
			return Invalidation{}, fmt.Errorf("%w: invalid invalidation %s", ErrInvalidInput, field)
		}
	}
	if err := decision.Validate(); err != nil {
		return Invalidation{}, ErrUnauthorized
	}
	return Invalidation{Tenant: principal.Tenant(), ResourceKind: in.ResourceKind, ResourceID: in.ResourceID, Projection: in.Projection, SchemaVersion: in.SchemaVersion, DefinitionVersion: in.DefinitionVersion, SourceWatermark: in.SourceWatermark}, nil
}

// MarshalBounded returns the wire hint and enforces the hard message bound.
func (i Invalidation) MarshalBounded() ([]byte, error) {
	b, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	if len(b) > 1024 {
		return nil, fmt.Errorf("%w: invalidation exceeds 1024 bytes", ErrInvalidInput)
	}
	return b, nil
}

func boundedToken(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsAny(value, "\r\n\x00")
}

// SurfaceObservation is the redacted result shape used to prove that denied
// surfaces do not vary by protected resource, count, cursor, or payload.
type SurfaceObservation struct {
	Visible     bool
	ErrorClass  string
	Reason      string
	Count       int
	ResourceIDs []string
	Cursor      string
	Payload     []byte
}

// CheckNoninterference checks a set of denied surface observations. Every
// denied surface must be the same bounded not-found/forbidden result and must
// carry no protected metadata. Authorized results are not compared because
// their payload is allowed to differ with the authorized resource.
func CheckNoninterference(observations []SurfaceObservation) error {
	var class, reason string
	for _, observation := range observations {
		if observation.Visible {
			continue
		}
		if observation.Count != 0 || len(observation.ResourceIDs) != 0 || observation.Cursor != "" || len(bytes.TrimSpace(observation.Payload)) != 0 {
			return fmt.Errorf("%w: denied surface disclosed protected metadata", ErrUnauthorized)
		}
		if observation.ErrorClass == "" || observation.Reason == "" {
			return fmt.Errorf("%w: denied surface lacks uniform explanation", ErrUnauthorized)
		}
		if class == "" {
			class, reason = observation.ErrorClass, observation.Reason
			continue
		}
		if class != observation.ErrorClass || reason != observation.Reason {
			return fmt.Errorf("%w: denied surfaces have distinguishable outcomes", ErrUnauthorized)
		}
	}
	return nil
}

// Digest returns a bounded deterministic identity for an operator view.
func (v View) Digest() string {
	b, _ := json.Marshal(struct {
		Policy          string
		Purpose         string
		ObservedAt      time.Time
		State           State
		ProjectionCount int
		WorkflowCount   int
		Outbox          bool
		Reconciliation  bool
	}{v.PolicyVersion, v.Purpose, v.ObservedAt, v.State, len(v.Projections), len(v.Workflows), v.Outbox != nil, v.Reconciliation != nil})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns counts and state only; it intentionally omits tenant and
// principal identifiers.
func (v View) Explain() string {
	return fmt.Sprintf("operator health state=%s projections=%d workflows=%d outbox=%t reconciliation=%t digest=%s", v.State, len(v.Projections), len(v.Workflows), v.Outbox != nil, v.Reconciliation != nil, v.Digest())
}
