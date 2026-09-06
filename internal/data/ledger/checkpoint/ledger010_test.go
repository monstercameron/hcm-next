package checkpoint_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
)

// TestTodo_LEDGER_010 proves signed checkpoints and integrity epochs: a
// manifest binds every included stream head, the root digest over them, the
// schema release, the signing key and the covered time window; an
// independent verifier accepts it offline; an incomplete or unsigned
// checkpoint is refused; and a late discovery produces a new corrective
// epoch instead of a rewritten signature.
func TestTodo_LEDGER_010(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	dir := f.liveKeyDirectory()
	svc := f.service(t, dir)

	manifest := f.mustCreateCheckpoint(t, svc, checkpointOne)

	t.Run("the manifest binds every stream head and the root digest over them", func(t *testing.T) {
		if manifest.EpochNumber != 1 {
			t.Fatalf("first checkpoint is epoch %d, want 1", manifest.EpochNumber)
		}
		if len(manifest.Streams) != 2 {
			t.Fatalf("manifest covers %d streams, want both", len(manifest.Streams))
		}
		for _, head := range manifest.Streams {
			if head.Sequence < 1 || head.ChainHash == "" || head.ChainAlgorithm == "" {
				t.Fatalf("stream head %+v does not bind a sequence, chain hash and algorithm", head)
			}
		}
		root, err := checkpoint.ComputeRootDigest(manifest.Streams)
		if err != nil {
			t.Fatalf("recompute root digest: %v", err)
		}
		if root != manifest.RootDigest {
			t.Fatalf("recorded root digest %s does not reproduce as %s", manifest.RootDigest, root)
		}
	})

	t.Run("the manifest binds the schema release, the key and the time window", func(t *testing.T) {
		if manifest.Schema.Version < 1 || manifest.Schema.Digest == "" {
			t.Fatalf("manifest schema release is %+v, want a version and a digest", manifest.Schema)
		}
		if manifest.Signature == nil || manifest.Signature.KeyID != devKeyID {
			t.Fatalf("manifest signature is %+v, want one naming %s", manifest.Signature, devKeyID)
		}
		if !manifest.CoversFrom.Before(manifest.CoversTo) {
			t.Fatalf("covered window [%s, %s) is not half-open and non-empty", manifest.CoversFrom, manifest.CoversTo)
		}
		if !manifest.CoversTo.Equal(checkpointOne) {
			t.Fatalf("covered window ends at %s, want the checkpoint instant %s", manifest.CoversTo, checkpointOne)
		}
		if !manifest.CoversFrom.Equal(recordedFirst) {
			t.Fatalf("the first epoch covers from %s, want the ledger's earliest recording %s",
				manifest.CoversFrom, recordedFirst)
		}
	})

	t.Run("an independent verifier accepts it with nothing but the manifest and the key", func(t *testing.T) {
		if err := checkpoint.Verify(manifest, dir); err != nil {
			t.Fatalf("verify: %v", err)
		}
		// The manifest read back out of PostgreSQL must verify identically:
		// a round trip that changed a single byte would break the signature.
		stored, err := svc.Store().Read(ctx, f.db.Conn, f.tenant, 1)
		if err != nil {
			t.Fatalf("read epoch 1: %v", err)
		}
		if err := checkpoint.Verify(stored, dir); err != nil {
			t.Fatalf("verify the stored manifest: %v", err)
		}
		want, err := manifest.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		got, err := stored.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got != want {
			t.Fatalf("the stored manifest digests as %s, want %s", got, want)
		}
	})

	t.Run("a second epoch chains onto the first", func(t *testing.T) {
		f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
		second := f.mustCreateCheckpoint(t, svc, checkpointTwo)

		if second.EpochNumber != 2 {
			t.Fatalf("second checkpoint is epoch %d, want 2", second.EpochNumber)
		}
		if second.PreviousEpochID != manifest.EpochID {
			t.Fatalf("epoch 2 follows %s, want %s", second.PreviousEpochID, manifest.EpochID)
		}
		wantPrevious, err := manifest.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if second.PreviousManifestDigest != wantPrevious {
			t.Fatalf("epoch 2 carries previous digest %s, want %s", second.PreviousManifestDigest, wantPrevious)
		}
		if !second.CoversFrom.Equal(manifest.CoversTo) {
			t.Fatalf("epoch 2 covers from %s, want the previous epoch's end %s", second.CoversFrom, manifest.CoversTo)
		}
		// The window is half-open, so no recorded instant is covered twice.
		if !second.CoversFrom.Before(second.CoversTo) {
			t.Fatalf("epoch 2's window [%s, %s) is empty", second.CoversFrom, second.CoversTo)
		}

		count, err := checkpoint.VerifyChain(ctx, f.db.Conn, f.tenant, dir)
		if err != nil {
			t.Fatalf("verify chain: %v", err)
		}
		if count != 2 {
			t.Fatalf("verified %d epochs, want 2", count)
		}
	})

	t.Run("late discovery creates a corrective epoch and never rewrites a signature", func(t *testing.T) {
		before, err := svc.Store().Read(ctx, f.db.Conn, f.tenant, 1)
		if err != nil {
			t.Fatalf("read epoch 1: %v", err)
		}

		f.appendLinked(t, f.tenant, streamTwo, 1, checkpointTwo.Add(time.Hour))
		var corrective checkpoint.Manifest
		f.inTx(t, func(tx dbport.Tx) error {
			var supersedeErr error
			corrective, supersedeErr = svc.Supersede(ctx, tx, checkpoint.CreateRequest{
				Tenant: f.tenant, Schema: f.release, At: checkpointTwo.AddDate(0, 1, 0),
			}, 1, "epoch 1 attested to a stream whose chain was later found broken")
			return supersedeErr
		})

		if corrective.EpochNumber != 3 {
			t.Fatalf("corrective checkpoint is epoch %d, want the next number 3", corrective.EpochNumber)
		}
		if corrective.CorrectsEpochID != before.EpochID {
			t.Fatalf("corrective epoch names %s, want epoch 1 %s", corrective.CorrectsEpochID, before.EpochID)
		}
		if corrective.CorrectsReason == "" {
			t.Fatal("a corrective epoch was recorded without saying why")
		}
		if err := checkpoint.Verify(corrective, dir); err != nil {
			t.Fatalf("verify corrective epoch: %v", err)
		}

		after, err := svc.Store().Read(ctx, f.db.Conn, f.tenant, 1)
		if err != nil {
			t.Fatalf("re-read epoch 1: %v", err)
		}
		if after.Signature.Value != before.Signature.Value {
			t.Fatal("the superseded epoch's signature was rewritten")
		}
		if err := checkpoint.Verify(after, dir); err != nil {
			t.Fatalf("the superseded epoch no longer verifies: %v", err)
		}
	})
}

