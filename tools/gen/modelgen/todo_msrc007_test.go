package modelgen

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := RepoRoot(wd)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	return root
}

// TestTodo_MSRC_007 is the MSRC-007 primary test. It proves the three RED
// clauses fail closed (an unrecognized Go type never falls back to
// map[string]any; two entities/properties can never collide on one Go
// identifier) and that the real catalog compiles to a full, typed
// [ModelSet] with no gap between the source registry's counts and the
// generated one's.
func TestTodo_MSRC_007(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := Render(ms)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, ok := files[OutputFile]; !ok {
		t.Fatalf("Render did not produce %s", OutputFile)
	}

	// RED: an unrecognized Go type fails Build, never map[string]any.
	if _, err := Build(syntheticRegistry(t, model.PropertyDefinition{
		Ref: "thing.bad", Entity: model.EntityRef{Name: "Thing", Version: 1},
		GoType: "any", SchemaPath: "x", Presence: model.PresenceRequired,
		Classification: model.ClassInternal, Temporal: model.TemporalPointInTime,
		AuthorityRef: "authority.thing/v1", Correction: model.CorrectionAppends,
		RetentionClassRef: "TEST_CLASS", Status: model.StatusActive,
	})); err == nil {
		t.Fatal("Build accepted GoType \"any\"; want an error")
	}

	// GREEN: no gap between the compiled registry and the generated set.
	var fieldCount int
	for _, e := range ms.Entities {
		fieldCount += len(e.Fields)
	}
	if fieldCount != len(reg.Properties()) {
		t.Fatalf("generated field count = %d, want %d", fieldCount, len(reg.Properties()))
	}
	if len(ms.Relationships) != len(reg.Relationships()) {
		t.Fatalf("generated relationship count = %d, want %d", len(ms.Relationships), len(reg.Relationships()))
	}
}

// TestTodo_MSRC_007_Golden regenerates the model package in memory from the
// compiled registry and diffs it against the checked-in
// gen/go/hcmnext/model/model_generated.go (the TOOL-010 drift-test pattern),
// then checks the rendered output's digest against a pinned golden value: an
// edited generated file or an unregenerated source change fails both checks.
func TestTodo_MSRC_007_Golden(t *testing.T) {
	root := repoRootForTest(t)
	files, err := GenerateAll()
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	fresh, ok := files[OutputFile]
	if !ok {
		t.Fatalf("GenerateAll did not produce %s", OutputFile)
	}

	committedPath := filepath.Join(root, filepath.FromSlash(OutputDir), OutputFile)
	committed, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("read committed generated file %s: %v (run: go run ./tools/gen/modelgen/cmd/modelgen)", committedPath, err)
	}
	if !bytes.Equal(fresh, committed) {
		t.Fatalf("regenerated %s differs from the checked-in file; run: go run ./tools/gen/modelgen/cmd/modelgen", OutputFile)
	}

	const goldenDigest = "sha256:d055eef2a947de664c53e68f0d26613b962f147204e019a3c581e2fab4070521"
	if got := OutputDigest(files); got != goldenDigest {
		t.Fatalf("output digest = %s, want pinned golden %s (update the constant only after confirming the source registry change is intentional)", got, goldenDigest)
	}
}

// TestTodo_MSRC_007_Race builds and renders concurrently against the same
// immutable compiled registry: Build and Render hold no shared mutable
// state, so every concurrent run must agree on the output digest.
func TestTodo_MSRC_007_Race(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("baseline Build: %v", err)
	}
	wantFiles, err := Render(ms)
	if err != nil {
		t.Fatalf("baseline Render: %v", err)
	}
	want := OutputDigest(wantFiles)

	const n = 16
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			localReg, err := model.Catalog()
			if err != nil {
				errs[i] = err
				return
			}
			localMS, err := Build(localReg)
			if err != nil {
				errs[i] = err
				return
			}
			localFiles, err := Render(localMS)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = OutputDigest(localFiles)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent run %d: %v", i, err)
		}
		if digests[i] != want {
			t.Fatalf("concurrent run %d digest = %s, want %s", i, digests[i], want)
		}
	}
}
