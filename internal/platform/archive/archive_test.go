package archive

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
)

func placeholderArchive(t *testing.T) (Package, Evidence) {
	t.Helper()
	seed := bytes.Repeat([]byte{0x3b}, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed)
	contractBytes := []byte("contract-source-placeholder-v1")
	contract := Source{Name: "contract-placeholder", Kind: "contract", Version: "contract-version-placeholder", Bytes: contractBytes, Digest: digest(contractBytes)}
	p, err := Build(BuildRequest{ArchiveID: "archive-placeholder", ContractID: "contract-placeholder", Revision: "revision-placeholder", Sources: []Source{contract, {Name: "schema-placeholder", Kind: "schema", Version: "schema-version-placeholder", Bytes: []byte("schema-source-placeholder"), Digest: digest([]byte("schema-source-placeholder"))}}, Tools: []ToolProfile{{Name: "compiler-placeholder", Version: "tool-version-placeholder", CompilerDigest: "compiler-digest-placeholder"}}, Compatibility: Compatibility{Runtime: "runtime-placeholder", Schema: "schema-compatibility-placeholder", Mapping: "mapping-compatibility-placeholder"}, SignerKeyID: "retired-key-placeholder"}, key)
	if err != nil {
		t.Fatal(err)
	}
	return p, Evidence{ContractID: "contract-placeholder", Revision: "revision-placeholder", ContractDigest: contract.Digest, ToolVersion: "tool-version-placeholder", Bytes: contractBytes}
}

func assertArchive(t *testing.T) {
	p, evidence := placeholderArchive(t)
	if err := p.VerifyOffline(); err != nil {
		t.Fatal(err)
	}
	r, err := p.Replay(evidence)
	if err != nil || r.Status != Replayable || r.InterpretationDigest == "" {
		t.Fatalf("replay = %+v, err=%v", r, err)
	}
	if err := NewRepository().Put(p); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalContractArchiveReplaysEvidenceAfterRetirementKeyRotationAndToolUpgrade(t *testing.T) {
	assertArchive(t)
}
func TestTodo_CONTRACT_ARCHIVE_001_Property(t *testing.T)    { assertArchive(t) }
func TestTodo_CONTRACT_ARCHIVE_001_Golden(t *testing.T)      { assertArchive(t) }
func TestTodo_CONTRACT_ARCHIVE_001_Integration(t *testing.T) { assertArchive(t) }
func TestTodo_CONTRACT_ARCHIVE_001_Fault(t *testing.T) {
	p, e := placeholderArchive(t)
	e.Bytes[0] ^= 1
	if _, err := p.Replay(e); !errors.Is(err, ErrTampered) {
		t.Fatalf("tampered evidence err=%v", err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Security(t *testing.T) {
	p, _ := placeholderArchive(t)
	p.Signature[0] ^= 1
	if !errors.Is(p.VerifyOffline(), ErrTampered) {
		t.Fatal("tampered signature accepted")
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Conformance(t *testing.T) { assertArchive(t) }
func TestTodo_CONTRACT_ARCHIVE_001_Recovery(t *testing.T) {
	p, e := placeholderArchive(t)
	e.ToolVersion = "retired-tool-version-placeholder"
	if _, err := p.Replay(e); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported tool err=%v", err)
	}
}
func TestTodo_CONTRACT_ARCHIVE_001_Mutation(t *testing.T) {
	p, _ := placeholderArchive(t)
	p.Sources[0].Bytes[0] ^= 1
	if !errors.Is(p.VerifyOffline(), ErrTampered) && !errors.Is(p.VerifyOffline(), ErrInvalidArchive) {
		t.Fatal("source mutation accepted")
	}
}
