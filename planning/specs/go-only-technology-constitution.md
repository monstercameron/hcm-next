# Go-Only Technology Constitution

## Binding Decision

HCM Next product, platform, UI, workflow, integration, agent, operations, build,
and developer-tooling code is authored in Go.

```text
                    HCM NEXT CODEBASE

                         Go only
                            |
          +-----------------+-----------------+
          v                 v                 v
   GWC / GoWebComponents  grpcbridge       SchemaFlux
      experience UI       transport edge   definitions/compiler
          +-----------------+-----------------+
                            |
                            v
                  Go domain/runtime packages
                            |
                            v
                 governed data/infrastructure
```

TypeScript, JavaScript application frameworks, Node services, and Node-based
build pipelines are not part of the target architecture.

## Core Libraries

### GWC / GoWebComponents

GWC owns the browser and presentation runtime:

- Go-authored components, pages, forms, state, routing, and interaction.
- Go/WASM and server-rendered output where appropriate.
- Design tokens, localization, accessibility, secure field rendering, and
  workflow-action binding.
- Generated transport clients wrapped by Go capability-facing packages.

Any minimal JavaScript or WebAssembly loader emitted by GWC is generated build
output. It may not contain handwritten business logic or become a second
application runtime.

### grpcbridge

grpcbridge owns supported transport adaptation:

```text
browser / customer / system request
              |
    HTTP | gRPC-Web | WebSocket | SSE
              |
         grpcbridge
              |
       canonical gRPC service
```

It handles protocol translation, streaming, metadata, cancellation, deadlines,
and transport errors. Domain policy, AuthZ, validation, workflow semantics, and
business error meaning remain in Go capability services.

### SchemaFlux

SchemaFlux owns repository-defined structured compilation and generation where
one source must produce multiple governed artifacts:

```text
definitions
  capability | workflow | data domain | classification | connector | widget
      |
      v
SchemaFlux parse -> normalize -> relate -> validate -> emit
      |
      +-- Go registries and validators
      +-- schemas and compatibility reports
      +-- fixtures and documentation
      +-- dependency/provenance indexes
```

SchemaFlux complements canonical Protobuf service contracts and database
migrations. It does not weaken runtime validation or create a second business
schema.

## Supporting Technology

“Core libraries” does not mean HCM Next must implement databases, cryptography,
or protocols from scratch. Supporting infrastructure may include PostgreSQL,
Protobuf/gRPC, OpenTelemetry, object storage, operating-system facilities, and
other reviewed open-source or managed infrastructure.

Selection order is:

```text
Go standard library
      |
HCM Next core libraries
      |
small maintained open-source Go package
      |
portable managed infrastructure when operationally safer
```

Every dependency requires a clear owner, pinned version, license review, SBOM
entry, vulnerability response, upgrade test, operational-cost assessment, and
replacement or maintained-fork strategy.

## Prohibited Target Dependencies

- TypeScript or JavaScript application source.
- React, Vite, Next.js, or another JavaScript UI runtime.
- Node API, workflow, integration, agent, or background-worker services.
- npm as a required production build or product-development toolchain.
- Duplicate REST and gRPC business implementations.
- Schema definitions maintained separately from Protobuf/SchemaFlux sources.
- A plugin model that executes arbitrary JavaScript inside the trusted runtime.

## Legacy Repository Treatment

Existing Node/TypeScript and React files are historical implementation evidence
only:

```text
legacy code and docs
       |
extract behaviors, fixtures, schemas, events, and acceptance cases
       |
       v
implement clean Go/GWC/grpcbridge/SchemaFlux vertical slice
       |
       v
run current conformance and outcome comparison
       |
       v
remove legacy runtime/build dependencies
```

Legacy code is not extended, deployed beside the Go platform, or retained as a
compatibility service. If an old public endpoint must remain compatible,
grpcbridge and the Go capability service implement that compatibility.

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

## Phase 1 Gate

Phase 1 cannot ship while a required product path depends on Node or TypeScript.
The gate requires:

- Go service, workflow, data, connector, and operations paths.
- GWC request, approval, simulation, inbox, and repair/timeline workspace.
- grpcbridge web and external transport behavior.
- SchemaFlux generation and drift checks for the selected bounded catalogs.
- Go-native unit, integration, conformance, fuzz, load, and browser tests.
- A build and deploy path that does not install Node or npm.
- Removal or explicit archival exclusion of legacy TypeScript/React artifacts
  from production images and release SBOMs.
