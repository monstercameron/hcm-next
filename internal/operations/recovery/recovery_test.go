package recovery

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTodo_RECOVERY_001(t *testing.T) {
	matrix := DefaultMatrix()
	if err := matrix.Validate(); err != nil {
		t.Fatalf("default recovery matrix is invalid: %v", err)
	}
	if got, want := len(matrix.Contracts), 10; got != want {
		t.Fatalf("contract count = %d, want %d", got, want)
	}
	for _, plane := range []Plane{Ledger, Artifacts, Config, Keys, Runtime, Outbox, Projection, Search, Analytics, Cache} {
		contract, ok := matrix.Contract(plane)
		if !ok {
			t.Fatalf("missing contract for %s", plane)
		}
		if contract.Owner == "" || contract.RTO.Minutes <= 0 || len(contract.SemanticChecks) == 0 {
			t.Fatalf("incomplete contract for %s: %+v", plane, contract)
		}
		if contract.RPO.NotApplicable && (!contract.Replayable || contract.Authority != Rebuildable) {
			t.Fatalf("RPO=N/A is not justified for %s: %+v", plane, contract)
		}
	}
	if explanation := matrix.Explain(); !strings.Contains(explanation, "ledger") || !strings.Contains(explanation, "rpo=N/A") {
		t.Fatalf("matrix explanation is incomplete: %s", explanation)
	}
}

func TestTodo_RECOVERY_001_Fault(t *testing.T) {
	matrix := DefaultMatrix()
	matrix.Contracts[0].Owner = ""
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("missing owner error = %v, want ErrInvalidContract", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[6].Replayable = false
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unjustified N/A error = %v, want ErrInvalidContract", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[2].Dependencies = append(matrix.Contracts[2].Dependencies, Plane("missing"))
	if err := matrix.Validate(); !errors.Is(err, ErrUnknownPlane) {
		t.Fatalf("unknown dependency error = %v, want ErrUnknownPlane", err)
	}
}

func TestTodo_RECOVERY_001_Recovery(t *testing.T) {
	matrix := DefaultMatrix()
	ordered := matrix.Ordered()
	if len(ordered) != len(matrix.Contracts) {
		t.Fatalf("ordered contract count = %d, want %d", len(ordered), len(matrix.Contracts))
	}
	for i, contract := range ordered {
		if contract.DependencyOrder != i+1 {
			t.Fatalf("ordered contract %d has order %d", i, contract.DependencyOrder)
		}
		for _, dependency := range contract.Dependencies {
			dep, ok := matrix.Contract(dependency)
			if !ok || dep.DependencyOrder >= contract.DependencyOrder {
				t.Fatalf("dependency %s is not before %s", dependency, contract.Plane)
			}
		}
	}
	copyOfFirst, ok := matrix.Contract(Ledger)
	if !ok {
		t.Fatal("ledger contract missing")
	}
	copyOfFirst.Dependencies[0] = Plane("mutated")
	ledger, _ := matrix.Contract(Ledger)
	if ledger.Dependencies[0] == Plane("mutated") {
		t.Fatal("Contract returned the matrix's dependency backing slice")
	}
}

func TestTodo_RECOVERY_001_Mutation(t *testing.T) {
	mutations := []func(*Matrix){
		func(m *Matrix) { m.Contracts[0].DependencyOrder = 0 },
		func(m *Matrix) { m.Contracts[0].RTO.Minutes = 0 },
		func(m *Matrix) { m.Contracts[0].SemanticChecks = nil },
		func(m *Matrix) { m.Contracts[0].Method = ModeRebuild },
		func(m *Matrix) { m.Contracts[0].Dependencies = []Plane{Projection} },
	}
	for i, mutate := range mutations {
		matrix := DefaultMatrix()
		mutate(&matrix)
		if err := matrix.Validate(); err == nil {
			t.Errorf("mutation %d unexpectedly validated", i)
		}
	}
}

func TestTodo_RECOVERY_002(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	verification := Verify(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()})
	if !verification.Verified() {
		t.Fatalf("backup verification failed: %s", verification.Explain())
	}
	if len(verification.SampledIndices) == 0 || len(verification.Checks) < 5 {
		t.Fatalf("verification lacks sample/check evidence: %+v", verification)
	}
	for _, block := range set.Blocks {
		if len(block.Ciphertext) == 0 || bytes.Contains(block.Ciphertext, []byte("ledger event")) {
			t.Fatalf("backup contains plaintext or an empty ciphertext block: %+v", block)
		}
	}
	if got := Explain(verification); !strings.Contains(got, "status=VERIFIED") {
		t.Fatalf("verification explanation = %q", got)
	}
}

