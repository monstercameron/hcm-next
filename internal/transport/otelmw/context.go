package otelmw

import (
	"context"
	"strings"

	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// deriveLoggingContext copies the trusted values transport.Admit already
// placed in ctx — the immutable *transport.Invocation and the verified
// *trust.Principal — onto internal/platform/logging's request-scoped
// context accessors, so that internal/platform/telemetry/otel's automatic
// context-to-span-attribute propagation (trace.go's contextAttributes) and
// any future log sink reading the same accessors see them.
//
// It never parses inbound metadata or headers itself: request_id here is
// the same requestID resolveRequestID already resolved (accepting a bounded
// caller-supplied "x-request-id" only after it passed that function's own
// screen, or minting one otherwise) and now exposed read-only through
// Invocation.RequestID. Nothing in this function trusts a caller-supplied
// principal — the only principal reference it can ever produce comes from
// the *trust.Principal the frozen authentication step already verified and
// wrote into ctx; a request that failed authentication never reaches this
// function at all, because admission never calls this package's handler for
// one.
//
// A ctx with no Invocation (a call reached this package outside its
// documented ordering requirement, or a test exercising it directly)
// returns ctx unchanged rather than panicking: this package instruments a
// call, it does not gate one.
func deriveLoggingContext(ctx context.Context) context.Context {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return ctx
	}

	// This system has one request-scoped correlation identifier —
	// Invocation.RequestID's own doc comment: "returns the correlation
	// identifier for this request" — so both of
	// internal/platform/logging's per-call and cross-call accessors are
	// populated from it. There is no second, longer-lived business
	// correlation identifier in the transport layer to distinguish them by.
	if id := inv.RequestID(); id != "" {
		ctx = logging.WithCorrelationID(ctx, id)
		ctx = logging.WithRequestID(ctx, id)
	}
	if id := inv.EvidenceID(); id != "" {
		ctx = logging.WithEvidenceIDs(ctx, id)
	}
	if p := inv.Principal(); p != nil {
		if ref := principalRef(p); ref != "" {
			// A ref that fails ValidatePrincipalRef (oversized, or
			// shaped like a raw identity) is dropped rather than
			// surfacing as a request failure: telemetry context
			// derivation is best-effort and must never change a
			// business result. It is also, today, redacted by the
			// frozen attribute allow-list regardless (see doc.go and
			// internal/platform/telemetry/otel/trace.go's own
			// comment on principal_ref), so a dropped ref changes
			// nothing an exporter could ever have observed.
			if next, err := logging.WithPrincipalRef(ctx, ref); err == nil {
				ctx = next
			}
		}
	}
	return ctx
}

// principalRef builds the opaque, bounded principal reference
// internal/platform/logging.WithPrincipalRef expects (see its own doc
// comment's example: "principal:worker:<uuid>") from the verified
// principal's subject kind and subject — both server-derived by
// authenticate() in internal/transport/invocation.go, never caller-supplied.
func principalRef(p *trust.Principal) string {
	subject := p.Subject()
	if subject == "" {
		return ""
	}
	return "principal:" + strings.ToLower(p.SubjectKind().String()) + ":" + subject
}
