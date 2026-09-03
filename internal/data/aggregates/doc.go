// Package aggregates is the thin Go read/append adapter over the DB-008,
// DB-009 and DB-010 bitemporal aggregate tables (owner: data plane; phase:
// P1A):
//
//	DB-008  migrations/00011_people_aggregates.sql        Person, IdentityClaim, Worker, Employment, Assignment
//	DB-009  migrations/00012_organization_aggregates.sql  LegalEntity, OrganizationUnit, Job, JobPosition, PositionOccupancy
//	DB-010  migrations/00013_compensation_aggregates.sql  CompensationPackage, CompensationComponent, CompensationBand, WorkforceBudget, BudgetReservation
//
// # Shape
//
// Every table is one bitemporal fact log rather than a root/revision pair: a
// single row carries the entity's own stable identity (entity_id, a plain
// uuid, plus canonical_id, the eid:v1:<kind>:<uuid> canonical text from
// internal/kernel/values) together with its attribute values, dated on two
// independent axes:
//
//	EffectiveFrom / EffectiveTo   business (valid) time, half-open [from, to)
//	RecordedAt / SupersededAt     system (transaction) time, half-open
//
// Put is the only way this package ever changes what a table says: it closes
// out (sets SupersededAt on) whichever live row for the same entity would
// otherwise overlap the new row's business-time range, then inserts the new
// row. Those two statements must commit or roll back together, so every
// caller passes a *pgx.Tx it began itself (see store.go's Executor doc) --
// never a bare *pgx.Conn -- whenever the entity being written might already
// have a live row. The migrations' aggregate_forbid_inplace_update
// trigger makes that the only mutation any of these tables ever accept -- a
// direct UPDATE of a business column, from anywhere other than that one
// controlled transition, is refused at the database level. CurrentAsOf reads
// "what holds true as of business time T, given everything recorded by
// system time now"; KnownAsOf additionally bounds the system-time axis, so a
// KnownAsOf query pinned to a past instant sees exactly what a caller could
// have seen at that instant, even after a later Put has since superseded the
// row it returns.
//
// # Import boundary
//
// This package imports internal/kernel/values (canonical identity, decimal
// money) and internal/ledger's port types only, plus pgx and the standard
// library. It does not import internal/data/ledger, internal/data/bitemporal,
// internal/data/tenancy, internal/data/schema, internal/intent/model or any
// internal/domains/* package directly; every table it owns is new, and every
// row it writes carries its own tenant_id column rather than depending on
// another package's session-scoping helper. A caller that wants row level
// security enforced sets the app.tenant_id session/transaction setting the
// same way internal/data/tenancy.WithTenant does (see that package's doc
// comment); this package does not re-export that helper, to keep the import
// list exactly as scoped.
//
// # Foreign keys go through the identity spine, never to a specific version
//
// A cross-entity reference (Worker.PersonRef, Assignment.EmploymentRef, ...)
// cannot foreign-key straight to person/employment/...: the referenced row
// is one version among many sharing that entity_id, and which version a
// reference resolves to depends on the reader's own AsOf/KnownAt bounds, so a
// static FK to a specific row would either pin the reference to one
// arbitrary version or refuse to let history exist. Resolving a reference at
// the same temporal bounds the rest of a read uses remains the caller's
// responsibility, the same tradeoff migrations/00005_ledger.sql already
// makes for ledger_event's source_ref/schema_ref.
//
// What every table's entity_id and every reference column does foreign-key
// to is aggregate_entity (migrations/00011): one row per (tenant_id,
// entity_id), registered by Put's internal upsert before that entity's first
// fact row. This still proves "some tenant-matched entity with this id
// exists" for every reference -- catching a wrong-tenant id, a typo'd uuid,
// or a reference to an entity nothing ever created -- without needing to
// pick a version. It does not prove the reference is the *right kind* of
// entity (aggregate_entity does not enforce that a person_ref names a person
// rather than a legal_entity); a caller wanting that checks Kind itself.
//
// # Fixture loader
//
// Loader (loader.go) parses the embedded testdata corpus -- copied verbatim
// from internal/domains/fixtures/testdata, which this package does not
// import -- and appends the corresponding rows across all fifteen tables in
// one tenant, so that DB-019's seeder and the promotion domain can read real
// rows through this adapter without hand-building fixtures a second time.
package aggregates
