package checkpoint_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
)

// recordingAnchor captures what it was asked to anchor, and can be made to
// fail, so the "anchoring never changes the checkpoint" claim is testable.
type recordingAnchor struct {
	name     string
	err      error
	anchored []checkpoint.Manifest
}

func (a *recordingAnchor) Name() string { return a.name }

func (a *recordingAnchor) Anchor(_ context.Context, m checkpoint.Manifest) (checkpoint.AnchorReceipt, error) {
	a.anchored = append(a.anchored, m)
	if a.err != nil {
		return checkpoint.AnchorReceipt{}, a.err
	}
	return checkpoint.AnchorReceipt{Provider: a.name, Reference: fmt.Sprintf("epoch-%d", m.EpochNumber)}, nil
}

func TestServiceCreateDerivesEverythingTheCallerCannotSupply(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedTwoStreams(t)
	svc := f.service(t, f.liveKeyDirectory())

	manifest := f.mustCreateCheckpoint(t, svc, checkpointOne)

	if manifest.EpochNumber != 1 {
		t.Errorf("epoch number is %d, want the derived 1", manifest.EpochNumber)
	}
	if manifest.EpochID == uuid.Nil {
		t.Error("no epoch identifier was minted")
	}
	if manifest.RootDigestAlgorithm != checkpoint.DigestAlgorithm {
		t.Errorf("root digest algorithm is %q, want %q", manifest.RootDigestAlgorithm, checkpoint.DigestAlgorithm)
	}
	if manifest.Signature == nil || manifest.Signature.Value == "" {
		t.Error("the manifest came back unsigned")
	}
	if !manifest.Signature.SignedAt.Equal(manifest.CreatedAt) {
		t.Errorf("signed at %s but created at %s", manifest.Signature.SignedAt, manifest.CreatedAt)
	}
}

func TestServiceCreateRefusesARequestWithNoTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	svc := f.service(t, f.liveKeyDirectory())

	err := f.inTxErr(func(tx dbport.Tx) error {
		_, createErr := svc.Create(context.Background(), tx, checkpoint.CreateRequest{Schema: f.release, At: checkpointOne})
		return createErr
	})
	if _, ok := errors.AsType[checkpoint.ErrManifestInvalid](err); !ok {
		t.Fatalf("Create without a tenant returned %v, want ErrManifestInvalid", err)
	}
}

func TestServiceCreateUsesItsClockWhenNoInstantIsGiven(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedTwoStreams(t)
	svc := f.service(t, f.liveKeyDirectory(), checkpoint.WithClock(func() time.Time { return checkpointOne }))

	var manifest checkpoint.Manifest
	f.inTx(t, func(tx dbport.Tx) error {
		var createErr error
		manifest, createErr = svc.Create(context.Background(), tx, checkpoint.CreateRequest{
			Tenant: f.tenant, Schema: f.release,
		})
		return createErr
	})
	if !manifest.CreatedAt.Equal(checkpointOne) {
		t.Fatalf("checkpoint created at %s, want the injected clock's %s", manifest.CreatedAt, checkpointOne)
	}
}

func TestServiceCreateRefusesAnEmptyWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedTwoStreams(t)
	svc := f.service(t, f.liveKeyDirectory())
	f.mustCreateCheckpoint(t, svc, checkpointOne)

	// A second checkpoint at the same instant would attest to a window in
	// which nothing was recorded.
	_, err := f.createCheckpoint(t, svc, checkpointOne)
	if _, ok := errors.AsType[checkpoint.ErrManifestInvalid](err); !ok {
		t.Fatalf("a repeated checkpoint returned %v, want ErrManifestInvalid", err)
	}
}

func TestServiceCoverageIsBoundedByTheCheckpointInstant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.appendLinked(t, f.tenant, streamOne, 0, recordedFirst)
	// A second event recorded after the checkpoint instant must not raise the
	// attested head: the window is half-open on recorded time.
	f.appendLinked(t, f.tenant, streamOne, 1, checkpointOne.Add(time.Hour))

	manifest := f.mustCreateCheckpoint(t, f.service(t, f.liveKeyDirectory()), checkpointOne)
	if len(manifest.Streams) != 1 {
		t.Fatalf("checkpoint covers %d streams, want 1", len(manifest.Streams))
	}
	if manifest.Streams[0].Sequence != 1 {
		t.Fatalf("attested head is %d, want 1: the later event is outside the window",
			manifest.Streams[0].Sequence)
	}
}

