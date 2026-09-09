package endpointmanifest_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/endpointmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func manifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repopath.RootDir(), filepath.FromSlash(endpointmanifest.DefaultPath))
}

// TestCheckedInEndpointManifestMatchesGeneratedManifest is this checker's
// primary test: definitions/api/endpoint-manifest.json must be exactly what
// internal/transport/manifest.Build() produces from the current source
// tree, never a stale or hand-edited copy.
func TestCheckedInEndpointManifestMatchesGeneratedManifest(t *testing.T) {
	if err := endpointmanifest.Check(manifestPath(t)); err != nil {
		t.Fatal(err)
	}
}

// TestCheckRejectsAStaleOrHandEditedManifest proves the check actually
// distinguishes a matching file from one that drifted: a manifest with one
// row's disposition edited by hand must fail.
func TestCheckRejectsAStaleOrHandEditedManifest(t *testing.T) {
	m, err := endpointmanifest.Load(manifestPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Endpoints) == 0 {
		t.Fatal("loaded manifest has zero endpoints")
	}

	tampered := *m
	tampered.Endpoints = append([]manifest.EndpointDefinition(nil), m.Endpoints...)
	tampered.Endpoints[0].Disposition = manifest.DispositionServed
	if tampered.Endpoints[0].Disposition == m.Endpoints[0].Disposition {
		// Pick a row that is not already SERVED so the mutation is real.
		for i := range tampered.Endpoints {
			if tampered.Endpoints[i].Disposition != manifest.DispositionRefusedP1A {
				continue
			}
			tampered.Endpoints[i].Disposition = manifest.DispositionServed
			break
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "endpoint-manifest.json")
	writeManifest(t, path, &tampered)

	if err := endpointmanifest.Check(path); err == nil {
		t.Fatal("expected Check to reject a hand-tampered manifest")
	}
}

// TestLoadRejectsMalformedJSON proves a syntactically broken file fails to
// load rather than silently parsing as an empty manifest.
func TestLoadRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	writeRaw(t, path, "{not json")
	if _, err := endpointmanifest.Load(path); err == nil {
		t.Fatal("expected Load to reject malformed JSON")
	}
}

// TestCheckMissingFileFails proves a missing checked-in manifest is a
// reported error, not a silently-passing check.
func TestCheckMissingFileFails(t *testing.T) {
	if err := endpointmanifest.Check(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("expected Check to fail for a missing file")
	}
}
