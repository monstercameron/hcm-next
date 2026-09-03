package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Action is a coarse operation kind on a [Resource].
type Action string

// The two actions the P1A service-to-service boundary distinguishes.
const (
	ActionRead  Action = "read"
	ActionWrite Action = "write"
)

func (a Action) valid() bool { return a == ActionRead || a == ActionWrite }

// Resource is a coarse cross-process data resource, named after the
// data_access reads/writes categories in
// definitions/architecture/process-roles.yaml. It is deliberately coarser
// than a table or a [authz.DataDomain]: this authorizer decides whether one
// process may reach a category of state belonging to another process at
// all, not which fields within it a principal may see (that is
// internal/trust/authz's job, one layer further in, once a call is already
// admitted here).
type Resource string

// The P1A resource vocabulary, one per data_access category that appears
// in process-roles.yaml for at least one process other than its owner.
const (
	ResourceTenant        Resource = "tenant"
	ResourceIntent        Resource = "intent"
	ResourceCapability    Resource = "capability"
	ResourceSnapshot      Resource = "snapshot"
	ResourceProposal      Resource = "proposal"
	ResourceObservation   Resource = "observation"
	ResourceWorkflowState Resource = "workflow_state"
	ResourceLedger        Resource = "ledger"
	ResourceOutbox        Resource = "outbox"
	ResourceProjection    Resource = "projection"
	ResourceSchema        Resource = "schema"
)

// Rule is one compiled-in allow-matrix entry: caller role may take action on
// resource. A rule never encodes a deny; absence from [AllowMatrix] is the
// deny.
type Rule struct {
	RuleID   string
	Caller   ProcessRole
	Resource Resource
	Action   Action
}

// AllowMatrix is the compiled-in, deny-by-default P1A service-to-service
// authorization policy. It is derived from the data_access.reads/writes
// declared for each process in definitions/architecture/process-roles.yaml:
//
//   - hcmnext fronts read/query capability dispatch across every resource
//     its own process declares reading (tenant, intent, capability,
//     snapshot, proposal, observation) plus the resources it queries
//     through the read path (ledger, outbox, projection, workflow state)
//     — read-only in every case, matching "hcmnext -> everything
//     read-only in P1A". Its material writes (intent, proposal, snapshot
//     draft state) happen in-process against its own domain code, which is
//     not a cross-process call this authorizer governs.
//   - worker reads and writes every resource its process declares
//     (workflow state, ledger events, outbox entries, observation
//     records) — it is the durable execution process, so both directions
//     are legitimate.
//   - projector reads the event stream (outbox, ledger) and writes only
//     projection tables, never a ledger or domain-of-record table, per
//     process-roles.yaml's projector degradation_policy.
//   - migrate reads and writes schema state; it is the highest
//     write-privilege, operator-invoked process and touches nothing else
//     here.
//   - scheduler and admin are declared in [ProcessRole] as reserved P1B
//     vocabulary but carry no rule here: any call presenting either role
//     is denied by the same deny-by-default path as an unrecognized
//     caller, which is deliberate until their P1B scope is defined.
var AllowMatrix = []Rule{
	{RuleID: "svc.worker.workflow_state.read", Caller: RoleWorker, Resource: ResourceWorkflowState, Action: ActionRead},
	{RuleID: "svc.worker.workflow_state.write", Caller: RoleWorker, Resource: ResourceWorkflowState, Action: ActionWrite},
	{RuleID: "svc.worker.ledger.read", Caller: RoleWorker, Resource: ResourceLedger, Action: ActionRead},
	{RuleID: "svc.worker.ledger.write", Caller: RoleWorker, Resource: ResourceLedger, Action: ActionWrite},
	{RuleID: "svc.worker.outbox.read", Caller: RoleWorker, Resource: ResourceOutbox, Action: ActionRead},
	{RuleID: "svc.worker.outbox.write", Caller: RoleWorker, Resource: ResourceOutbox, Action: ActionWrite},
	{RuleID: "svc.worker.observation.read", Caller: RoleWorker, Resource: ResourceObservation, Action: ActionRead},
	{RuleID: "svc.worker.observation.write", Caller: RoleWorker, Resource: ResourceObservation, Action: ActionWrite},

	{RuleID: "svc.projector.outbox.read", Caller: RoleProjector, Resource: ResourceOutbox, Action: ActionRead},
	{RuleID: "svc.projector.ledger.read", Caller: RoleProjector, Resource: ResourceLedger, Action: ActionRead},
	{RuleID: "svc.projector.projection.write", Caller: RoleProjector, Resource: ResourceProjection, Action: ActionWrite},

	{RuleID: "svc.hcmnext.tenant.read", Caller: RoleHCMNext, Resource: ResourceTenant, Action: ActionRead},
	{RuleID: "svc.hcmnext.intent.read", Caller: RoleHCMNext, Resource: ResourceIntent, Action: ActionRead},
	{RuleID: "svc.hcmnext.capability.read", Caller: RoleHCMNext, Resource: ResourceCapability, Action: ActionRead},
	{RuleID: "svc.hcmnext.snapshot.read", Caller: RoleHCMNext, Resource: ResourceSnapshot, Action: ActionRead},
	{RuleID: "svc.hcmnext.proposal.read", Caller: RoleHCMNext, Resource: ResourceProposal, Action: ActionRead},
	{RuleID: "svc.hcmnext.observation.read", Caller: RoleHCMNext, Resource: ResourceObservation, Action: ActionRead},
	{RuleID: "svc.hcmnext.workflow_state.read", Caller: RoleHCMNext, Resource: ResourceWorkflowState, Action: ActionRead},
	{RuleID: "svc.hcmnext.ledger.read", Caller: RoleHCMNext, Resource: ResourceLedger, Action: ActionRead},
	{RuleID: "svc.hcmnext.outbox.read", Caller: RoleHCMNext, Resource: ResourceOutbox, Action: ActionRead},
	{RuleID: "svc.hcmnext.projection.read", Caller: RoleHCMNext, Resource: ResourceProjection, Action: ActionRead},

	{RuleID: "svc.migrate.schema.read", Caller: RoleMigrate, Resource: ResourceSchema, Action: ActionRead},
	{RuleID: "svc.migrate.schema.write", Caller: RoleMigrate, Resource: ResourceSchema, Action: ActionWrite},
}

