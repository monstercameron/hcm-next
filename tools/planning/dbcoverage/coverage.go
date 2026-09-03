// Package dbcoverage checks that the published model corpus has an explicit
// physical (database) or non-physical disposition.  It is deliberately a
// pure package: callers choose how to load the model registry and can run it
// in CI without a database connection or generated-code side effects.
package dbcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/storagemanifest"
)

// Kind is the model object kind reported by the checker.
type Kind string

const (
	Entity       Kind = "ENTITY"
	Property     Kind = "PROPERTY"
	Relationship Kind = "RELATIONSHIP"
	State        Kind = "STATE"
	Transition   Kind = "TRANSITION"
)

// Disposition is an affirmative answer to where an object lives.  DATABASE
// is intentionally broad: the storage manifest supplies the precise table,
// event, artifact or projection target.
type Disposition string

const (
	Database    Disposition = "DATABASE"
	NonDatabase Disposition = "NON_DATABASE"
)

// Object is the small, source-oriented contract accepted by Check. Source is
// retained in diagnostics so a failed CI run points at the exact model file.
type Object struct {
	Kind        Kind
	Name        string
	Source      string
	Owner       string
	Disposition Disposition
	StorageRef  string
}

// Gap identifies one object whose disposition is absent or incomplete.
type Gap struct {
	Kind   Kind
	Name   string
	Source string
	Detail string
}

func (g Gap) String() string {
	return fmt.Sprintf("%s %s (%s): %s", g.Kind, g.Name, g.Source, g.Detail)
}

// Report is deterministic and suitable for a checked-in CI artifact.
type Report struct {
	Total         int
	Verified      int
	ByKind        map[Kind]int
	ByFile        map[string]int
	ByDisposition map[Disposition]int
	Gaps          []Gap
	Digest        string
}

// FullyVerified reports whether every supplied object has one complete
// disposition.
func (r Report) FullyVerified() bool { return r.Total == r.Verified && len(r.Gaps) == 0 }

// Check validates objects without inferring a disposition from names. This
// is the important safety property of DB-COVERAGE-001: adding an object
// cannot silently pass because it happens to resemble a table.
func Check(objects []Object) Report {
	r := Report{ByKind: map[Kind]int{}, ByFile: map[string]int{}, ByDisposition: map[Disposition]int{}}
	rows := append([]Object(nil), objects...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Source != rows[j].Source {
			return rows[i].Source < rows[j].Source
		}
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		if rows[i].Owner != rows[j].Owner {
			return rows[i].Owner < rows[j].Owner
		}
		if rows[i].Disposition != rows[j].Disposition {
			return rows[i].Disposition < rows[j].Disposition
		}
		return rows[i].StorageRef < rows[j].StorageRef
	})
	for _, o := range rows {
		r.Total++
		r.ByKind[o.Kind]++
		source := o.Source
		if source == "" {
			source = "<unknown>"
		}
		r.ByFile[source]++
		if !validKind(o.Kind) {
			r.Gaps = append(r.Gaps, Gap{o.Kind, o.Name, source, "unknown model object kind"})
			continue
		}
		if strings.TrimSpace(o.Name) == "" {
			r.Gaps = append(r.Gaps, Gap{o.Kind, o.Name, source, "object has no canonical name"})
			continue
		}
		if strings.TrimSpace(o.Owner) == "" {
			r.Gaps = append(r.Gaps, Gap{o.Kind, o.Name, source, "object has no owner"})
			continue
		}
		if o.Disposition != Database && o.Disposition != NonDatabase {
			r.Gaps = append(r.Gaps, Gap{o.Kind, o.Name, source, "missing database or non-database disposition"})
			continue
		}
		if strings.TrimSpace(o.StorageRef) == "" {
			detail := "non-database disposition has no schema target"
			if o.Disposition == Database {
				detail = "database disposition has no storage reference"
			}
			r.Gaps = append(r.Gaps, Gap{o.Kind, o.Name, source, detail})
			continue
		}
		r.Verified++
		r.ByDisposition[o.Disposition]++
	}
	r.Digest = digest(rows)
	return r
}

func validKind(k Kind) bool {
	switch k {
	case Entity, Property, Relationship, State, Transition:
		return true
	default:
		return false
	}
}

func digest(rows []Object) string {
	h := sha256.New()
	for _, o := range rows {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00", o.Kind, o.Name, o.Source, o.Disposition, o.StorageRef)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// FromManifests adapts the canonical model and DB-002 manifest into this
// checker. Properties and relationships inherit the owning entity's
// disposition; lifecycle rows are represented by the entity's registered
// lifecycle assignment and are therefore checked as non-database metadata.
func FromManifests(reg *model.Registry, storage storagemanifest.DispositionManifest, source string) []Object {
	byKey := make(map[string]storagemanifest.EntityDisposition, len(storage.Entities))
	for _, row := range storage.Entities {
		byKey[row.Key] = row
	}
	var out []Object
	for _, e := range reg.Entities() {
		row := byKey[e.Key]
		d := Disposition("")
		ref := row.Target
		if row.Disposition != storagemanifest.DispositionMismatch {
			d = Database
		}
		out = append(out, Object{Kind: Entity, Name: e.Ref.String(), Source: source, Owner: row.Owner, Disposition: d, StorageRef: ref})
		for _, p := range reg.Properties() {
			if p.Entity == e.Ref {
				out = append(out, Object{Kind: Property, Name: string(p.Ref), Source: source, Owner: row.Owner, Disposition: d, StorageRef: ref})
			}
		}
		if e.LifecycleAssignment != "" {
			out = append(out, Object{Kind: State, Name: e.Ref.String() + "/lifecycle", Source: source, Owner: e.Ref.String(), Disposition: NonDatabase, StorageRef: "lifecycle:" + e.LifecycleAssignment})
		}
	}
	for _, rel := range reg.Relationships() {
		out = append(out, Object{Kind: Relationship, Name: rel.Ref.String(), Source: source, Owner: rel.SourceEntity.String(), Disposition: NonDatabase, StorageRef: "registry"})
	}
	return out
}
