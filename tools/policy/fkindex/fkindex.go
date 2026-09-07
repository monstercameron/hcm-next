// Package fkindex audits foreign-key indexes and the data stores that use them.
package fkindex

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// ForeignKey is one live catalog foreign-key constraint.
type ForeignKey struct {
	Schema      string   `json:"schema"`
	Table       string   `json:"table"`
	Name        string   `json:"name"`
	Columns     []string `json:"columns"`
	Covered     bool     `json:"covered"`
	Partitioned bool     `json:"partitioned,omitempty"`
}

// Reference identifies a raw SQL literal that uses a foreign-key column.
type Reference struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// IndexSpec is the deterministic decision for one foreign key.
type IndexSpec struct {
	Schema       string      `json:"schema"`
	Table        string      `json:"table"`
	ForeignKey   string      `json:"foreign_key"`
	Name         string      `json:"name"`
	Columns      []string    `json:"columns"`
	Unreferenced bool        `json:"unreferenced"`
	References   []Reference `json:"references,omitempty"`
}

const auditSQL = `
WITH fk_rel AS (
    SELECT con.*,
           COALESCE(inh.inhparent, con.conrelid) AS relation_oid
      FROM pg_constraint con
      LEFT JOIN pg_inherits inh ON inh.inhrelid = con.conrelid
     WHERE con.contype = 'f'
)
SELECT n.nspname,
       c.relname,
       con.conname,
       array_agg(a.attname ORDER BY fk.ord)::text[],
       EXISTS (
         SELECT 1
           FROM pg_index i
          WHERE i.indrelid = con.relation_oid
            AND i.indisvalid
            AND i.indisready
            AND i.indnkeyatts >= cardinality(con.conkey)
            AND NOT EXISTS (
                SELECT 1
                  FROM unnest(con.conkey) WITH ORDINALITY AS f(attnum, ord)
                  LEFT JOIN LATERAL unnest(i.indkey[0:i.indnkeyatts-1]) WITH ORDINALITY AS x(attnum, ord)
                    ON x.ord = f.ord
                 WHERE x.attnum IS DISTINCT FROM f.attnum
            )
       ),
       EXISTS (SELECT 1 FROM pg_partitioned_table p WHERE p.partrelid = con.relation_oid)
  FROM fk_rel con
  JOIN pg_class c ON c.oid = con.relation_oid
  JOIN pg_namespace n ON n.oid = c.relnamespace
  JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS fk(attnum, ord) ON true
  JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = fk.attnum
 WHERE con.contype = 'f'
   AND n.nspname = current_schema()
 GROUP BY n.nspname, c.relname, con.conname, con.conrelid, con.conkey, con.relation_oid
 ORDER BY n.nspname, c.relname, con.conname`