func TestServiceAnchoringIsPluggableAndNeverChangesTheCheckpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a successful anchor sees the signed manifest", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		anchor := &recordingAnchor{name: "worm"}
		svc := f.service(t, f.liveKeyDirectory(), checkpoint.WithAnchor(anchor))

		manifest := f.mustCreateCheckpoint(t, svc, checkpointOne)
		if len(anchor.anchored) != 1 {
			t.Fatalf("the anchor saw %d manifests, want 1", len(anchor.anchored))
		}
		if anchor.anchored[0].Signature.Value != manifest.Signature.Value {
			t.Fatal("the anchor was handed a different manifest than the one returned")
		}
	})

	t.Run("a failing anchor leaves a valid, recorded checkpoint", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		anchor := &recordingAnchor{name: "worm", err: errors.New("provider unavailable")}
		svc := f.service(t, dir, checkpoint.WithAnchor(anchor))

		var manifest checkpoint.Manifest
		err := f.inTxErr(func(tx dbport.Tx) error {
			// The anchor's failure is returned alongside the signed manifest
			// and is deliberately swallowed here: the transaction must still
			// commit, because the checkpoint is complete evidence whether or
			// not it was published anywhere.
			manifest, _ = svc.Create(ctx, tx, checkpoint.CreateRequest{
				Tenant: f.tenant, Schema: f.release, At: checkpointOne,
			})
			return nil
		})
		if err != nil {
			t.Fatalf("transaction: %v", err)
		}
		if manifest.Signature == nil || manifest.Signature.Value == "" {
			t.Fatal("an anchor failure produced an unsigned manifest")
		}
		if err := checkpoint.Verify(manifest, dir); err != nil {
			t.Fatalf("an anchor failure produced an unverifiable checkpoint: %v", err)
		}
		stored, err := svc.Store().Read(ctx, f.db.Conn, f.tenant, 1)
		if err != nil {
			t.Fatalf("the checkpoint was not recorded despite the anchor failing: %v", err)
		}
		if err := checkpoint.Verify(stored, dir); err != nil {
			t.Fatalf("verify stored: %v", err)
		}
	})
}

func TestServiceSupersede(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	dir := f.liveKeyDirectory()
	svc := f.service(t, dir)
	first := f.mustCreateCheckpoint(t, svc, checkpointOne)

	t.Run("a corrective epoch requires a reason", func(t *testing.T) {
		err := f.inTxErr(func(tx dbport.Tx) error {
			_, supersedeErr := svc.Supersede(ctx, tx, checkpoint.CreateRequest{
				Tenant: f.tenant, Schema: f.release, At: checkpointTwo,
			}, 1, "")
			return supersedeErr
		})
		if _, ok := errors.AsType[checkpoint.ErrManifestInvalid](err); !ok {
			t.Fatalf("Supersede with no reason returned %v, want ErrManifestInvalid", err)
		}
	})

	t.Run("superseding an epoch that does not exist is refused", func(t *testing.T) {
		err := f.inTxErr(func(tx dbport.Tx) error {
			_, supersedeErr := svc.Supersede(ctx, tx, checkpoint.CreateRequest{
				Tenant: f.tenant, Schema: f.release, At: checkpointTwo,
			}, 99, "because")
			return supersedeErr
		})
		if _, ok := errors.AsType[checkpoint.ErrEpochNotFound](err); !ok {
			t.Fatalf("Supersede of a missing epoch returned %v, want ErrEpochNotFound", err)
		}
	})

	t.Run("a corrective epoch is added, never substituted", func(t *testing.T) {
		f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
		var corrective checkpoint.Manifest
		f.inTx(t, func(tx dbport.Tx) error {
			var supersedeErr error
			corrective, supersedeErr = svc.Supersede(ctx, tx, checkpoint.CreateRequest{
				Tenant: f.tenant, Schema: f.release, At: checkpointTwo,
			}, 1, "epoch 1 was taken over a stream later found broken")
			return supersedeErr
		})

		if corrective.EpochNumber != 2 {
			t.Fatalf("corrective epoch is numbered %d, want 2", corrective.EpochNumber)
		}
		if corrective.CorrectsEpochID != first.EpochID {
			t.Fatalf("corrective epoch corrects %s, want %s", corrective.CorrectsEpochID, first.EpochID)
		}
		epochs, err := svc.Store().List(ctx, f.db.Conn, f.tenant)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(epochs) != 2 {
			t.Fatalf("the tenant holds %d epochs, want the original and its correction", len(epochs))
		}
		if _, err := checkpoint.VerifyChain(ctx, f.db.Conn, f.tenant, dir); err != nil {
			t.Fatalf("the chain does not verify after a correction: %v", err)
		}
	})
}

func TestVerifyChainOnAnEmptyLedgerIsNotAFalsePass(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	count, err := checkpoint.VerifyChain(context.Background(), f.db.Conn, f.tenant, f.liveKeyDirectory())
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	// Zero epochs verify vacuously; the count is what tells a caller that
	// nothing was actually checked.
	if count != 0 {
		t.Fatalf("VerifyChain reported %d epochs for a tenant with none", count)
	}
}
