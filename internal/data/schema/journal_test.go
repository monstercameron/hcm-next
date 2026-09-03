package schema_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/schema"
	"github.com/monstercameron/hcm-next/migrations"
)

func testRelease(t *testing.T) schema.Release {
	t.Helper()
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("artifact digest: %v", err)
	}
	return schema.Release{
		ID:                 uuid.New(),
		Version:            "p1a-0001",
		ArtifactDigest:     digest,
		SourceDigest:       digest,
		ToolVersion:        "hcmnext-migrate/test",
		CompatibilityClass: schema.CompatibilityBackward,
		Owner:              "data-plane",
		Reversible:         true,
		TrustedTimeSource:  "TEST_CLOCK",
	}
}

// TestTodo_DB_006 proves the schema-release and migration-journal contract:
// nothing is applied without digest, tool version, checksum, owner, status and
// trusted-time evidence; duplicate application is idempotent; and a checksum
// mismatch blocks startup.
func TestTodo_DB_006(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tick := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	journal := schema.NewJournal(db.SQL, "hcmnext-migrate/test", "tester",
		schema.WithClock(clock), schema.WithTrustedTimeSource("TEST_CLOCK"))

	release := testRelease(t)
	if err := journal.EnsureRelease(ctx, release); err != nil {
		t.Fatalf("record release: %v", err)
	}
	releaseID, err := journal.ReleaseID(ctx, release.Version)
	if err != nil {
		t.Fatalf("read release: %v", err)
	}

	files, err := migrations.Files()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	first := files[0]

	t.Run("a release without a digest is refused", func(t *testing.T) {
		bad := release
		bad.Version = "p1a-no-digest"
		bad.ArtifactDigest = ""
		if err := journal.EnsureRelease(ctx, bad); err == nil {
			t.Fatal("a release without an artifact digest was recorded")
		}
		bad = release
		bad.Version = "p1a-no-tool"
		bad.ToolVersion = ""
		if err := journal.EnsureRelease(ctx, bad); err == nil {
			t.Fatal("a release without a tool version was recorded")
		}
	})

	t.Run("recording the same release twice is idempotent", func(t *testing.T) {
		if err := journal.EnsureRelease(ctx, release); err != nil {
			t.Fatalf("re-record release: %v", err)
		}
		var count int
		if err := db.QueryRow(ctx,
			`SELECT count(*) FROM schema_release WHERE release_version = $1`,
			release.Version).Scan(&count); err != nil {
			t.Fatalf("count releases: %v", err)
		}
		if count != 1 {
			t.Fatalf("release recorded %d times, want once", count)
		}
	})

	t.Run("a release version cannot change its artifact digest", func(t *testing.T) {
		forged := release
		forged.ArtifactDigest = fixtureDigestB
		err := journal.EnsureRelease(ctx, forged)
		var mismatch schema.ErrReleaseDigestMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("re-declaring a release with a new digest returned %v, want ErrReleaseDigestMismatch", err)
		}
		if mismatch.Recorded != release.ArtifactDigest || mismatch.Actual != fixtureDigestB {
			t.Fatalf("mismatch reports recorded=%s actual=%s", mismatch.Recorded, mismatch.Actual)
		}
	})

	t.Run("an entry moves from running to applied", func(t *testing.T) {
		already, err := journal.Applied(ctx, releaseID, first)
		if err != nil {
			t.Fatalf("read applied state: %v", err)
		}
		if already {
			t.Fatal("a fresh migration reported as already applied")
		}
		entry, err := journal.Begin(ctx, releaseID, first, schema.DirectionUp)
		if err != nil {
			t.Fatalf("open journal entry: %v", err)
		}

		var status string
		var finished *time.Time
		if err := db.QueryRow(ctx,
			`SELECT status, finished_at FROM migration_journal WHERE journal_id = $1`,
			entry.JournalID).Scan(&status, &finished); err != nil {
			t.Fatalf("read journal entry: %v", err)
		}
		if status != schema.StatusRunning {
			t.Fatalf("entry status is %s, want %s", status, schema.StatusRunning)
		}
		if finished != nil {
			t.Fatal("a running entry already has a finish time")
		}

		if err := journal.Succeed(ctx, entry); err != nil {
			t.Fatalf("close journal entry: %v", err)
		}
		var toolVersion, appliedBy, trusted, checksum string
		var startedAt, finishedAt time.Time
		if err := db.QueryRow(ctx, `
			SELECT status, tool_version, applied_by, trusted_time_source, checksum, started_at, finished_at
			FROM migration_journal WHERE journal_id = $1`, entry.JournalID).
			Scan(&status, &toolVersion, &appliedBy, &trusted, &checksum, &startedAt, &finishedAt); err != nil {
			t.Fatalf("read applied entry: %v", err)
		}
		if status != schema.StatusApplied {
			t.Fatalf("entry status is %s, want %s", status, schema.StatusApplied)
		}
		if toolVersion == "" || appliedBy == "" || trusted == "" {
			t.Fatalf("entry lacks evidence: tool=%q by=%q time-source=%q", toolVersion, appliedBy, trusted)
		}
		if checksum != first.Checksum {
			t.Fatalf("entry checksum %s, want %s", checksum, first.Checksum)
		}
		if !finishedAt.After(startedAt) {
			t.Fatalf("journal window [%s, %s) is not half-open", startedAt, finishedAt)
		}
	})

	t.Run("duplicate application is idempotent", func(t *testing.T) {
		already, err := journal.Applied(ctx, releaseID, first)
		if err != nil {
			t.Fatalf("read applied state: %v", err)
		}
		if !already {
			t.Fatal("re-applying an applied migration was not reported as already applied")
		}
		var count int
		if err := db.QueryRow(ctx, `
			SELECT count(*) FROM migration_journal
			WHERE release_id = $1 AND migration_version = $2 AND status = 'APPLIED' AND direction = 'UP'`,
			releaseID, first.Version).Scan(&count); err != nil {
			t.Fatalf("count applied entries: %v", err)
		}
		if count != 1 {
			t.Fatalf("%d applied entries for migration %d, want exactly one", count, first.Version)
		}
	})

	t.Run("a checksum mismatch blocks startup", func(t *testing.T) {
		forged := first
		forged.Checksum = fixtureDigestB

		_, err := journal.Applied(ctx, releaseID, forged)
		var mismatch schema.ErrChecksumMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("re-applying with a different checksum returned %v, want ErrChecksumMismatch", err)
		}
		if mismatch.Recorded != first.Checksum || mismatch.Actual != fixtureDigestB {
			t.Fatalf("mismatch reports recorded=%s actual=%s", mismatch.Recorded, mismatch.Actual)
		}

		err = journal.VerifyChecksums(ctx, releaseID, []migrations.File{forged})
		if !errors.As(err, &mismatch) {
			t.Fatalf("VerifyChecksums returned %v, want ErrChecksumMismatch", err)
		}
		if err := journal.VerifyChecksums(ctx, releaseID, files); err != nil {
			t.Fatalf("VerifyChecksums rejected the embedded migrations: %v", err)
		}
	})

	t.Run("a failure is recorded with its cause", func(t *testing.T) {
		second := files[1]
		entry, err := journal.Begin(ctx, releaseID, second, schema.DirectionUp)
		if err != nil {
			t.Fatalf("open journal entry: %v", err)
		}
		if err := journal.Fail(ctx, entry, errors.New("relation already exists")); err != nil {
			t.Fatalf("record failure: %v", err)
		}
		var status, detail string
		if err := db.QueryRow(ctx,
			`SELECT status, failure_detail FROM migration_journal WHERE journal_id = $1`,
			entry.JournalID).Scan(&status, &detail); err != nil {
			t.Fatalf("read failed entry: %v", err)
		}
		if status != schema.StatusFailed || detail == "" {
			t.Fatalf("failed entry has status %s and detail %q", status, detail)
		}
		// A failed attempt does not count as applied, so the migration may be
		// retried and rolled forward.
		applied, err := journal.Applied(ctx, releaseID, second)
		if err != nil || applied {
			t.Fatalf("retry after failure: applied=%v err=%v", applied, err)
		}
		entry, err = journal.Begin(ctx, releaseID, second, schema.DirectionUp)
		if err != nil {
			t.Fatalf("retry after failure: %v", err)
		}
		if err := journal.RollForward(ctx, entry); err != nil {
			t.Fatalf("roll forward: %v", err)
		}
	})

	t.Run("a rolled back migration can be applied again", func(t *testing.T) {
		// The applied up-entry for migration 1 is in effect.
		applied, err := journal.Applied(ctx, releaseID, first)
		if err != nil || !applied {
			t.Fatalf("migration %d is not applied: applied=%v err=%v", first.Version, applied, err)
		}

		down, err := journal.Begin(ctx, releaseID, first, schema.DirectionDown)
		if err != nil {
			t.Fatalf("open down entry: %v", err)
		}
		if err := journal.Succeed(ctx, down); err != nil {
			t.Fatalf("close down entry: %v", err)
		}
		if err := journal.MarkRolledBack(ctx, releaseID, first.Version); err != nil {
			t.Fatalf("mark rolled back: %v", err)
		}

		applied, err = journal.Applied(ctx, releaseID, first)
		if err != nil {
			t.Fatalf("read applied state: %v", err)
		}
		if applied {
			t.Fatal("a rolled back migration still reports as applied")
		}

		// Re-applying it journals a second attempt rather than colliding with
		// the first.
		again, err := journal.Begin(ctx, releaseID, first, schema.DirectionUp)
		if err != nil {
			t.Fatalf("re-open journal entry after rollback: %v", err)
		}
		if err := journal.Succeed(ctx, again); err != nil {
			t.Fatalf("close the re-application: %v", err)
		}

		var attempts int
		if err := db.QueryRow(ctx, `
			SELECT count(*) FROM migration_journal
			WHERE release_id = $1 AND migration_version = $2 AND direction = 'UP' AND status = 'APPLIED'`,
			releaseID, first.Version).Scan(&attempts); err != nil {
			t.Fatalf("count applied entries: %v", err)
		}
		if attempts != 2 {
			t.Fatalf("the journal holds %d applied up-entries for migration %d, want 2 (one rolled back)",
				attempts, first.Version)
		}
	})
}

