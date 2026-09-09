package evidence_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// TestTodo_LEDGER_012 is the primary acceptance test: an export over a real
// ledger produces a package that an offline verifier - holding only the
// bytes and the epoch public keys - accepts, and that package actually
// contains the covered events, their chains, the signed epochs, the schema
// release and a manifest digest over every part.
func TestTodo_LEDGER_012(t *testing.T) {
	f := newFixture(t)
	epoch := f.seedCoveredWindow(t)

	pkg := f.mustExport(t, f.request())

	t.Run("the package is a path-to-bytes layout with no archive", func(t *testing.T) {
		want := []string{
			"epochs/0001.json",
			"header.json",
			"manifest.json",
			"streams/0001/chain.json",
			"streams/0001/events.json",
			"streams/0002/chain.json",
			"streams/0002/events.json",
		}
		if got := pkg.Paths(); !equalStrings(got, want) {
			t.Fatalf("package paths = %v, want %v", got, want)
		}
		if files := pkg.Files(); len(files) != len(want) {
			t.Fatalf("Files() holds %d paths, want %d", len(files), len(want))
		}
		if pkg.TotalBytes() == 0 {
			t.Fatal("the package reports zero bytes")
		}
	})

	t.Run("the manifest covers every part and the stated window", func(t *testing.T) {
		m := pkg.Manifest
		if m.Tenant != f.tenant {
			t.Errorf("manifest tenant = %s, want %s", m.Tenant, f.tenant)
		}
		if !m.CoversFrom.Equal(windowFrom) || !m.CoversTo.Equal(windowTo) {
			t.Errorf("manifest covers [%s, %s), want [%s, %s)", m.CoversFrom, m.CoversTo, windowFrom, windowTo)
		}
		if m.Schema != f.release {
			t.Errorf("manifest schema release = %+v, want %+v", m.Schema, f.release)
		}
		if m.Digest == "" || m.DigestAlgorithm != evidence.DigestAlgorithm {
			t.Errorf("manifest digest = %q/%q, want a %s digest", m.Digest, m.DigestAlgorithm, evidence.DigestAlgorithm)
		}
		// Every path except the manifest itself carries a part row; a digest
		// can never cover its own bytes.
		if len(m.Parts) != len(pkg.Paths())-1 {
			t.Fatalf("manifest lists %d parts for %d paths", len(m.Parts), len(pkg.Paths()))
		}
		byKind := map[evidence.PartKind]int{}
		for _, part := range m.Parts {
			byKind[part.Kind]++
			if part.Length == 0 || part.Digest == "" {
				t.Errorf("part %s carries length %d and digest %q", part.Path, part.Length, part.Digest)
			}
		}
		for kind, want := range map[evidence.PartKind]int{
			evidence.PartHeader: 1, evidence.PartStreamChain: 2,
			evidence.PartStreamEvents: 2, evidence.PartEpoch: 1,
		} {
			if byKind[kind] != want {
				t.Errorf("package holds %d %s parts, want %d", byKind[kind], kind, want)
			}
		}
	})

	t.Run("the exported content is the covered slice and its chain prefix", func(t *testing.T) {
		content, err := evidence.NewExporter().Read(context.Background(), f.db.Conn, f.request())
		if err != nil {
			t.Fatalf("read content: %v", err)
		}
		if len(content.Streams) != 2 {
			t.Fatalf("content covers %d streams, want 2", len(content.Streams))
		}
		if content.Streams[0].StreamKey != streamOne || content.Streams[1].StreamKey != streamTwo {
			t.Fatalf("streams are not in key order: %s, %s", content.Streams[0].StreamKey, content.Streams[1].StreamKey)
		}
		if got := len(content.Streams[0].Events); got != 2 {
			t.Errorf("stream one carries %d covered events, want 2", got)
		}
		if got := len(content.Streams[0].Links); got != 2 {
			t.Errorf("stream one carries %d chain links, want 2", got)
		}
		if content.Streams[0].Head.Sequence != 2 || content.Streams[0].Head.ChainHash == "" {
			t.Errorf("stream one head = %+v, want sequence 2 with a chain hash", content.Streams[0].Head)
		}
		if len(content.Epochs) != 1 || content.Epochs[0].EpochNumber != epoch.EpochNumber {
			t.Fatalf("content carries %d epochs, want epoch %d", len(content.Epochs), epoch.EpochNumber)
		}
		if content.Epochs[0].Signature == nil {
			t.Fatal("the exported epoch carries no signature")
		}
	})

	t.Run("an offline verifier accepts it with nothing but bytes and keys", func(t *testing.T) {
		report := mustVerify(t, pkg.Files(), f.liveKeyDirectory())
		if report.StreamsVerified != 2 {
			t.Errorf("verified %d streams, want 2", report.StreamsVerified)
		}
		if report.ChainLinksVerified != 3 {
			t.Errorf("verified %d chain links, want 3", report.ChainLinksVerified)
		}
		if report.EventsVerified != 3 {
			t.Errorf("verified %d events, want 3", report.EventsVerified)
		}
		if report.EpochsVerified != 1 {
			t.Errorf("verified %d epochs, want 1", report.EpochsVerified)
		}
		if err := report.Err(); err != nil {
			t.Errorf("Err() on a clean report = %v, want nil", err)
		}
	})

	t.Run("the recorded event digests reproduce under the cell's digester", func(t *testing.T) {
		// The opt-in binding: with the profile the cell appended under, the
		// payload itself is checked and not only its recorded digest.
		mustVerify(t, pkg.Files(), f.liveKeyDirectory(),
			evidence.WithEventDigester(datalogger.SHA256Digester{}))
	})

	t.Run("verifying the assembled package checks the serialized bytes", func(t *testing.T) {
		if report := evidence.VerifyPackage(pkg, f.liveKeyDirectory()); !report.OK() {
			t.Fatalf("VerifyPackage reported %v", kinds(report))
		}
	})

	t.Run("an export refuses a window no signed epoch covers", func(t *testing.T) {
		// The seeded checkpoint stops at windowTo; a window running past it
		// would be a report, not evidence.
		_, err := evidence.NewExporter().Export(context.Background(), f.db.Conn, evidence.Request{
			Tenant: f.tenant, From: windowFrom, To: checkpointTwo, Schema: f.release,
		})
		if !isIncompleteCoverage(err) {
			t.Fatalf("export over an unattested window = %v, want ErrIncompleteEpochCoverage", err)
		}
	})
}

