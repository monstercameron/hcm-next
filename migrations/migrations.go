// Package migrations owns the authoritative SQL-first schema of the Human Capital Management Suite
// data plane (owner: data plane; phase: P1A).
//
// The SQL files in this directory are the only place the physical schema is
// declared. They are embedded so that the migration command, the application and
// the test harness all apply exactly the same bytes; nothing rebuilds the schema
// from a second source of truth.
//
// Every file is a Goose migration with an Up and a Down section. Reversibility is
// a property of the file: an irreversible migration must say so before it is
// activated, and DB-006 records that fact on the schema release.
package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// FS holds the embedded migration files. Callers pass it to Goose directly.
//
//go:embed *.sql
var FS embed.FS

// Dialect is the only SQL dialect these migrations are written for.
const Dialect = "postgres"

// DigestAlgorithm names the algorithm used for artifact and file checksums.
const DigestAlgorithm = "sha256"

// File is one migration file with its version, name and content checksum.
type File struct {
	Version  int64
	Name     string
	Checksum string
}

// Files returns every embedded migration ordered by version, each with the
// checksum of its exact bytes. The checksum is what DB-006 journals and what a
// startup check compares against, so it is computed over content only.
func Files() ([]File, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read migration directory: %w", err)
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		body, err := FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		files = append(files, File{
			Version:  version,
			Name:     name,
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations embedded")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

// ArtifactDigest returns one digest over the whole migration tree. It binds each
// file's name, length and content so that reordering, renaming or truncating a
// file changes the release identity.
func ArtifactDigest() (string, error) {
	files, err := Files()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00", f.Version, f.Name, f.Checksum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TargetVersion returns the highest embedded migration version.
func TargetVersion() (int64, error) {
	files, err := Files()
	if err != nil {
		return 0, err
	}
	return files[len(files)-1].Version, nil
}

// NewestReversibleVersion returns the highest embedded migration version
// whose Down section does not declare irreversibility. Migrations whose
// Down raises over durable evidence (the declared "is irreversible"
// convention) refuse goose Down by design, so rollback-cycling tests must
// stop here instead of at TargetVersion, which goose cannot skip past.
func NewestReversibleVersion() (int64, error) {
	files, err := Files()
	if err != nil {
		return 0, err
	}
	for i := len(files) - 1; i >= 0; i-- {
		body, err := FS.ReadFile(files[i].Name)
		if err != nil {
			return 0, fmt.Errorf("read migration %s: %w", files[i].Name, err)
		}
		down := body
		if idx := strings.Index(string(body), "-- +goose Down"); idx >= 0 {
			down = body[idx:]
		}
		if !strings.Contains(string(down), "is irreversible") {
			return files[i].Version, nil
		}
	}
	return 0, fmt.Errorf("no reversible migration embedded")
}

func parseVersion(name string) (int64, error) {
	idx := strings.Index(name, "_")
	if idx <= 0 {
		return 0, fmt.Errorf("migration %s: expected <version>_<name>.sql", name)
	}
	version, err := strconv.ParseInt(name[:idx], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("migration %s: %w", name, err)
	}
	if version <= 0 {
		return 0, fmt.Errorf("migration %s: version must be positive", name)
	}
	return version, nil
}
