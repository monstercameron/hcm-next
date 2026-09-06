package provenance_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

func TestWriteStatementThenLoadStatementRoundTrips(t *testing.T) {
	signed := mustSignGolden(t)
	path := filepath.Join(t.TempDir(), "provenance.json")

	if err := provenance.WriteStatement(path, signed); err != nil {
		t.Fatalf("WriteStatement: %v", err)
	}

	loaded, err := provenance.LoadStatement(path)
	if err != nil {
		t.Fatalf("LoadStatement: %v", err)
	}

	ok, err := provenance.VerifyStatementSignature(*loaded)
	if err != nil {
		t.Fatalf("VerifyStatementSignature(loaded): %v", err)
	}
	if !ok {
		t.Fatal("a statement round-tripped through WriteStatement/LoadStatement must still verify")
	}
	if loaded.Builder.ID != signed.Builder.ID {
		t.Errorf("Builder.ID = %q, want %q", loaded.Builder.ID, signed.Builder.ID)
	}
}

// TestLoadStatementRecoversCleanlyFromCorruptFiles feeds LoadStatement a
// missing file, truncated JSON and an empty file in turn: each must fail
// with a clean, descriptive error - never a panic - and a subsequent load
// of a valid file must still succeed, proving one bad file does not wedge
// the loader for anything that follows. See generate_test.go's
// TestTodo_SUPPLY_001_Recovery for SUPPLY-001's named RECOVERY matrix test
// (recovering from a failed build attempt, the other half of this
// package's recovery story).
func TestLoadStatementRecoversCleanlyFromCorruptFiles(t *testing.T) {
	dir := t.TempDir()

	missingPath := filepath.Join(dir, "missing.json")
	if _, err := provenance.LoadStatement(missingPath); err == nil {
		t.Error("expected an error for a missing provenance file")
	}

	truncatedPath := filepath.Join(dir, "truncated.json")
	if err := os.WriteFile(truncatedPath, []byte(`{"schema_version":1,"subjects":[`), 0o644); err != nil {
		t.Fatalf("writing truncated fixture: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("LoadStatement panicked on truncated JSON: %v", r)
			}
		}()
		if _, err := provenance.LoadStatement(truncatedPath); err == nil {
			t.Error("expected an error for truncated JSON")
		}
	}()

	emptyPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyPath, []byte(``), 0o644); err != nil {
		t.Fatalf("writing empty fixture: %v", err)
	}
	if _, err := provenance.LoadStatement(emptyPath); err == nil {
		t.Error("expected an error for an empty file")
	}

	// Recovery: after two bad reads, a valid statement still loads and
	// verifies cleanly - the loader carries no corrupted shared state.
	validPath := filepath.Join(dir, "valid.json")
	signed := mustSignGolden(t)
	if err := provenance.WriteStatement(validPath, signed); err != nil {
		t.Fatalf("WriteStatement: %v", err)
	}
	loaded, err := provenance.LoadStatement(validPath)
	if err != nil {
		t.Fatalf("LoadStatement (valid, after prior failures): %v", err)
	}
	ok, err := provenance.VerifyStatementSignature(*loaded)
	if err != nil || !ok {
		t.Fatalf("valid statement failed to verify after recovery: ok=%v err=%v", ok, err)
	}
}
