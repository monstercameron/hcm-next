package sandbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
)

// EffectAttempt names one kind of outbound reach a [Fence] refused. It is a
// closed vocabulary, the same discipline internal/workflow/replay.EffectAttempt
// uses for the WF-RUN-013 recorder: a refusal an auditor could not classify
// would be a refusal an auditor could not rule on.
type EffectAttempt string

// The declared attempts a sandbox fence may refuse.
const (
	// AttemptConnectorReach is a read routed at a system-of-record connector
	// (internal/connectivity.Connector) that names a destination outside this
	// sandbox's synthetic fixture.
	AttemptConnectorReach EffectAttempt = "CONNECTOR_REACH"
	// AttemptMessageSend delivers a message to a person.
	AttemptMessageSend EffectAttempt = "MESSAGE_SEND"
	// AttemptWebhookDispatch calls an outbound webhook.
	AttemptWebhookDispatch EffectAttempt = "WEBHOOK_DISPATCH"
	// AttemptPaymentTransfer moves money.
	AttemptPaymentTransfer EffectAttempt = "PAYMENT_TRANSFER"
	// AttemptFilingSubmission submits a regulatory filing.
	AttemptFilingSubmission EffectAttempt = "FILING_SUBMISSION"
	// AttemptTelemetryExport ships a span or metric to a real collector.
	AttemptTelemetryExport EffectAttempt = "TELEMETRY_EXPORT"
)

// ErrExternalEffectFenced is the sentinel every [FencedEffectError] wraps.
var ErrExternalEffectFenced = errors.New("sandbox: external effect fenced")

// FencedEffectError is the typed refusal a [Fence] returns: which adapter
// tried, what kind of attempt it was, and the destination it named. It names
// both because "an adapter reached outside" and "which outside it reached"
// are two different facts an auditor needs, and a refusal that only carried
// one of them would still leave the other to reconstruct from logs.
type FencedEffectError struct {
	Adapter     string
	Attempt     EffectAttempt
	Destination string
}

func (e *FencedEffectError) Error() string {
	return fmt.Sprintf("sandbox: adapter %q refused %s to destination %q", e.Adapter, e.Attempt, e.Destination)
}

// Unwrap lets a caller test with errors.Is(err, ErrExternalEffectFenced).
func (e *FencedEffectError) Unwrap() error { return ErrExternalEffectFenced }

// Refusal is one recorded attempt a [Fence] turned away.
type Refusal struct {
	Adapter     string
	Attempt     EffectAttempt
	Destination string
	At          time.Time
}

// Fence is the hard side-effect fence: it refuses every attempt handed to it
// and records the refusal, unconditionally. There is no allow path on this
// type at all - an adapter that should be permitted (a governed read of the
// sandbox's own synthetic fixture, say) is never routed through [Fence.Refuse]
// in the first place; see [FencedConnector] for how that admission decision
// is kept separate from, and prior to, the fence itself.
//
// It is safe for concurrent use: two adapters, or two sandboxes sharing one
// process, may record refusals through their own Fence at the same time.
type Fence struct {
	now func() time.Time

	mu       sync.Mutex
	refusals []Refusal
}

// NewFence returns a fence that timestamps refusals with now. A nil now means
// [time.Now] in UTC.
func NewFence(now func() time.Time) *Fence {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Fence{now: now}
}

// Refuse records one attempt and returns the typed error naming it. Every
// call to Refuse is one more entry [Fence.Refusals] returns; nothing this
// type does ever forgets a refusal or merges two into one.
func (f *Fence) Refuse(adapter string, attempt EffectAttempt, destination string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusals = append(f.refusals, Refusal{Adapter: adapter, Attempt: attempt, Destination: destination, At: f.now()})
	return &FencedEffectError{Adapter: adapter, Attempt: attempt, Destination: destination}
}

// Refusals returns every attempt this fence has turned away, oldest first.
func (f *Fence) Refusals() []Refusal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Refusal(nil), f.refusals...)
}

// Count returns len(f.Refusals()) without allocating a copy.
func (f *Fence) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refusals)
}

// FencedConnector is a [connectivity.Connector] that only ever forwards to
// inner when inner's own [connectivity.Descriptor.SourceRef] is on an
// explicit, fixed allow-list of synthetic fixture sources. Every other
// source - in particular anything that looks like a real external system,
// production or otherwise - is refused through fence before inner is ever
// called.
//
// The allow-list is supplied once, at construction, by this package's own
// [New]: it is never a field a caller's Options or ServeConfig can widen,
// which is what makes fence enforcement independent of application
// configuration (SANDBOX-001's REFACTOR clause). Descriptor, Bounds and
// Capabilities are pure metadata about the connector itself - the same
// values [connectivity.Connector]'s own doc comment calls "plain values that
// state, send, apply or commit nothing" - so they are always forwarded
// without a check; only the three methods that actually reach the external
// system (SchemaVersion, Snapshot, Read) are gated.
type FencedConnector struct {
	inner   connectivity.Connector
	fence   *Fence
	allowed map[string]bool
}

var _ connectivity.Connector = (*FencedConnector)(nil)

// NewFencedConnector wraps inner behind fence, admitting only the named
// allowedSourceRefs.
func NewFencedConnector(inner connectivity.Connector, fence *Fence, allowedSourceRefs ...string) *FencedConnector {
	allowed := make(map[string]bool, len(allowedSourceRefs))
	for _, ref := range allowedSourceRefs {
		allowed[ref] = true
	}
	return &FencedConnector{inner: inner, fence: fence, allowed: allowed}
}

// Descriptor forwards unconditionally: it identifies the connector, it does
// not reach it.
func (c *FencedConnector) Descriptor() connectivity.Descriptor { return c.inner.Descriptor() }

// Bounds forwards unconditionally, for the same reason as Descriptor.
func (c *FencedConnector) Bounds() connectivity.Bounds { return c.inner.Bounds() }

// Capabilities forwards unconditionally, for the same reason as Descriptor.
func (c *FencedConnector) Capabilities() []connectivity.Capability { return c.inner.Capabilities() }

// SchemaVersion refuses unless inner's SourceRef is allow-listed.
func (c *FencedConnector) SchemaVersion(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	if err := c.admit(); err != nil {
		return "", err
	}
	return c.inner.SchemaVersion(ctx, object)
}

// Snapshot refuses unless inner's SourceRef is allow-listed.
func (c *FencedConnector) Snapshot(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	if err := c.admit(); err != nil {
		return "", err
	}
	return c.inner.Snapshot(ctx, object)
}

// Read refuses unless inner's SourceRef is allow-listed.
func (c *FencedConnector) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	if err := c.admit(); err != nil {
		return connectivity.Page{}, err
	}
	return c.inner.Read(ctx, req)
}

// admit answers the one question every gated method asks first.
func (c *FencedConnector) admit() error {
	ref := c.inner.Descriptor().SourceRef
	if c.allowed[ref] {
		return nil
	}
	return c.fence.Refuse("connectivity.Connector", AttemptConnectorReach, ref)
}
