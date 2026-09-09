# Go Technology Constitution

## Binding Decision

The Human Capital Management Suite product core is authored in Go: services, workflow runtime, domain
packages, data access, connectors, operations workers, and command-line tools.
Protobuf/gRPC is the canonical contract. PostgreSQL is the initial store.

```text
                    HCM NEXT CODEBASE

                     Go product core
                            |
          +-----------------+-----------------+
          v                 v                 v
   UI                  transport edge      definition generation
   preferred: GWC      preferred: grpcbridge  preferred: SchemaFlux
   fallback: Go SSR    fallback: grpc-gateway fallback: protoc + Go
             HTML              / connect-go            codegen
          +-----------------+-----------------+
                            |
                            v
                  Go domain/runtime packages
                            |
                            v
              Protobuf/gRPC + PostgreSQL + OpenTelemetry
```

Three house libraries are the preferred choice for their roles. A preference
is not a dependency: each library becomes a release dependency only after it
passes the qualification fixture named below, and each has a named fallback
that the release uses otherwise. The 2026-08-14 audit found that SchemaFlux
is an LLM operations library rather than the deterministic schema compiler
earlier drafts assumed, and that grpcbridge covers a narrower transport and
authentication boundary than claimed. Those findings are why qualification is
a gate and not a formality.

## Preferred Libraries and Their Qualification Fixtures

### GWC / GoWebComponents (preferred UI)

GWC owns the browser and presentation runtime when qualified:

- Go-authored components, pages, forms, state, routing, and interaction.
- Go/WASM and server-rendered output where appropriate.
- Design tokens, localization, accessibility, secure field rendering, and
  workflow-action binding.

Qualification fixture, run before P1B: the Promotion request, approval, and
timeline workspace passes keyboard-only completion, a screen-reader pass
(NVDA on Windows, VoiceOver on macOS), WCAG 2.2 AA contrast and reflow checks,
and renders provenance-bearing bindings with field masking applied before the
component tree. If GWC fails the fixture and cannot be fixed inside the P1B
window, the workspace ships as Go server-rendered HTML with progressive
enhancement, and GWC is re-evaluated for a later release.

Any minimal JavaScript or WebAssembly loader emitted by GWC is generated build
output and may not contain handwritten business logic.

### grpcbridge (preferred transport edge)

grpcbridge owns transport adaptation when qualified:

```text
browser / customer / system request
              |
        HTTP | gRPC-Web
              |
     grpcbridge (or fallback)
              |
       canonical gRPC service
```

Qualification fixture, run in M2: the same Protobuf vector, digest,
server-derived principal, authorization result, and typed error round-trip
through direct gRPC and through the edge; unauthenticated metadata cannot
select trusted context; deadlines and cancellation propagate. WebSocket and
SSE are not Phase 1 requirements. If grpcbridge fails, the edge is
grpc-gateway or connect-go, both of which have passed this fixture elsewhere.

### SchemaFlux (preferred definition generation)

SchemaFlux is used only for what it is proven to do: generating Go registries,
documentation, and fixtures from structured definition files, offline and
deterministically.

Qualification fixture, run in M2: a bounded catalog (the fourteen intent
definitions and the P1A capability manifests) compiles to byte-identical output
across two runs and two machines with no network access and no model call. If
that fails, definitions are plain Protobuf and generation is protoc with Go
code generation; the registry is a compiled-in Go table as the capability
registry's `BOOTSTRAP` profile already requires.

SchemaFlux never becomes a second business schema, and Protobuf remains the
contract authority in either case.

## Supporting Technology

“Core libraries” does not mean Human Capital Management Suite must implement databases, cryptography,
or protocols from scratch. Supporting infrastructure may include PostgreSQL,
Protobuf/gRPC, OpenTelemetry, object storage, operating-system facilities, and
other reviewed open-source or managed infrastructure.

Selection order is:

```text
Go standard library
      |
Human Capital Management Suite core libraries
      |
small maintained open-source Go package
      |
portable managed infrastructure when operationally safer
```

Every dependency requires a clear owner, pinned version, license review, SBOM
entry, vulnerability response, upgrade test, operational-cost assessment, and
replacement or maintained-fork strategy.

## What the Release Excludes and What Development May Use

Excluded from the release image and the production request path:

- TypeScript or JavaScript application source on the request path.
- React, Vite, Next.js, or another JavaScript UI runtime.
- Node API, workflow, integration, agent, or background-worker services.
- A parallel REST business implementation beside the gRPC contract.
- Schema definitions maintained separately from the Protobuf sources.
- A plugin model that executes arbitrary JavaScript inside the trusted runtime.

Permitted in the development toolchain, and excluded from the release:

- Node-based browser test runners (Playwright or equivalent) for the
  accessibility and interaction fixtures; a Go-native driver may replace them
  when it covers the same assertions.
- Formatters, linters, and documentation tooling of any language.

The line is the release SBOM: nothing in the excluded list appears in it.

## Legacy Repository Treatment

Existing Node/TypeScript and React files are historical implementation evidence
and a comparison baseline:

```text
legacy code and docs
       |
extract behaviors, fixtures, schemas, events, and acceptance cases
       |
       v
implement the Go vertical slice
       |
       v
P1A: run beside legacy; compare outcomes
       |
       v
P1B: Go owns the write path; legacy excluded from the release
```

Legacy code is not extended with new behavior. It may keep running beside the
Go slice through P1A so that customer evidence does not wait on the rewrite. A
Node compatibility service does not survive into P1B; if an old public endpoint
must remain compatible, the Go edge and capability service implement it.

## Target Repository Shape

```text
cmd/
  api/                 Go service entrypoint
  worker/              durable/background execution
  admin/               Go administrative CLI

internal/
  capabilities/        semantic capability implementations
  domains/             HCM domain packages
  workflow/            compiler and durable runtime
  authz/               policy and scope enforcement
  integrations/        connector runtime and adapters
  data/                repositories, ledger, projections, outbox
  platform/            identity, config, telemetry, reliability

ui/
  components/          GWC components
  pages/               typed PageDefinitions and Go pages
  assets/              governed static assets

schema/
  proto/               canonical service contracts
  schemaflux/          structured definition sources/backends

tests/
  conformance/         reference and legacy behavior fixtures
  integration/         connector/data/runtime tests
  browser/             GWC accessibility and interaction tests
```

Exact package names may evolve, but language and ownership boundaries do not.

## Release Gates

P1A (read-only, beside legacy):

- Go service, data, connector-read, and simulation paths.
- The qualified edge (grpcbridge or fallback) passing its M2 fixture.
- The qualified generator (SchemaFlux or protoc) passing its M2 fixture.
- Go-native unit, integration, and conformance tests.

P1B (Go-owned write path):

- Everything in P1A plus the workflow runtime and outbox in Go.
- The workspace passing the UI qualification fixture on GWC or the Go SSR
  fallback.
- A release image and SBOM containing no Node, TypeScript, React, or Vite
  runtime; the development toolchain is not in scope of this check.
- Legacy TypeScript/React artifacts excluded from the release image.