// TestTodo_DB_006_Conformance proves the journal's own vocabulary and window
// rules are enforced by the database, not only by the Go writer.
func TestTodo_DB_006_Conformance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	journal := schema.NewJournal(db.SQL, "hcmnext-migrate/test", "tester")
	release := testRelease(t)
	if err := journal.EnsureRelease(ctx, release); err != nil {
		t.Fatalf("record release: %v", err)
	}
	releaseID, err := journal.ReleaseID(ctx, release.Version)
	if err != nil {
		t.Fatalf("read release: %v", err)
	}

	insert := func(status, direction string, finished any) error {
		return db.ExecErr(`
			INSERT INTO migration_journal (
				journal_id, release_id, migration_version, migration_name, direction,
				checksum, checksum_algorithm, tool_version, applied_by, status,
				started_at, finished_at, trusted_time_source)
			VALUES ($1, $2, 1, 'x.sql', $3, $4, 'sha256', 'tool', 'tester', $5,
				timestamptz '2026-03-01T00:00:00Z', $6::timestamptz, 'TEST_CLOCK')`,
			uuid.New(), releaseID, direction, fixtureDigestA, status, finished)
	}

	if err := insert("SKIPPED", schema.DirectionUp, "2026-03-01T00:00:01Z"); err == nil {
		t.Fatal("an unknown journal status was accepted")
	}
	if err := insert(schema.StatusApplied, "SIDEWAYS", "2026-03-01T00:00:01Z"); err == nil {
		t.Fatal("an unknown migration direction was accepted")
	}
	if err := insert(schema.StatusApplied, schema.DirectionUp, nil); err == nil {
		t.Fatal("a terminal journal entry without a finish time was accepted")
	}
	if err := insert(schema.StatusApplied, schema.DirectionUp, "2026-03-01T00:00:00Z"); err == nil {
		t.Fatal("a zero-length journal window was accepted")
	}
	if err := insert(schema.StatusApplied, schema.DirectionUp, "2026-03-01T00:00:01Z"); err != nil {
		t.Fatalf("a well-formed journal entry was rejected: %v", err)
	}
	// The partial unique index makes a second APPLIED up-row impossible while the
	// first is still in effect.
	if err := insert(schema.StatusApplied, schema.DirectionUp, "2026-03-01T00:00:02Z"); err == nil {
		t.Fatal("a second APPLIED entry for the same migration was accepted")
	}

	// A rollback marker may only sit on an applied up-entry, and never before it
	// finished.
	if err := db.ExecErr(`
		UPDATE migration_journal SET rolled_back_at = timestamptz '2026-02-28T00:00:00Z'
		WHERE release_id = $1 AND status = 'APPLIED'`, releaseID); err == nil {
		t.Fatal("a rollback marker before the migration finished was accepted")
	}
	db.Exec(t, `
		UPDATE migration_journal SET rolled_back_at = timestamptz '2026-03-02T00:00:00Z'
		WHERE release_id = $1 AND status = 'APPLIED'`, releaseID)

	// Once the first entry is out of effect the migration may be applied again.
	if err := insert(schema.StatusApplied, schema.DirectionUp, "2026-03-01T00:00:03Z"); err != nil {
		t.Fatalf("re-applying a rolled back migration was rejected: %v", err)
	}
}

