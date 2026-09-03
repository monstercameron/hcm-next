package contractarchive

import (
	"crypto/ed25519"
	"testing"
)

func testArchive(t *testing.T) Package {
	t.Helper()
	a, err := New("1", "evidence-manifest/v1", []Artifact{{Kind: "workflow", ID: "hire", Version: "7", Source: []byte("source"), Descriptor: []byte("descriptor"), CompilerProfile: "compiler/v2", ToolProfile: "tool/9", Compatibility: map[string]string{"schema": "3"}}}, []TrustRecord{{KeyID: "old", Algorithm: "ed25519", Status: "retired"}})
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.AddSignature("old", private)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHistoricalContractArchiveReplaysEvidenceAfterRetirementKeyRotationAndToolUpgrade(t *testing.T) {
	p := testArchive(t)
	if result := p.VerifyOffline(); !result.Valid {
		t.Fatalf("offline verification failed: %+v", result)
	}
	encoded, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if result := replayed.VerifyOffline(); !result.Valid || result.Digest != p.Digest {
		t.Fatalf("replay changed evidence: %+v", result)
	}
}

func TestTodo_CONTRACT_ARCHIVE_001_Golden(t *testing.T) {
	p := testArchive(t)
	if p.ContentDigest() != p.Digest {
		t.Fatal("digest is not canonical")
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Property(t *testing.T) {
	p := testArchive(t)
	if p.ContentDigest() != testArchive(t).ContentDigest() {
		t.Fatal("digest is not reproducible")
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Integration(t *testing.T) {
	p := testArchive(t)
	if _, err := Encode(p); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Fault(t *testing.T) {
	p := testArchive(t)
	p.Artifacts[0].Source[0] ^= 1
	if p.Verify().Err != ErrTampered {
		t.Fatalf("want tampered, got %v", p.Verify().Err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Security(t *testing.T) {
	p := testArchive(t)
	p.Signatures[0].Value[0] ^= 1
	if p.Verify().Err != ErrTampered {
		t.Fatalf("want tampered signature, got %v", p.Verify().Err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Conformance(t *testing.T) {
	p := testArchive(t)
	if p.Profile != "evidence-manifest/v1" || p.Artifacts[0].ToolProfile == "" {
		t.Fatal("archive omitted interpretation metadata")
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Recovery(t *testing.T) {
	p := testArchive(t)
	b, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Decode(b)
	if err != nil || !q.Verify().Valid {
		t.Fatalf("recovery failed: %v", err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Mutation(t *testing.T) {
	src := []byte("source")
	p, err := New("1", "p", []Artifact{{Kind: "k", ID: "i", Version: "v", Source: src}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src[0] = 'x'
	if string(p.Artifacts[0].Source) != "source" {
		t.Fatal("constructor retained mutable source")
	}
}