// TestTodo_LEDGER_012_Golden pins the exact bytes a package commits to. The
// per-part digests and the manifest digest over them are recorded in
// testdata; a change to the framing, the field order, the path layout or the
// projection changes them, and that change has to be a deliberate edit of
// the golden file rather than a silent break in every package an auditor
// already holds.
func TestTodo_LEDGER_012_Golden(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build golden package: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "manifest_digest=%s\n", pkg.Manifest.Digest)
	for _, part := range pkg.Manifest.Parts {
		fmt.Fprintf(&b, "part=%s kind=%s length=%d digest=%s\n",
			part.Path, part.Kind, part.Length, part.Digest)
	}
	got := b.String()

	golden := filepath.Join("testdata", "golden_package.txt")
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
		t.Fatalf("evidence package bytes changed.\n got:\n%s\nwant:\n%s", got, want)
	}

	// The pinned bytes are not merely stable: they verify.
	mustVerify(t, pkg.Files(), goldenKeyDirectory(t))
}

// TestTodo_LEDGER_012_Race proves the property the whole design rests on:
// the package is a pure function of (tenant, window, ledger state, schema
// release). Concurrent exports of one window are byte-identical, so a
// difference between two copies of a package is always a difference in the
// evidence and never in when or by whom it was taken.
func TestTodo_LEDGER_012_Race(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)

	// A pgx connection is not safe for concurrent use, so each exporter gets
	// its own session. That is also the honest shape of the claim: separate
	// sessions, separate snapshots, identical bytes.
	const exports = 8
	conns := make([]*pgxadapter.Conn, exports)
	for i := range conns {
		conns[i] = f.db.NewConn(t)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([]map[string][]byte, 0, exports)
		failed  []error
	)
	for i := range exports {
		wg.Add(1)
		go func(conn *pgxadapter.Conn) {
			defer wg.Done()
			pkg, err := evidence.NewExporter().Export(context.Background(), conn, f.request())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			results = append(results, pkg.Files())
		}(conns[i])
	}
	wg.Wait()
	for _, err := range failed {
		t.Fatalf("concurrent export: %v", err)
	}
	if len(results) != exports {
		t.Fatalf("%d exports returned, want %d", len(results), exports)
	}

	first := results[0]
	for i, other := range results[1:] {
		if len(other) != len(first) {
			t.Fatalf("export %d holds %d paths, want %d", i+1, len(other), len(first))
		}
		for path, raw := range first {
			if !bytes.Equal(other[path], raw) {
				t.Fatalf("export %d differs from export 0 at %s", i+1, path)
			}
		}
	}
}