// allowIndex is [AllowMatrix] compiled into a lookup table once, at package
// initialization.
var allowIndex = func() map[ProcessRole]map[Resource]map[Action]string {
	idx := make(map[ProcessRole]map[Resource]map[Action]string, len(AllowMatrix))
	for _, rule := range AllowMatrix {
		byResource, ok := idx[rule.Caller]
		if !ok {
			byResource = make(map[Resource]map[Action]string)
			idx[rule.Caller] = byResource
		}
		byAction, ok := byResource[rule.Resource]
		if !ok {
			byAction = make(map[Action]string)
			byResource[rule.Resource] = byAction
		}
		byAction[rule.Action] = rule.RuleID
	}
	return idx
}()

// Deny reasons. Stable tokens, matched by callers and alert rules, never by
// the English sentence around them.
const (
	ReasonNoVerifiedIdentity = "no_verified_workload_identity"
	ReasonIdentityExpired    = "workload_identity_expired"
	ReasonUnknownRole        = "unrecognized_process_role"
	ReasonNoMatchingRule     = "no_matching_allow_rule"
)

// Effect is the outcome of one [Authorize] call.
type Effect uint8

// Effects. EffectUnspecified is never a legal outcome of a completed
// [Decision].
const (
	EffectUnspecified Effect = iota
	EffectAllow
	EffectDeny
)

// String returns the stable wire token.
func (e Effect) String() string {
	switch e {
	case EffectAllow:
		return "ALLOW"
	case EffectDeny:
		return "DENY"
	default:
		return "EFFECT_UNSPECIFIED"
	}
}

// Request is one service-to-service authorization question: does the
// verified caller identity have authority to take action on resource, as of
// evaluatedAt.
type Request struct {
	// Caller must be an [Identity] a [Verifier] actually produced. A nil
	// (zero-value) Caller is refused, never treated as an anonymous allow.
	Caller      Identity
	Resource    Resource
	Action      Action
	EvaluatedAt time.Time
}

// Decision is the explainable result of one [Authorize] call.
type Decision struct {
	Effect      Effect
	RuleID      string
	Reason      string
	Caller      ProcessRole
	Resource    Resource
	Action      Action
	EvaluatedAt time.Time
	DecisionID  string
}

// ErrInvalidRequest is returned when a [Request] is malformed enough that no
// decision, not even a deny, can be computed from it.
var ErrInvalidRequest = errors.New("workload: invalid authorization request")

// Authorize evaluates req against [AllowMatrix] and returns an explainable
// [Decision]. It is pure, deny-by-default and fails closed: a caller with no
// verified identity, an expired identity, an unrecognized role, or a
// caller/resource/action triple absent from [AllowMatrix] is denied with a
// stable reason, never silently admitted because the matrix has nothing to
// say about it.
func Authorize(req Request) (Decision, error) {
	if !req.Action.valid() {
		return Decision{}, fmt.Errorf("%w: action %q", ErrInvalidRequest, req.Action)
	}
	if req.Resource == "" {
		return Decision{}, fmt.Errorf("%w: resource is empty", ErrInvalidRequest)
	}

	base := Decision{
		Caller:      req.Caller.role,
		Resource:    req.Resource,
		Action:      req.Action,
		EvaluatedAt: req.EvaluatedAt,
	}

	if req.Caller == (Identity{}) {
		return deny(base, ReasonNoVerifiedIdentity), nil
	}
	if !req.Caller.role.Valid() {
		return deny(base, ReasonUnknownRole), nil
	}
	if !req.EvaluatedAt.IsZero() && !req.Caller.ValidAt(req.EvaluatedAt) {
		return deny(base, ReasonIdentityExpired), nil
	}

	if byResource, ok := allowIndex[req.Caller.role]; ok {
		if byAction, ok := byResource[req.Resource]; ok {
			if ruleID, ok := byAction[req.Action]; ok {
				base.Effect = EffectAllow
				base.RuleID = ruleID
				base.DecisionID = decisionID(base)
				return base, nil
			}
		}
	}
	return deny(base, ReasonNoMatchingRule), nil
}

func deny(base Decision, reason string) Decision {
	base.Effect = EffectDeny
	base.Reason = reason
	base.DecisionID = decisionID(base)
	return base
}

// decisionID derives a durable, explainable decision identifier from every
// field the decision was computed from.
func decisionID(d Decision) string {
	h := sha256.New()
	fmt.Fprintf(h, "effect=%s;rule=%s;reason=%s;caller=%s;resource=%s;action=%s;at=%d",
		d.Effect, d.RuleID, d.Reason, d.Caller, d.Resource, d.Action, d.EvaluatedAt.UnixNano())
	return "ev:svcauthz:" + hex.EncodeToString(h.Sum(nil))[:32]
}
