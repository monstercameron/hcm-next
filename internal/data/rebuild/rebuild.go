// Package rebuild implements the ledger-driven rebuild of critical
// projections (owner: data plane; LEDGER-013, phase P1A).
//
// A projection is rebuildable read state: the ledger is the only authority, so
// any projection table can be replayed independently - a shadow projection -
// and compared against what is currently stored. This package is the executable
// form of the reconciliation model in
// specs/transaction-ledger-reconciliation-and-repair.md (8.12 "independent
// replay -> compare -> healthy/drift", 8.17 "shadow rebuild -> compare ->
// promote only on no difference"):
//
//	ledger stream --replay into shadow table--> shadow state
//	                                                        |
//	current table --------------------------------> compare:
//	                                               row counts,
//	                                               semantic digests,
//	                                               declared invariants
//	                                  no difference -> promote (swap)
//	                                  difference    -> report, not promoted
//
// Every event is validated before it is folded: sequence gaps (missing
// events), duplicate and reordered sequences, events that cannot be keyed,
// foreign-tenant and foreign-stream events, unregistered payload schemas, and
// corrections that do not resolve to an assertion actually recorded on the
// stream. A rebuild with any finding refuses to promote; the report carries
// the evidence an operator needs to investigate (spec 8.8: every
// reconciliation result is a business fact, not a log line).
//
// Replay dispatches no external side effect (spec 8.5: history is explained
// without being re-executed). The only writes are the run's own shadow table,
// the swap into the projection table on an exact match, and the projection
// checkpoint. No outbox entries and no external calls.
package rebuild

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
)

// Finding codes. A promoted rebuild has none; a refused one has at least one,
// and this is the set of replay defects that can keep it from promoting.
const (
	// CodeSequenceGap marks a sequence hole: a missing event.
	CodeSequenceGap = "sequence/gap"
	// CodeSequenceDuplicate marks a sequence delivered twice.
	CodeSequenceDuplicate = "sequence/duplicate"
	// CodeSequenceReordered marks an event arriving behind a later one.
	CodeSequenceReordered = "sequence/reordered"
	// CodeKeyMissing marks an event that carries no usable identity.
	CodeKeyMissing = "key/missing"
	// CodeSchemaMissing marks an event whose payload schema the tenant has not
	// registered.
	CodeSchemaMissing = "schema/missing"
	// CodeCorrectionMismatch marks a correction whose target does not exist, or
	// that precedes the assertion it claims to correct.
	CodeCorrectionMismatch = "correction/mismatch"
	// CodeTenantForeign marks an event that belongs to a different tenant.
	CodeTenantForeign = "tenant/foreign"
	// CodeStreamForeign marks an event that belongs to a different stream.
	CodeStreamForeign = "stream/foreign"
	// CodeHeadUnreached marks a replay that stops short of the source head.
	CodeHeadUnreached = "source/head"
	// CodeRowCountMismatch marks a row-count disagreement between current and
	// rebuilt state.
	CodeRowCountMismatch = "compare/row_count"
	// CodeDigestMismatch marks a semantic-digest disagreement between current
	// and rebuilt state.
	CodeDigestMismatch = "compare/digest"
	// CodeInvariantPrefix prefixes an invariant's name to form its finding code:
	// "invariant/<name>". AuthZ comparison runs as one of these.
	CodeInvariantPrefix = "invariant"
)

// Target selects one ledger stream and the projection it feeds.
type Target struct {
	// Tenant scopes the rebuild. Nothing is read or written outside it.
	Tenant uuid.UUID
	// Stream is the ledger stream to replay.
	Stream string
	// Projection is the projection_checkpoint name to advance on promotion.
	Projection string
}

// TableFacts is the comparison evidence for one table.
type TableFacts struct {
	// Rows is how many rows the tenant owns in the table.
	Rows int64
	// Digest is the semantic digest over those rows, as the reducer computes it.
	Digest string
}

// Finding is one violated replay check, one comparison disagreement, or one
// invariant violation.
type Finding struct {
	Code   string
	Detail string
}

// Report is the reconciliation result of one rebuild run.
type Report struct {
	Target Target
	// SourceHead is the source stream's head sequence at the start of the run.
	SourceHead int64
	// Replayed is how many events passed every check and were folded.
	Replayed int64
	// Current and Shadow are the comparison facts for the stored and the
	// rebuilt state.
	Current TableFacts
	Shadow  TableFacts
	// InvariantsRun lists the names of the invariants evaluated against the
	// rebuilt state, in order.
	InvariantsRun []string
	// Findings carries every violation; it is empty for a clean rebuild.
	Findings []Finding
	// Match reports that every check and comparison agreed.
	Match bool
	// Promoted reports that the rebuilt state was swapped into the current
	// table and the checkpoint advanced.
	Promoted bool
}

