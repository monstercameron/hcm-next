package capability

import (
	"context"
	"time"
)

// AuthorizationDecision is a decision already made by an authorization
// component this package does not implement (TRUST-011/GOVERN-001..003 are
// not prerequisites here). The gateway only enforces the decision it is
// handed.
type AuthorizationDecision string

const (
	Allow AuthorizationDecision = "ALLOW"
	Deny  AuthorizationDecision = "DENY"
)

// Authorization is the caller-supplied decision input CAP-002 requires. It is
// produced elsewhere (a policy engine, a session, a test fixture); this
// package neither authenticates nor computes it.
type Authorization struct {
	Decision   AuthorizationDecision
	Scopes     []string
	Reason     string
	SubjectRef string
}

func (a Authorization) hasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// InvocationEvidence is one gateway decision - an invocation or a refusal -
// recorded for evidence. Reference: capability-registry-and-lifecycle.md
// "Security, Failure, and Evidence".
type InvocationEvidence struct {
	CapabilityID      string
	CapabilityVersion uint32
	SubjectRef        string
	Decision          string // "INVOKED" or the refusal Code
	ReasonCode        string
	OccurredAt        time.Time
}

// EvidenceSink is the port an invocation evidence record is written through.
// The concrete sink (a ledger append, a test recorder) lives outside this
// package.
type EvidenceSink interface {
	RecordInvocation(ctx context.Context, evt InvocationEvidence) (evidenceID string, err error)
}

// InvokeRequest is one call into the governed gateway.
type InvokeRequest struct {
	Capability    Key
	Payload       any
	Authorization Authorization
}

// InvokeResult is a successful invocation's typed result.
type InvokeResult struct {
	Response   any
	EvidenceID string
}

// Gateway is CAP-002's governed capability gateway: the one path every
// transport shares (capability-registry-and-lifecycle.md, platform-plane-
// model.md). It resolves the exact capability version, requires an
// already-made Authorization decision, enforces the P1A zero-effect rule and
// records evidence for every invocation and refusal.
type Gateway struct {
	registry *Registry
	evidence EvidenceSink
	now      func() time.Time
}

// GatewayOption configures a Gateway.
type GatewayOption func(*Gateway)

// WithClock replaces the gateway's source of evidence timestamps.
func WithClock(now func() time.Time) GatewayOption {
	return func(g *Gateway) { g.now = now }
}

// NewGateway builds a Gateway over a registry and an evidence sink.
func NewGateway(registry *Registry, evidence EvidenceSink, opts ...GatewayOption) *Gateway {
	g := &Gateway{registry: registry, evidence: evidence, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Invoke resolves req.Capability, enforces authorization, effect class and
// status, and - only once every check passes - calls the bound handler.
// Every path, success or refusal, records one InvocationEvidence.
func (g *Gateway) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	rec, found := g.registry.Lookup(req.Capability)
	if !found {
		return g.refuse(ctx, req, CodeUnknownCapability, "capability is not registered")
	}

	handler, status, _ := g.registry.handlerFor(req.Capability)
	if status == StatusRetired {
		return g.refuse(ctx, req, CodeCapabilityDisabled, "capability version is retired")
	}

	if req.Authorization.Decision != Allow {
		reason := req.Authorization.Reason
		if reason == "" {
			reason = "authorization decision is not ALLOW"
		}
		return g.refuse(ctx, req, CodeUnauthorized, reason)
	}
	if !req.Authorization.hasScope(rec.Definition.AuthZScopeRef) {
		return g.refuse(ctx, req, CodeUnauthorized, "authorization does not grant "+rec.Definition.AuthZScopeRef)
	}

	if rec.Definition.EffectClass.IsWrite() {
		return g.refuse(ctx, req, CodeWriteEffectRefusedP1A, "capability declares a write effect class; P1A invokes zero-effect capabilities only")
	}

	response, err := handler(ctx, req.Payload)
	if err != nil {
		return g.refuse(ctx, req, CodeHandlerFailed, err.Error())
	}

	evidenceID, evErr := g.evidence.RecordInvocation(ctx, InvocationEvidence{
		CapabilityID:      req.Capability.ID,
		CapabilityVersion: req.Capability.Version,
		SubjectRef:        req.Authorization.SubjectRef,
		Decision:          "INVOKED",
		OccurredAt:        g.now(),
	})
	if evErr != nil {
		return InvokeResult{}, evErr
	}

	return InvokeResult{Response: response, EvidenceID: evidenceID}, nil
}

// refuse records refusal evidence and returns the typed GatewayError. It
// never calls the handler.
func (g *Gateway) refuse(ctx context.Context, req InvokeRequest, code, reason string) (InvokeResult, error) {
	evidenceID, evErr := g.evidence.RecordInvocation(ctx, InvocationEvidence{
		CapabilityID:      req.Capability.ID,
		CapabilityVersion: req.Capability.Version,
		SubjectRef:        req.Authorization.SubjectRef,
		Decision:          "REFUSED",
		ReasonCode:        code,
		OccurredAt:        g.now(),
	})
	if evErr != nil {
		evidenceID = ""
	}
	return InvokeResult{}, &GatewayError{
		Code:       code,
		Capability: req.Capability.ID,
		Version:    req.Capability.Version,
		Reason:     reason,
		EvidenceID: evidenceID,
	}
}
