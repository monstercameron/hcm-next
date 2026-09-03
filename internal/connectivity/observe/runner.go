package observe

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity"
)

// RunStatus is how an observation run ended.
type RunStatus string

// The run outcomes. There is no "success" that hides an incomplete traversal:
// a bounded run that stopped early says so, and the caller resumes.
const (
	// RunCompleted means the traversal reached the end of its snapshot.
	RunCompleted RunStatus = "COMPLETED"
	// RunBounded means the run stopped at its own page or record limit with
	// more of the snapshot left. Its checkpoint resumes exactly where it left.
	RunBounded RunStatus = "BOUNDED"
	// RunInterrupted means the external system failed mid-traversal. The
	// checkpoint still names the last page that was durably observed.
	RunInterrupted RunStatus = "INTERRUPTED"
	// RunAlreadyComplete means the checkpoint already covered the snapshot, so
	// the run read nothing. It is the idempotent re-run outcome.
	RunAlreadyComplete RunStatus = "ALREADY_COMPLETE"
)

// RunRequest is one bounded observation run over one object.
type RunRequest struct {
	// RunID identifies this run in the checkpoint and in evidence.
	RunID string
	// TenantID scopes everything the run stores.
	TenantID string
	// Object selects the record family.
	Object connectivity.ObjectKind
	// Mode selects full, incremental or delta traversal.
	Mode connectivity.ReadMode
	// Since bounds an incremental traversal.
	Since time.Time
	// PageSize is the requested page size. Zero uses the connection's
	// MaxPageSize.
	PageSize int
	// MaxPages caps this run's pages. Zero uses the connection's
	// MaxPagesPerRun. It is clamped down to the connection's bound, never up.
	MaxPages int
	// Restart abandons any stored checkpoint and traverses a fresh snapshot.
	// It is an explicit act: an accidental restart would re-observe everything
	// under a new snapshot, which is expensive rather than wrong.
	Restart bool
	// RawArtifactRef, when set, is recorded on each observation as the pointer
	// to separately retained raw provider bytes.
	RawArtifactRef *string
}

// RunResult reports what one run observed.
type RunResult struct {
	RunID      string
	Status     RunStatus
	Object     connectivity.ObjectKind
	SnapshotID string
	// Pages and Records are what this run read, not the traversal total.
	Pages   int
	Records int
	// Appended and Duplicate split the pages by whether the store already held
	// identical evidence. A resumed run that re-reads a page it had persisted
	// but not checkpointed reports it as a duplicate, not a second page.
	Appended  int
	Duplicate int
	// ObservationIDs are this run's observations in page order.
	ObservationIDs []uuid.UUID
	// Checkpoint is the resume point after the run.
	Checkpoint Checkpoint
	// Freshness is the least reliable verdict any page produced, so that one
	// stale page cannot be averaged away.
	Freshness Freshness
	// Cause is the classified failure that interrupted the run, if any.
	Cause error
}

// Runner walks an external object in bounded pages, persisting one immutable
// observation per page and committing a fenced checkpoint after each.
//
// The order matters and is the whole design: the observation is durable before
// the checkpoint that would skip past it. A crash between the two re-reads one
// page and appends it idempotently; a crash the other way round would lose it.
type Runner struct {
	// Connector is the read-only external surface.
	Connector connectivity.Connector
	// Connection supplies tenancy, capability and bounds, and must be usable.
	Connection *connectivity.ConnectorConnection
	// Observations persists evidence.
	Observations ObservationStore
	// Checkpoints persists resume points.
	Checkpoints CheckpointStore
	// FreshnessBudget is how old a watermark may be before a page is stale.
	// Zero makes every page's freshness UNKNOWN, which is the honest default
	// for a connector that declares no budget.
	FreshnessBudget time.Duration
	// Now supplies checkpoint timestamps.
	Now func() time.Time
}

// Validate reports whether the runner is wired.
func (r *Runner) Validate() error {
	const op = "observe.Runner.Validate"
	switch {
	case r.Connector == nil:
		return newError(op, ErrIncomplete, "runner has no connector")
	case r.Connection == nil:
		return newError(op, ErrIncomplete, "runner has no connection")
	case r.Observations == nil:
		return newError(op, ErrIncomplete, "runner has no observation store")
	case r.Checkpoints == nil:
		return newError(op, ErrIncomplete, "runner has no checkpoint store")
	}
	return nil
}

