package edge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"connectrpc.com/connect"

	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// defaultMaxBodyBytes bounds an inbound request body. Bounded decoding is part
// of the strict-decoding contract.
const defaultMaxBodyBytes = 4 << 20

// Options configures [NewHandler].
type Options struct {
	// Config is the shared admission configuration. Pass the same value used
	// for the gRPC server; that shared value is what makes the two transports
	// derive identical trusted context rather than merely similar context.
	Config transport.Config
	// Intent is the BusinessIntent lifecycle handler port. Optional.
	Intent transport.IntentHandler
	// Registry is the discovery handler port. Optional.
	Registry transport.RegistryHandler
	// MaxBodyBytes bounds an inbound body. Zero means 4 MiB.
	MaxBodyBytes int
	// HandlerOptions are appended after the options this package sets.
	HandlerOptions []connect.HandlerOption
}

// Configuration errors for [NewHandler].
var (
	ErrNoHandlers = errors.New("edge: at least one handler port must be configured")
	ErrNoVerifier = errors.New("edge: a trust.Verifier is required")
)

// NewHandler builds the HTTP edge for the configured handler ports.
//
// The returned handler is the whole edge: strict JSON admission screening,
// the connect procedure mux with the admission interceptor installed, and the
// HTTP status projection that overrides connect's own mapping where the
// canonical error projection table disagrees with it.
func NewHandler(opts Options) (http.Handler, error) {
	if opts.Config.Verifier == nil {
		return nil, ErrNoVerifier
	}
	if opts.Intent == nil && opts.Registry == nil {
		return nil, ErrNoHandlers
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	handlerOptions := []connect.HandlerOption{
		connect.WithReadMaxBytes(maxBody),
		connect.WithInterceptors(admissionInterceptor{cfg: opts.Config}),
	}
	handlerOptions = append(handlerOptions, opts.HandlerOptions...)

	mux := http.NewServeMux()
	if h := opts.Intent; h != nil {
		mount(mux, ProcedureCreateIntent, unary(h.CreateIntent), handlerOptions)
		mount(mux, ProcedureGetIntent, unary(h.GetIntent), handlerOptions)
		mount(mux, ProcedureListIntents, unary(h.ListIntents), handlerOptions)
		mount(mux, ProcedureSimulateIntent, unary(h.SimulateIntent), handlerOptions)
		mount(mux, ProcedureSubmitIntent, unary(h.SubmitIntent), handlerOptions)
		mount(mux, ProcedureCancelIntent, unary(h.CancelIntent), handlerOptions)
		mount(mux, ProcedureSupersedeIntent, unary(h.SupersedeIntent), handlerOptions)
		mount(mux, ProcedureExplainIntent, unary(h.ExplainIntent), handlerOptions)
		mount(mux, ProcedureListIntentTimeline, unary(h.ListIntentTimeline), handlerOptions)
	}
	if h := opts.Registry; h != nil {
		mount(mux, ProcedureListIntentDefinitions, unary(h.ListIntentDefinitions), handlerOptions)
		mount(mux, ProcedureGetIntentDefinition, unary(h.GetIntentDefinition), handlerOptions)
		mount(mux, ProcedureListCapabilities, unary(h.ListCapabilities), handlerOptions)
		mount(mux, ProcedureGetCapability, unary(h.GetCapability), handlerOptions)
	}

	return statusOverrideMiddleware(strictJSONMiddleware(mux, opts.Config, maxBody)), nil
}

// unary adapts a handler port method to the connect unary signature. The
// adapter does nothing but move the message across; every rule that applies to
// the call already ran in the interceptor.
func unary[Req, Res any](fn func(context.Context, *Req) (*Res, error)) func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error) {
	return func(ctx context.Context, req *connect.Request[Req]) (*connect.Response[Res], error) {
		res, err := fn(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}
}

// mount registers one procedure on the mux.
func mount[Req, Res any](mux *http.ServeMux, procedure string, fn func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error), opts []connect.HandlerOption) {
	mux.Handle(procedure, connect.NewUnaryHandler(procedure, fn, opts...))
}

// strictJSONMiddleware screens a JSON request body before connect decodes it.
//
// It exists because JSON is a laxer encoding than Protobuf, and connect's
// JSON codec discards unknown fields by design. The endpoint contract requires
// the opposite: an unknown field, a duplicate key, invalid UTF-8 or an
// ambiguous numeric literal is a rejected request. Screening here, rather than
// inside a replacement codec, is what lets the rejection carry the owned code
// and the field paths instead of a decoder's own message.
//
// It runs transport.PreAdmit first, in the same order native gRPC does:
// reserved-metadata screen, correlation identifier, authentication, and only
// then structure. Screening the body for an anonymous caller would hand them a
// validity oracle that a gRPC caller does not get, which is a parity failure
// as much as a security one.
func strictJSONMiddleware(next http.Handler, cfg transport.Config, maxBody int) http.Handler {
	errorWriter := connect.NewErrorWriter()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		factory, known := requestFactories[r.URL.Path]
		if !known || r.Method != http.MethodPost || !isJSONContentType(r.Header) {
			next.ServeHTTP(w, r)
			return
		}

		principal, requestID, admitErr := transport.PreAdmit(r.Context(), cfg,
			transport.MapMetadata(r.Header), r.URL.Path)
		if admitErr != nil {
			writeOwned(errorWriter, w, r, admitErr)
			return
		}
		evidence := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}

		body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBody)+1))
		if err != nil {
			writeOwned(errorWriter, w, r, envelope.New(envelope.CodeInvalidArgument,
				reasonStrictDecoding, "the request body could not be read").
				WithViolation("(request)", "the request body could not be read", ruleMalformedJSON).
				WithCorrelation(requestID).WithEvidence(evidence))
			return
		}
		if len(body) > maxBody {
			writeOwned(errorWriter, w, r, envelope.New(envelope.CodeResourceExhausted,
				reasonStrictDecoding, "the request body exceeds the accepted size").
				WithViolation("(request)", "the request body exceeds the accepted size", ruleBodyTooLarge).
				WithCorrelation(requestID).WithEvidence(evidence))
			return
		}

		if violations := screenJSONBody(body, factory()); len(violations) > 0 {
			owned := envelope.New(envelope.CodeInvalidArgument, reasonStrictDecoding,
				"the request is malformed or structurally invalid").
				WithCorrelation(requestID).WithEvidence(evidence)
			for _, v := range violations {
				owned.WithViolation(v.FieldPath, v.Description, v.RuleRef)
			}
			writeOwned(errorWriter, w, r, owned)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

// reasonStrictDecoding is the owned reason identifier for a strict-decoding
// rejection at the edge.
const reasonStrictDecoding = "structural.request_rejected"

// writeOwned writes an owned error as a protocol-correct connect response and
// records the HTTP status the canonical projection table requires.
func writeOwned(ew *connect.ErrorWriter, w http.ResponseWriter, r *http.Request, owned *envelope.Error) {
	setStatusOverride(r.Context(), owned.HTTPStatus())
	if err := ew.Write(w, r, ToConnectError(owned)); err != nil {
		http.Error(w, "", owned.HTTPStatus())
	}
}
