// Package deferredschema generates review-only migration previews and a
// storage-disposition preview for the ten future HCM domains that have no
// funded production authority yet: payroll, benefits, time, leave,
// recruiting, talent, learning, case, access and regulatory (DB-016).
//
// It reads the entity vocabulary for those domains from the checked-in model
// documents under planning/data/models (rewards-payroll-workforce.md,
// talent-experience-cases.md, connectivity-access-content.md) and distills
// one representative aggregate pair per domain -- a mutable "head" table
// (current state) and an append-only "evidence" table (its immutable
// revision/ledger trail) -- into domains.go. This mirrors the shape DB-023
// already committed to for the Leave domain (LeaveRequest/LeaveRecord) and
// the same OPERATIONAL/PERMANENT split migration 00023's journey_worker
// documents for a single append-only aggregate.
//
// Generate renders each domain into a goose-shaped migration preview
// (tenant_ref FK, RLS tenant_isolation policy, forbid_mutation trigger on the
// append-only table) and one row per table in
// definitions/storage/storage-disposition.yaml's exact shape plus a
// disposition field (DRAFT or CONFORMANCE). Nothing this package renders is
// ever applied to migrations/ or definitions/: WritePreviewSet only ever
// writes under testdata/preview, and generate_test.go's golden test proves
// the checked-in preview files are byte-identical to what Generate produces
// today, so they cannot silently drift from this package's own logic.
//
// Disposition rule: a domain is CONFORMANCE only when planning/todos.md
// already carries a funded, dependency-tracked "materialize this domain"
// todo (today, only DB-023 for Leave). Every other domain is DRAFT: pure
// exploratory vocabulary with no funded definition, exactly as
// planning/data/models/README.md's "Coverage rule" describes. Validate
// checks this distinction actually holds, not just that the field is one of
// the two allowed strings.
//
// validate.go is the second half of the contract: it parses every preview
// file, proves every table declares row-level security and every append-only
// table also carries a forbid_mutation trigger, and cross-checks that no
// preview table name already exists as a live migrations/ table or as a
// declared write in the Phase 1 capability registry (internal/capability) --
// the two ways a deferred domain could accidentally acquire production
// authority.
package deferredschema
