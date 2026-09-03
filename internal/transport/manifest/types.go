package manifest

// Disposition is the total, exhaustive exposure decision for one RPC method
// (ENDPOINT-001 "P1A disposition", PROTO-010 "descriptor-level RPC exposure
// disposition"). Every method of every generated service has exactly one.
type Disposition string

// The three declared dispositions. There is no fourth: a method either
// serves real requests today, exists on the wire but is refused for the
// current release, or is not reachable at all.
const (
	// DispositionServed means the method executes for an authorized caller
	// today, under the current release (P1A).
	DispositionServed Disposition = "SERVED"
	// DispositionRefusedP1A means the method is present in the compiled
	// service descriptor and reachable on the wire, but every invocation is
	// refused for the duration of the P1A release
	// (planning/next-steps.md: "P1A ... must persist zero worker,
	// employment, assignment, organization, position, compensation or
	// budget mutations"). SubmitIntent, CancelIntent, SupersedeIntent and
	// ExecuteIntent are this: they exist in the proto so the P1B write path
	// never needs a breaking wire change, but nothing may call them yet
	// (ExecuteIntent is the one exception a composed cell can lift, and only
	// under an explicit internal/intent/app.ExecutionAuthority no released
	// composition sets).
	DispositionRefusedP1A Disposition = "REFUSED_P1A"
	// DispositionNotExposed means the method is declared in the descriptor
	// but bound to no server implementation at all (no grpcserver adapter,
	// no HTTP projection). No current method uses this value; it exists so
	// a future RPC added to the .proto without a corresponding manifest row
	// fails PROTO-010's totality check instead of silently defaulting to
	// served.
	DispositionNotExposed Disposition = "NOT_EXPOSED"
)

// Valid reports whether d is one of the three declared dispositions.
func (d Disposition) Valid() bool {
	switch d {
	case DispositionServed, DispositionRefusedP1A, DispositionNotExposed:
		return true
	default:
		return false
	}
}

// IntentBehavior classifies what a method does to BusinessIntent state, per
// planning/specs/http-grpc-endpoint-contract.md's manifest field
// "intent_behavior: CREATES | CONSUMES | EMITS | OBSERVES | NON_MATERIAL".
type IntentBehavior string

const (
	// IntentBehaviorCreates originates a new BusinessIntent instance.
	IntentBehaviorCreates IntentBehavior = "CREATES"
	// IntentBehaviorConsumes advances an existing instance by consuming its
	// current proposal revision or lifecycle position (submit, cancel).
	IntentBehaviorConsumes IntentBehavior = "CONSUMES"
	// IntentBehaviorEmits produces a new linked instance from an existing
	// one without mutating the original's history (supersede).
	IntentBehaviorEmits IntentBehavior = "EMITS"
	// IntentBehaviorObserves reads current or historical instance state
	// without changing it.
	IntentBehaviorObserves IntentBehavior = "OBSERVES"
	// IntentBehaviorNonMaterial names a method whose result has no bearing
	// on BusinessIntent state at all: pure discovery, or a simulation whose
	// entire contract is "no effect".
	IntentBehaviorNonMaterial IntentBehavior = "NON_MATERIAL"
)

// Valid reports whether b is one of the five declared behaviors.
func (b IntentBehavior) Valid() bool {
	switch b {
	case IntentBehaviorCreates, IntentBehaviorConsumes, IntentBehaviorEmits,
		IntentBehaviorObserves, IntentBehaviorNonMaterial:
		return true
	default:
		return false
	}
}

// IdempotencyClass names how a caller may safely retry a method.
type IdempotencyClass string

const (
	// IdempotencyReadSafe means the method has no side effect; a caller may
	// retry it any number of times with no additional coordination.
	IdempotencyReadSafe IdempotencyClass = "READ_SAFE"
	// IdempotencyKey means the method is a write-shaped operation whose
	// safe retry depends on the request's idempotency_key field
	// deduplicating repeated delivery.
	IdempotencyKey IdempotencyClass = "IDEMPOTENCY_KEY"
	// IdempotencyNone means the method has neither property: it is not
	// declared safe to retry without an explicit new decision. No current
	// method uses this value.
	IdempotencyNone IdempotencyClass = "NONE"
)

// Valid reports whether c is one of the three declared classes.
func (c IdempotencyClass) Valid() bool {
	switch c {
	case IdempotencyReadSafe, IdempotencyKey, IdempotencyNone:
		return true
	default:
		return false
	}
}

