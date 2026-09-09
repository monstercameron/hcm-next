package onboarding

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// ObjectResult reports one object's extraction outcome.
type ObjectResult struct {
	Object connectivity.ObjectKind
	Run    observe.RunResult
	// SnapshotChanged is set when the source-consistency check refused to
	// resume because the connector now serves a different snapshot than the
	// one a stored checkpoint addresses. When set, Run.Cause wraps
	// [ErrSnapshotChanged] and no read was attempted for this object.
	SnapshotChanged bool
}

// ExtractRequest scopes one manifest-driven extraction attempt.
type ExtractRequest struct {
	// RunID identifies this attempt in checkpoints and evidence.
	RunID string
	// Mode selects full, incremental or delta traversal for every object.
	Mode connectivity.ReadMode
	// Since bounds an incremental traversal.
	Since time.Time
	// Restart abandons every object's stored checkpoint and traverses a
	// fresh snapshot. See [observe.RunRequest.Restart]: it is an explicit
	// act, never inferred.
	Restart bool
	// MaxRunsPerObject bounds how many resumed runs one object may take to
	// reach completion. Zero uses a generous internal default.
	MaxRunsPerObject int
	// PageSize requests a page size for every object. Zero uses the
	// connection's MaxPageSize.
	PageSize int
	// MaxPagesPerRun caps how many pages one attempt may consume per object
	// before stopping bounded, leaving a resumable checkpoint. Zero uses the
	// connection's own bound.
	MaxPagesPerRun int
}

// Extractor is a manifest-scoped, resumable snapshot extraction built on top
// of [observe.Runner]'s checkpointed page loop.
//
// It adds exactly one thing the runner does not already give for free: a
// proactive source-consistency check before any page is requested on resume,
// so a source whose snapshot changed underneath a stored checkpoint is
// refused before a wasted read, rather than discovered by one. Everything
// else - durable-before-checkpointed ordering, idempotent re-append on
// restart, schema-drift detection mid-traversal - is the runner's own
// contract; this type does not re-implement it.
type Extractor struct {
	// Connector is the read-only external surface.
	Connector connectivity.Connector
	// Connection supplies tenancy, capability and bounds, and must be usable.
	Connection *connectivity.ConnectorConnection
	// Observations persists evidence.
	Observations observe.ObservationStore
	// Checkpoints persists resume points.
	Checkpoints observe.CheckpointStore
	// FreshnessBudget is how old a watermark may be before a page is stale.
	FreshnessBudget time.Duration
	// Now supplies checkpoint timestamps. Nil uses the wall clock.
	Now func() time.Time
}

func (e Extractor) runner(connector connectivity.Connector) *observe.Runner {
	return &observe.Runner{
		Connector:       connector,
		Connection:      e.Connection,
		Observations:    e.Observations,
		Checkpoints:     e.Checkpoints,
		FreshnessBudget: e.FreshnessBudget,
		Now:             e.Now,
	}
}

const defaultMaxRunsPerObject = 64

// Extract walks every object m declares, applying its field allow-list, and
// returns one result per object in manifest order.
//
// A source-consistency failure on one object stops that object's traversal
// without touching any other object's checkpoint or aborting the batch: the
// caller sees SnapshotChanged set on that object's result and may still act
// on every other object's outcome.
func (e Extractor) Extract(ctx context.Context, m OnboardingManifest, req ExtractRequest) ([]ObjectResult, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	maxRuns := req.MaxRunsPerObject
	if maxRuns <= 0 {
		maxRuns = defaultMaxRunsPerObject
	}

	results := make([]ObjectResult, 0, len(m.Objects))
	for _, obj := range m.Objects {
		result, err := e.extractObject(ctx, m, obj, req, maxRuns)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// checkSourceConsistency proactively compares a stored, incomplete
// checkpoint's snapshot against the connector's current snapshot for object,
// before any Read is attempted. It is a pure read-side check: it never writes
// a checkpoint and never advances a fence, so a caller free to retry it as
// often as it likes.
func (e Extractor) checkSourceConsistency(
	ctx context.Context, tenantID string, object connectivity.ObjectKind, restart bool,
) (stored observe.Checkpoint, found bool, changed bool, err error) {
	if restart {
		return observe.Checkpoint{}, false, false, nil
	}
	key := observe.CheckpointKey{TenantID: tenantID, ConnectionID: e.Connection.ID(), Object: object}
	stored, found, err = e.Checkpoints.Load(ctx, key)
	if err != nil || !found || stored.Complete {
		return stored, found, false, err
	}
	live, err := e.Connector.Snapshot(ctx, object)
	if err != nil {
		return stored, found, false, err
	}
	return stored, found, live != stored.SnapshotID, nil
}

func (e Extractor) extractObject(
	ctx context.Context, m OnboardingManifest, obj connectivity.ObjectKind, req ExtractRequest, maxRuns int,
) (ObjectResult, error) {
	const op = "onboarding.Extractor.Extract"
	result := ObjectResult{Object: obj}

	stored, _, changed, err := e.checkSourceConsistency(ctx, m.TenantID, obj, req.Restart)
	if err != nil {
		return result, err
	}
	if changed {
		result.SnapshotChanged = true
		result.Run = observe.RunResult{
			RunID: req.RunID, Object: obj, Status: observe.RunInterrupted,
			SnapshotID: stored.SnapshotID, Checkpoint: stored,
			Cause: newError(op, ErrSnapshotChanged,
				"object %s: checkpoint addresses snapshot %q, source now serves a different one",
				obj, stored.SnapshotID),
		}
		return result, nil
	}

	connector := e.Connector
	if allowed := m.FieldAllowList[obj]; len(allowed) > 0 {
		connector = FilterFields(connector, map[connectivity.ObjectKind][]string{obj: allowed})
	}

	run, err := observe.RunToCompletion(ctx, e.runner(connector), observe.RunRequest{
		RunID:    req.RunID,
		TenantID: m.TenantID,
		Object:   obj,
		Mode:     req.Mode,
		Since:    req.Since,
		Restart:  req.Restart,
		PageSize: req.PageSize,
		MaxPages: req.MaxPagesPerRun,
	}, maxRuns)
	if err != nil {
		return result, err
	}
	result.Run = run
	return result, nil
}
