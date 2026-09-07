package evidence

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
)

// Querier is the minimal database capability an export needs. It is
// internal/data/ledger.Querier itself, so whatever handle a caller already
// reads the ledger through satisfies this one unchanged.
type Querier = datalogger.Querier

// Reused value types. An evidence package carries the same stream heads a
// checkpoint attests to, the same chain links internal/data/ledger/hashchain
// records, and the same event references a correction names. Aliasing them
// rather than redeclaring keeps one definition of each and lets a verifier
// hand a package's chain straight to [hashchain.Digester.VerifyLinks].
type (
	StreamHead    = checkpoint.StreamHead
	SchemaRelease = checkpoint.SchemaRelease
	ChainLink     = hashchain.ChainedLink
	EventDigest   = hashchain.EventDigest
	EventRef      = datalogger.EventRef
	KeyDirectory  = checkpoint.KeyDirectory
	// Epoch is one signed checkpoint epoch (LEDGER-010) as a package carries
	// it. It is [checkpoint.Manifest] itself: an epoch part's bytes are the
	// signed structure, and re-declaring it here would create a second,
	// driftable definition of something this package must never alter.
	Epoch = checkpoint.Manifest
	// Digester recomputes an event's payload digest. It is
	// internal/data/ledger.Digester, so a cell hands [WithEventDigester] the
	// very digester it appended with.
	Digester = datalogger.Digester
)

// SchemaVersion is the version of the manifest's own canonical projection.
// It is inside the digested bytes, so a package built under one version can
// never be verified as though it were another.
const SchemaVersion = 1

// LayoutVersion is the version of the path layout described in the package
// doc. It is recorded in the header and the manifest so a verifier that
// meets a layout it does not know refuses rather than guesses.
const LayoutVersion = 1

// DigestAlgorithm names the algorithm every digest in a package uses.
const DigestAlgorithm = "sha256"

// Fixed paths. Every other path is derived by [StreamChainPath],
// [StreamEventsPath] and [EpochPath] from an index, never from a stream key
// or an epoch identifier: a stream key is caller-supplied text, and letting
// it choose a path would let it choose where its bytes land.
const (
	ManifestPath = "manifest.json"
	HeaderPath   = "header.json"
)

// StreamChainPath is the path of the nth exported stream's chain part,
// numbered from zero in stream-key order.
func StreamChainPath(index int) string { return numberedPath("streams/", "/chain.json", index+1) }

// StreamEventsPath is the path of the nth exported stream's covered-event
// part, numbered from zero in stream-key order.
func StreamEventsPath(index int) string { return numberedPath("streams/", "/events.json", index+1) }

// EpochPath is the path of the nth exported checkpoint epoch, numbered from
// zero in epoch-number order.
func EpochPath(index int) string { return numberedPath("epochs/", ".json", index+1) }

func numberedPath(prefix, suffix string, number int) string {
	// Same shape as fmt.Sprintf("%s%04d%s", ...) without the formatter: the
	// number is zero-padded to four characters, the sign counting as one.
	var digits [24]byte
	raw := strconv.AppendInt(digits[:0], int64(number), 10)
	var b strings.Builder
	b.Grow(len(prefix) + len(raw) + 4 + len(suffix))
	b.WriteString(prefix)
	body := raw
	if raw[0] == '-' {
		b.WriteByte('-')
		body = raw[1:]
	}
	for pad := 4 - len(raw); pad > 0; pad-- {
		b.WriteByte('0')
	}
	b.Write(body)
	b.WriteString(suffix)
	return b.String()
}

// PartKind says what a path in the package holds. It is inside the digested
// manifest, so a part cannot be re-purposed by moving it.
type PartKind string

// The four part kinds. There is no fifth.
const (
	PartHeader       PartKind = "HEADER"
	PartStreamChain  PartKind = "STREAM_CHAIN"
	PartStreamEvents PartKind = "STREAM_EVENTS"
	PartEpoch        PartKind = "EPOCH"
)

// Part is one path in the package as the manifest describes it.
type Part struct {
	Path      string
	Kind      PartKind
	Length    int
	Digest    string
	Algorithm string
}

// Manifest describes a whole evidence package: what it covers, what parts it
// is made of, and one digest over all of them.
type Manifest struct {
	SchemaVersion int
	LayoutVersion int
	Tenant        uuid.UUID
	// CoversFrom/CoversTo is the half-open recorded-time window the package
	// makes its claim over.
	CoversFrom time.Time
	CoversTo   time.Time
	Schema     SchemaRelease
	// Parts is every path in the package except [ManifestPath] itself,
	// ordered by path.
	Parts []Part
	// Digest folds the fields above and every part row into one value. It is
	// recomputed and compared by [Verify], never trusted as given.
	Digest          string
	DigestAlgorithm string
}

