package evidence_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

// TestWireProjectionsCarryNoMapAndNoZone proves the two rules codec.go
// states about the bytes: every instant crosses as UTC nanoseconds so no
// encoder's zone choice can move a digest, and no part is a JSON object with
// caller-chosen keys, because a map's iteration order is unspecified.
func TestWireProjectionsCarryNoMapAndNoZone(t *testing.T) {
	t.Parallel()

	utc, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	t.Run("the same instants in another zone produce the same bytes", func(t *testing.T) {
		east := time.FixedZone("east", 5*3600)
		shifted := goldenContent(t)
		shifted.CoversFrom = shifted.CoversFrom.In(east)
		shifted.CoversTo = shifted.CoversTo.In(east)
		for i := range shifted.Streams {
			events := append([]evidence.Event(nil), shifted.Streams[i].Events...)
			for j := range events {
				events[j].RecordedAt = events[j].RecordedAt.In(east)
				events[j].OccurredAt = events[j].OccurredAt.In(east)
				events[j].EffectiveAt = events[j].EffectiveAt.In(east)
			}
			shifted.Streams[i].Events = events
		}

		built, err := evidence.Build(shifted)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if built.Manifest.Digest != utc.Manifest.Digest {
			t.Fatalf("the same instants in another zone produced %s, want %s",
				built.Manifest.Digest, utc.Manifest.Digest)
		}
	})

	t.Run("a nanosecond tail PostgreSQL never stored does not move a digest", func(t *testing.T) {
		tailed := goldenContent(t)
		events := append([]evidence.Event(nil), tailed.Streams[0].Events...)
		events[0].RecordedAt = events[0].RecordedAt.Add(37 * time.Nanosecond)
		tailed.Streams[0].Events = events

		built, err := evidence.Build(tailed)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if built.Manifest.Digest != utc.Manifest.Digest {
			t.Fatal("a sub-microsecond tail changed the package")
		}
	})

	t.Run("every part is an ordered structure, never a keyed map of content", func(t *testing.T) {
		for _, path := range utc.Paths() {
			raw, _ := utc.Part(path)
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("%s is not a JSON object: %v", path, err)
			}
			for key, value := range doc {
				switch value.(type) {
				case []any:
					// Ordered by construction, which is what reproducibility
					// needs.
				case map[string]any:
					// Only the signed epoch nests an object, and it is the
					// checkpoint package's own fixed structure.
					if !strings.HasPrefix(path, "epochs/") {
						t.Errorf("%s carries a nested object at %q", path, key)
					}
				}
			}
			if !bytes.HasSuffix(raw, []byte("\n")) {
				t.Errorf("%s does not end in a newline", path)
			}
		}
	})
}

// TestAbsentAndZeroAreTheSameAbsenceInTheBytes proves the uuidText rule: the
// nil UUID and "no identifier" must be one spelling in the bytes, not two,
// or the same evidence could digest two ways.
func TestAbsentAndZeroAreTheSameAbsenceInTheBytes(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	events := append([]evidence.Event(nil), content.Streams[0].Events...)
	events[0].CausationID = uuid.Nil
	content.Streams[0].Events = events

	pkg, err := evidence.Build(content)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, ok := pkg.Part(evidence.StreamEventsPath(0))
	if !ok {
		t.Fatal("the package has no events part for stream one")
	}
	if bytes.Contains(raw, []byte(uuid.Nil.String())) {
		t.Fatalf("the all-zero identifier was spelled out: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"causation_id":""`)) {
		t.Fatalf("an absent identifier is not the empty string: %s", raw)
	}

	// And it survives a round trip as absence rather than becoming a value.
	mustVerify(t, pkg.Files(), goldenKeyDirectory(t))
}

// TestCorrectionLineageSurvivesTheRoundTrip proves why an envelope carries
// its correction target: a package is meant to make an event's lineage
// checkable, so the event it supersedes has to cross with it.
func TestCorrectionLineageSurvivesTheRoundTrip(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	events := append([]evidence.Event(nil), content.Streams[0].Events...)
	events[1].Corrects = &evidence.EventRef{StreamKey: streamOne, Sequence: 1}
	content.Streams[0].Events = events

	pkg, err := evidence.Build(content)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := pkg.Part(evidence.StreamEventsPath(0))
	if !bytes.Contains(raw, []byte(`"corrects_stream_key":"`+streamOne+`"`)) {
		t.Fatalf("the correction target is not in the bytes: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"corrects_sequence":1`)) {
		t.Fatalf("the corrected sequence is not in the bytes: %s", raw)
	}

	// A correction changes the bytes, so it changes the digest: lineage is
	// evidence, not annotation.
	plain, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if plain.Manifest.Digest == pkg.Manifest.Digest {
		t.Fatal("adding a correction target left the package digest unchanged")
	}
	mustVerify(t, pkg.Files(), goldenKeyDirectory(t))
}

// TestEpochPartsAreTheSignedStructureNotAReprojection proves the decision
// codec.go argues for: an epoch part is marshalled from the checkpoint
// manifest itself, so what an auditor re-verifies is the signed structure
// and not a second, driftable shape of it.
func TestEpochPartsAreTheSignedStructureNotAReprojection(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	pkg, err := evidence.Build(content)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, ok := pkg.Part(evidence.EpochPath(0))
	if !ok {
		t.Fatal("the package has no epoch part")
	}

	var decoded evidence.Epoch
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("an epoch part does not decode as a checkpoint manifest: %v", err)
	}
	original := content.Epochs[0]
	if decoded.EpochID != original.EpochID || decoded.EpochNumber != original.EpochNumber {
		t.Fatalf("the epoch part is not the epoch: %s/%d", decoded.EpochID, decoded.EpochNumber)
	}
	if decoded.Signature == nil || decoded.Signature.Value != original.Signature.Value {
		t.Fatal("the epoch part lost the signature that makes it evidence")
	}
	if len(decoded.Streams) != len(original.Streams) {
		t.Fatalf("the epoch part attests to %d streams, want %d", len(decoded.Streams), len(original.Streams))
	}

	// Re-encoding what came back reproduces the part exactly, which is what
	// makes an epoch a reproducible member of the digest.
	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(append(again, '\n'), raw) {
		t.Fatalf("an epoch part does not round-trip:\n got %s\nwant %s", again, raw)
	}
}

// TestMalformedPartsAreRefusedWithTheirPath proves decodePart's contract:
// bytes that are not the projection they claim to be name the path they were
// found at, so an auditor is told which file to look at.
func TestMalformedPartsAreRefusedWithTheirPath(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dir := goldenKeyDirectory(t)

	for _, path := range []string{
		evidence.HeaderPath, evidence.StreamChainPath(0),
		evidence.StreamEventsPath(0), evidence.EpochPath(0),
	} {
		t.Run(path, func(t *testing.T) {
			files := mutate(t, pkg.Files(), path, func([]byte) []byte { return []byte("{\n") })
			report := evidence.Verify(files, dir)
			named := false
			for _, finding := range report.Findings {
				if finding.Kind == evidence.FindingMalformedPart && finding.Part == path {
					named = true
				}
			}
			if !named {
				t.Fatalf("unparseable bytes at %s reported %v with no malformed finding naming it",
					path, report.Findings)
			}
		})
	}

	t.Run("an identifier that is not a uuid", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.StreamChainPath(0), func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["links"].([]any)[0].(map[string]any)["event_id"] = "not-a-uuid"
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
	})
}
