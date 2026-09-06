// Package uow implements DB-018: a Go repository and unit-of-work contract
// over the bitemporal aggregate tables migrations/00011-00013 already declare
// (owner: data plane; phase: P1A).
//
// # The gap this closes
//
// internal/data/aggregates' Put helpers (PutPerson, PutWorker, ...) already
// append a new bitemporal revision and supersede whichever live row it
// overlaps, but they take no caller-supplied version: a Put call supersedes
// whatever happens to be live at the moment it runs, whether or not the
// caller's own in-memory copy of the aggregate is still current. A caller
// that loaded revision N, computed a change against it, and then called
// PutWorker after someone else had already appended revision N+1 would
// silently supersede N+1 instead of N -- a classic lost update, and exactly
// the "bypasses expected version" RED case DB-018's todo names. This package
// adds the missing check without touching aggregates or its migrations:
// [AggregateRepository.Save] takes the version the caller last observed and
// refuses, rather than blindly writes, when the aggregate has moved on.
//
// # Version, without a version column
//
// migrations/00011 has no integer version column (its concurrency story is
// the bitemporal envelope itself: exactly one row per entity is ever live).
// [Version] here is the 1-based count of revisions ever appended for an
// entity -- 0 means "no revision exists yet". A [PostgresWorkerRepository]
// Save call checks that count atomically, in the same UPDATE statement that
// supersedes the current live row (or, for the first revision, in the same
// INSERT ... ON CONFLICT DO NOTHING that registers the entity in
// aggregate_entity): the WHERE clause is evaluated against the freshest
// committed row once a concurrent writer's lock on the same row releases, so
// a second writer racing the first either sees its predecessor already
// superseded or its expected count already stale, and its statement affects
// zero rows either way. That is the same compare-and-swap shape
// internal/data/runtimestate's stored version columns use (queue_version,
// item_version, ...); this package derives the count instead of storing it,
// because the migration this repository must not touch never declared a
// column to store it in.
//
// # Unit of work
//
// [Begin] opens one transaction bound to one tenant (internal/data/tenancy's
// WithTenant, called once as Begin's own first statement). [Register] binds
// an [AggregateRepository] under a caller-chosen kind name; [Stage] queues
// one aggregate append against that kind's repository. [UnitOfWork.Commit]
// runs every staged append against the one transaction Begin opened and
// commits only if every one of them succeeds -- the first conflict rolls the
// whole transaction back and returns a [*ConflictError] naming which kind,
// which entity, and the expected versus actual version. There is no Begin
// method on [UnitOfWork] and Commit refuses a second call: nesting a unit of
// work inside another is not a shape this package's API can express.
//
// # What ships here
//
// [PostgresWorkerRepository] is the one pgtest-backed reference
// implementation, over the Worker table migrations/00011 already declares
// (DB-008); no new migration. [MemoryRepository] is a storage-free fake
// implementing the same [AggregateRepository] contract for tests that want
// unit-of-work behavior without a database. [RunConformance] is the suite
// every [AggregateRepository] implementation -- this package's own two, and
// any later one over a different DB-008/009/010 table -- must pass.
package uow
