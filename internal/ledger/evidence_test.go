package ledger

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"

	checkpointadapter "github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
	evidenceadapter "github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
)

// TestEvidenceExporterSatisfiesThePort proves the adapter this package hands
// out actually implements the port it declares, so a signature drift in
// internal/data/ledger/evidence is a build failure here rather than at some
// future call site.
func TestEvidenceExporterSatisfiesThePort(t *testing.T) {
	var _ EvidenceExporter = NewEvidenceExporter()
	var _ EvidenceExporter = evidenceadapter.NewExporter()
	if NewEvidenceExporter() == nil {
		t.Fatal("NewEvidenceExporter returned nil")
	}
}

// TestEvidenceExportTakesAQuerierNotATransaction proves the deliberate
// difference from [Appender] and [Checkpointer]: an export writes nothing,
// so it neither needs nor should hold a write transaction open while it
// reads a window that may be large.
func TestEvidenceExportTakesAQuerierNotATransaction(t *testing.T) {
	// Assigning the port's method set to a signature spelled in terms of
	// Querier only compiles if no transaction appears in it.
	exporter := NewEvidenceExporter()
	var _ func(context.Context, Querier, EvidenceRequest) (EvidencePackage, error) = wrapExport(exporter)
	var _ func(context.Context, Querier, EvidenceRequest) (EvidenceContent, error) = wrapRead(exporter)
}

// TestEvidenceTypesAreTheAdaptersOwn proves the re-exported types are
// aliases rather than a second, driftable copy: a value produced by the
// adapter is assignable to the port's name in both directions.
func TestEvidenceTypesAreTheAdaptersOwn(t *testing.T) {
	var (
		req      EvidenceRequest  = evidenceadapter.Request{}
		manifest EvidenceManifest = evidenceadapter.Manifest{}
		content  EvidenceContent  = evidenceadapter.Content{}
		part     EvidencePart     = evidenceadapter.Part{}
		kind     EvidencePartKind = evidenceadapter.PartHeader
		stream   EvidenceStream   = evidenceadapter.Stream{}
		event    EvidenceEvent    = evidenceadapter.Event{}
		finding  EvidenceFinding  = evidenceadapter.Finding{}
		report   EvidenceReport   = evidenceadapter.Report{}
	)
	var _ evidenceadapter.Request = req
	var _ evidenceadapter.Manifest = manifest
	var _ evidenceadapter.Content = content
	var _ evidenceadapter.Part = part
	var _ evidenceadapter.PartKind = kind
	var _ evidenceadapter.Stream = stream
	var _ evidenceadapter.Event = event
	var _ evidenceadapter.Finding = finding
	var _ evidenceadapter.Report = report

	// An epoch a package carries is the checkpoint manifest the port already
	// exports, so a caller needs no third name for the same signed value.
	content.Epochs = []CheckpointManifest{{}}
	if len(content.Epochs) != 1 {
		t.Fatal("an evidence package's epochs are not the port's checkpoint manifests")
	}
}

// TestEvidenceFindingKindsAreTheAdaptersOwn proves the re-exported finding
// kinds are the adapter's values and not a second copy of the strings, and
// that the eleven of them are distinct - a caller branches on these, so two
// collapsing into one would silently merge two different failures.
func TestEvidenceFindingKindsAreTheAdaptersOwn(t *testing.T) {
	cases := map[EvidenceKind]evidenceadapter.FindingKind{
		EvidenceMissingPart:          evidenceadapter.FindingMissingPart,
		EvidenceUnlistedPart:         evidenceadapter.FindingUnlistedPart,
		EvidenceTamperedPart:         evidenceadapter.FindingTamperedPart,
		EvidenceManifestDigest:       evidenceadapter.FindingManifestDigest,
		EvidenceMalformedPart:        evidenceadapter.FindingMalformedPart,
		EvidenceTamperedEvent:        evidenceadapter.FindingTamperedEvent,
		EvidenceBrokenChain:          evidenceadapter.FindingBrokenChain,
		EvidenceUnattestedHead:       evidenceadapter.FindingUnattestedHead,
		EvidenceMissingEpochCoverage: evidenceadapter.FindingMissingEpochCoverage,
		EvidenceSignatureMismatch:    evidenceadapter.FindingSignatureMismatch,
		EvidenceTenantLeak:           evidenceadapter.FindingTenantLeak,
	}
	if len(cases) != 11 {
		t.Fatalf("the port re-exports %d distinct finding kinds, want 11", len(cases))
	}
	for port, adapter := range cases {
		if port != adapter {
			t.Errorf("port kind %q is not the adapter's %q", port, adapter)
		}
		if port == "" {
			t.Error("a finding kind is the empty string")
		}
	}
}

