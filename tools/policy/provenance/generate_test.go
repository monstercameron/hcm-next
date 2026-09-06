package provenance_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

func provenanceRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not find go.mod above %s", file)
		}
		root = parent
	}
}

func checkedInSBOMDigest(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(provenance.DefaultSBOMPath)))
	if err != nil {
		t.Fatalf("reading checked-in SBOM: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func trustedFixtureKey(t *testing.T, root string) (ed25519.PrivateKey, map[string]bool) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash("tools/planning/gateevidence/testdata/dev-signing-key.yaml"))
	priv, err := provenance.LoadSigningKeyFixture(path)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture(%s): %v", path, err)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("fixture private key did not expose an Ed25519 public key")
	}
	return priv, map[string]bool{hex.EncodeToString(pub): true}
}

// TestTodo_SUPPLY_001 is SUPPLY-001's named PRIMARY test. It verifies the
// checked-in signed provenance document against the checked-in CycloneDX
// SBOM, proving the exact subject digest, builder, source/toolchain metadata,
// trusted Ed25519 signature, and SBOM reference form one admitted chain.
func TestTodo_SUPPLY_001(t *testing.T) {
	root := provenanceRepoRoot(t)
	path := filepath.Join(root, filepath.FromSlash("definitions/supply-chain/provenance.json"))
	statement, err := provenance.LoadStatement(path)
	if err != nil {
		t.Fatalf("LoadStatement(%s): %v", path, err)
	}
	_, trusted := trustedFixtureKey(t, root)

	if err := provenance.Verify(*statement, provenance.VerifyOptions{
		TrustedPublicKeys: trusted,
		SBOMDigest:        checkedInSBOMDigest(t, root),
	}); err != nil {
		t.Fatalf("checked-in provenance did not verify against checked-in SBOM: %v", err)
	}
	if len(statement.Subjects) != 1 || statement.Subjects[0].Name != "hcmnext" {
		t.Fatalf("Subjects = %+v, want the generated hcmnext artifact", statement.Subjects)
	}
}

// TestTodo_SUPPLY_001_Golden is SUPPLY-001's GOLDEN matrix test. It pins the
// generated statement's stable schema and the canonical build-config digest
// while allowing the artifact digest and generation timestamp to follow the
// current checked-in build.
func TestTodo_SUPPLY_001_Golden(t *testing.T) {
	root := provenanceRepoRoot(t)
	path := filepath.Join(root, filepath.FromSlash("definitions/supply-chain/provenance.json"))
	statement, err := provenance.LoadStatement(path)
	if err != nil {
		t.Fatalf("LoadStatement(%s): %v", path, err)
	}

	if statement.SchemaVersion != provenance.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", statement.SchemaVersion, provenance.SchemaVersion)
	}
	if statement.PredicateType != provenance.PredicateType {
		t.Errorf("PredicateType = %q, want %q", statement.PredicateType, provenance.PredicateType)
	}
	if statement.Builder.ID != provenance.BuilderID {
		t.Errorf("Builder.ID = %q, want %q", statement.Builder.ID, provenance.BuilderID)
	}
	if statement.Source.Repository != provenance.RootModulePath {
		t.Errorf("Source.Repository = %q, want %q", statement.Source.Repository, provenance.RootModulePath)
	}
	if statement.Source.Commit == "" || statement.Source.Ref == "" {
		t.Errorf("Source = %+v, want explicit commit and ref values", statement.Source)
	}
	if statement.BuildConfig.GOOS != "windows" || statement.BuildConfig.GOARCH != "arm64" {
		t.Errorf("BuildConfig target = %s/%s, want windows/arm64", statement.BuildConfig.GOOS, statement.BuildConfig.GOARCH)
	}
	if statement.BuildConfig.ConfigDigest != provenance.ConfigDigest(
		statement.BuildConfig.GoVersion,
		statement.BuildConfig.GOOS,
		statement.BuildConfig.GOARCH,
		statement.BuildConfig.Flags,
	) {
		t.Error("BuildConfig.ConfigDigest does not match its canonical configuration")
	}
	if statement.SBOM.Path != provenance.DefaultSBOMPath {
		t.Errorf("SBOM.Path = %q, want %q", statement.SBOM.Path, provenance.DefaultSBOMPath)
	}
	if len(statement.Subjects) != 1 || len(statement.Subjects[0].SHA256) != sha256.Size*2 || strings.ToLower(statement.Subjects[0].SHA256) != statement.Subjects[0].SHA256 {
		t.Errorf("Subjects = %+v, want one lowercase sha256 digest", statement.Subjects)
	}
}