// Event is one covered ledger event as the package carries it: the whole
// envelope the ledger recorded, including the correction target that makes
// its lineage checkable.
type Event struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	AssertionClass  datalogger.AssertionClass
	Authority       string
	SourceRef       string
	SchemaRef       string
	Payload         []byte
	ArtifactRef     string
	CanonicalLength int
	Digest          string
	DigestAlgorithm string
	OccurredAt      time.Time
	EffectiveAt     time.Time
	RecordedAt      time.Time
	CorrelationID   uuid.UUID
	CausationID     uuid.UUID
	IdempotencyKey  string
	// Corrects is the event this one supersedes, or nil.
	Corrects *EventRef
}

// Stream is one covered stream: its whole hash chain from genesis to the
// head as of the window's end, and the envelopes of the events inside the
// window.
type Stream struct {
	StreamKey string
	// Head is the stream's position as of the window's end, in the same
	// shape a checkpoint attests to.
	Head StreamHead
	// Links and Digests run from sequence 1 to Head.Sequence with no gaps.
	// The prefix before the window is what makes the chain checkable at all;
	// it carries digests, never payloads.
	Links   []ChainLink
	Digests []EventDigest
	// Events are the covered envelopes, ordered by sequence ascending. It is
	// legitimately empty for a stream that only supplies chain prefix.
	Events []Event
}

// Content is everything a package is built from. [Export] reads it from
// PostgreSQL; [Build] turns it into bytes. Splitting them is what lets the
// golden test pin a digest over a fixture that never touches a database.
type Content struct {
	Tenant     uuid.UUID
	CoversFrom time.Time
	CoversTo   time.Time
	Schema     SchemaRelease
	// Streams are ordered by stream key; [Build] sorts them anyway so a
	// caller cannot change the digest by changing the read order.
	Streams []Stream
	// Epochs are the signed checkpoint epochs whose windows intersect the
	// covered window, ordered by epoch number.
	Epochs []Epoch
}

// ErrContentInvalid reports a package that could not be built because what
// it was given is not evidence. Missing lists every problem rather than only
// the first, so a caller assembling content is told everything at once.
type ErrContentInvalid struct {
	Tenant  uuid.UUID
	Missing []string
}

func (ErrContentInvalid) Code() string { return "LEDGER_EVIDENCE_CONTENT_INVALID" }

func (e ErrContentInvalid) Error() string {
	return e.Code() + ": tenant " + e.Tenant.String() + ": " + strings.Join(e.Missing, "; ")
}

// ErrTenantLeak reports a row naming a tenant other than the one being
// exported. It is the fail-closed backstop behind row level security: RLS
// makes such a row unreadable, and this makes an unreadable row that somehow
// arrived anyway refuse to become evidence.
type ErrTenantLeak struct {
	Expected uuid.UUID
	Found    uuid.UUID
	Where    string
}

func (ErrTenantLeak) Code() string { return "LEDGER_EVIDENCE_TENANT_LEAK" }

func (e ErrTenantLeak) Error() string {
	return e.Code() + ": " + e.Where + " names tenant " + e.Found.String() + ", but this package covers tenant " + e.Expected.String()
}

// ErrIncompleteEpochCoverage reports an export whose window is not wholly
// covered by signed checkpoint epochs. A package whose events no signature
// reaches is a report, not evidence, so it is refused at export rather than
// handed over with a caveat.
type ErrIncompleteEpochCoverage struct {
	Tenant   uuid.UUID
	GapFrom  time.Time
	GapUntil time.Time
}

func (ErrIncompleteEpochCoverage) Code() string { return "LEDGER_EVIDENCE_EPOCH_COVERAGE_INCOMPLETE" }

func (e ErrIncompleteEpochCoverage) Error() string {
	return e.Code() + ": tenant " + e.Tenant.String() + " has no signed checkpoint epoch covering [" +
		e.GapFrom.UTC().Format(time.RFC3339Nano) + ", " + e.GapUntil.UTC().Format(time.RFC3339Nano) + ")"
}

// ErrPackageMalformed reports package bytes that could not be read as a
// package at all: an absent or unparseable manifest. Everything a package
// can be wrong about once it parses is a [Finding], not an error.
type ErrPackageMalformed struct {
	Path   string
	Reason string
}

func (ErrPackageMalformed) Code() string { return "LEDGER_EVIDENCE_PACKAGE_MALFORMED" }

func (e ErrPackageMalformed) Error() string {
	return e.Code() + ": " + e.Path + ": " + e.Reason
}

// Truncate normalizes an instant to UTC at the resolution PostgreSQL's
// timestamptz keeps. It is [checkpoint.Truncate]: a package's window is
// compared against epoch windows that were signed at that resolution, so the
// two must round the same way or a covering epoch would look like a gap.
func Truncate(t time.Time) time.Time { return checkpoint.Truncate(t) }