// TestTodo_LEDGER_010_Golden pins the exact bytes a checkpoint commits to.
// The root digest, the canonical manifest digest and the Ed25519 signature
// over it are all recorded in testdata; a change to the framing, the field
// order or the projection changes them, and that change must be a deliberate
// edit of the golden file rather than a silent break in every previously
// signed manifest.
func TestTodo_LEDGER_010_Golden(t *testing.T) {
	t.Parallel()
	_, priv := loadDevKey(t)
	signer, err := checkpoint.NewEd25519Signer(devKeyID, priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}

	// A fully pinned manifest: nothing here is derived from the clock, a
	// random identifier or the migration tree.
	streams := []checkpoint.StreamHead{
		{StreamKey: "worker:2", Sequence: 3, ChainHash: strings.Repeat("b", 64), ChainAlgorithm: "sha256"},
		{StreamKey: "worker:1", Sequence: 7, ChainHash: strings.Repeat("a", 64), ChainAlgorithm: "sha256"},
	}
	root, err := checkpoint.ComputeRootDigest(streams)
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	manifest := checkpoint.Manifest{
		SchemaVersion: checkpoint.ManifestSchemaVersion,
		Tenant:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EpochID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		EpochNumber:   1,
		Schema:        checkpoint.SchemaRelease{Version: 28, Digest: strings.Repeat("c", 64)},
		Streams:       streams,
		RootDigest:    root, RootDigestAlgorithm: checkpoint.DigestAlgorithm,
		CoversFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CoversTo:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	dir := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: devKeyID, PublicKey: signer.PublicKey(), NotBefore: keyValidFrom,
	})
	signed, err := checkpoint.Sign(manifest, signer, dir)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	digest, err := signed.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	got := strings.Join([]string{
		"root_digest=" + root,
		"manifest_digest=" + digest,
		"signature=" + signed.Signature.Value,
	}, "\n") + "\n"

	golden := filepath.Join("testdata", "golden_manifest.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	wantBytes, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.ReplaceAll(string(wantBytes), "\r\n", "\n"); got != want {
		t.Fatalf("checkpoint bytes changed.\n got:\n%s\nwant:\n%s", got, want)
	}

	// The golden signature must also verify, so the file cannot be updated to
	// any string that merely matches.
	if err := checkpoint.Verify(signed, dir); err != nil {
		t.Fatalf("verify the golden manifest: %v", err)
	}

	t.Run("signing is deterministic", func(t *testing.T) {
		again, err := checkpoint.Sign(manifest, signer, dir)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if again.Signature.Value != signed.Signature.Value {
			t.Fatal("two signings of the same manifest produced different signatures")
		}
	})

	t.Run("stream order does not change the bytes", func(t *testing.T) {
		reordered := manifest
		reordered.Streams = []checkpoint.StreamHead{streams[1], streams[0]}
		out, err := checkpoint.Sign(reordered, signer, dir)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if out.Signature.Value != signed.Signature.Value {
			t.Fatal("reordering the stream heads changed the signature")
		}
	})
}