// TestTodo_SUPPLY_001_Recovery is SUPPLY-001's RECOVERY matrix test. A
// failed build attempt must return cleanly, and the next read/verification of
// the durable signed evidence must remain usable; Generate keeps its build
// output in a temporary directory and never leaves a partial statement.
func TestTodo_SUPPLY_001_Recovery(t *testing.T) {
	root := provenanceRepoRoot(t)
	if _, err := provenance.Generate(root, provenance.Options{
		Pattern:  "./cmd/does-not-exist",
		SBOMPath: provenance.DefaultSBOMPath,
	}); err == nil {
		t.Fatal("Generate with a missing package must fail")
	}

	path := filepath.Join(root, filepath.FromSlash("definitions/supply-chain/provenance.json"))
	statement, err := provenance.LoadStatement(path)
	if err != nil {
		t.Fatalf("LoadStatement after failed generation: %v", err)
	}
	_, trusted := trustedFixtureKey(t, root)
	if err := provenance.Verify(*statement, provenance.VerifyOptions{
		TrustedPublicKeys: trusted,
		SBOMDigest:        checkedInSBOMDigest(t, root),
	}); err != nil {
		t.Fatalf("durable provenance failed after recovery from a build error: %v", err)
	}
}

func TestGenerateBuildsStatementAndReportsSBOMReadErrors(t *testing.T) {
	root := provenanceRepoRoot(t)
	t.Setenv("HCM_NEXT_PROVENANCE_COMMIT", "commit-from-test")
	t.Setenv("HCM_NEXT_PROVENANCE_REF", "refs/heads/test")

	statement, err := provenance.Generate(root, provenance.Options{
		Pattern:       "./cmd/hcmnext",
		SBOMPath:      provenance.DefaultSBOMPath,
		CommitEnvVars: []string{"HCM_NEXT_PROVENANCE_COMMIT"},
		RefEnvVars:    []string{"HCM_NEXT_PROVENANCE_REF"},
		Now:           func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Generate(valid inputs): %v", err)
	}
	if statement == nil {
		t.Fatal("Generate returned a nil statement")
	}
	if statement.Signature != nil {
		t.Fatal("Generate must return an unsigned statement")
	}
	if statement.Subjects[0].Name != "hcmnext" || len(statement.Subjects[0].SHA256) != sha256.Size*2 {
		t.Fatalf("generated subject = %+v, want hcmnext with a sha256 digest", statement.Subjects)
	}
	if statement.Source.Commit != "commit-from-test" || statement.Source.Ref != "refs/heads/test" {
		t.Fatalf("generated source = %+v, want test environment values", statement.Source)
	}
	if statement.GeneratedAt != "2026-09-06T12:00:00Z" {
		t.Fatalf("GeneratedAt = %q, want fixed timestamp", statement.GeneratedAt)
	}

	_, err = provenance.Generate(root, provenance.Options{
		Pattern:  "./cmd/hcmnext",
		SBOMPath: "does-not-exist/sbom.json",
	})
	if err == nil || !strings.Contains(err.Error(), "reading SBOM") {
		t.Fatalf("Generate(missing SBOM) error = %v, want an SBOM read error", err)
	}
}
