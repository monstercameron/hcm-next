// Package transporttest (this file): the EDGE-001 conformance kit.
//
// EDGE-001 requires ONE admission pipeline every published edge runs
// identically: native gRPC, the HTTP/Connect edge and the gRPC-over-WebSocket
// tunnel. internal/transport already is that one pipeline
// ([transport.Admit]/[transport.PreAdmit]/[transport.CapDeadline] over one
// shared [transport.Config]); what proves the claim is a refusal matrix driven
// through every edge with the assertion that the outcomes are field-identical.
//
// This file owns the transport-agnostic half of that proof: the eight-row
// matrix itself (RefusalMatrix), the malformed-request builders every row
// needs (WithUnknownField, the oversized-field length, the wrong-audience and
// expired-credential claim variants) and the comparison
// (AssertConformant). Driving an actual wire transport - minting a real
// gRPC/Connect/tunnel client and sending the scenario over it - is left to the
// caller, because only the caller knows the concrete transport shapes; this
// package would otherwise have to import connect-go, grpc-go and the tunnel
// library itself, which is exactly the dependency LIB-003 keeps out of
// anything that is not internal/transport, internal/transport/edge,
// internal/transport/grpcserver or internal/transport/cell.
package transporttest

import (
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Reporter is the minimal test-failure surface [AssertConformant] needs.
// *testing.T and *testing.B both satisfy it; declaring it locally rather than
// taking a *testing.T keeps this package (like fixture.go beside it) free of
// a hard "testing" import.
type Reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// ConformanceCredential selects which bearer credential a [ConformanceScenario]
// presents.
type ConformanceCredential int

// Credential selectors a conformance caller must resolve to an actual
// Authorization header value (or its absence).
const (
	// ConformanceCredentialValid presents the fixture's ordinary, currently
	// valid credential.
	ConformanceCredentialValid ConformanceCredential = iota
	// ConformanceCredentialMissing presents no credential at all.
	ConformanceCredentialMissing
	// ConformanceCredentialWrongAudience presents a credential signed for an
	// audience the listener does not answer to. See [WrongAudienceClaims].
	ConformanceCredentialWrongAudience
	// ConformanceCredentialExpired presents a credential whose validity
	// window has already closed. See [ExpiredClaims].
	ConformanceCredentialExpired
)

// ConformanceMethod selects which IntentService method a [ConformanceScenario]
// calls. Two methods cover the matrix: GetIntent needs an identifier (so the
// oversized-field and unknown-field rows have somewhere to put their attack),
// and ListIntents needs nothing at all (so the rows that must succeed are not
// coupled to whatever intents happen to exist in the composed cell's store).
type ConformanceMethod int

// Method selectors.
const (
	ConformanceMethodGetIntent ConformanceMethod = iota
	ConformanceMethodListIntents
)

// ConformanceOversizedIntentIDLen exceeds every transport's configured
// string-field bound (4096 bytes as of this writing; see
// internal/transport/validate.go). The exact bound is deliberately not
// imported here - it is unexported, and a conformance kit that hard-coded the
// implementation's own constant would stop testing anything the moment the
// two drifted apart from a shared source. 5000 stays safely past any bound
// this transport is ever likely to configure.
const ConformanceOversizedIntentIDLen = 5000

// ConformanceUnknownFieldNumber is the field number [WithUnknownField] tags a
// message with. It names no field on any request message this repository
// generates, which is what makes the field "material" (unrecognized by every
// version of the schema this server was built against) rather than a
// forward-compatible addition a lenient decoder would be right to ignore.
const ConformanceUnknownFieldNumber protowire.Number = 9999

// ConformanceScenario is one row of the EDGE-001 refusal matrix: a
// transport-agnostic description of one request that every admission edge
// must answer identically, because every edge ultimately calls
// [transport.Admit] (directly, or - for the tunnel's per-RPC admission -
// through the very same grpcserver interceptor the native gRPC edge runs)
// over the one [transport.Config] the composed cell shares between them.
type ConformanceScenario struct {
	// Name identifies the row in test output.
	Name string
	// Method selects which call the scenario makes.
	Method ConformanceMethod
	// Credential selects which bearer credential to present.
	Credential ConformanceCredential
	// ExtraMetadata is additional header/metadata pairs layered on top of the
	// credential. The one matrix row that populates this is the
	// caller-selected-trusted-field row, which adds a reserved trusted-context
	// name a hostile caller has no legitimate reason to send.
	ExtraMetadata map[string]string
	// IntentID is the GetIntentRequest.IntentId value the scenario sends.
	// Unused when Method is ConformanceMethodListIntents.
	IntentID string
	// UnknownField appends a material unknown Protobuf field
	// ([ConformanceUnknownFieldNumber]) to the request via [WithUnknownField].
	UnknownField bool
	// Deadline overrides the client-requested deadline. Zero means the
	// caller's own sane default; the "deadline beyond cap" row sets this far
	// past any deadline cap a test harness would configure, to prove an
	// oversized request does not hang or get refused - it gets capped.
	Deadline time.Duration
	// WantSuccess is true for the rows that must be admitted and answered.
	WantSuccess bool
	// WantCode is the owned code every edge's refusal must carry. Meaningful
	// only when WantSuccess is false.
	WantCode envelope.Code
}

// RefusalMatrix is the EDGE-001 conformance matrix: the eight request shapes
// RED requires be driven identically through native gRPC, the HTTP/Connect
// edge and the gRPC-over-WebSocket tunnel - missing credential, wrong
// audience, expired token, a caller-selected trusted field, an oversized
// message, an unknown field, a deadline beyond the server's cap, and a valid
// call.
func RefusalMatrix() []ConformanceScenario {
	return []ConformanceScenario{
		{
			Name:       "missing credential",
			Method:     ConformanceMethodGetIntent,
			Credential: ConformanceCredentialMissing,
			IntentID:   KnownIntentID,
			WantCode:   envelope.CodeUnauthenticated,
		},
		{
			Name:       "wrong audience",
			Method:     ConformanceMethodGetIntent,
			Credential: ConformanceCredentialWrongAudience,
			IntentID:   KnownIntentID,
			WantCode:   envelope.CodeUnauthenticated,
		},
		{
			Name:       "expired token",
			Method:     ConformanceMethodGetIntent,
			Credential: ConformanceCredentialExpired,
			IntentID:   KnownIntentID,
			WantCode:   envelope.CodeUnauthenticated,
		},
		{
			Name:          "caller-selected trusted field",
			Method:        ConformanceMethodGetIntent,
			Credential:    ConformanceCredentialValid,
			ExtraMetadata: map[string]string{"x-tenant": "victim-corp"},
			IntentID:      KnownIntentID,
			WantCode:      envelope.CodeInvalidArgument,
		},
		{
			Name:       "oversized message",
			Method:     ConformanceMethodGetIntent,
			Credential: ConformanceCredentialValid,
			IntentID:   strings.Repeat("o", ConformanceOversizedIntentIDLen),
			WantCode:   envelope.CodeInvalidArgument,
		},
		{
			Name:         "unknown field",
			Method:       ConformanceMethodGetIntent,
			Credential:   ConformanceCredentialValid,
			IntentID:     KnownIntentID,
			UnknownField: true,
			WantCode:     envelope.CodeInvalidArgument,
		},
		{
			Name:        "deadline beyond cap",
			Method:      ConformanceMethodListIntents,
			Credential:  ConformanceCredentialValid,
			Deadline:    24 * time.Hour,
			WantSuccess: true,
		},
		{
			Name:        "valid call",
			Method:      ConformanceMethodListIntents,
			Credential:  ConformanceCredentialValid,
			WantSuccess: true,
		},
	}
}

// EffectiveDeadline returns the deadline a caller should apply for the
// scenario: Deadline when it is set, and def otherwise.
func (s ConformanceScenario) EffectiveDeadline(def time.Duration) time.Duration {
	if s.Deadline > 0 {
		return s.Deadline
	}
	return def
}

// ConformanceOutcome is what one edge produced for one [ConformanceScenario].
// Err is nil exactly when the call succeeded.
type ConformanceOutcome struct {
	Err *envelope.Error
}

// ConformanceCaller drives one edge with one scenario and reports what that
// edge produced. A conformance suite builds one per edge (native gRPC, the
// HTTP/Connect edge, the tunnel) and runs [RefusalMatrix] through each.
type ConformanceCaller func(scenario ConformanceScenario) ConformanceOutcome

// WrongAudienceClaims returns [DefaultClaims] signed for an audience the
// fixture verifier does not answer to. The claims are otherwise valid: only
// the audience is wrong, so a verifier that accepted this credential would be
// failing on audience alone, not on some other defect riding along with it.
func WrongAudienceClaims(now time.Time) trust.Claims {
	c := DefaultClaims(now)
	c.Audience = Audience + "-wrong"
	return c
}

// ExpiredClaims returns [DefaultClaims] whose validity window closed one hour
// before now. Issued-at is moved back with it so the window stays internally
// consistent (issued before it expired) rather than merely broken in two
// places at once.
func ExpiredClaims(now time.Time) trust.Claims {
	c := DefaultClaims(now)
	c.IssuedAtUnix = now.Add(-2 * time.Hour).Unix()
	c.ExpiresAtUnix = now.Add(-time.Hour).Unix()
	return c
}

// WithUnknownField returns a clone of msg carrying one unrecognized Protobuf
// field at fieldNumber, which is how a "material unknown field" is presented
// to a server compiled against the current descriptor.
func WithUnknownField(msg proto.Message, fieldNumber protowire.Number) proto.Message {
	out := proto.Clone(msg)
	raw := protowire.AppendTag(nil, fieldNumber, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 1)
	out.ProtoReflect().SetUnknown(protoreflect.RawFields(raw))
	return out
}

// AssertConformant fails r unless every outcome in outcomes agrees with every
// other one, and the agreed-upon outcome matches what scenario declares it
// must be.
//
// "Agrees" means field-identical, not merely same-code: two owned errors that
// differ in their violations, their retryability or their projected HTTP
// status are not the same refusal wearing two costumes, and a matrix that only
// compared codes would not catch that.
func AssertConformant(r Reporter, scenario ConformanceScenario, outcomes map[string]ConformanceOutcome) {
	r.Helper()
	if len(outcomes) == 0 {
		r.Errorf("%s: no edges reported an outcome", scenario.Name)
		return
	}

	names := make([]string, 0, len(outcomes))
	for name := range outcomes {
		names = append(names, name)
	}
	sort.Strings(names)

	base := outcomes[names[0]]
	for _, name := range names[1:] {
		other := outcomes[name]
		assertOutcomeParity(r, scenario.Name, names[0], base, name, other)
	}

	switch {
	case scenario.WantSuccess && base.Err != nil:
		r.Errorf("%s (%s): want success, got %v", scenario.Name, names[0], base.Err)
	case !scenario.WantSuccess && base.Err == nil:
		r.Errorf("%s (%s): want %v, got success", scenario.Name, names[0], scenario.WantCode)
	case !scenario.WantSuccess && base.Err.Code() != scenario.WantCode:
		r.Errorf("%s (%s): code = %v, want %v", scenario.Name, names[0], base.Err.Code(), scenario.WantCode)
	}
}

// assertOutcomeParity compares two edges' outcomes for the same scenario.
func assertOutcomeParity(r Reporter, scenarioName, leftName string, left ConformanceOutcome, rightName string, right ConformanceOutcome) {
	r.Helper()
	if (left.Err == nil) != (right.Err == nil) {
		r.Errorf("%s: %s success=%v but %s success=%v (errs: %v / %v)",
			scenarioName, leftName, left.Err == nil, rightName, right.Err == nil, left.Err, right.Err)
		return
	}
	if left.Err == nil {
		return
	}
	if left.Err.Code() != right.Err.Code() {
		r.Errorf("%s: owned code differs: %s=%v %s=%v", scenarioName, leftName, left.Err.Code(), rightName, right.Err.Code())
	}
	if left.Err.Retryable() != right.Err.Retryable() {
		r.Errorf("%s: retryability differs: %s=%v %s=%v", scenarioName, leftName, left.Err.Retryable(), rightName, right.Err.Retryable())
	}
	if left.Err.Message() != right.Err.Message() {
		r.Errorf("%s: message differs: %s=%q %s=%q", scenarioName, leftName, left.Err.Message(), rightName, right.Err.Message())
	}
	if left.Err.HTTPStatus() != right.Err.HTTPStatus() {
		r.Errorf("%s: projected HTTP status differs: %s=%d %s=%d", scenarioName, leftName, left.Err.HTTPStatus(), rightName, right.Err.HTTPStatus())
	}
	leftViolations, rightViolations := left.Err.Violations(), right.Err.Violations()
	if len(leftViolations) != len(rightViolations) {
		r.Errorf("%s: violation count differs: %s=%d (%v) %s=%d (%v)",
			scenarioName, leftName, len(leftViolations), leftViolations, rightName, len(rightViolations), rightViolations)
		return
	}
	for i := range leftViolations {
		if leftViolations[i] != rightViolations[i] {
			r.Errorf("%s: violation %d differs: %s=%+v %s=%+v", scenarioName, i, leftName, leftViolations[i], rightName, rightViolations[i])
		}
	}
}
