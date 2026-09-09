// Package schema owns the schema release and migration journal of the data plane
// (owner: data plane; phase: P1A).
//
// DB-006 requires that no migration is applied without a recorded artifact
// digest, tool version, compatibility class, checksum, owner, start and end
// status, and trusted-time evidence. This package is the only writer of those
// two tables. It is used by the migration command and by the tests that prove
// duplicate application is idempotent and that a checksum mismatch blocks
// startup.
package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// Journal statuses, mirroring the CHECK constraint in 00001_platform_control.sql.
const (
	StatusPlanned       = "PLANNED"
	StatusRunning       = "RUNNING"
	StatusApplied       = "APPLIED"
	StatusFailed        = "FAILED"
	StatusRolledForward = "ROLLED_FORWARD"
)

// Migration directions.
const (
	DirectionUp   = "UP"
	DirectionDown = "DOWN"
)

// Compatibility classes a release may declare.
const (
	CompatibilityBackward = "BACKWARD_COMPATIBLE"
	CompatibilityForward  = "FORWARD_COMPATIBLE"
	CompatibilityFull     = "FULL"
	CompatibilityBreaking = "BREAKING"
)

// Release is the identity of one schema artifact. Two releases with the same
// version must have the same artifact digest, or the platform refuses to run.
type Release struct {
	ID                 uuid.UUID
	Version            string
	ArtifactDigest     string
	SourceDigest       string
	ToolVersion        string
	CompatibilityClass string
	Owner              string
	Reversible         bool
	TrustedTimeSource  string
}

// ErrReleaseDigestMismatch is returned when a release version is already
// recorded with a different artifact digest. The recorded release is
// authoritative; the running binary is the one that is wrong.
type ErrReleaseDigestMismatch struct {
	Version  string
	Recorded string
	Actual   string
}

func (e ErrReleaseDigestMismatch) Error() string {
	return fmt.Sprintf("schema release %s is recorded with artifact digest %s but this build carries %s",
		e.Version, e.Recorded, e.Actual)
}

// ErrChecksumMismatch is returned when an already applied migration no longer
// matches the bytes embedded in this build. It blocks startup.
type ErrChecksumMismatch struct {
	Version  int64
	Name     string
	Recorded string
	Actual   string
}

func (e ErrChecksumMismatch) Error() string {
	return fmt.Sprintf("migration %d (%s) was applied with checksum %s but this build carries %s",
		e.Version, e.Name, e.Recorded, e.Actual)
}

// Entry identifies one journal row that is in flight.
type Entry struct {
	JournalID uuid.UUID
	ReleaseID uuid.UUID
	Version   int64
	Name      string
	Direction string
	Checksum  string
	StartedAt time.Time
}

// Journal writes the schema release and migration journal tables.
type Journal struct {
	db                *sql.DB
	toolVersion       string
	appliedBy         string
	trustedTimeSource string
	now               func() time.Time
}

// Option adjusts a Journal.
type Option func(*Journal)

// WithClock replaces the time source. Tests use it to make evidence explicit.
func WithClock(now func() time.Time) Option {
	return func(j *Journal) { j.now = now }
}

// WithTrustedTimeSource names the evidence for the recorded timestamps.
func WithTrustedTimeSource(source string) Option {
	return func(j *Journal) { j.trustedTimeSource = source }
}

// NewJournal builds a journal writer bound to one database handle.
func NewJournal(db *sql.DB, toolVersion, appliedBy string, opts ...Option) *Journal {
	j := &Journal{
		db:                db,
		toolVersion:       toolVersion,
		appliedBy:         appliedBy,
		trustedTimeSource: "HOST_CLOCK",
		now:               func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// EnsureRelease records the release if it is new and verifies it otherwise.
// Recording the same release twice is idempotent.
func (j *Journal) EnsureRelease(ctx context.Context, release Release) error {
	if release.ArtifactDigest == "" {
		return errors.New("schema release requires an artifact digest")
	}
	if release.ToolVersion == "" {
		return errors.New("schema release requires a tool version")
	}

	var recordedDigest string
	err := j.db.QueryRowContext(ctx,
		`SELECT artifact_digest FROM schema_release WHERE release_version = $1`,
		release.Version).Scan(&recordedDigest)
	switch {
	case err == nil:
		if recordedDigest != release.ArtifactDigest {
			return ErrReleaseDigestMismatch{
				Version:  release.Version,
				Recorded: recordedDigest,
				Actual:   release.ArtifactDigest,
			}
		}
		return nil
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("read schema release %s: %w", release.Version, err)
	}

	id := release.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	trusted := release.TrustedTimeSource
	if trusted == "" {
		trusted = j.trustedTimeSource
	}
	_, err = j.db.ExecContext(ctx, `
		INSERT INTO schema_release (
			release_id, release_version, artifact_digest, digest_algorithm, source_digest,
			tool_version, compatibility_class, owner, reversible, recorded_at, trusted_time_source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (release_version) DO NOTHING`,
		id, release.Version, release.ArtifactDigest, migrations.DigestAlgorithm, release.SourceDigest,
		release.ToolVersion, release.CompatibilityClass, release.Owner, release.Reversible,
		j.now(), trusted)
	if err != nil {
		return fmt.Errorf("record schema release %s: %w", release.Version, err)
	}
	return nil
}

// ReleaseID returns the recorded identifier of a release version.
func (j *Journal) ReleaseID(ctx context.Context, version string) (uuid.UUID, error) {
	var id uuid.UUID
	err := j.db.QueryRowContext(ctx,
		`SELECT release_id FROM schema_release WHERE release_version = $1`, version).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read schema release %s: %w", version, err)
	}
	return id, nil
}

// Applied reports whether an up-migration is recorded as applied and still in
// effect, which is what makes repeated application idempotent. It returns
// ErrChecksumMismatch when the recorded checksum disagrees with the bytes in
// this build, because that disagreement must block startup rather than be
// treated as "already done".
func (j *Journal) Applied(ctx context.Context, releaseID uuid.UUID, file migrations.File) (bool, error) {
	var recorded string
	err := j.db.QueryRowContext(ctx, `
		SELECT checksum FROM migration_journal
		WHERE release_id = $1 AND migration_version = $2
		  AND direction = 'UP' AND status = 'APPLIED' AND rolled_back_at IS NULL`,
		releaseID, file.Version).Scan(&recorded)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read migration journal for %s: %w", file.Name, err)
	}
	if recorded != file.Checksum {
		return false, ErrChecksumMismatch{
			Version:  file.Version,
			Name:     file.Name,
			Recorded: recorded,
			Actual:   file.Checksum,
		}
	}
	return true, nil
}

