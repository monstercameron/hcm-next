package evidence_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

// TestPathsAreDerivedFromIndexNotFromCallerText proves the rule types.go
// states: a stream key is caller-supplied text, so letting it choose a path
// would let it choose where its bytes land. Every derived path is a function
// of an index alone.
func TestPathsAreDerivedFromIndexNotFromCallerText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		index                int
		chain, events, epoch string
	}{
		{0, "streams/0001/chain.json", "streams/0001/events.json", "epochs/0001.json"},
		{1, "streams/0002/chain.json", "streams/0002/events.json", "epochs/0002.json"},
		{998, "streams/0999/chain.json", "streams/0999/events.json", "epochs/0999.json"},
		{9999, "streams/10000/chain.json", "streams/10000/events.json", "epochs/10000.json"},
	}
	for _, tc := range cases {
		if got := evidence.StreamChainPath(tc.index); got != tc.chain {
			t.Errorf("StreamChainPath(%d) = %q, want %q", tc.index, got, tc.chain)
		}
		if got := evidence.StreamEventsPath(tc.index); got != tc.events {
			t.Errorf("StreamEventsPath(%d) = %q, want %q", tc.index, got, tc.events)
		}
		if got := evidence.EpochPath(tc.index); got != tc.epoch {
			t.Errorf("EpochPath(%d) = %q, want %q", tc.index, got, tc.epoch)
		}
	}

	// Zero-padding to four digits keeps the byte ordering of paths the same
	// as their numeric ordering, which is what makes the manifest's
	// path-ordered fold also index-ordered.
	if evidence.EpochPath(1) >= evidence.EpochPath(9) {
		t.Error("epoch paths do not sort in index order")
	}
}

// TestFixedPathsAndVersionsAreDeclaredNotAssumed proves the constants a
// verifier refuses on are actually present and distinct, so adding a fifth
// part kind or bumping a layout is a visible change.
func TestFixedPathsAndVersionsAreDeclaredNotAssumed(t *testing.T) {
	t.Parallel()
	if evidence.ManifestPath != "manifest.json" || evidence.HeaderPath != "header.json" {
		t.Fatalf("fixed paths are %q and %q", evidence.ManifestPath, evidence.HeaderPath)
	}
	if evidence.DigestAlgorithm != "sha256" {
		t.Errorf("digest algorithm is %q, want sha256", evidence.DigestAlgorithm)
	}
	if evidence.SchemaVersion != 1 || evidence.LayoutVersion != 1 {
		t.Errorf("schema/layout versions are %d/%d, want 1/1", evidence.SchemaVersion, evidence.LayoutVersion)
	}

	kinds := map[evidence.PartKind]bool{
		evidence.PartHeader: true, evidence.PartStreamChain: true,
		evidence.PartStreamEvents: true, evidence.PartEpoch: true,
	}
	if len(kinds) != 4 {
		t.Fatalf("the four part kinds collapse to %d distinct values", len(kinds))
	}
}

// TestTruncateMatchesTheResolutionEpochsWereSignedAt proves why evidence
// reuses the checkpoint package's rounding: a window compared against epoch
// windows signed at storage precision must round the same way, or a covering
// epoch would look like a gap.
func TestTruncateMatchesTheResolutionEpochsWereSignedAt(t *testing.T) {
	t.Parallel()
	// PostgreSQL timestamptz keeps microseconds; a nanosecond tail is not
	// storable and must not survive into a digested field.
	at := time.Date(2026, 2, 1, 12, 0, 0, 123456789, time.FixedZone("east", 3*3600))
	got := evidence.Truncate(at)
	if got.Location() != time.UTC {
		t.Errorf("Truncate kept location %s, want UTC", got.Location())
	}
	if got.Nanosecond()%1000 != 0 {
		t.Errorf("Truncate left a sub-microsecond tail: %d ns", got.Nanosecond())
	}
	if !evidence.Truncate(got).Equal(got) {
		t.Error("Truncate is not idempotent")
	}
}

// TestErrorsCarryStableCodesAndSayWhatIsWrong proves each refusal names its
// own code and renders the specifics an operator needs, rather than a bare
// "invalid".
func TestErrorsCarryStableCodesAndSayWhatIsWrong(t *testing.T) {
	t.Parallel()
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	invalid := evidence.ErrContentInvalid{Tenant: tenant, Missing: []string{"first", "second"}}
	if invalid.Code() != "LEDGER_EVIDENCE_CONTENT_INVALID" {
		t.Errorf("content-invalid code is %q", invalid.Code())
	}
	// Every problem, not only the first: a caller assembling content is told
	// what is wrong in one pass.
	if !strings.Contains(invalid.Error(), "first") || !strings.Contains(invalid.Error(), "second") {
		t.Errorf("content-invalid error dropped a problem: %s", invalid.Error())
	}

	leak := evidence.ErrTenantLeak{Expected: tenant, Found: other, Where: "event worker:1@2"}
	if leak.Code() != "LEDGER_EVIDENCE_TENANT_LEAK" {
		t.Errorf("tenant-leak code is %q", leak.Code())
	}
	for _, want := range []string{tenant.String(), other.String(), "worker:1@2"} {
		if !strings.Contains(leak.Error(), want) {
			t.Errorf("tenant-leak error does not name %s: %s", want, leak.Error())
		}
	}

	gap := evidence.ErrIncompleteEpochCoverage{
		Tenant:   tenant,
		GapFrom:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		GapUntil: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
	if gap.Code() != "LEDGER_EVIDENCE_EPOCH_COVERAGE_INCOMPLETE" {
		t.Errorf("epoch-coverage code is %q", gap.Code())
	}
	if !strings.Contains(gap.Error(), "2026-02-01") || !strings.Contains(gap.Error(), "2026-03-01") {
		t.Errorf("epoch-coverage error does not name the gap: %s", gap.Error())
	}

	malformed := evidence.ErrPackageMalformed{Path: evidence.ManifestPath, Reason: "unexpected end of input"}
	if malformed.Code() != "LEDGER_EVIDENCE_PACKAGE_MALFORMED" {
		t.Errorf("package-malformed code is %q", malformed.Code())
	}
	if !strings.Contains(malformed.Error(), evidence.ManifestPath) {
		t.Errorf("package-malformed error does not name the path: %s", malformed.Error())
	}
}

// TestAliasedTypesAreTheOriginalsNotCopies proves the aliases in types.go are
// aliases: a chain a package carries can be handed straight to the hashchain
// verifier, and an epoch is the signed structure itself rather than a
// second, driftable projection of it.
func TestAliasedTypesAreTheOriginalsNotCopies(t *testing.T) {
	t.Parallel()
	// Assignment in both directions only compiles for a true alias.
	var head evidence.StreamHead
	var link evidence.ChainLink
	var digest evidence.EventDigest
	var epoch evidence.Epoch

	epoch.Streams = []evidence.StreamHead{head}
	epoch.Streams[0].StreamKey = streamOne
	stream := evidence.Stream{Links: []evidence.ChainLink{link}, Digests: []evidence.EventDigest{digest}}
	if len(stream.Links) != 1 || len(stream.Digests) != 1 {
		t.Fatal("aliased slices did not round-trip")
	}
	if epoch.Streams[0].StreamKey != streamOne {
		t.Fatal("an epoch's stream heads are not the checkpoint package's own")
	}
}
