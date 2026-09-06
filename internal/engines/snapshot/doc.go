// Package snapshot is the source-neutral resolver behind SNAPSHOT-001: given
// a caller-declared ConsistencyRequirement and the exact business inputs a
// caller names, it returns one immutable ReadSnapshot binding a canonical
// digest over typed InputEntry values, or refuses with a distinct sentinel
// error naming exactly what was missing, mismatched or untrustworthy.
//
// Semantic owner: shared-engines. Phase: P1A.
//
// # Kernel-pure and source-neutral
//
// The package touches no database and no migration, and -- per
// definitions/architecture/package-dependency-policy.yaml's
// engine-must-not-import-domain-implementation rule -- it imports nothing
// under internal/domains. That is a deliberate consequence of what
// "source-neutral" has to mean: a domain's own native state (internal/
// domains/people's WorkerFacts, internal/data/workforce's
// LayeredWorkerFacts), another system's reported observation (internal/
// connectivity's external captures) and a versioned reference/config
// artifact cannot be told apart by which Go type carries them into this
// package, because this package only ever sees one type -- InputEntry.
// AuthorityClass is the one field that says which kind of source produced an
// entry (NATIVE_STATE, EXTERNAL_OBSERVATION or REFERENCE_CONFIG); shape never
// leaks that distinction, only the descriptor does.
//
// # Resolve refuses rather than repairs
//
// Resolve treats every requested InputRequest as mandatory and every
// InputEntry the configured Source answers with as untrusted until proven
// otherwise. A missing required descriptor on an entry, an entry the caller
// never requested, a requested input the source never answered, a duplicate
// answer, a tenant that does not match the resolve's own requirement, a
// known-at outside the declared horizon, a returned authority that does not
// match what the caller pinned for that input, or a watermark below a
// declared floor are each refused through their own sentinel error rather
// than folded into one generic failure -- so a caller (or a test) can tell
// exactly which contract broke.
//
// # What later todos add
//
// SNAPSHOT-002 layers REQUIRED/OPTIONAL/CONDITIONAL per-input policy and
// execution-time barrier waits on top of this package's current all-or-
// nothing baseline, where every requested input must resolve or the whole
// read is refused. SNAPSHOT-003 turns a resolved (or refused) snapshot into a
// domain-facing completeness verdict such as COMPLETE, PARTIAL or DENIED.
// Neither widens what this package promises on its own: a ReadSnapshot this
// package returns has already satisfied every requirement it was asked to
// satisfy, for every input it was asked to resolve.
package snapshot