// TestTodo_LEDGER_012_Security proves the refusals that make a package
// evidence rather than a report: one altered byte fails verification naming
// the part it was altered in, and another tenant's event is refused both at
// export and at verification.
func TestTodo_LEDGER_012_Security(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)
	pkg := f.mustExport(t, f.request())
	dir := f.liveKeyDirectory()

	t.Run("one altered byte fails verification and names the part", func(t *testing.T) {
		for _, path := range pkg.Paths() {
			if path == evidence.ManifestPath {
				continue
			}
			t.Run(path, func(t *testing.T) {
				report := evidence.Verify(mutate(t, pkg.Files(), path, flipOneByte), dir)
				if report.OK() {
					t.Fatal("a package with an altered byte verified")
				}
				named := false
				for _, finding := range report.Findings {
					if finding.Part == path {
						named = true
					}
				}
				if !named {
					t.Fatalf("no finding names %s; got %v", path, report.Findings)
				}
				if !report.Has(evidence.FindingTamperedPart) {
					t.Fatalf("altering %s reported %v, want a tampered part", path, kinds(report))
				}
			})
		}
	})

	t.Run("re-stating the altered part in the manifest fails at the manifest digest", func(t *testing.T) {
		// The stronger adversary: one who also rewrites the manifest's row
		// for the part it altered. The manifest's own fold over every row no
		// longer reproduces.
		files := mutate(t, pkg.Files(), evidence.HeaderPath, flipOneByte)
		altered := files[evidence.HeaderPath]
		files[evidence.ManifestPath] = rewriteJSON(t, files[evidence.ManifestPath], func(doc map[string]any) {
			for _, entry := range doc["parts"].([]any) {
				row := entry.(map[string]any)
				if row["path"] == evidence.HeaderPath {
					row["length"] = float64(len(altered))
					row["digest"] = "0" + strings.Repeat("f", 63)
				}
			}
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingManifestDigest) {
			t.Fatalf("a rewritten manifest reported %v, want a manifest digest mismatch", kinds(report))
		}
	})

	t.Run("another tenant's events are refused at export", func(t *testing.T) {
		other := uuid.New()
		f.seedTenant(t, other)
		f.appendLinked(t, other, streamOne, 0, recordedFirst)

		content, err := evidence.NewExporter().Read(context.Background(), f.db.Conn, f.request())
		if err != nil {
			t.Fatalf("read content: %v", err)
		}
		for _, stream := range content.Streams {
			for _, event := range stream.Events {
				if event.Tenant != f.tenant {
					t.Fatalf("export carried tenant %s into a package for %s", event.Tenant, f.tenant)
				}
			}
		}
		// The other tenant's identical stream key did not merge into this
		// tenant's chain: the head is still what this tenant recorded.
		if content.Streams[0].Head.Sequence != 2 {
			t.Fatalf("stream one head = %d after another tenant appended, want 2", content.Streams[0].Head.Sequence)
		}

		// The fail-closed backstop behind row level security: a row that
		// somehow arrived anyway refuses to become evidence.
		leaky := content
		leaky.Streams = append([]evidence.Stream(nil), content.Streams...)
		events := append([]evidence.Event(nil), leaky.Streams[0].Events...)
		events[0].Tenant = other
		leaky.Streams[0].Events = events
		if _, err := evidence.Build(leaky); !isTenantLeak(err) {
			t.Fatalf("Build with a foreign event = %v, want ErrTenantLeak", err)
		}
	})

	t.Run("another tenant's event is refused at verification", func(t *testing.T) {
		other := uuid.New().String()
		files := mutate(t, pkg.Files(), evidence.StreamEventsPath(0), func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["events"].([]any)[0].(map[string]any)["tenant"] = other
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingTenantLeak) {
			t.Fatalf("a foreign event reported %v, want a tenant leak", kinds(report))
		}
	})

	t.Run("a package whose signing key the directory refuses fails closed", func(t *testing.T) {
		revoked := f.keyDirectory(time.Time{}, keyValidFrom)
		report := evidence.Verify(pkg.Files(), revoked)
		if !report.Has(evidence.FindingSignatureMismatch) {
			t.Fatalf("a revoked key reported %v, want a signature mismatch", kinds(report))
		}
		// With no epoch left standing, nothing attests to the window either.
		if !report.Has(evidence.FindingMissingEpochCoverage) {
			t.Fatalf("a revoked key reported %v, want missing epoch coverage too", kinds(report))
		}
	})

	t.Run("verification needs no database handle at all", func(t *testing.T) {
		// The signature of the offline entry point is the proof: bytes and a
		// key directory, and nothing that could reach PostgreSQL.
		var verify func(map[string][]byte, evidence.KeyDirectory, ...evidence.VerifyOption) evidence.Report = evidence.Verify
		if report := verify(pkg.Files(), dir); !report.OK() {
			t.Fatalf("offline verification reported %v", kinds(report))
		}
	})
}