func TestTodo_RECOVERY_002_Golden(t *testing.T) {
	first, publicKey, encryptionKey := testBackup(t)
	second, _, _ := testBackup(t)
	if first.Manifest.Digest != second.Manifest.Digest || !bytes.Equal(first.Manifest.Signature, second.Manifest.Signature) {
		t.Fatalf("deterministic nonce input did not produce a stable manifest")
	}
	if first.Manifest.BlockCount != 3 || first.Manifest.ContentHash == "" || first.Manifest.Digest == "" {
		t.Fatalf("incomplete backup manifest: %+v", first.Manifest)
	}
	if _, err := VerifyReadable(first, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 3}); err != nil {
		t.Fatalf("full sample verification failed: %v", err)
	}
}

func TestTodo_RECOVERY_002_Fault(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)

	missing := cloneBackupSet(set)
	missing.Blocks = missing.Blocks[:2]
	if Verify(missing, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("missing block was accepted")
	}

	corrupt := cloneBackupSet(set)
	corrupt.Blocks[1].Ciphertext[0] ^= 0xff
	if Verify(corrupt, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("corrupt ciphertext was accepted")
	}

	badSignature := cloneBackupSet(set)
	badSignature.Manifest.Signature[0] ^= 0xff
	if Verify(badSignature, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("bad signature was accepted")
	}

	badRetention := cloneBackupSet(set)
	badRetention.Manifest.RetainUntil = fixedNow().Add(-time.Minute)
	if Verify(badRetention, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("expired retention was accepted")
	}
}

func TestTodo_RECOVERY_002_Security(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	wrongKey := bytes.Repeat([]byte{0x99}, 32)
	verification := Verify(set, publicKey, wrongKey, VerifyOptions{Now: fixedNow()})
	if verification.Verified() || !strings.Contains(verification.Failure, "encryption key") {
		t.Fatalf("wrong key result = %+v", verification)
	}

	badRequest := testRequest()
	badRequest.TenantID = "tenant-a,tenant-b"
	if _, err := Create(badRequest, encryptionKey, testSigningKey(t)); !errors.Is(err, ErrUnboundedTenant) {
		t.Fatalf("unbounded tenant error = %v, want ErrUnboundedTenant", err)
	}
}

func TestTodo_RECOVERY_002_Recovery(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	plaintext, verification, err := Restore(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 1})
	if err != nil || !verification.Verified() {
		t.Fatalf("restore verification failed: verification=%+v err=%v", verification, err)
	}
	if got, want := string(bytes.Join(plaintext, nil)), "ledger event 0ledger event 1ledger event 2"; got != want {
		t.Fatalf("restored plaintext = %q, want %q", got, want)
	}

	repository := NewRepository()
	if err := repository.Put(set); err != nil {
		t.Fatalf("put backup: %v", err)
	}
	stored, ok := repository.Get(set.Manifest.SetID)
	if !ok {
		t.Fatal("stored backup missing")
	}
	stored.Blocks[0].Ciphertext[0] ^= 0xff
	storedAgain, _ := repository.Get(set.Manifest.SetID)
	if bytes.Equal(stored.Blocks[0].Ciphertext, storedAgain.Blocks[0].Ciphertext) {
		t.Fatal("repository returned a mutable backing copy")
	}
	replacement := cloneBackupSet(set)
	replacement.Manifest.Digest = "sha256:replacement"
	if err := repository.Put(replacement); !errors.Is(err, ErrImmutable) {
		t.Fatalf("replacement error = %v, want ErrImmutable", err)
	}
}

func TestTodo_RECOVERY_002_Recovery_ManifestTraversal(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	set.Manifest.BlockHashes[2] = set.Manifest.BlockHashes[1]
	verification := Verify(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 1})
	if verification.Verified() {
		t.Fatal("manifest traversal accepted mismatched block inventory")
	}
}

func testBackup(t *testing.T) (BackupSet, ed25519.PublicKey, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x21}, 32)
	signingKey := testSigningKey(t)
	request := testRequest()
	request.NonceReader = bytes.NewReader(bytes.Repeat([]byte{0x42}, 36))
	set, err := Create(request, key, signingKey)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return set, signingKey.Public().(ed25519.PublicKey), key
}

func testRequest() CreateRequest {
	return CreateRequest{
		SetID: "backup-001", TenantID: "tenant-a", PolicyID: "daily-ledger", SourcePlane: "ledger", Watermark: "ledger:42",
		KeyReference: "kms://tenant-a/backup", KeyVersion: "key-v3", RetainUntil: fixedNow().Add(24 * time.Hour), Now: fixedNow(),
		Blocks: [][]byte{[]byte("ledger event 0"), []byte("ledger event 1"), []byte("ledger event 2")},
	}
}

func testSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := bytes.Repeat([]byte{0x31}, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed)
}

func fixedNow() time.Time { return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) }