// Begin opens a journal entry for one migration attempt. The entry starts in
// RUNNING; the caller closes it with Succeed, Fail or RollForward.
func (j *Journal) Begin(ctx context.Context, releaseID uuid.UUID, file migrations.File, direction string) (Entry, error) {
	entry := Entry{
		JournalID: uuid.New(),
		ReleaseID: releaseID,
		Version:   file.Version,
		Name:      file.Name,
		Direction: direction,
		Checksum:  file.Checksum,
		StartedAt: j.now(),
	}
	_, err := j.db.ExecContext(ctx, `
		INSERT INTO migration_journal (
			journal_id, release_id, migration_version, migration_name, direction,
			checksum, checksum_algorithm, tool_version, applied_by, status,
			started_at, trusted_time_source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		entry.JournalID, entry.ReleaseID, entry.Version, entry.Name, entry.Direction,
		entry.Checksum, migrations.DigestAlgorithm, j.toolVersion, j.appliedBy, StatusRunning,
		entry.StartedAt, j.trustedTimeSource)
	if err != nil {
		return Entry{}, fmt.Errorf("open migration journal entry for %s: %w", file.Name, err)
	}
	return entry, nil
}

// MarkRolledBack records that the applied up-migration for this version is no
// longer in effect. The DOWN entry is the evidence of the rollback; this marks
// the UP entry so the migration can be applied again and journaled again.
func (j *Journal) MarkRolledBack(ctx context.Context, releaseID uuid.UUID, version int64) error {
	_, err := j.db.ExecContext(ctx, `
		UPDATE migration_journal
		SET rolled_back_at = $3
		WHERE release_id = $1 AND migration_version = $2
		  AND direction = 'UP' AND status = 'APPLIED' AND rolled_back_at IS NULL`,
		releaseID, version, j.now())
	if err != nil {
		return fmt.Errorf("mark migration %d rolled back: %w", version, err)
	}
	return nil
}

// Succeed closes a journal entry as applied.
func (j *Journal) Succeed(ctx context.Context, entry Entry) error {
	return j.finish(ctx, entry, StatusApplied, nil)
}

// Fail closes a journal entry as failed, recording the cause.
func (j *Journal) Fail(ctx context.Context, entry Entry, cause error) error {
	return j.finish(ctx, entry, StatusFailed, cause)
}

// RollForward closes a journal entry as rolled forward, which is how an
// irreversible migration is repaired.
func (j *Journal) RollForward(ctx context.Context, entry Entry) error {
	return j.finish(ctx, entry, StatusRolledForward, nil)
}

func (j *Journal) finish(ctx context.Context, entry Entry, status string, cause error) error {
	finished := j.now()
	if !finished.After(entry.StartedAt) {
		// The journal window is half-open; a zero-length window is not evidence.
		finished = entry.StartedAt.Add(time.Microsecond)
	}
	var detail any
	if cause != nil {
		detail = cause.Error()
	}
	result, err := j.db.ExecContext(ctx, `
		UPDATE migration_journal
		SET status = $2, finished_at = $3, failure_detail = $4
		WHERE journal_id = $1`,
		entry.JournalID, status, finished, detail)
	if err != nil {
		return fmt.Errorf("close migration journal entry %s: %w", entry.Name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("close migration journal entry %s: %w", entry.Name, err)
	}
	if affected != 1 {
		return fmt.Errorf("close migration journal entry %s: %d rows updated", entry.Name, affected)
	}
	return nil
}

// VerifyChecksums compares every applied up-migration against the bytes in this
// build. It is the startup guard: any disagreement is fatal.
func (j *Journal) VerifyChecksums(ctx context.Context, releaseID uuid.UUID, files []migrations.File) error {
	recorded := make(map[int64]string, len(files))
	rows, err := j.db.QueryContext(ctx, `
		SELECT migration_version, checksum FROM migration_journal
		WHERE release_id = $1 AND direction = 'UP' AND status = 'APPLIED'
		  AND rolled_back_at IS NULL`, releaseID)
	if err != nil {
		return fmt.Errorf("read migration journal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return fmt.Errorf("read migration journal: %w", err)
		}
		recorded[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration journal: %w", err)
	}

	for _, file := range files {
		have, ok := recorded[file.Version]
		if !ok {
			continue
		}
		if have != file.Checksum {
			return ErrChecksumMismatch{
				Version:  file.Version,
				Name:     file.Name,
				Recorded: have,
				Actual:   file.Checksum,
			}
		}
	}
	return nil
}