// TestTodo_LEDGER_010_Race proves the read and verify path is safe to fan
// out, and that concurrent checkpoint creation cannot produce two epochs
// with the same number: the epoch number is allocated from a uniquely
// constrained column inside the caller's transaction, so the loser is
// refused rather than silently overwriting.
func TestTodo_LEDGER_010_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.seedTwoStreams(t)
	dir := f.liveKeyDirectory()
	svc := f.service(t, dir)

	signed := f.mustCreateCheckpoint(t, svc, checkpointOne)
	wantDigest, err := signed.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	t.Run("concurrent verification agrees", func(t *testing.T) {
		const readers = 8
		digests := make([]string, readers)
		errs := make([]error, readers)
		var wg sync.WaitGroup
		for i := range readers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn := f.db.NewConn(t)
				stored, readErr := svc.Store().Read(ctx, conn, f.tenant, 1)
				if readErr != nil {
					errs[i] = readErr
					return
				}
				if verifyErr := checkpoint.Verify(stored, dir); verifyErr != nil {
					errs[i] = verifyErr
					return
				}
				digests[i], errs[i] = stored.CanonicalDigest()
			}()
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("reader %d: %v", i, err)
			}
			if digests[i] != wantDigest {
				t.Fatalf("reader %d read digest %s, want %s", i, digests[i], wantDigest)
			}
		}
	})

	t.Run("a second epoch with the same number is refused", func(t *testing.T) {
		// Re-signing the epoch that already exists, with a fresh identifier,
		// stands in for two racing creators that both computed epoch 1.
		duplicate := signed
		duplicate.EpochID = uuid.New()
		resigned, err := checkpoint.Sign(duplicate, f.signer, dir)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		err = f.inTxErr(func(tx dbport.Tx) error {
			return checkpoint.NewStore().Append(ctx, tx, resigned, dir)
		})
		var already checkpoint.ErrEpochAlreadyRecorded
		if !errors.As(err, &already) {
			t.Fatalf("appending a duplicate epoch number returned %v, want ErrEpochAlreadyRecorded", err)
		}
	})
}

