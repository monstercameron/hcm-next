package auditpack_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The golden fixture is pure: fixed identifiers, fixed instants, fixed
// amounts and a signature from a fixed Ed25519 seed. Nothing about it comes
// from a clock, a random source or a database, so its bytes are reproducible
// on any machine - the same property internal/data/ledger/evidence's own
// golden fixture rests on.
var (
	goldenTenant = uuid.MustParse("66666666-6666-6666-6666-666666666666")
	goldenRunID  = "run-golden-0001"
	goldenFrom   = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	goldenTo     = time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	goldenSchema = evidence.SchemaRelease{Version: 1, Digest: strings.Repeat("g", 64)}
	goldenEpoch  = uuid.MustParse("77777777-7777-7777-7777-777777777777")
	goldenCorr   = uuid.MustParse("88888888-8888-8888-8888-888888888888")
)

func goldenKeyDirectory(t testing.TB) checkpoint.KeyDirectory {
	t.Helper()
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: testKeyID, PublicKey: testSigner(t).PublicKey(), NotBefore: keyValidFrom,
	})
}

// checkpointRevokedDirectory reports the golden signing key as having been
// revoked before the golden epoch was ever signed, so a directory built from
// it refuses the epoch's signature outright.
func checkpointRevokedDirectory(t testing.TB) checkpoint.KeyDirectory {
	t.Helper()
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: testKeyID, PublicKey: testSigner(t).PublicKey(),
		NotBefore: keyValidFrom, RevokedAt: keyValidFrom,
	})
}

// goldenContent assembles the pure evidence.Content the golden test pins: one
// reconciling run - register 1000.00, bank file 800.00, tax liability
// 200.00, filing acknowledgment 200.00 - with a real hash chain and a real
// checkpoint signature.
func goldenContent(t testing.TB) evidence.Content {
	t.Helper()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("chain registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)
	streamKey := auditpack.StreamKey(goldenRunID)

	lines := []struct {
		kind   auditpack.TotalKind
		amount string
	}{
		{auditpack.KindRegister, "1000.00"},
		{auditpack.KindBankFile, "800.00"},
		{auditpack.KindTaxLiability, "200.00"},
		{auditpack.KindFilingAcknowledgment, "200.00"},
	}

	var digests []evidence.EventDigest
	var events []evidence.Event
	for i, line := range lines {
		seq := int64(i + 1)
		amount, err := values.NewDecimal(line.amount, 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("decimal %s: %v", line.amount, err)
		}
		payload, err := auditpack.EncodeLine(goldenRunID, line.kind, amount, "USD")
		if err != nil {
			t.Fatalf("encode line %s: %v", line.kind, err)
		}
		algorithm, digest, length, err := (datalogger.SHA256Digester{}).Digest(payload, auditpack.LineSchemaRef)
		if err != nil {
			t.Fatalf("digest line %s: %v", line.kind, err)
		}
		eventID := uuid.MustParse(fmt.Sprintf("55555555-5555-5555-5555-%012d", seq))
		recordedAt := goldenFrom.Add(time.Duration(seq) * time.Minute)
		events = append(events, evidence.Event{
			Tenant: goldenTenant, StreamKey: streamKey, Sequence: seq, EventID: eventID,
			AssertionClass: datalogger.TransactionFact, SourceRef: auditpack.SourceRef, SchemaRef: auditpack.LineSchemaRef,
			Payload: payload, CanonicalLength: length, Digest: digest, DigestAlgorithm: algorithm,
			OccurredAt: goldenFrom, EffectiveAt: goldenFrom, RecordedAt: recordedAt,
			CorrelationID: goldenCorr, IdempotencyKey: fmt.Sprintf("golden-%s-%d", line.kind, seq),
		})
		digests = append(digests, evidence.EventDigest{Sequence: seq, EventID: eventID, Digest: digest})
	}

	links, err := digester.Fold(streamKey, digests)
	if err != nil {
		t.Fatalf("fold %s: %v", streamKey, err)
	}
	last := links[len(links)-1]
	stream := evidence.Stream{
		StreamKey: streamKey,
		Head: evidence.StreamHead{
			StreamKey: streamKey, Sequence: last.Sequence,
			ChainHash: last.ChainHash, ChainAlgorithm: last.Algorithm,
		},
		Links: links, Digests: digests, Events: events,
	}

	heads := []evidence.StreamHead{stream.Head}
	root, err := checkpoint.ComputeRootDigest(heads)
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	epoch := evidence.Epoch{
		SchemaVersion: checkpoint.ManifestSchemaVersion, Tenant: goldenTenant,
		EpochID: goldenEpoch, EpochNumber: 1, Schema: goldenSchema,
		Streams: heads, RootDigest: root, RootDigestAlgorithm: checkpoint.DigestAlgorithm,
		CoversFrom: goldenFrom, CoversTo: goldenTo, CreatedAt: goldenTo,
	}

	return evidence.Content{
		Tenant: goldenTenant, CoversFrom: goldenFrom, CoversTo: goldenTo, Schema: goldenSchema,
		Streams: []evidence.Stream{stream}, Epochs: []evidence.Epoch{signGoldenEpoch(t, epoch)},
	}
}

func signGoldenEpoch(t testing.TB, epoch evidence.Epoch) evidence.Epoch {
	t.Helper()
	signer := testSigner(t)
	dir := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: testKeyID, PublicKey: signer.PublicKey(), NotBefore: keyValidFrom,
	})
	epoch.Signature = nil
	signed, err := checkpoint.Sign(epoch, signer, dir)
	if err != nil {
		t.Fatalf("sign epoch %d: %v", epoch.EpochNumber, err)
	}
	return signed
}

// goldenPackage builds the golden auditor package end to end: resolve,
// reconcile, build.
func goldenPackage(t testing.TB) auditpack.Package {
	t.Helper()
	content := goldenContent(t)
	totals, err := auditpack.ResolveFromContent(content, goldenTenant, goldenRunID)
	if err != nil {
		t.Fatalf("resolve golden totals: %v", err)
	}
	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile golden totals: %v", err)
	}
	if !decision.OK() {
		t.Fatalf("golden totals do not reconcile: %v", decision.Err())
	}
	pkg, err := auditpack.Build(content, totals, decision)
	if err != nil {
		t.Fatalf("build golden package: %v", err)
	}
	return pkg
}
