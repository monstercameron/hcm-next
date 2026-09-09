// Package modelbinding implements MSRC-009: it binds each drafted definition
// in [github.com/monstercameron/human-capital-management-suite/internal/intent/definitions] to the
// exact generated model behavior its
// [github.com/monstercameron/human-capital-management-suite/internal/intent.Binding] names — the
// aggregate roots its subjects resolve to, and the properties it reads and
// writes — by resolving every one of those bare strings against the
// generated registry
// [github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/model.Registry] (MSRC-007).
//
// This is a second, independent binding layer, not a replacement for
// [intent.CheckCoverage]: that checker proves a definition declares every
// required binding *element* (a subject kind, a decision, a lifecycle
// transition on all five dimensions, and so on) without knowing whether the
// entity or property strings inside those elements actually exist anywhere.
// modelbinding proves the strings resolve to real, generated model behavior,
// and adds one rule coverage cannot express: a WriteProperty naming a
// property [gen/go/hcmnext/model.PropertyMeta] marks Immutable is refused,
// not silently accepted.
//
// [Bind] is the general checker over any binding slice; [BindCatalog] runs
// it against the real fourteen drafted definitions
// ([definitions.NewRegistry], [definitions.Bindings]) and the real compiled
// generated registry ([model.New]) — the "exercise the real fourteen
// definitions against the real generated registry" integration this todo's
// GREEN clause requires.
package modelbinding