// TestTodo_LEDGER_012_Mutation drives one targeted forgery per finding kind
// and asserts the verifier names that exact failure. A verifier that only
// said "invalid" would tell an auditor nothing about what to do next, so the
// typed finding is the behaviour under test.
func TestTodo_LEDGER_012_Mutation(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)
	pkg := f.mustExport(t, f.request())
	dir := f.liveKeyDirectory()

	cases := []struct {
		name  string
		files func(t *testing.T) map[string][]byte
		want  evidence.FindingKind
	}{
		{
			name: "a listed part removed from the package",
			files: func(*testing.T) map[string][]byte {
				files := pkg.Files()
				delete(files, evidence.EpochPath(0))
				return files
			},
			want: evidence.FindingMissingPart,
		},
		{
			name: "bytes no part row covers",
			files: func(*testing.T) map[string][]byte {
				files := pkg.Files()
				files["streams/0003/events.json"] = []byte("{}\n")
				return files
			},
			want: evidence.FindingUnlistedPart,
		},
		{
			name: "a package with no manifest at all",
			files: func(*testing.T) map[string][]byte {
				files := pkg.Files()
				delete(files, evidence.ManifestPath)
				return files
			},
			want: evidence.FindingMalformedPart,
		},
		{
			name: "an event digest the chain no longer folds to its head",
			files: func(t *testing.T) map[string][]byte {
				return mutate(t, pkg.Files(), evidence.StreamChainPath(0), func(raw []byte) []byte {
					return rewriteJSON(t, raw, func(doc map[string]any) {
						doc["event_digests"].([]any)[0].(map[string]any)["digest"] = strings.Repeat("a", 64)
					})
				})
			},
			want: evidence.FindingBrokenChain,
		},
		{
			name: "a covered event the chain does not commit to",
			files: func(t *testing.T) map[string][]byte {
				return mutate(t, pkg.Files(), evidence.StreamEventsPath(0), func(raw []byte) []byte {
					return rewriteJSON(t, raw, func(doc map[string]any) {
						doc["events"].([]any)[0].(map[string]any)["digest"] = strings.Repeat("b", 64)
					})
				})
			},
			want: evidence.FindingTamperedEvent,
		},
		{
			name: "an event whose payload no longer reproduces its digest",
			files: func(t *testing.T) map[string][]byte {
				return mutate(t, pkg.Files(), evidence.StreamEventsPath(0), func(raw []byte) []byte {
					return rewriteJSON(t, raw, func(doc map[string]any) {
						// []byte crosses as base64; "Zm9yZ2Vk" is "forged".
						doc["events"].([]any)[0].(map[string]any)["payload"] = "Zm9yZ2Vk"
					})
				})
			},
			want: evidence.FindingTamperedEvent,
		},
		{
			name: "an epoch signature made over other content",
			files: func(t *testing.T) map[string][]byte {
				return mutate(t, pkg.Files(), evidence.EpochPath(0), func(raw []byte) []byte {
					return rewriteJSON(t, raw, func(doc map[string]any) {
						doc["Signature"].(map[string]any)["Value"] = strings.Repeat("0", 128)
					})
				})
			},
			want: evidence.FindingSignatureMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := evidence.Verify(tc.files(t), dir,
				evidence.WithEventDigester(datalogger.SHA256Digester{}))
			if !report.Has(tc.want) {
				t.Fatalf("reported %v, want %s", kinds(report), tc.want)
			}
		})
	}

	t.Run("an epoch that verifies but covers less than the window", func(t *testing.T) {
		// A properly signed epoch, so the signature check passes and what is
		// left is the coverage claim itself.
		short := f.epochPart(t, pkg, func(m *evidence.Epoch) {
			m.CoversTo = windowTo.AddDate(0, 0, -1)
			m.CreatedAt = m.CoversTo
		})
		report := evidence.Verify(short, dir)
		if !report.Has(evidence.FindingMissingEpochCoverage) {
			t.Fatalf("reported %v, want missing epoch coverage", kinds(report))
		}
		if report.Has(evidence.FindingSignatureMismatch) {
			t.Fatalf("reported a signature mismatch on a properly re-signed epoch: %v", kinds(report))
		}
	})

	t.Run("a covered stream no epoch in the package attests to", func(t *testing.T) {
		dropped := f.epochPart(t, pkg, func(m *evidence.Epoch) {
			m.Streams = m.Streams[:1]
			root, err := checkpoint.ComputeRootDigest(m.Streams)
			if err != nil {
				t.Fatalf("root digest: %v", err)
			}
			m.RootDigest = root
		})
		report := evidence.Verify(dropped, dir)
		if !report.Has(evidence.FindingUnattestedHead) {
			t.Fatalf("reported %v, want an unattested head", kinds(report))
		}
	})

	t.Run("an epoch attesting to a head the exported chain does not reproduce", func(t *testing.T) {
		forged := f.epochPart(t, pkg, func(m *evidence.Epoch) {
			heads := append([]evidence.StreamHead(nil), m.Streams...)
			heads[0].ChainHash = strings.Repeat("d", 64)
			m.Streams = heads
			root, err := checkpoint.ComputeRootDigest(heads)
			if err != nil {
				t.Fatalf("root digest: %v", err)
			}
			m.RootDigest = root
		})
		report := evidence.Verify(forged, dir)
		if !report.Has(evidence.FindingUnattestedHead) {
			t.Fatalf("reported %v, want an unattested head", kinds(report))
		}
	})
}