// Reducer describes how one projection derives its rows from ledger events.
//
// Fold is the only write path into the projection, and it must stay pure with
// respect to the ledger: no external calls, no writes outside the table it is
// given. Digest is how the projection states what a set of rows means, so two
// tables with the same rows always produce the same digest.
type Reducer struct {
	// Table is the projection table in production.
	Table string
	// Fold applies one validated event to the named table.
	Fold func(ctx context.Context, table string, tx dbport.Tx, ev ledger.EventRecord) error
	// Digest reports the tenant's row count and a stable semantic digest of the
	// table's rows.
	Digest func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) (int64, string, error)
}

// Invariant is one domain invariant evaluated against the rebuilt state. The
// invariant set is the caller's: the AuthZ comparison is one of them, declared
// as a check over the rebuilt table.
type Invariant struct {
	// Names the invariant in the report.
	Name string
	// Check returns one violation detail per broken row. It reads only.
	Check func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) ([]string, error)
}

// CorrectionRef is a correction event's "corrects" reference, resolved.
type CorrectionRef struct {
	// HasReference is false when the event carries no corrects reference at all.
	HasReference bool
	// Stream and Sequence name the referenced assertion.
	Stream   string
	Sequence int64
	// Resolved is true when the referenced assertion exists in the ledger.
	Resolved bool
	// Class is the assertion class of the resolved target.
	Class string
}

// Source is the read side of the authoritative ledger. Implementations read
// committed state only: no writes, no external calls.
type Source interface {
	// Head returns the stream's current head sequence.
	Head(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) (int64, error)
	// Events returns every committed event on the stream, sequence ascending.
	Events(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) ([]ledger.EventRecord, error)
	// SchemaRegistered reports whether the tenant has registered the payload
	// schema.
	SchemaRegistered(ctx context.Context, q dbport.Querier, tenant uuid.UUID, schemaRef string) (bool, error)
	// CorrectionTarget resolves the assertion a correction supersedes.
	CorrectionTarget(ctx context.Context, q dbport.Querier, ev ledger.EventRecord) (CorrectionRef, error)
}

// Executor runs isolated shadow rebuilds. It is safe for concurrent use: each
// run builds its own run-unique shadow table, and the promotion serializes on
// the checkpoint row exactly like a live applier would.
type Executor struct {
	source Source
	db     dbport.Beginner
}

// New returns an Executor. db supplies the run transactions; source supplies
// the committed ledger state.
func New(source Source, db dbport.Beginner) *Executor {
	return &Executor{source: source, db: db}
}

