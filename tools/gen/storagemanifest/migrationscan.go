package storagemanifest

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MigrationInventory is what this package can observe about the real
// physical schema by reading migrations/*.sql as plain text. It never
// connects to a database and never edits the migration files; it is the
// "checked against migrations" half of DB-002/003/004.
type MigrationInventory struct {
	// Tables is every name following CREATE TABLE, including partitions.
	Tables map[string]bool

	// StreamKinds is the ledger_stream.stream_kind CHECK (... IN (...)) allow-list.
	StreamKinds map[string]bool

	// DefinitionKinds is the definition_version.definition_kind CHECK (... IN (...)) allow-list.
	DefinitionKinds map[string]bool

	// ColumnChecks is every "<column> IN ('A', 'B', ...)" CHECK constraint
	// body found anywhere in the migrated schema, keyed by lower-cased column
	// name. DB-004 uses this to compare a generated lifecycle CHECK fragment
	// against the real constraint the migrations already declare, when one
	// exists, without hardcoding which tables carry which columns.
	ColumnChecks map[string]map[string]bool

	// SourceFiles is the sorted list of migration files scanned, for
	// diagnostics and for the manifest's own provenance record.
	SourceFiles []string
}

var (
	createTablePattern = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	// checkInPattern captures "<column> IN (\n 'A', 'B', ...\n)" across lines.
	checkInPattern = regexp.MustCompile(`(?is)(\w+)\s+IN\s*\(([^)]*)\)`)
	quotedPattern  = regexp.MustCompile(`'([^']*)'`)
)

// ScanMigrations reads every *.sql file directly under migrationsDir (no
// recursion — the migration tree is flat) and builds a [MigrationInventory].
func ScanMigrations(migrationsDir string) (MigrationInventory, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return MigrationInventory{}, err
	}
	inv := MigrationInventory{
		Tables:          map[string]bool{},
		StreamKinds:     map[string]bool{},
		DefinitionKinds: map[string]bool{},
		ColumnChecks:    map[string]map[string]bool{},
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return MigrationInventory{}, errors.New("storagemanifest: no *.sql migration files found in " + migrationsDir)
	}
	for _, name := range files {
		path := filepath.Join(migrationsDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return MigrationInventory{}, err
		}
		text := string(body)
		for _, m := range createTablePattern.FindAllStringSubmatch(text, -1) {
			inv.Tables[strings.ToLower(m[1])] = true
		}
		for _, m := range checkInPattern.FindAllStringSubmatch(text, -1) {
			column := strings.ToLower(m[1])
			values := map[string]bool{}
			for _, v := range quotedPattern.FindAllStringSubmatch(m[2], -1) {
				values[v[1]] = true
			}
			if len(values) == 0 {
				continue
			}
			if inv.ColumnChecks[column] == nil {
				inv.ColumnChecks[column] = map[string]bool{}
			}
			for v := range values {
				inv.ColumnChecks[column][v] = true
			}
			switch {
			case strings.Contains(column, "stream_kind"):
				for v := range values {
					inv.StreamKinds[v] = true
				}
			case strings.Contains(column, "definition_kind"):
				for v := range values {
					inv.DefinitionKinds[v] = true
				}
			}
		}
		inv.SourceFiles = append(inv.SourceFiles, name)
	}
	return inv, nil
}

// HasTable reports whether name (case-insensitive) is a real CREATE TABLE
// target in the scanned migrations.
func (inv MigrationInventory) HasTable(name string) bool { return inv.Tables[strings.ToLower(name)] }

// HasStreamKind reports whether kind is a declared ledger_stream.stream_kind value.
func (inv MigrationInventory) HasStreamKind(kind string) bool { return inv.StreamKinds[kind] }

// ColumnCheck returns the real allow-list CHECK constraint values found for
// column (case-insensitive), and whether one exists at all.
func (inv MigrationInventory) ColumnCheck(column string) (map[string]bool, bool) {
	v, ok := inv.ColumnChecks[strings.ToLower(column)]
	return v, ok
}