// TestTodo_DB_006_Golden pins the release vocabulary.
func TestTodo_DB_006_Golden(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)

	for _, class := range []string{
		schema.CompatibilityBackward, schema.CompatibilityForward,
		schema.CompatibilityFull, schema.CompatibilityBreaking,
	} {
		if err := db.ExecErr(`
			INSERT INTO schema_release (
				release_id, release_version, artifact_digest, digest_algorithm, source_digest,
				tool_version, compatibility_class, owner, reversible, trusted_time_source)
			VALUES ($1, $2, $3, 'sha256', $3, 'tool', $4, 'data-plane', true, 'TEST_CLOCK')`,
			uuid.New(), "release-"+class, fixtureDigestA, class); err != nil {
			t.Fatalf("compatibility class %s was rejected: %v", class, err)
		}
	}
	if err := db.ExecErr(`
		INSERT INTO schema_release (
			release_id, release_version, artifact_digest, digest_algorithm, source_digest,
			tool_version, compatibility_class, owner, reversible, trusted_time_source)
		VALUES ($1, 'release-unknown', $2, 'sha256', $2, 'tool', 'PROBABLY_FINE', 'data-plane', true, 'TEST_CLOCK')`,
		uuid.New(), fixtureDigestA); err == nil {
		t.Fatal("an undeclared compatibility class was accepted")
	}
	if err := db.ExecErr(`
		INSERT INTO schema_release (
			release_id, release_version, artifact_digest, digest_algorithm, source_digest,
			tool_version, compatibility_class, owner, reversible, trusted_time_source)
		VALUES ($1, 'release-blank', '', 'sha256', $2, 'tool', 'FULL', 'data-plane', true, 'TEST_CLOCK')`,
		uuid.New(), fixtureDigestA); err == nil {
		t.Fatal("a release with a blank artifact digest was accepted")
	}
}
