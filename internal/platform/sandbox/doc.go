// Package sandbox implements SANDBOX-001: a synthetic Promotion sandbox with
// a hard side-effect fence, on top of the SANDBOX/EXECUTE contract
// internal/intent.ModeContractFor already declares (INTENT-023).
//
// # What a sandbox is
//
// [Sandbox] composes the real application - the same *internal/intent/app.Cell
// a production cell runs, wired through the same explicit Option-style seams
// internal/application's composition root uses (ARCH-GO-020) - against a
// tenant that is marked as synthetic by construction: its slug always carries
// the [TenantPrefix], so a row this package wrote can never be mistaken for a
// pilot tenant's, and TENANT-002/PROMO-002's own fixture corpus
// (internal/data/aggregates.LoadFixtures, by way of internal/data/seed.Seed)
// is what the tenant's domain truth is seeded from.
//
// The contract itself already guarantees what a sandboxed EXECUTE may do:
// [intent.ModeContractFor] for (ModeExecute, EnvironmentSandbox) allows a
// domain commit (the sandbox has truth of its own) and forbids an external
// effect and a live approval consumption, unconditionally, from a fixed table
// no caller can override. This package's own job starts where that table's
// authority ends: proving the composed cell never gets a live path to a real
// destination in the first place, regardless of what a caller configures.
//
// # The fence
//
// [Fence] is the hard side-effect fence itself: any adapter wired behind it
// that is asked to reach a destination this sandbox does not recognise as its
// own synthetic fixture refuses with a typed [FencedEffectError] naming the
// adapter and the destination, and records the attempt
// ([Fence.Refusals]) rather than silently dropping it. [FencedConnector]
// applies this to the one adapter seam the composed P1A cell exposes for a
// real external system today, internal/connectivity.Connector
// (CellConfig.Incumbent): every read is checked against the sandbox's fixed
// allow-list of synthetic source references before it is ever forwarded to
// the wrapped connector, so a cell that got misconfigured with a live
// connector pointed at a real HRIS still cannot read through it. This is the
// REFACTOR property SANDBOX-001 asks for: fence enforcement lives in this
// package, not in whatever ServeConfig or Options a caller happened to build.
//
// The composed cell also opens no network listener at all: [Sandbox] calls
// the cell's *internal/intent/app.IntentService methods directly in the
// caller's own process, the same Go-level surface
// internal/transport/cell adapts onto gRPC. There is structurally no dial-out
// path for the proof in [Sandbox.RunPromotionProof] to reach, because nothing
// in this package ever binds a socket.
//
// # Reset
//
// [Sandbox.Reset] drops every row this sandbox's tenant owns - across every
// table in the schema that carries a tenant_id column, discovered from
// PostgreSQL's own catalog rather than a hand-maintained list, deleted in an
// order [dependencyOrder] derives from the schema's own foreign keys so a
// child row is always gone before the parent it references - and re-seeds the
// tenant from the same fixture corpus through internal/data/seed.Seed, whose
// content digest is what "restores a named baseline" means here: two resets
// of the same tenant produce the same [ResetReport.SeedDigest]. Every
// statement Reset issues is scoped by an exact tenant_id equality predicate,
// which is also what [TestTodo_SANDBOX_001_Security] and
// [TestTodo_SANDBOX_001_Race] hold onto: two sandboxes sharing one schema
// never touch each other's rows, concurrently or otherwise.
package sandbox
