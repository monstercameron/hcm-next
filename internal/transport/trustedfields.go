package transport

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ResolvedScope is the server-derived scope that admission wrote into the
// request and recorded on the [Invocation].
type ResolvedScope struct {
	TenantID            string
	OrganizationScopeID string
	Purpose             string
}

// Fully qualified names of the request-message types that carry trusted
// context. Matching on the descriptor's full name rather than on a Go type
// means a new service picks the enforcement up for free.
const (
	scopeContextFullName       = "hcmnext.common.v1.ScopeContext"
	principalReferenceFullName = "hcmnext.intents.v1.PrincipalReference"
)

// Reason and rule identifiers for trusted-field rejections.
const (
	reasonCallerSelectedAuthority = "trusted_context.caller_selected_authority"
	reasonPurposeNotAuthorized    = "trusted_context.purpose_not_authorized"
	ruleTrustedRequestBoundary    = "trusted_request_boundary.server_derived_field"
	rulePurposeAuthorization      = "trusted_request_boundary.purpose_authorization"
)

// ApplyTrustedContext rejects any caller attempt to select trusted context
// inside the request message, then overwrites the message's trusted fields
// with server-derived values and returns the resolved scope.
//
// Two message-level rules cover the current surface, and both are expressed
// against descriptors so they extend to methods that do not exist yet:
//
//   - A hcmnext.common.v1.ScopeContext field may carry a tenant or
//     organization scope only if it exactly matches what the server derived.
//     Anything else is a rejection, not a silent overwrite, because a client
//     that believes it is acting on another tenant must be told it is wrong.
//     A purpose may be requested, and is checked against the principal's
//     authorized purposes.
//   - A hcmnext.intents.v1.PrincipalReference field is entirely server-derived.
//     A populated one is a rejection; admission then fills it from the
//     verified principal.
//
// A nil message resolves the scope from the principal alone, which is what a
// method with no scope field gets.
func ApplyTrustedContext(msg proto.Message, principal *trust.Principal) (ResolvedScope, *envelope.Error) {
	resolved := ResolvedScope{
		TenantID:            principal.Tenant().String(),
		OrganizationScopeID: principal.OrganizationScopeID(),
		Purpose:             principal.DefaultPurpose(),
	}
	if msg == nil {
		return resolved, nil
	}

	m := msg.ProtoReflect()
	fields := m.Descriptor().Fields()

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind {
			continue
		}
		switch string(fd.Message().FullName()) {
		case scopeContextFullName:
			scope, _ := m.Get(fd).Message().Interface().(*commonv1.ScopeContext)
			next, err := reconcileScope(string(fd.Name()), scope, principal, resolved)
			if err != nil {
				return ResolvedScope{}, err
			}
			resolved = next
			m.Set(fd, protoreflect.ValueOfMessage((&commonv1.ScopeContext{
				TenantId:            resolved.TenantID,
				OrganizationScopeId: resolved.OrganizationScopeID,
				Purpose:             resolved.Purpose,
			}).ProtoReflect()))
		case principalReferenceFullName:
			if m.Has(fd) {
				return ResolvedScope{}, envelope.New(envelope.CodeInvalidArgument,
					reasonCallerSelectedAuthority,
					"the request may not select trusted context").
					WithViolation(string(fd.Name()),
						"the acting principal is derived server-side from the verified credential",
						ruleTrustedRequestBoundary)
			}
			m.Set(fd, protoreflect.ValueOfMessage(serverPrincipalReference(principal).ProtoReflect()))
		}
	}
	return resolved, nil
}

// reconcileScope checks a caller-supplied ScopeContext against the
// server-derived values and returns the effective scope.
func reconcileScope(fieldName string, scope *commonv1.ScopeContext, principal *trust.Principal, derived ResolvedScope) (ResolvedScope, *envelope.Error) {
	if scope == nil {
		return derived, nil
	}
	reject := func(sub, description, rule string) *envelope.Error {
		return envelope.New(envelope.CodeInvalidArgument,
			reasonCallerSelectedAuthority,
			"the request may not select trusted context").
			WithViolation(fieldName+"."+sub, description, rule)
	}

	if got := scope.GetTenantId(); got != "" && got != derived.TenantID {
		return ResolvedScope{}, reject("tenant_id",
			"the tenant is derived server-side from the verified credential",
			ruleTrustedRequestBoundary)
	}
	if got := scope.GetOrganizationScopeId(); got != "" && got != derived.OrganizationScopeID {
		return ResolvedScope{}, reject("organization_scope_id",
			"the organization scope is derived server-side from the verified credential",
			ruleTrustedRequestBoundary)
	}

	out := derived
	if requested := scope.GetPurpose(); requested != "" {
		if !principal.AuthorizesPurpose(requested) {
			return ResolvedScope{}, envelope.New(envelope.CodePermissionDenied,
				reasonPurposeNotAuthorized,
				"the requested purpose of processing is not authorized for this principal").
				WithViolation(fieldName+".purpose",
					"the principal is not authorized for the requested purpose of processing",
					rulePurposeAuthorization)
		}
		out.Purpose = requested
	}
	return out, nil
}

// serverPrincipalReference projects the verified principal onto the wire
// PrincipalReference the domain consumes. The assurance reference points at
// the authentication evidence record, never at raw identity-provider claims.
func serverPrincipalReference(p *trust.Principal) *intentsv1.PrincipalReference {
	return &intentsv1.PrincipalReference{
		PrincipalId:          p.Subject(),
		Kind:                 initiatorKind(p.SubjectKind()),
		IdentityAssuranceRef: p.EvidenceID(),
	}
}

// initiatorKind maps an authenticated actor kind onto the wire initiator kind.
func initiatorKind(k trust.SubjectKind) intentsv1.InitiatorKind {
	switch k {
	case trust.SubjectKindHuman:
		return intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN
	case trust.SubjectKindService:
		return intentsv1.InitiatorKind_INITIATOR_KIND_SERVICE
	case trust.SubjectKindAgent:
		return intentsv1.InitiatorKind_INITIATOR_KIND_AGENT
	case trust.SubjectKindIntegration:
		return intentsv1.InitiatorKind_INITIATOR_KIND_INTEGRATION
	default:
		return intentsv1.InitiatorKind_INITIATOR_KIND_UNSPECIFIED
	}
}