// TestTodo_LEDGER_010_Mutation kills the mutants that would make a
// checkpoint look valid without being one: an omitted stream head, an
// unlinked head quietly skipped, a root digest that is trusted rather than
// recomputed, an edited manifest, a signature from a revoked or expired key,
// and an epoch chain with a hole in it.
func TestTodo_LEDGER_010_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an omitted stream head is refused", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		trimmed := signed
		trimmed.Streams = signed.Streams[:1]
		err := checkpoint.Verify(trimmed, dir)
		var mismatch checkpoint.ErrRootDigestMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("dropping a stream head returned %v, want ErrRootDigestMismatch", err)
		}
	})

	t.Run("a stream whose head has no chain link cannot be covered", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.appendLinked(t, f.tenant, streamOne, 0, recordedFirst)
		f.appendUnlinked(t, f.tenant, streamTwo, 0, recordedFirst)

		_, err := f.createCheckpoint(t, f.service(t, f.liveKeyDirectory()), checkpointOne)
		var incomplete checkpoint.ErrIncompleteCoverage
		if !errors.As(err, &incomplete) {
			t.Fatalf("checkpoint over an unlinked head returned %v, want ErrIncompleteCoverage", err)
		}
		if len(incomplete.Missing) != 1 || !strings.HasPrefix(incomplete.Missing[0], streamTwo+"@") {
			t.Fatalf("error names %v, want the uncoverable stream %s", incomplete.Missing, streamTwo)
		}
	})

	t.Run("a tenant with nothing recorded has nothing to attest to", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		_, err := f.createCheckpoint(t, f.service(t, f.liveKeyDirectory()), checkpointOne)
		var incomplete checkpoint.ErrIncompleteCoverage
		if !errors.As(err, &incomplete) {
			t.Fatalf("checkpoint over an empty ledger returned %v, want ErrIncompleteCoverage", err)
		}
	})

	t.Run("a trusted root digest would hide an edited head", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		edited := signed
		edited.Streams = append([]checkpoint.StreamHead(nil), signed.Streams...)
		edited.Streams[0].Sequence = 99
		if err := checkpoint.Verify(edited, dir); err == nil {
			t.Fatal("an edited stream head verified; the root digest was trusted rather than recomputed")
		}
	})

	t.Run("an edited manifest field breaks the signature", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		edited := signed
		edited.CoversTo = signed.CoversTo.Add(time.Hour)
		err := checkpoint.Verify(edited, dir)
		var invalid checkpoint.ErrSignatureInvalid
		if !errors.As(err, &invalid) {
			t.Fatalf("widening the covered window returned %v, want ErrSignatureInvalid", err)
		}
	})

	t.Run("an epoch chain with a hole is reported at the exact epoch", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		svc := f.service(t, dir)
		first := f.mustCreateCheckpoint(t, svc, checkpointOne)
		f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
		f.mustCreateCheckpoint(t, svc, checkpointTwo)

		// A third epoch that claims to follow epoch 1 rather than epoch 2 is
		// exactly what a removed epoch would leave behind.
		forged := first
		forged.EpochID = uuid.New()
		forged.EpochNumber = 3
		forged.PreviousEpochID = first.EpochID
		digest, err := first.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		forged.PreviousManifestDigest = digest
		forged.CreatedAt = checkpointTwo.AddDate(0, 1, 0)
		forged.CoversFrom = checkpointTwo
		forged.CoversTo = forged.CreatedAt
		signedForgery, err := checkpoint.Sign(forged, f.signer, dir)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		f.inTx(t, func(tx dbport.Tx) error {
			return checkpoint.NewStore().Append(ctx, tx, signedForgery, dir)
		})

		_, err = checkpoint.VerifyChain(ctx, f.db.Conn, f.tenant, dir)
		var broken checkpoint.ErrEpochChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("VerifyChain returned %v, want ErrEpochChainBroken", err)
		}
		if broken.EpochNumber != 3 {
			t.Fatalf("chain break reported at epoch %d, want 3", broken.EpochNumber)
		}
	})
}