// FuzzTodo_LEDGER_012 mutates a sound package's bytes arbitrarily and
// asserts the two properties an offline verifier must hold under any input:
// it never panics, and it never reports a mutated part as sound. Verify is
// the one entry point an auditor points at bytes they did not produce, so
// hostile bytes are its normal input rather than an edge case.
func FuzzTodo_LEDGER_012(f *testing.F) {
	base, err := evidence.Build(goldenContent(f))
	if err != nil {
		f.Fatalf("build seed package: %v", err)
	}
	files := base.Files()
	paths := base.Paths()
	dir := goldenKeyDirectory(f)

	f.Add(0, 0, byte(1))
	f.Add(1, 40, byte(255))
	f.Add(3, 7, byte(0))
	f.Add(5, 120, byte(64))

	f.Fuzz(func(t *testing.T, pathIndex, offset int, value byte) {
		if pathIndex < 0 || offset < 0 {
			t.Skip()
		}
		path := paths[pathIndex%len(paths)]
		raw := append([]byte(nil), files[path]...)
		at := offset % len(raw)
		if raw[at] == value {
			t.Skip()
		}
		raw[at] = value

		mutated := make(map[string][]byte, len(files))
		for p, b := range files {
			mutated[p] = append([]byte(nil), b...)
		}
		mutated[path] = raw

		report := evidence.Verify(mutated, dir)
		// Every finding is renderable; a report an auditor cannot read is
		// not a report.
		for _, finding := range report.Findings {
			if finding.String() == "" {
				t.Fatalf("a finding of kind %q rendered as empty", finding.Kind)
			}
		}
		if path == evidence.ManifestPath {
			// The manifest is the one part no digest covers byte for byte -
			// it carries the digest - so a change to its insignificant
			// whitespace legitimately changes nothing. Its content is bound
			// by the fold over its part rows, which the other paths exercise.
			return
		}
		if report.OK() {
			t.Fatalf("a package with byte %d of %s set to %d verified as sound", at, path, value)
		}
		if !report.Has(evidence.FindingTamperedPart) {
			t.Fatalf("altering byte %d of %s reported %v, want a tampered part", at, path, kinds(report))
		}
	})
}