// Audit reads the live PostgreSQL catalog. An index covers an FK only when its
// leading key columns equal the constrained columns in the same order.
func Audit(ctx context.Context, q dbport.Querier) ([]ForeignKey, error) {
	rows, err := q.Query(ctx, auditSQL)
	if err != nil {
		return nil, fmt.Errorf("fkindex: query catalog: %w", err)
	}
	defer rows.Close()

	var out []ForeignKey
	for rows.Next() {
		var fk ForeignKey
		var schema string
		if err := rows.Scan(&schema, &fk.Table, &fk.Name, &fk.Columns, &fk.Covered, &fk.Partitioned); err != nil {
			return nil, fmt.Errorf("fkindex: scan catalog: %w", err)
		}
		// The catalog schema is the per-test pgtest schema, not a stable
		// migration identifier. The audit is deliberately search_path scoped.
		fk.Schema = ""
		out = append(out, fk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fkindex: catalog rows: %w", err)
	}
	return out, nil
}

// References scans non-test Go files beneath internal/data. Only raw string
// literals are considered SQL, and a literal is a reference when it contains
// the table and one FK column together with a WHERE, JOIN, ON, or USING token.
func References(root string, fks []ForeignKey) (map[string][]Reference, error) {
	out := make(map[string][]Reference, len(fks))
	for _, fk := range fks {
		out[fkKey(fk)] = nil
	}
	base := filepath.Join(root, "internal", "data")
	fset := token.NewFileSet()
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") || filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || len(lit.Value) < 2 || lit.Value[0] != '`' {
				return true
			}
			text := lit.Value[1 : len(lit.Value)-1]
			lower := strings.ToLower(text)
			for _, fk := range fks {
				if !sqlReference(lower, fk) {
					continue
				}
				line := fset.Position(lit.Pos()).Line + strings.Count(text[:referenceOffset(lower, fk)], "\n")
				ref := Reference{File: filepath.ToSlash(mustRel(root, path)), Line: line}
				key := fkKey(fk)
				if !containsReference(out[key], ref) {
					out[key] = append(out[key], ref)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fkindex: scan data SQL: %w", err)
	}
	for key := range out {
		sort.Slice(out[key], func(i, j int) bool {
			if out[key][i].File != out[key][j].File {
				return out[key][i].File < out[key][j].File
			}
			return out[key][i].Line < out[key][j].Line
		})
	}
	return out, nil
}

// Plan returns referenced missing indexes and visible unreferenced decisions.
func Plan(fks []ForeignKey, refs map[string][]Reference) []IndexSpec {
	var out []IndexSpec
	byName := make(map[string]int)
	for _, fk := range fks {
		if fk.Covered {
			continue
		}
		locations := append([]Reference(nil), refs[fkKey(fk)]...)
		spec := IndexSpec{
			Schema:     fk.Schema,
			Table:      fk.Table,
			ForeignKey: fk.Name,
			Name:       indexName(fk.Table, fk.Columns),
			Columns:    append([]string(nil), fk.Columns...),
			References: locations,
		}
		if len(locations) == 0 {
			spec.Unreferenced = true
		}
		if prior, ok := byName[spec.Name]; ok {
			out[prior].References = append(out[prior].References, locations...)
			out[prior].References = uniqueReferences(out[prior].References)
			continue
		}
		byName[spec.Name] = len(out)
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func uniqueReferences(refs []Reference) []Reference {
	out := refs[:0]
	for _, ref := range refs {
		if !containsReference(out, ref) {
			out = append(out, ref)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func fkKey(fk ForeignKey) string { return fk.Schema + "." + fk.Table + "." + fk.Name }

func indexName(table string, columns []string) string {
	name := "ix_" + table + "_" + strings.Join(columns, "_")
	if len(name) <= 63 {
		return name
	}
	return name[:63]
}

func sqlReference(sql string, fk ForeignKey) bool {
	if !hasToken(sql, fk.Table) || !hasClause(sql) {
		return false
	}
	for _, col := range fk.Columns {
		if hasToken(sql, col) {
			return true
		}
	}
	return false
}

func referenceOffset(sql string, fk ForeignKey) int {
	table := strings.Index(sql, fk.Table)
	for _, col := range fk.Columns {
		if i := strings.Index(sql, col); i >= 0 && (table < 0 || i < table) {
			table = i
		}
	}
	if table < 0 {
		return 0
	}
	return table
}

func hasClause(sql string) bool {
	for _, word := range []string{"where", "join", " on ", "using"} {
		if word == " on " {
			if strings.Contains(" "+sql+" ", word) {
				return true
			}
			continue
		}
		if hasToken(sql, word) {
			return true
		}
	}
	return false
}

func hasToken(text, want string) bool {
	want = strings.ToLower(strings.Trim(want, `"`))
	for i := 0; i < len(text); {
		idx := strings.Index(text[i:], want)
		if idx < 0 {
			return false
		}
		idx += i
		beforeOK := idx == 0 || !isIdent(text[idx-1])
		end := idx + len(want)
		afterOK := end == len(text) || !isIdent(text[end])
		if beforeOK && afterOK {
			return true
		}
		i = idx + 1
	}
	return false
}

func isIdent(r byte) bool { return r == '_' || unicode.IsLetter(rune(r)) || unicode.IsDigit(rune(r)) }

func containsReference(refs []Reference, want Reference) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