// TestVerifyEvidenceIsOffline proves the port's verification entry point
// needs nothing but package bytes and a key directory: no database handle
// appears in its signature, which is what makes an auditor's check possible
// without production credentials.
func TestVerifyEvidenceIsOffline(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := checkpointadapter.NewEd25519Signer("hcmnext:evidence:port-test", priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	dir := checkpointadapter.NewStaticKeyDirectory(CheckpointKeyStatus{
		KeyID: signer.KeyID(), PublicKey: signer.PublicKey(),
	})

	// Bytes that are not a package at all: verification still concludes, in
	// one typed finding, without ever having reached for a connection.
	report := VerifyEvidence(map[string][]byte{"header.json": []byte("{}")}, dir)
	if report.OK() {
		t.Fatal("bytes with no manifest verified")
	}
	if !report.Has(EvidenceMalformedPart) {
		t.Fatalf("bytes with no manifest reported %v, want a malformed part", report.Findings)
	}
	if err := report.Err(); err == nil {
		t.Fatal("a failing report returned no error")
	}

	t.Run("an empty package is refused rather than vacuously accepted", func(t *testing.T) {
		empty := VerifyEvidence(map[string][]byte{}, dir)
		if empty.OK() {
			t.Fatal("an empty bytes map verified")
		}
	})

	t.Run("verifying an unbuilt package is the same refusal", func(t *testing.T) {
		var pkg EvidencePackage
		if VerifyEvidencePackage(pkg, dir).OK() {
			t.Fatal("a zero package verified")
		}
	})

	t.Run("no private key material is reachable through the port", func(t *testing.T) {
		if strings.Contains(signer.PublicKey(), "PRIVATE") || len(signer.PublicKey()) != ed25519.PublicKeySize*2 {
			t.Fatalf("PublicKey() = %q, want a %d-byte hex key", signer.PublicKey(), ed25519.PublicKeySize)
		}
	})
}

// TestBuildEvidenceIsPureAndRefusesEmptyContent proves the port's build
// entry point is the adapter's pure one: no clock, no database and no
// identifier source, so content that is not evidence is refused the same way
// every time.
func TestBuildEvidenceIsPureAndRefusesEmptyContent(t *testing.T) {
	first, firstErr := BuildEvidence(EvidenceContent{})
	second, secondErr := BuildEvidence(EvidenceContent{})
	if firstErr == nil || secondErr == nil {
		t.Fatal("empty content produced a package")
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("two refusals of one content differ:\n%s\n%s", firstErr, secondErr)
	}
	if len(first.Paths()) != 0 || len(second.Paths()) != 0 {
		t.Fatal("a refused build still returned paths")
	}
}

// TestWithEvidenceEventDigesterAcceptsEitherProfile proves why the payload
// check is a seam rather than a default: which canonicalization profile
// minted an event's digest is a property of the cell that recorded it, so
// the port takes the cell's own [Digester].
func TestWithEvidenceEventDigesterAcceptsEitherProfile(t *testing.T) {
	registry, err := NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("build digest registry: %v", err)
	}
	var kernel Digester = NewKernelDigester(registry)
	if opt := WithEvidenceEventDigester(kernel); opt == nil {
		t.Fatal("the kernel digester produced no option")
	}
	// The option is the adapter's own type, so it can be passed straight
	// through to either verification entry point.
	var _ []EvidenceOption = []evidenceadapter.VerifyOption{WithEvidenceEventDigester(kernel)}

	_, priv, keyErr := ed25519.GenerateKey(nil)
	if keyErr != nil {
		t.Fatalf("generate key: %v", keyErr)
	}
	signer, signerErr := checkpointadapter.NewEd25519Signer("hcmnext:evidence:port-test", priv)
	if signerErr != nil {
		t.Fatalf("build signer: %v", signerErr)
	}
	dir := checkpointadapter.NewStaticKeyDirectory(CheckpointKeyStatus{
		KeyID: signer.KeyID(), PublicKey: signer.PublicKey(),
	})
	report := VerifyEvidence(map[string][]byte{}, dir, WithEvidenceEventDigester(kernel))
	if report.OK() {
		t.Fatal("an empty package verified with a digester supplied")
	}
}

func wrapExport(e EvidenceExporter) func(context.Context, Querier, EvidenceRequest) (EvidencePackage, error) {
	return e.Export
}

func wrapRead(e EvidenceExporter) func(context.Context, Querier, EvidenceRequest) (EvidenceContent, error) {
	return e.Read
}
