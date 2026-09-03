// Package connectivity is the read-only connectivity plane: the ports through
// which HCM Next observes an incumbent HRIS without changing it.
//
// Semantic owner: connectivity. Phase: P1A.
//
// # The read-only claim is structural, not documentary
//
// P1A is sold as a paid observation, preflight and simulation release: running
// it must leave the incumbent byte-for-byte unchanged. A sentence promising
// that is worth nothing, so the promise is made by the shape of the types.
// [Connector] exposes no method that could mutate an external system, and no
// method returns a handle that could. There is no Write, no Create, no Apply,
// no generic Do. A connector implementation that wanted to write would have to
// grow a method outside this interface, which [TestTodo_INTG_008] style
// reflection over the method set rejects.
//
// # What this package owns
//
//   - [ConnectorDefinition] and [Registry]: the immutable, versioned catalogue
//     of what a named connector version can do (INTG-001). A published version
//     never changes; a change is a new version with a new digest.
//   - [ConnectorConnection]: one tenant's bound instance of a definition, with
//     an explicit lifecycle legality table and evidence on every transition
//     (INTG-002). Credentials appear only as opaque [CredentialRef] values.
//   - [Connector], [ReadRequest], [Page] and [Cursor]: bounded observation of
//     external records with stable-tie-break pagination.
//   - [Error]: the four classes a caller must be able to tell apart -
//     credential, permission, schema and transient - plus the bounds and
//     cursor classes the pagination contract needs.
//
// Persisting what was observed is the job of the observe subpackage; framing
// an in-memory incumbent to observe is the job of fakeincumbent.
//
// # Dependency direction
//
// This package defines ports. Domain, capability and application packages
// adapt to the interfaces declared here; nothing here imports them.
package connectivity