func (r *Runner) now() time.Time {
	if r.Now == nil {
		return time.Now().UTC()
	}
	return r.Now().UTC()
}

// Run performs one bounded observation run.
//
// It returns a result even when the external system failed: an interrupted run
// still observed whatever it observed, and discarding that on the way out
// would make a partial outage look like a total one.
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	const op = "observe.Runner.Run"
	if err := r.Validate(); err != nil {
		return RunResult{}, err
	}
	switch {
	case strings.TrimSpace(req.RunID) == "":
		return RunResult{}, newError(op, ErrIncomplete, "run has no id")
	case strings.TrimSpace(req.TenantID) == "":
		return RunResult{}, newError(op, ErrIncomplete, "run has no tenant")
	case !req.Object.Valid():
		return RunResult{}, newError(op, ErrIncomplete, "run has no object kind")
	}
	mode := req.Mode
	if mode == "" {
		mode = connectivity.ReadFull
	}
	if !mode.Valid() {
		return RunResult{}, newError(op, ErrIncomplete, "run has unknown read mode %q", string(mode))
	}
	if err := r.Connection.RequireUsable(); err != nil {
		return RunResult{}, err
	}
	want := connectivity.Capability{Object: req.Object, Operation: connectivity.OperationRead}
	if !r.Connection.Supports(want) {
		return RunResult{}, connectivity.Fail(op, connectivity.ErrUnsupported,
			"connection %s does not claim capability %s", r.Connection.ID(), want)
	}

	bounds := r.Connection.Bounds()
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = bounds.MaxPageSize
	}
	if pageSize > bounds.MaxPageSize {
		return RunResult{}, connectivity.Fail(op, connectivity.ErrBounds,
			"run requests page size %d, connection bound is %d", pageSize, bounds.MaxPageSize)
	}
	maxPages := req.MaxPages
	if maxPages <= 0 || maxPages > bounds.MaxPagesPerRun {
		maxPages = bounds.MaxPagesPerRun
	}

	key := CheckpointKey{
		TenantID:     req.TenantID,
		ConnectionID: r.Connection.ID(),
		Object:       req.Object,
	}
	stored, found, err := r.Checkpoints.Load(ctx, key)
	if err != nil {
		return RunResult{}, err
	}

	result := RunResult{RunID: req.RunID, Object: req.Object, Freshness: FreshnessFresh}

	var (
		snapshot string
		cursor   connectivity.Cursor
		fence    = stored.Fence
		pages    = stored.PagesCommitted
		records  = stored.RecordsCommitted
	)
	switch {
	case found && !req.Restart && stored.Complete:
		result.Status = RunAlreadyComplete
		result.SnapshotID = stored.SnapshotID
		result.Checkpoint = stored
		return result, nil
	case found && !req.Restart:
		snapshot = stored.SnapshotID
		cursor, err = stored.Cursor()
		if err != nil {
			return RunResult{}, err
		}
	default:
		snapshot, err = r.Connector.Snapshot(ctx, req.Object)
		if err != nil {
			return RunResult{}, err
		}
		cursor = connectivity.StartCursor(snapshot)
		fence, pages, records = stored.Fence, 0, 0
	}
	result.SnapshotID = snapshot

	firstSchema := ""
	for readPages := 0; ; readPages++ {
		if readPages >= maxPages {
			result.Status = RunBounded
			break
		}
		if int(records)+pageSize > bounds.MaxRecordsPerRun && int(records) >= bounds.MaxRecordsPerRun {
			result.Status = RunBounded
			break
		}

		page, readErr := r.Connector.Read(ctx, connectivity.ReadRequest{
			Object: req.Object,
			Mode:   mode,
			Cursor: cursor,
			Limit:  pageSize,
			Since:  req.Since,
		})
		if readErr != nil {
			result.Status = RunInterrupted
			result.Cause = readErr
			if class, ok := connectivity.ClassOf(readErr); ok && class == connectivity.ClassSchema {
				result.Freshness = FreshnessUnavailable
			}
			break
		}
		if firstSchema == "" {
			firstSchema = page.SchemaVersion
		}
		if page.SchemaVersion != firstSchema {
			result.Status = RunInterrupted
			result.Freshness = FreshnessUnavailable
			result.Cause = connectivity.Fail(op, connectivity.ErrSchema,
				"schema drifted mid-traversal: page 1 read %q, page %d read %q",
				firstSchema, pages+1, page.SchemaVersion)
			break
		}

		startCursor := cursor
		pages++
		obs, recErr := Record(page, RecordOptions{
			TenantID:        req.TenantID,
			Descriptor:      r.Connector.Descriptor(),
			PageSequence:    pages,
			StartCursor:     startCursor,
			FreshnessBudget: r.FreshnessBudget,
			RawArtifactRef:  req.RawArtifactRef,
		})
		if recErr != nil {
			return result, recErr
		}
		appended, appendErr := r.Observations.Append(ctx, obs)
		if appendErr != nil {
			return result, appendErr
		}

		records += uint64(len(page.Records))
		fence++
		checkpoint := Checkpoint{
			Key:              key,
			RunID:            req.RunID,
			SnapshotID:       snapshot,
			CursorToken:      page.NextCursor.MustToken(),
			Fence:            fence,
			PagesCommitted:   pages,
			RecordsCommitted: records,
			Complete:         page.Complete,
			UpdatedAt:        r.now(),
		}
		if commitErr := r.Checkpoints.Commit(ctx, checkpoint); commitErr != nil {
			return result, commitErr
		}

		result.Pages++
		result.Records += len(page.Records)
		result.ObservationIDs = append(result.ObservationIDs, obs.ObservationID)
		result.Checkpoint = checkpoint
		if appended.Existing {
			result.Duplicate++
		} else {
			result.Appended++
		}
		result.Freshness = leastReliable(result.Freshness, obs.Freshness)

		cursor = page.NextCursor
		if page.Complete {
			result.Status = RunCompleted
			break
		}
	}

	if result.Checkpoint.Fence == 0 {
		// Nothing was committed this run; report the checkpoint as it stands so
		// the caller can resume from the last durable position.
		result.Checkpoint = stored
	}
	if result.Status == "" {
		result.Status = RunBounded
	}
	if result.Pages == 0 && result.Status != RunInterrupted {
		result.Freshness = FreshnessUnknown
	}
	return result, nil
}

