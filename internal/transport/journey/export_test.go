package journey

import (
	"time"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
)

// This file exposes two unexported behaviours to the external test package
// and to nothing else. Both are deliberately not part of the package's real
// API: a caller outside this package must never compute a detail digest of
// its own (the server stamps the one that counts), and the watch bounds are
// properties of this service rather than knobs on the wire.

// DetailDigestForTest recomputes the change-detection digest of d.
func DetailDigestForTest(d *journeyv1.JourneyDetail) string { return detailDigest(d) }

// WatchBoundsForTest reports the compiled-in default poll interval and the
// stream ceiling, so a test asserts against the same constants the handler
// uses rather than restating them.
func WatchBoundsForTest() (defaultPoll, maxLifetime time.Duration) {
	return defaultWatchPollInterval, watchMaxLifetime
}

// EffectivePollIntervalForTest reports the interval deps would actually poll
// at, which is what proves the option is honoured and that its default is
// the compiled-in one.
func EffectivePollIntervalForTest(deps Dependencies) time.Duration { return deps.pollInterval() }

// EffectiveCursorTTLForTest reports the cursor lifetime deps would actually
// issue, so the default is asserted against the compiled-in constant rather
// than a number a test restates.
func EffectiveCursorTTLForTest(deps Dependencies) time.Duration { return deps.cursorTTL() }

// CursorEnabledForTest reports whether deps would issue stream cursors at
// all, which is the documented "nil key means no cursor" composition made
// checkable without opening a stream.
func CursorEnabledForTest(deps Dependencies) bool {
	_, ok := deps.cursorSigner()
	return ok
}

// WatchStreamIDForTest reports the stream identity WatchJourney binds a
// cursor to for one journey. A test that mints a cursor for a *different*
// stream, to prove it is refused, has to name the real id space rather than
// guess at it.
func WatchStreamIDForTest(intentID string) string { return watchStreamID(intentID) }

// WatchJourneyForTest runs the WatchJourney handler directly against a
// caller-supplied server stream, bypassing grpc-go's transport entirely.
//
// It exists for exactly one claim no test driven through a real connection
// can make honestly: that the handler holds at most one message and stops
// producing while a send is blocked. Over a real connection the thing that
// blocks is grpc-go's flow-control window, whose size is dynamic, so a test
// there would be measuring the transport's buffering rather than this
// handler's. A stream whose Send simply never returns measures the handler.
func WatchJourneyForTest(
	deps Dependencies,
	req *journeyv1.WatchJourneyRequest,
	stream journeyv1.JourneyService_WatchJourneyServer,
) error {
	return (&server{deps: deps}).WatchJourney(req, stream)
}

// The three workforce conversions are total in both directions, and the
// inverses exist so a client built on this package's own types produces the
// same messages a page does. Exposing the round trips is how the external
// test package proves neither direction drops a field, without either
// direction becoming part of the real API.

// WorkerRoundTripForTest renders a worker onto the wire and back.
func WorkerRoundTripForTest(w workspace.WorkerSummary) workspace.WorkerSummary {
	return fromWorker(toWorker(w))
}

// WorkforceOptionsRoundTripForTest renders the placement options onto the
// wire and back.
func WorkforceOptionsRoundTripForTest(o workspace.WorkforceOptions) workspace.WorkforceOptions {
	return fromWorkforceOptions(toWorkforceOptions(o))
}

// CreateWorkerRequestRoundTripForTest renders a create form onto the wire and
// back.
func CreateWorkerRequestRoundTripForTest(in workspace.WorkerInput) workspace.WorkerInput {
	return fromCreateWorkerRequest(toCreateWorkerRequest(in))
}