// TestTodo_LEDGER_010_Security proves the key rules: an expired key, a
// revoked key and a key the directory has never heard of all fail to produce
// a signature at all, a signature that verifies arithmetically under an
// unauthorized key is still refused, and one tenant's epochs are never
// reachable from another's.
func TestTodo_LEDGER_010_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an expired key never signs", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		// The key's validity window closes before the checkpoint instant.
		expired := f.keyDirectory(checkpointOne.Add(-time.Hour), time.Time{})
		_, err := f.createCheckpoint(t, f.service(t, expired), checkpointOne)
		var notUsable checkpoint.ErrKeyNotUsable
		if !errors.As(err, &notUsable) {
			t.Fatalf("signing with an expired key returned %v, want ErrKeyNotUsable", err)
		}
		if !strings.Contains(notUsable.Error(), "validity window") {
			t.Fatalf("error %q does not say the key was outside its window", notUsable)
		}
	})

	t.Run("a revoked key never signs", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		revoked := f.keyDirectory(time.Time{}, checkpointOne.Add(-time.Hour))
		_, err := f.createCheckpoint(t, f.service(t, revoked), checkpointOne)
		var notUsable checkpoint.ErrKeyNotUsable
		if !errors.As(err, &notUsable) {
			t.Fatalf("signing with a revoked key returned %v, want ErrKeyNotUsable", err)
		}
		if !strings.Contains(notUsable.Error(), "revoked") {
			t.Fatalf("error %q does not say the key was revoked", notUsable)
		}
	})

	t.Run("an unknown key never signs", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		empty := checkpoint.NewStaticKeyDirectory()
		_, err := f.createCheckpoint(t, f.service(t, empty), checkpointOne)
		var notUsable checkpoint.ErrKeyNotUsable
		if !errors.As(err, &notUsable) {
			t.Fatalf("signing with an unknown key returned %v, want ErrKeyNotUsable", err)
		}
	})

	t.Run("a valid signature from an unauthorized key is still refused", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		// The signature verifies arithmetically; the key was not permitted to
		// make it at the instant the manifest claims it was made.
		revokedEarlier := f.keyDirectory(time.Time{}, checkpointOne.Add(-time.Hour))
		err := checkpoint.Verify(signed, revokedEarlier)
		var notUsable checkpoint.ErrKeyNotUsable
		if !errors.As(err, &notUsable) {
			t.Fatalf("verify under a revoked key returned %v, want ErrKeyNotUsable", err)
		}

		// Revoking the key AFTER the checkpoint was made does not retroactively
		// invalidate it: the attestation was validly made when it was made.
		revokedLater := f.keyDirectory(time.Time{}, checkpointOne.Add(time.Hour))
		if err := checkpoint.Verify(signed, revokedLater); err != nil {
			t.Fatalf("a later revocation invalidated an earlier valid signature: %v", err)
		}
	})

	t.Run("a checkpoint cannot be attributed to a different key", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		signed := f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		reattributed := signed
		reattributed.Signature = &checkpoint.Signature{
			Algorithm: signed.Signature.Algorithm,
			KeyID:     "hcmnext:checkpoint:someone-else",
			PublicKey: signed.Signature.PublicKey,
			Value:     signed.Signature.Value,
			SignedAt:  signed.Signature.SignedAt,
		}
		if err := checkpoint.Verify(reattributed, dir); err == nil {
			t.Fatal("a checkpoint re-attributed to another key id verified")
		}
	})

	t.Run("one tenant's epochs are not reachable from another", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		other := uuid.New()
		f.seedTenant(t, other)
		if _, err := checkpoint.NewStore().Read(ctx, f.db.Conn, other, 1); err == nil {
			t.Fatal("another tenant read this tenant's epoch 1")
		}
		count, err := checkpoint.VerifyChain(ctx, f.db.Conn, other, dir)
		if err != nil {
			t.Fatalf("verify chain for a tenant with no epochs: %v", err)
		}
		if count != 0 {
			t.Fatalf("a tenant with no checkpoints verified %d epochs", count)
		}
	})

	t.Run("no private key material reaches the database", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedTwoStreams(t)
		dir := f.liveKeyDirectory()
		f.mustCreateCheckpoint(t, f.service(t, dir), checkpointOne)

		_, priv := loadDevKey(t)
		var stored string
		if err := f.db.QueryRow(ctx, `
			SELECT signing_public_key || ':' || signature_value || ':' || signing_key_id
			FROM ledger_checkpoint_epoch WHERE tenant_id = $1`, f.tenant).Scan(&stored); err != nil {
			t.Fatalf("read stored key material: %v", err)
		}
		seedHex := strings.ToLower(hexOf(priv.Seed()))
		if strings.Contains(strings.ToLower(stored), seedHex) {
			t.Fatal("the private key seed was written to the database")
		}
	})
}

// TestSigningKeyFixtureIsLabelledAsATestKey guards the one private key in
// this package: it must stay a fixture, and must say so where anyone reading
// it will see it.
func TestSigningKeyFixtureIsLabelledAsATestKey(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", devKeyFile))
	if err != nil {
		t.Fatalf("read signing key fixture: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"TEST FIXTURE ONLY", "NEVER BE USED TO SIGN A PRODUCTION"} {
		if !strings.Contains(body, want) {
			t.Errorf("the signing key fixture does not carry the warning %q", want)
		}
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}
