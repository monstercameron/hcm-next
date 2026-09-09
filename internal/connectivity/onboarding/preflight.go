package onboarding

import (
	"context"
	"crypto/ed25519"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// PreflightResult is the outcome of one [Preflight.Run] call.
//
// Passed is false whenever Reason is non-nil. A caller that wants the
// rejection as an error reads the second return value of Run; Reason carries
// the identical value for a caller that already has a Result in hand and
// wants to branch on it without a type assertion.
type PreflightResult struct {
	Manifest  OnboardingManifest
	Passed    bool
	Reason    *Error
	CheckedAt time.Time
}

// Preflight is the ONBOARD-001 gate: it checks a signed manifest against a
// live connection and connector before any extraction may begin.
//
// Every check is a refusal, not a best-effort warning. A manifest that does
// not verify, names a connector, connection, tenant or authority other than
// the ones supplied, claims a capability or schema version the connector does
// not actually publish right now, or carries an invalid budget is rejected
// before a single byte is read - and, structurally, before this package could
// have persisted anything: it has no import path to an authoritative table,
// a business event, an outbox entry, human work or a provider request.
type Preflight struct {
	// Connector is the live external surface the manifest will be checked
	// against.
	Connector connectivity.Connector
	// Connection is the tenant connection the manifest names.
	Connection *connectivity.ConnectorConnection
	// PublicKey verifies the manifest's signature.
	PublicKey ed25519.PublicKey
	// Now supplies the check timestamp. Nil uses the wall clock.
	Now func() time.Time
}

func (p Preflight) now() time.Time {
	if p.Now == nil {
		return time.Now().UTC()
	}
	return p.Now().UTC()
}

// Run checks sm against p's live connection and connector.
//
// It returns as soon as it finds the first violation, matching
// [connectivity.ConnectorDefinition.Validate]'s own rationale: a caller
// fixing a rejected manifest fixes one field at a time, and a list of twenty
// complaints is not more useful than the first one. The returned error is
// always either nil or an *[Error] with Code [CodeManifestRejected]; the
// identical value is also PreflightResult.Reason.
func (p Preflight) Run(ctx context.Context, sm SignedManifest) (PreflightResult, error) {
	const op = "onboarding.Preflight.Run"
	checkedAt := p.now()

	reject := func(field, state, version, format string, args ...any) (PreflightResult, error) {
		err := rejected(op, field, state, version, format, args...)
		return PreflightResult{Manifest: sm.Manifest, Passed: false, Reason: err, CheckedAt: checkedAt}, err
	}

	if err := ctx.Err(); err != nil {
		return reject("context", "", "", "context: %v", err)
	}
	if p.Connector == nil || p.Connection == nil {
		return reject("preflight", "", "", "preflight is not wired to a live connector and connection")
	}
	if len(p.PublicKey) != ed25519.PublicKeySize {
		return reject("signer_key", "", "", "preflight has no valid verification key")
	}

	status, verr := VerifyManifest(sm, p.PublicKey)
	if status != VerifyValid {
		return reject("signature", string(status), "", "manifest signature does not verify: %v", verr)
	}

	m := sm.Manifest
	descriptor := p.Connector.Descriptor()

	if m.ConnectorID != descriptor.ConnectorID {
		return reject("connector_id", descriptor.ConnectorID, "",
			"manifest pins connector %q but the live connector is %q", m.ConnectorID, descriptor.ConnectorID)
	}
	if m.ConnectorVersion != p.Connection.ConnectorVersion() {
		return reject("connector_version", "", p.Connection.ConnectorVersion().String(),
			"manifest pins connector version %s but the connection is bound to %s",
			m.ConnectorVersion, p.Connection.ConnectorVersion())
	}
	if m.ConnectionID != p.Connection.ID() {
		return reject("connection_id", p.Connection.ID(), "",
			"manifest names connection %q but preflight was given %q", m.ConnectionID, p.Connection.ID())
	}
	if m.TenantID != p.Connection.TenantID() {
		return reject("tenant_id", p.Connection.TenantID(), "",
			"manifest names tenant %q but the connection belongs to %q", m.TenantID, p.Connection.TenantID())
	}
	if m.SourceAuthorityRef != descriptor.AuthorityRef {
		return reject("source_authority_ref", descriptor.AuthorityRef, "",
			"manifest names source authority %q but the connector observes under %q; the authority is unresolved",
			m.SourceAuthorityRef, descriptor.AuthorityRef)
	}

	state := p.Connection.State()
	if !state.Usable() {
		return reject("connection_state", string(state), "",
			"connection %s is %s; only ACTIVE or DEGRADED connections may onboard", p.Connection.ID(), state)
	}

	for _, obj := range m.Objects {
		want := connectivity.Capability{Object: obj, Operation: connectivity.OperationRead}
		if !p.Connection.Supports(want) {
			return reject("capability", string(state), "",
				"connection %s does not claim capability %s required by the manifest", p.Connection.ID(), want)
		}
	}

	for _, obj := range m.Objects {
		live, err := p.Connector.SchemaVersion(ctx, obj)
		if err != nil {
			return reject("schema_version", "", "", "reading schema version for %s: %v", obj, err)
		}
		if pinned := m.SchemaVersionPins[obj]; pinned != live {
			return reject("schema_version", live, pinned,
				"object %s schema version is %q, manifest pins %q", obj, live, pinned)
		}
	}

	if err := m.Budget.Validate(); err != nil {
		return reject("budget", "", "", "manifest budget is invalid: %v", err)
	}

	return PreflightResult{Manifest: m, Passed: true, CheckedAt: checkedAt}, nil
}