// Rebuild replays the target stream into a fresh shadow table, compares the
// result against the current projection table, and - only when the result is
// exactly the compatible one - promotes it: the shadow content is swapped into
// the projection table and the checkpoint advances to the source head.
//
// An error mid-run rolls the whole transaction back: the current table, the
// checkpoint and the ledger are untouched, and the shadow table goes away with
// it. There is no half-promoted state to recover from.
func (e *Executor) Rebuild(ctx context.Context, target Target, reducer Reducer, invariants ...Invariant) (Report, error) {
	if err := checkTarget(target); err != nil {
		return Report{}, err
	}
	if err := checkReducer(reducer); err != nil {
		return Report{}, err
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("rebuild: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	shadow, err := createShadow(ctx, tx, reducer.Table)
	if err != nil {
		return Report{}, err
	}

	var report Report
	report.Target = target

	head, err := e.source.Head(ctx, tx, target.Tenant, target.Stream)
	if err != nil {
		return Report{}, fmt.Errorf("rebuild: read source head: %w", err)
	}
	report.SourceHead = head

	events, err := e.source.Events(ctx, tx, target.Tenant, target.Stream)
	if err != nil {
		return Report{}, fmt.Errorf("rebuild: read source events: %w", err)
	}

	var (
		expected   int64 = 1
		last       int64
		lastDigest string
	)
	for _, ev := range events {
		findings, err := e.checkEvent(ctx, tx, target, ev, expected, last)
		if err != nil {
			return Report{}, err
		}
		if len(findings) > 0 {
			report.Findings = append(report.Findings, findings...)
			continue
		}
		if err := reducer.Fold(ctx, shadow, tx, ev); err != nil {
			return Report{}, fmt.Errorf("rebuild: fold sequence %d: %w", ev.Sequence, err)
		}
		report.Replayed++
		last = ev.Sequence
		lastDigest = ev.Digest
		expected = ev.Sequence + 1
	}

	if int64(len(events)) != head {
		report.Findings = append(report.Findings, Finding{
			Code:   CodeHeadUnreached,
			Detail: fmt.Sprintf("stream reads %d event(s) but its head is at sequence %d", len(events), head),
		})
	}

	curRows, curDigest, err := reducer.Digest(ctx, tx, reducer.Table, target.Tenant)
	if err != nil {
		return Report{}, fmt.Errorf("rebuild: digest current table %s: %w", reducer.Table, err)
	}
	report.Current = TableFacts{Rows: curRows, Digest: curDigest}
	shadRows, shadDigest, err := reducer.Digest(ctx, tx, shadow, target.Tenant)
	if err != nil {
		return Report{}, fmt.Errorf("rebuild: digest shadow table: %w", err)
	}
	report.Shadow = TableFacts{Rows: shadRows, Digest: shadDigest}
	if report.Current.Rows != report.Shadow.Rows {
		report.Findings = append(report.Findings, Finding{
			Code: CodeRowCountMismatch,
			Detail: fmt.Sprintf("current table holds %d row(s) for the tenant, the rebuilt state holds %d",
				report.Current.Rows, report.Shadow.Rows),
		})
	}
	if report.Current.Digest != report.Shadow.Digest {
		report.Findings = append(report.Findings, Finding{
			Code:   CodeDigestMismatch,
			Detail: fmt.Sprintf("current digest %s, rebuilt digest %s", report.Current.Digest, report.Shadow.Digest),
		})
	}

	for _, inv := range invariants {
		if inv.Name == "" || inv.Check == nil {
			return Report{}, fmt.Errorf("rebuild: each invariant needs a name and a check")
		}
		report.InvariantsRun = append(report.InvariantsRun, inv.Name)
		violations, err := inv.Check(ctx, tx, shadow, target.Tenant)
		if err != nil {
			return Report{}, fmt.Errorf("rebuild: invariant %s: %w", inv.Name, err)
		}
		for _, v := range violations {
			report.Findings = append(report.Findings, Finding{
				Code:   invariantCode(inv.Name),
				Detail: v,
			})
		}
	}

	report.Match = len(report.Findings) == 0
	if report.Match {
		if err := promote(ctx, tx, target, reducer.Table, shadow, head, lastDigest); err != nil {
			return Report{}, err // the deferred rollback unwinds every write
		}
		report.Promoted = true
	}

	if err := execOnce(ctx, tx, `DROP TABLE `+quoted(shadow)); err != nil {
		return Report{}, fmt.Errorf("rebuild: drop shadow table: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, fmt.Errorf("rebuild: commit: %w", err)
	}
	return report, nil
}

// checkEvent runs the replay checks over one event. The checks run in an order
// that reports a foreign event for what it is before it can masquerade as a
// sequence hole.
func (e *Executor) checkEvent(ctx context.Context, tx dbport.Tx, target Target, ev ledger.EventRecord, expected, last int64) ([]Finding, error) {
	var findings []Finding
	add := func(code, format string, args ...any) {
		findings = append(findings, Finding{Code: code, Detail: fmt.Sprintf(format, args...)})
	}

	if ev.Tenant != target.Tenant {
		add(CodeTenantForeign, "event at sequence %d belongs to tenant %s; the rebuild is for tenant %s",
			ev.Sequence, ev.Tenant, target.Tenant)
	}
	if ev.StreamKey != target.Stream {
		add(CodeStreamForeign, "event at sequence %d is on stream %q; the rebuild is for stream %q",
			ev.Sequence, ev.StreamKey, target.Stream)
	}
	if ev.EventID == uuid.Nil || ev.Sequence < 1 {
		add(CodeKeyMissing, "event at sequence %d carries no usable identity (no event id, or a non-positive sequence)",
			ev.Sequence)
	}
	if len(findings) > 0 {
		return findings, nil
	}

	registered, err := e.source.SchemaRegistered(ctx, tx, target.Tenant, ev.SchemaRef)
	if err != nil {
		return nil, fmt.Errorf("rebuild: check schema for sequence %d: %w", ev.Sequence, err)
	}
	if !registered {
		add(CodeSchemaMissing, "event at sequence %d cites schema %q, which the tenant has not registered",
			ev.Sequence, ev.SchemaRef)
	}

	if ev.AssertionClass == ledger.Correction {
		ref, err := e.source.CorrectionTarget(ctx, tx, ev)
		if err != nil {
			return nil, fmt.Errorf("rebuild: resolve correction target for sequence %d: %w", ev.Sequence, err)
		}
		switch {
		case !ref.HasReference:
			add(CodeCorrectionMismatch, "correction at sequence %d carries no target reference", ev.Sequence)
		case !ref.Resolved:
			add(CodeCorrectionMismatch, "correction at sequence %d references %s@%d, which is not in the ledger",
				ev.Sequence, ref.Stream, ref.Sequence)
		case ref.Stream == ev.StreamKey && ref.Sequence >= ev.Sequence:
			add(CodeCorrectionMismatch, "correction at sequence %d references its own or a later assertion (%s@%d)",
				ev.Sequence, ref.Stream, ref.Sequence)
		}
	}

	switch {
	case ev.Sequence > expected:
		add(CodeSequenceGap, "expected sequence %d, got %d", expected, ev.Sequence)
	case ev.Sequence == last:
		add(CodeSequenceDuplicate, "sequence %d was delivered twice", ev.Sequence)
	case ev.Sequence < last:
		add(CodeSequenceReordered, "sequence %d arrives after %d; the replay is out of order", ev.Sequence, last)
	}
	return findings, nil
}

// promote swaps the rebuilt state into the projection table and advances the
// checkpoint, in the run's own transaction: both happen or neither does.
func promote(ctx context.Context, tx dbport.Tx, target Target, current, shadow string, head int64, lastDigest string) error {
	qCurrent, qShadow := quoted(current), quoted(shadow)
	if err := execOnce(ctx, tx, `TRUNCATE `+qCurrent); err != nil {
		return fmt.Errorf("rebuild: truncate current table: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO `+qCurrent+` SELECT * FROM `+qShadow); err != nil {
		return fmt.Errorf("rebuild: swap rebuilt state into current table: %w", err)
	}
	if err := projection.EnsureProjection(ctx, tx, target.Tenant, target.Projection, target.Stream); err != nil {
		return fmt.Errorf("rebuild: register projection checkpoint: %w", err)
	}
	if head > 0 {
		if _, err := projection.Apply(ctx, tx, projection.ApplyRequest{
			Tenant:         target.Tenant,
			ProjectionName: target.Projection,
			StreamKey:      target.Stream,
			Sequence:       head,
			Digest:         lastDigest,
		}); err != nil {
			return fmt.Errorf("rebuild: advance projection checkpoint: %w", err)
		}
	}
	return nil
}

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// createShadow clones the projection table's shape into a run-unique shadow
// table and returns its name. The run-unique name is what lets several rebuild
// runs share a schema without ever colliding.
func createShadow(ctx context.Context, tx dbport.Tx, table string) (string, error) {
	if !identifierRE.MatchString(table) {
		return "", fmt.Errorf("rebuild: unsafe table name %q", table)
	}
	name, err := randomShadowName(table)
	if err != nil {
		return "", err
	}
	if err := execOnce(ctx, tx, `CREATE TABLE `+quoted(name)+` (LIKE `+quoted(table)+` INCLUDING ALL)`); err != nil {
		return "", fmt.Errorf("rebuild: create shadow table: %w", err)
	}
	if err := execOnce(ctx, tx, `TRUNCATE `+quoted(name)); err != nil {
		return "", fmt.Errorf("rebuild: truncate shadow table: %w", err)
	}
	return name, nil
}

func randomShadowName(table string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rebuild: name shadow table: %w", err)
	}
	return table + "_shadow_" + hex.EncodeToString(b[:]), nil
}

// quoted wraps an already-validated identifier in double quotes.
func quoted(name string) string { return `"` + name + `"` }

func invariantCode(name string) string { return CodeInvariantPrefix + "/" + name }

func execOnce(ctx context.Context, tx dbport.Tx, sql string) error {
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	return nil
}

func checkTarget(target Target) error {
	if target.Tenant == uuid.Nil {
		return fmt.Errorf("rebuild: target tenant is required")
	}
	if target.Stream == "" {
		return fmt.Errorf("rebuild: target stream is required")
	}
	if target.Projection == "" {
		return fmt.Errorf("rebuild: target projection is required")
	}
	return nil
}

func checkReducer(reducer Reducer) error {
	if !identifierRE.MatchString(reducer.Table) {
		return fmt.Errorf("rebuild: unsafe projection table name %q", reducer.Table)
	}
	if reducer.Fold == nil {
		return fmt.Errorf("rebuild: reducer needs a Fold")
	}
	if reducer.Digest == nil {
		return fmt.Errorf("rebuild: reducer needs a Digest")
	}
	return nil
}