// ---- the pure golden fixture ---------------------------------------------

// The golden package is built from pinned values only: fixed identifiers,
// fixed instants, fixed digests and a signature from a fixed Ed25519 seed.
// Nothing about it comes from a clock, a random source or the migration
// tree, so its bytes are reproducible on any machine.
var (
	goldenTenant  = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	goldenEpochID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	goldenFrom    = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	goldenTo      = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	goldenSchema  = evidence.SchemaRelease{Version: 28, Digest: strings.Repeat("c", 64)}
)

func goldenKeyDirectory(t testing.TB) evidence.KeyDirectory {
	t.Helper()
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: testKeyID, PublicKey: testSigner(t).PublicKey(), NotBefore: keyValidFrom,
	})
}

// goldenContent assembles the pure Content the golden test pins: two streams
// whose chains are actually folded, three covered events, and one signed
// epoch attesting to both heads.
func goldenContent(t testing.TB) evidence.Content {
	t.Helper()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("chain registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	streams := []evidence.Stream{
		goldenStream(t, digester, streamOne, 2),
		goldenStream(t, digester, streamTwo, 1),
	}
	heads := make([]evidence.StreamHead, 0, len(streams))
	for _, s := range streams {
		heads = append(heads, s.Head)
	}
	root, err := checkpoint.ComputeRootDigest(heads)
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}

	epoch := evidence.Epoch{
		SchemaVersion: checkpoint.ManifestSchemaVersion,
		Tenant:        goldenTenant,
		EpochID:       goldenEpochID,
		EpochNumber:   1,
		Schema:        goldenSchema,
		Streams:       heads,
		RootDigest:    root, RootDigestAlgorithm: checkpoint.DigestAlgorithm,
		CoversFrom: goldenFrom, CoversTo: goldenTo, CreatedAt: goldenTo,
	}
	return evidence.Content{
		Tenant: goldenTenant, CoversFrom: goldenFrom, CoversTo: goldenTo,
		Schema:  goldenSchema,
		Streams: streams,
		Epochs:  []evidence.Epoch{signEpoch(t, epoch)},
	}
}

