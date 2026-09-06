// Package binding answers one question for every published capability, and
// refuses to answer it vaguely: which exact wire method carries it, which
// exact typed Go symbol implements it, and which exact generated model
// entities and properties it reads and writes.
//
// BIND-001 ("Bind each published capability to exact models, wire
// descriptors and one typed Go handler") states the bar as three
// simultaneous facts per capability, not three separate registries that
// happen to mention the same identifier:
//
//   - exactly one wire descriptor, named "<ServiceFullName>/<MethodName>",
//     read from the compiled generated ServiceDescs of the four registered
//     services (intents, registry, admin, journey) rather than retyped;
//   - exactly one typed Go handler symbol, named by module-relative package
//     path plus receiver and function name, whose existence is proved by an
//     AST scan of the live source tree (see [ScanHandlerSymbols]);
//   - the model entities and properties it reads and writes, resolved
//     through internal/intent/modelbinding against the generated
//     gen/go/hcmnext/model registry, never restated here.
//
// Anything short of all three is a typed [Gap], never a silent omission and
// never a plausible-looking guess. The gap kinds are exhaustive: a
// capability with no wire method, more than one wire method, no handler,
// more than one handler, a handler symbol that does not exist in the tree,
// or no resolvable model binding; a handler symbol claimed by two
// capabilities; and a wire method that no capability claims at all. A
// capability contributes an [Entry] only when it has no gaps — partial
// binding is not binding, the same rule internal/intent/modelbinding
// applies one layer down.
//
// # Purity
//
// This package is kernel-pure: [Build] reads compiled-in tables and
// generated descriptors, allocates, sorts and hashes. It opens no file,
// reads no clock, starts no server and imports no transport package
// (internal/capability is rank 3 in definitions/architecture/
// package-dependency-policy.yaml; internal/transport is rank 5, and
// importing upward is what that manifest exists to prevent). The AST scan
// that proves a handler symbol exists is therefore split in two: the pure
// parse over already-read sources lives here ([ScanHandlerSymbols]); the
// file reading lives in this package's tests, which walk the live tree.
//
// Like tools/uxqual/pagedef, wire.go reaches the generated grpc.ServiceDesc
// values without importing google.golang.org/grpc: LIB-003
// (definitions/architecture/library-firewall.yaml) confines that module to
// gen/, internal/transport, internal/intent/protomap, internal/engines/wire
// and the composition roots, and internal/capability/binding is none of
// them. Reading the exported fields of a value you already hold never
// requires naming the type, so this package never spells grpc.ServiceDesc.
//
// # What this package is not
//
// It is not a second capability registry (internal/capability owns
// publication), not a second endpoint manifest (internal/transport/manifest
// owns RPC exposure disposition), and not a second model registry
// (gen/go/hcmnext/model owns entities and properties). It is the join over
// those three, and its whole value is that the join is total and refuses to
// close a hole by inventing an edge.
package binding