// freshnessRank orders the verdicts from most to least reliable, so that
// combining page verdicts can never round a bad one up.
var freshnessRank = map[Freshness]int{
	FreshnessFresh:       0,
	FreshnessStale:       1,
	FreshnessUnknown:     2,
	FreshnessPartial:     3,
	FreshnessUnavailable: 4,
}

func leastReliable(a, b Freshness) Freshness {
	if freshnessRank[b] > freshnessRank[a] {
		return b
	}
	return a
}

// RunToCompletion repeatedly runs until the traversal completes, the bound is
// reached with no progress, or the external system fails.
//
// It exists because "resume until done" is the operation a caller actually
// wants, and writing that loop by hand is where duplicate or dropped pages get
// introduced. maxRuns bounds the loop so a connector that never reports
// completion cannot spin forever.
func RunToCompletion(ctx context.Context, runner *Runner, req RunRequest, maxRuns int) (RunResult, error) {
	const op = "observe.RunToCompletion"
	if maxRuns <= 0 {
		return RunResult{}, newError(op, ErrIncomplete, "maxRuns must be positive")
	}
	var total RunResult
	total.RunID = req.RunID
	total.Object = req.Object
	total.Freshness = FreshnessFresh

	for attempt := 0; attempt < maxRuns; attempt++ {
		next := req
		next.Restart = req.Restart && attempt == 0
		result, err := runner.Run(ctx, next)
		if err != nil {
			return total, err
		}
		total.Pages += result.Pages
		total.Records += result.Records
		total.Appended += result.Appended
		total.Duplicate += result.Duplicate
		total.ObservationIDs = append(total.ObservationIDs, result.ObservationIDs...)
		total.SnapshotID = result.SnapshotID
		total.Checkpoint = result.Checkpoint
		total.Status = result.Status
		total.Cause = result.Cause
		if result.Pages > 0 {
			total.Freshness = leastReliable(total.Freshness, result.Freshness)
		}

		switch result.Status {
		case RunCompleted, RunAlreadyComplete, RunInterrupted:
			return total, nil
		}
		if result.Pages == 0 {
			return total, nil
		}
	}
	return total, nil
}

// IsExternalFailure reports whether err came from the external system rather
// than from this process. A caller retries the former and fixes the latter.
func IsExternalFailure(err error) bool {
	if err == nil {
		return false
	}
	var typed *connectivity.Error
	return errors.As(err, &typed)
}