// goldenStream folds a real chain over pinned event digests, so the golden
// package's chains verify rather than merely being present.
func goldenStream(t testing.TB, digester *hashchain.Digester, streamKey string, count int) evidence.Stream {
	t.Helper()
	digests := make([]evidence.EventDigest, 0, count)
	for i := 1; i <= count; i++ {
		digests = append(digests, evidence.EventDigest{
			Sequence: int64(i),
			EventID:  uuid.MustParse(fmt.Sprintf("33333333-3333-3333-3333-%012d", pinnedID(streamKey, i))),
			Digest:   fmt.Sprintf("%064x", pinnedID(streamKey, i)),
		})
	}
	links, err := digester.Fold(streamKey, digests)
	if err != nil {
		t.Fatalf("fold %s: %v", streamKey, err)
	}

	events := make([]evidence.Event, 0, count)
	for i, d := range digests {
		payload := fmt.Appendf(nil, "%s-%d", streamKey, d.Sequence)
		events = append(events, evidence.Event{
			Tenant: goldenTenant, StreamKey: streamKey, Sequence: d.Sequence, EventID: d.EventID,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      "hcmnext:test", SchemaRef: schemaRef,
			Payload:         payload,
			CanonicalLength: len(payload),
			Digest:          d.Digest, DigestAlgorithm: evidence.DigestAlgorithm,
			OccurredAt: goldenFrom, EffectiveAt: goldenFrom,
			RecordedAt:     goldenFrom.AddDate(0, 0, i+1),
			CorrelationID:  uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			IdempotencyKey: fmt.Sprintf("%s#%d", streamKey, d.Sequence),
		})
	}

	last := links[len(links)-1]
	return evidence.Stream{
		StreamKey: streamKey,
		Head: evidence.StreamHead{
			StreamKey: streamKey, Sequence: last.Sequence,
			ChainHash: last.ChainHash, ChainAlgorithm: last.Algorithm,
		},
		Links: links, Digests: digests, Events: events,
	}
}

// pinnedID turns a stream key and a sequence into a small fixed number, so
// every identifier and digest in the golden fixture is derived rather than
// typed out and still never varies between runs.
func pinnedID(streamKey string, sequence int) int {
	base := 100
	if streamKey == streamTwo {
		base = 200
	}
	return base + sequence
}

// ---- forging helpers -----------------------------------------------------

// signEpoch signs (or re-signs) an epoch with the fixture key, so a test that
// edits an epoch exercises the binding it is aiming at rather than tripping
// over a signature that no longer matches for an unrelated reason.
func signEpoch(t testing.TB, epoch evidence.Epoch) evidence.Epoch {
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

// epochPart returns the package's bytes with its single epoch part replaced
// by an edited, validly re-signed epoch.
//
// The epoch part's bytes are the signed structure's own JSON, so the
// replacement is produced the same way the package produced it. The
// manifest's part row is deliberately left alone: the point of these cases
// is what the verifier concludes *beyond* "the bytes changed", and both
// findings are reported in one pass.
func (f fixture) epochPart(t *testing.T, pkg evidence.Package, edit func(*evidence.Epoch)) map[string][]byte {
	t.Helper()
	raw, ok := pkg.Part(evidence.EpochPath(0))
	if !ok {
		t.Fatal("the package has no epoch part")
	}
	var epoch evidence.Epoch
	if err := json.Unmarshal(raw, &epoch); err != nil {
		t.Fatalf("decode epoch part: %v", err)
	}
	edit(&epoch)

	body, err := json.Marshal(signEpoch(t, epoch))
	if err != nil {
		t.Fatalf("encode forged epoch: %v", err)
	}
	files := pkg.Files()
	files[evidence.EpochPath(0)] = append(body, '\n')
	return files
}

// ---- assertions ----------------------------------------------------------

func isTenantLeak(err error) bool {
	_, ok := errors.AsType[evidence.ErrTenantLeak](err)
	return ok
}

func isIncompleteCoverage(err error) bool {
	_, ok := errors.AsType[evidence.ErrIncompleteEpochCoverage](err)
	return ok
}

func asContentInvalid(err error) (evidence.ErrContentInvalid, bool) {
	return errors.AsType[evidence.ErrContentInvalid](err)
}

func asPackageMalformed(err error) (evidence.ErrPackageMalformed, bool) {
	return errors.AsType[evidence.ErrPackageMalformed](err)
}

func isPackageMalformed(err error) bool {
	_, ok := asPackageMalformed(err)
	return ok
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
