package evidence_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
)

// TestComputeDigestFoldsCoverageAndEveryPartRow proves the manifest digest is
// a function of what the package covers and of every part row, and of
// nothing else - not the order the parts were assembled in, and not the
// digest field itself, which can never cover its own bytes.
func TestComputeDigestFoldsCoverageAndEveryPartRow(t *testing.T) {
	t.Parallel()
	base := sampleManifest()
	want, err := evidence.ComputeDigest(base)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	t.Run("part order does not change the digest", func(t *testing.T) {
		shuffled := base
		shuffled.Parts = []evidence.Part{base.Parts[2], base.Parts[0], base.Parts[1]}
		got, err := evidence.ComputeDigest(shuffled)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got != want {
			t.Fatalf("reordering the parts changed the digest:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("the recorded digest is not folded into itself", func(t *testing.T) {
		claimed := base
		claimed.Digest = strings.Repeat("f", 64)
		got, err := evidence.ComputeDigest(claimed)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got != want {
			t.Fatal("the manifest's own digest field entered the preimage")
		}
	})

	t.Run("a duplicated path is refused rather than folded twice", func(t *testing.T) {
		doubled := base
		doubled.Parts = append(append([]evidence.Part(nil), base.Parts...), base.Parts[0])
		if _, err := evidence.ComputeDigest(doubled); err == nil {
			t.Fatal("a manifest listing one path twice produced a digest")
		}
	})

	t.Run("every folded field moves the digest", func(t *testing.T) {
		mutations := map[string]func(*evidence.Manifest){
			"tenant":         func(m *evidence.Manifest) { m.Tenant = uuid.MustParse("99999999-9999-9999-9999-999999999999") },
			"window start":   func(m *evidence.Manifest) { m.CoversFrom = m.CoversFrom.AddDate(0, 0, -1) },
			"window end":     func(m *evidence.Manifest) { m.CoversTo = m.CoversTo.AddDate(0, 0, 1) },
			"schema version": func(m *evidence.Manifest) { m.Schema.Version++ },
			"schema digest":  func(m *evidence.Manifest) { m.Schema.Digest = strings.Repeat("e", 64) },
			"layout version": func(m *evidence.Manifest) { m.LayoutVersion++ },
			"part path":      func(m *evidence.Manifest) { m.Parts[0].Path = "elsewhere.json" },
			"part kind":      func(m *evidence.Manifest) { m.Parts[0].Kind = evidence.PartEpoch },
			"part length":    func(m *evidence.Manifest) { m.Parts[0].Length++ },
			"part digest":    func(m *evidence.Manifest) { m.Parts[0].Digest = strings.Repeat("d", 64) },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				altered := base
				altered.Parts = append([]evidence.Part(nil), base.Parts...)
				mutate(&altered)
				got, err := evidence.ComputeDigest(altered)
				if err != nil {
					t.Fatalf("digest: %v", err)
				}
				if got == want {
					t.Fatalf("changing the %s left the digest unchanged", name)
				}
			})
		}
	})
}

// TestBuildIsPureAndRefusesContentThatIsNotEvidence proves the two properties
// Build exists for: the same content always produces the same bytes, and
// content that would make a package claim more than it proves is refused
// with every problem named at once.
func TestBuildIsPureAndRefusesContentThatIsNotEvidence(t *testing.T) {
	t.Parallel()

	t.Run("the same content always produces the same bytes", func(t *testing.T) {
		first, err := evidence.Build(goldenContent(t))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		second, err := evidence.Build(goldenContent(t))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if first.Manifest.Digest != second.Manifest.Digest {
			t.Fatalf("two builds of one content produced %s and %s", first.Manifest.Digest, second.Manifest.Digest)
		}
		for path, raw := range first.Files() {
			if !bytes.Equal(second.Files()[path], raw) {
				t.Fatalf("two builds of one content differ at %s", path)
			}
		}
	})

	t.Run("the read order of streams cannot change the digest", func(t *testing.T) {
		ordered := goldenContent(t)
		reversed := goldenContent(t)
		reversed.Streams = []evidence.Stream{reversed.Streams[1], reversed.Streams[0]}

		a, err := evidence.Build(ordered)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		b, err := evidence.Build(reversed)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if a.Manifest.Digest != b.Manifest.Digest {
			t.Fatal("reading the streams in another order changed the package")
		}
	})

	t.Run("refusals name every problem at once", func(t *testing.T) {
		empty := evidence.Content{}
		_, err := evidence.Build(empty)
		invalid, ok := asContentInvalid(err)
		if !ok {
			t.Fatalf("Build(empty) = %v, want ErrContentInvalid", err)
		}
		if len(invalid.Missing) < 4 {
			t.Fatalf("empty content reported only %d problems: %v", len(invalid.Missing), invalid.Missing)
		}
	})

	t.Run("a chain that does not run from genesis is refused", func(t *testing.T) {
		content := goldenContent(t)
		content.Streams[0].Links = content.Streams[0].Links[1:]
		if _, err := evidence.Build(content); err == nil {
			t.Fatal("a package was built over a chain that starts in the middle")
		}
	})

	t.Run("an event outside the covered window is refused", func(t *testing.T) {
		content := goldenContent(t)
		events := append([]evidence.Event(nil), content.Streams[0].Events...)
		events[0].RecordedAt = content.CoversTo.AddDate(0, 0, 1)
		content.Streams[0].Events = events
		if _, err := evidence.Build(content); err == nil {
			t.Fatal("a package covered an event recorded outside its own window")
		}
	})

	t.Run("a window no epoch covers is refused", func(t *testing.T) {
		content := goldenContent(t)
		content.Epochs = nil
		if _, err := evidence.Build(content); err == nil {
			t.Fatal("a package was built with no signed epoch at all")
		}
	})

	t.Run("an unsigned epoch is refused", func(t *testing.T) {
		content := goldenContent(t)
		epochs := append([]evidence.Epoch(nil), content.Epochs...)
		epochs[0].Signature = nil
		content.Epochs = epochs
		if _, err := evidence.Build(content); err == nil {
			t.Fatal("a package carried an unsigned epoch")
		}
	})

	t.Run("an epoch belonging to another tenant is a refusal, not a list entry", func(t *testing.T) {
		content := goldenContent(t)
		epochs := append([]evidence.Epoch(nil), content.Epochs...)
		epochs[0].Tenant = uuid.New()
		content.Epochs = epochs
		if _, err := evidence.Build(content); !isTenantLeak(err) {
			t.Fatalf("Build with a foreign epoch = %v, want ErrTenantLeak", err)
		}
	})
}

