package checkpoint_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
)

func TestTruncateNormalizesToStoragePrecision(t *testing.T) {
	// PostgreSQL's timestamptz holds microseconds. A manifest signed with
	// nanosecond precision would not survive a round trip and its own
	// signature would stop verifying, which would look exactly like
	// tampering.
	in := time.Date(2026, 3, 1, 12, 0, 0, 123456789, time.FixedZone("east", 3600))
	got := checkpoint.Truncate(in)

	if got.Location() != time.UTC {
		t.Fatalf("Truncate returned a time in %s, want UTC", got.Location())
	}
	if got.Nanosecond()%int(checkpoint.StoragePrecision) != 0 {
		t.Fatalf("Truncate returned %s, which is finer than %s", got, checkpoint.StoragePrecision)
	}
	if !got.Equal(checkpoint.Truncate(got)) {
		t.Fatal("Truncate is not idempotent")
	}
	if got.Nanosecond() != 123456000 {
		t.Fatalf("Truncate returned %d nanoseconds, want 123456000", got.Nanosecond())
	}
}

func TestStoreRoundTripPreservesTheSignedBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	dir := f.liveKeyDirectory()
	svc := f.service(t, dir)
	store := checkpoint.NewStore()

	signed := f.mustCreateCheckpoint(t, svc, checkpointOne)
	want, err := signed.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	t.Run("Read returns the manifest with its heads", func(t *testing.T) {
		stored, err := store.Read(ctx, f.db.Conn, f.tenant, 1)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(stored.Streams) != len(signed.Streams) {
			t.Fatalf("stored manifest covers %d streams, want %d", len(stored.Streams), len(signed.Streams))
		}
		got, err := stored.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got != want {
			t.Fatalf("the round-tripped manifest digests as %s, want %s", got, want)
		}
	})

	t.Run("Latest returns the highest epoch", func(t *testing.T) {
		latest, ok, err := store.Latest(ctx, f.db.Conn, f.tenant)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if !ok || latest.EpochNumber != 1 {
			t.Fatalf("Latest returned epoch %d (ok=%t), want 1", latest.EpochNumber, ok)
		}
	})

	t.Run("Latest reports absence rather than failing", func(t *testing.T) {
		other := uuid.New()
		f.seedTenant(t, other)
		if _, ok, err := store.Latest(ctx, f.db.Conn, other); err != nil || ok {
			t.Fatalf("Latest for a tenant with no epochs = ok %t, err %v; want false, nil", ok, err)
		}
	})

	t.Run("List returns every epoch in order", func(t *testing.T) {
		f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
		f.mustCreateCheckpoint(t, svc, checkpointTwo)

		epochs, err := store.List(ctx, f.db.Conn, f.tenant)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(epochs) != 2 {
			t.Fatalf("List returned %d epochs, want 2", len(epochs))
		}
		if epochs[0].EpochNumber != 1 || epochs[1].EpochNumber != 2 {
			t.Fatalf("List returned epochs %d, %d; want 1, 2", epochs[0].EpochNumber, epochs[1].EpochNumber)
		}
		for _, epoch := range epochs {
			if len(epoch.Streams) == 0 {
				t.Fatalf("epoch %d came back with no stream heads", epoch.EpochNumber)
			}
		}
	})

	t.Run("a missing epoch is a typed absence", func(t *testing.T) {
		_, err := store.Read(ctx, f.db.Conn, f.tenant, 99)
		if _, ok := errors.AsType[checkpoint.ErrEpochNotFound](err); !ok {
			t.Fatalf("read of a missing epoch returned %v, want ErrEpochNotFound", err)
		}
	})
}

func TestStoreRefusesAnUnverifiableManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	dir := f.liveKeyDirectory()
	signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

	tampered := signed
	tampered.EpochID = uuid.New()
	tampered.CoversTo = signed.CoversTo.Add(time.Hour)

	err := f.inTxErr(func(tx dbport.Tx) error {
		return checkpoint.NewStore().Append(ctx, tx, tampered, dir)
	})
	if _, ok := errors.AsType[checkpoint.ErrSignatureInvalid](err); !ok {
		t.Fatalf("Append of a tampered manifest returned %v, want ErrSignatureInvalid", err)
	}

	var count int
	if err := f.db.QueryRow(ctx, `
		SELECT count(*) FROM ledger_checkpoint_epoch WHERE tenant_id = $1`, f.tenant).Scan(&count); err != nil {
		t.Fatalf("count epochs: %v", err)
	}
	if count != 1 {
		t.Fatalf("the schema holds %d epochs, want only the one legitimately created", count)
	}
}

func TestStoredEpochsAreAppendOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	f.mustCreateCheckpoint(t, f.service(t, f.liveKeyDirectory()), checkpointOne)

	// The forbid_mutation trigger, not application code, is what makes a
	// signature impossible to rewrite: prove it against the database.
	if err := f.db.ExecErr(`
		UPDATE ledger_checkpoint_epoch SET signature_value = 'rewritten' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("a stored checkpoint signature was updated in place")
	}
	if err := f.db.ExecErr(`
		DELETE FROM ledger_checkpoint_epoch WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("a stored checkpoint was deleted")
	}
	if err := f.db.ExecErr(`
		UPDATE ledger_checkpoint_stream_head SET head_sequence = 99 WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("a stored checkpoint stream head was updated in place")
	}

	stored, err := checkpoint.NewStore().Read(ctx, f.db.Conn, f.tenant, 1)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := checkpoint.Verify(stored, f.liveKeyDirectory()); err != nil {
		t.Fatalf("the checkpoint no longer verifies after the refused mutations: %v", err)
	}
}