// EndpointDefinition is one versioned, generated manifest row: everything
// planning/specs/http-grpc-endpoint-contract.md's "Endpoint manifest"
// section requires to bind exactly one RPC method to one owner, one
// capability set, one transport exposure and one P1A disposition.
//
// Every slice field is sorted and de-duplicated by [Build] so two
// EndpointDefinition values for the same method compare equal regardless of
// how the inputs were assembled.
type EndpointDefinition struct {
	// EndpointID is the stable manifest identity: "<ServiceFullName>/<MethodName>".
	EndpointID string `json:"endpoint_id"`
	// ServiceFullName is the Protobuf service's fully qualified name, e.g.
	// "hcmnext.intents.v1.IntentService".
	ServiceFullName string `json:"service_full_name"`
	// MethodName is the RPC method name, e.g. "CreateIntent".
	MethodName string `json:"method_name"`

	// OwnerDomain is the one semantic owner of this route
	// ("every route has one semantic owner").
	OwnerDomain string `json:"owner_domain"`

	// CapabilityRefs names the capability_id(s) this method is bound to,
	// sorted. A generic lifecycle method (see IntentBehavior) binds to
	// every capability it can currently dispatch to, rather than one; a
	// typed method binds to exactly one.
	CapabilityRefs []string `json:"capability_definition_refs"`

	// IntentBehavior classifies the method's effect on BusinessIntent state.
	IntentBehavior IntentBehavior `json:"intent_behavior"`
	// AcceptedIntentDefinitionRefs names the intent.Ref values (as
	// "<intent_type_id>/v<version>") this method currently accepts or
	// discovers, sorted. Empty for a method whose disposition is
	// DispositionRefusedP1A: nothing is currently accepted.
	AcceptedIntentDefinitionRefs []string `json:"accepted_intent_definition_refs"`

	// RequestType and ResponseType are the fully qualified Protobuf message
	// names of the method's input and output.
	RequestType  string `json:"request_type"`
	ResponseType string `json:"response_type"`
	// ErrorCodes lists the canonical hcmnext.common.v1.ErrorCode names this
	// method may return, per the canonical error projection table.
	ErrorCodes []string `json:"error_codes"`

	// GRPCProcedure is the gRPC full method path
	// ("/<service_full_name>/<method_name>"), identical to
	// grpc.UnaryServerInfo.FullMethod and connect.Spec.Procedure.
	GRPCProcedure string `json:"grpc_procedure"`
	// HTTPMethod, HTTPPathTemplate and HTTPBodyBinding describe the
	// grpcbridge/connect-go HTTP projection: HTTPBodyBinding is "*" for a
	// method whose entire request is the JSON body (every POST here) and ""
	// for a method with no body (every GET here).
	HTTPMethod       string `json:"http_method"`
	HTTPPathTemplate string `json:"http_path_template"`
	HTTPBodyBinding  string `json:"http_body_binding"`

	// Disposition is the total P1A exposure decision (see [Disposition]).
	Disposition Disposition `json:"disposition"`
	// DispositionReason names why, citing the controlling spec section.
	DispositionReason string `json:"disposition_reason"`

	// AuthnAssurance is the minimum trust.Assurance level
	// (internal/trust) a caller's authentication event must reach.
	AuthnAssurance string `json:"authn_assurance"`
	// AuthzAction names the authorization action this method requires.
	AuthzAction string `json:"authz_action"`
	// PurposePolicyRef names the purpose-limitation policy this method's
	// ScopeContext.purpose is checked against.
	PurposePolicyRef string `json:"purpose_policy_ref"`
	// TenantScopeDerivation names where trusted tenant/organization scope
	// comes from: every request here derives it from the caller-supplied
	// (but server-validated) hcmnext.common.v1.ScopeContext, never from an
	// interceptor-only side channel, so the value is uniform today.
	TenantScopeDerivation string `json:"tenant_scope_derivation"`

	// ClassificationRef names the data classification floor this method's
	// request/response is held to.
	ClassificationRef string `json:"classification_ref"`

	// RequiredFieldPaths lists the dotted request field paths this method
	// cannot proceed without (ENDPOINT-001 "request presence rules"). See
	// presence.go for provenance against internal/transport/validate.go.
	RequiredFieldPaths []string `json:"required_field_paths"`

	// IdempotencyClass and IdempotencyKeySource describe safe retry.
	IdempotencyClass     IdempotencyClass `json:"idempotency_class"`
	IdempotencyKeySource string           `json:"idempotency_key_source"`

	// RevisionPolicy names how this method binds an expected resource
	// revision (planning/specs/http-grpc-endpoint-contract.md "expected
	// revision / ETag policy").
	RevisionPolicy string `json:"revision_policy"`

	// DeadlineBudgetMillis is the server-capped deadline this method is
	// bound to, in milliseconds server-side (planning/specs
	// "Every request has a server-capped deadline").
	DeadlineBudgetMillis int64 `json:"deadline_budget_millis"`
	// RetryPolicy and HedgingPolicy name the method's retry/hedging
	// classification. Hedging is out of scope for P1A (TOOL-009), so every
	// row's HedgingPolicy is "NOT_APPLICABLE_P1A" today.
	RetryPolicy   string `json:"retry_policy"`
	HedgingPolicy string `json:"hedging_policy"`

	// PaginationPolicy, FieldMaskPolicy and OrderingPolicy name the
	// method's list-shaped behavior. Non-list methods carry
	// "NOT_APPLICABLE".
	PaginationPolicy string `json:"pagination_policy"`
	FieldMaskPolicy  string `json:"field_mask_policy"`
	OrderingPolicy   string `json:"ordering_policy"`

	// RateBudgetRef and EvidencePolicyRef name the shared rate/resource
	// budget and evidence/audit policy this method obeys.
	RateBudgetRef     string `json:"rate_budget_ref"`
	EvidencePolicyRef string `json:"evidence_policy_ref"`

	// CompatibilityStatus and Phase record the method's lifecycle
	// compatibility state and the delivery phase it is scoped to.
	CompatibilityStatus string `json:"compatibility_status"`
	Phase               string `json:"phase"`
}

// EndpointManifest is the total, ordered set of [EndpointDefinition] rows:
// every RPC method of every service [Build] inspects, with no duplicate and
// no gap.
type EndpointManifest struct {
	// SchemaVersion is the manifest document's own format version. It is
	// bumped only when EndpointDefinition's field set changes shape.
	SchemaVersion int `json:"schema_version"`
	// Endpoints is sorted ascending by EndpointID.
	Endpoints []EndpointDefinition `json:"endpoints"`
}