// TestPackageIsImmutableByConstruction proves what the value's whole point
// depends on: what a caller is handed is a copy, so mutating it cannot
// change the package that was attested to.
func TestPackageIsImmutableByConstruction(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	files := pkg.Files()
	for path := range files {
		files[path][0] = 'X'
	}
	delete(files, evidence.ManifestPath)

	fresh := pkg.Files()
	if len(fresh) != len(pkg.Paths()) {
		t.Fatalf("Files() returned %d paths after a caller deleted one, want %d", len(fresh), len(pkg.Paths()))
	}
	for path, raw := range fresh {
		if raw[0] == 'X' {
			t.Fatalf("a caller's mutation reached the package at %s", path)
		}
	}

	part, ok := pkg.Part(evidence.HeaderPath)
	if !ok {
		t.Fatal("the package has no header")
	}
	part[0] = 'X'
	again, _ := pkg.Part(evidence.HeaderPath)
	if again[0] == 'X' {
		t.Fatal("Part returned the package's own bytes rather than a copy")
	}

	if _, ok := pkg.Part("streams/9999/chain.json"); ok {
		t.Fatal("Part reported a path the package does not hold")
	}
	if pkg.TotalBytes() <= 0 {
		t.Fatal("TotalBytes reported nothing for a non-empty package")
	}
}

// TestOpenParsesAndRefusesOnlyWhatCannotBeRead proves the split the design
// rests on: everything a package can be wrong about is a finding from
// Verify, so opening a tampered package succeeds and only bytes that are not
// a package at all are an error.
func TestOpenParsesAndRefusesOnlyWhatCannotBeRead(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	t.Run("a round trip preserves the manifest", func(t *testing.T) {
		reopened, err := evidence.Open(pkg.Files())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if reopened.Manifest.Digest != pkg.Manifest.Digest {
			t.Fatalf("reopened digest %s, want %s", reopened.Manifest.Digest, pkg.Manifest.Digest)
		}
		if len(reopened.Manifest.Parts) != len(pkg.Manifest.Parts) {
			t.Fatalf("reopened %d parts, want %d", len(reopened.Manifest.Parts), len(pkg.Manifest.Parts))
		}
		if !equalStrings(reopened.Paths(), pkg.Paths()) {
			t.Fatalf("reopened paths %v, want %v", reopened.Paths(), pkg.Paths())
		}
	})

	t.Run("a tampered but parseable package still opens", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.HeaderPath, flipOneByte)
		if _, err := evidence.Open(files); err != nil {
			t.Fatalf("Open refused a tampered package: %v", err)
		}
	})

	t.Run("bytes with no manifest are not a package", func(t *testing.T) {
		files := pkg.Files()
		delete(files, evidence.ManifestPath)
		_, err := evidence.Open(files)
		if !isPackageMalformed(err) {
			t.Fatalf("Open with no manifest = %v, want ErrPackageMalformed", err)
		}
	})

	t.Run("an unparseable manifest is not a package", func(t *testing.T) {
		files := pkg.Files()
		files[evidence.ManifestPath] = []byte("{not json")
		_, err := evidence.Open(files)
		malformed, ok := asPackageMalformed(err)
		if !ok {
			t.Fatalf("Open with a broken manifest = %v, want ErrPackageMalformed", err)
		}
		if malformed.Path != evidence.ManifestPath {
			t.Fatalf("the refusal names %q, want %q", malformed.Path, evidence.ManifestPath)
		}
	})
}

// sampleManifest is a manifest-shaped value for digest arithmetic. It never
// becomes a package, so its part digests need not be digests of anything.
func sampleManifest() evidence.Manifest {
	return evidence.Manifest{
		SchemaVersion: evidence.SchemaVersion,
		LayoutVersion: evidence.LayoutVersion,
		Tenant:        goldenTenant,
		CoversFrom:    goldenFrom,
		CoversTo:      goldenTo,
		Schema:        goldenSchema,
		Parts: []evidence.Part{
			{Path: evidence.HeaderPath, Kind: evidence.PartHeader, Length: 10, Digest: strings.Repeat("1", 64), Algorithm: evidence.DigestAlgorithm},
			{Path: evidence.StreamChainPath(0), Kind: evidence.PartStreamChain, Length: 20, Digest: strings.Repeat("2", 64), Algorithm: evidence.DigestAlgorithm},
			{Path: evidence.EpochPath(0), Kind: evidence.PartEpoch, Length: 30, Digest: strings.Repeat("3", 64), Algorithm: evidence.DigestAlgorithm},
		},
		DigestAlgorithm: evidence.DigestAlgorithm,
	}
}
