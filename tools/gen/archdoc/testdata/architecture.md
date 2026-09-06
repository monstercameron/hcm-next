# HCM Next Repository Architecture

Generated from the checked-in architecture manifests and the current Go package tree.

- Module: `github.com/monstercameron/hcm-next`
- Source graph: 3105b223e2d7a1b43de4c11cd77f3a507af330eef3cd1a5acb19a3f8aa35890c
- Package count: 651
- Within-module edge count: 1513
- Source manifests: `definitions/architecture/repository-layout.yaml`, `definitions/architecture/package-dependency-policy.yaml`, `definitions/architecture/dependency-roles.yaml`

## Declared layers and roots

| Layer | Declared root | Owner | Phase | Purpose |
| --- | --- | --- | --- | --- |
| application | `internal/application` | platform-foundation | P1A | Single explicit application composition root (ARCH-GO-020). Builds registries, governance, workflows, engines, domains, ports/adapters and worker roles for one process role from a validated Config value, and exposes the Start/Stop lifecycle cmd/* invokes. |
| trust | `internal/authn` | governance-and-trust | P1A | Authentication adapters: the tenant federation issuer registry (AUTHN-001) and the federation registry that binds issuers to the internal/trust/federation port. |
| kernel | `internal/kernel` | platform-foundation | P1A | Identity/reference/revision, temporal, decimal/money, digest and evidence primitives. |
| intent | `internal/intent` | intent-and-capability | P1A | BusinessIntent definitions, instances, families, lifecycle, proposals/change requests. |
| capability | `internal/capability` | intent-and-capability | P1A | Capability descriptors, modes, side-effect declarations, handlers/gateway/discovery. |
| governance | `internal/governance` | governance-and-trust | P1A | AuthZ/legal/privacy/entitlement/risk/DLP composition over independent policy subsystems. |
| workflow | `internal/workflow` | workflow-runtime | P1B | Workflow definition, compiler, durable runtime and thin step adapters. |
| engines | `internal/engines` | shared-engines | P1A | Reusable transform/rules/population/eligibility/etc. engines independent of domains. |
| domains | `internal/domains` | domain-teams | P1A | HCM domain packages (people, organization, position, compensation, ...). |
| transaction | `internal/transaction` | transaction-and-conflict | P1B | Transaction plan/participants/prepare/commit/receipt and conflict analysis (port/adapter). |
| data | `internal/ledger` | data-and-ledger | P1A | Append-only ledger event truth (port/adapter). |
| data | `internal/data` | data-and-ledger | P1A | Repositories, projections, outbox and other serving/distribution stores (port/adapter). |
| connectivity | `internal/humanwork` | connectivity | deferred | Human messaging (email/SMS/inbox/push/Slack/Teams); deferred beyond P1A scope. |
| connectivity | `internal/connectivity` | connectivity | P1A | System integration connectors, APIs, webhooks, files, government gateways. |
| trust | `internal/trust` | governance-and-trust | P1A | Identity/session/federation/workload-identity primitives. |
| operations | `internal/operations` | operations-and-assurance | P1A | Telemetry, SLOs, reconciliation, incidents, integrity, DR overlays. |
| platform | `internal/platform` | platform-foundation | P1A | Process bootstrap, build identity, config, telemetry and reliability plumbing shared by commands. |
| transport | `internal/transport` | experience-and-transport | P1A | Experience/API transport adaptation (grpcbridge or fallback edge, protocol exposure). |

## Allowed dependency edges

Ranked layers may depend on the same layer or a lower-ranked layer. Port packages are allowed dependencies for business layers; concrete adapters remain behind their ports.

| Importing layer | Allowed ranked layers | Port roots |
| --- | --- | --- |
| kernel | kernel | none |
| engines | kernel, engines | none |
| domains | kernel, engines, domains | `internal/transaction`, `internal/ledger`, `internal/data` |
| capabilities | kernel, engines, domains, capabilities | `internal/transaction`, `internal/ledger`, `internal/data` |
| workflow | kernel, engines, domains, capabilities, workflow | `internal/transaction`, `internal/ledger`, `internal/data` |
| transport | kernel, engines, domains, capabilities, workflow, transport | none |

Forbidden edge rules are evaluated by `tools/policy/depedge`: `kernel-must-not-import-upward`, `engine-must-not-import-domain-implementation`, `workflow-must-not-import-domain-persistence`, `transport-must-not-import-store`, `business-must-not-import-concrete-adapter`.

## Library firewall roots

Third-party modules are admitted only at the owning roots declared by `dependency-roles.yaml`. Empty root lists mean the module is not expected to be imported directly.

| Library selector | Role | Allowed import roots |
| --- | --- | --- |
| `golang.org/x/` | INFRASTRUCTURE_MECHANIC | none |
| `google.golang.org/` | INFRASTRUCTURE_MECHANIC | none |
| `go.opentelemetry.io/` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw` |
| `github.com/cockroachdb/apd/v3` | INFRASTRUCTURE_MECHANIC | `internal/kernel` |
| `github.com/fergusstrange/embedded-postgres` | DEV_TEST_ONLY | `test`, `tools`, `internal/data/pgtest` |
| `github.com/google/uuid` | INFRASTRUCTURE_MECHANIC | `internal/kernel`, `internal/intent`, `internal/ledger`, `internal/data`, `internal/connectivity`, `internal/transaction`, `internal/humanwork`, `internal/workflow`, `internal/operations/explorer`, `internal/operations/reconcile`, `internal/engines/wire/digest`, `cmd`, `test` |
| `github.com/jackc/pgx/v5` | INFRASTRUCTURE_MECHANIC | `internal/data`, `internal/ledger`, `migrations`, `internal/platform/bootstrap`, `cmd/hcmnext`, `cmd/migrate` |
| `github.com/lib/pq` | INFRASTRUCTURE_MECHANIC | `internal/data`, `internal/ledger`, `migrations` |
| `github.com/pressly/goose/v3` | INFRASTRUCTURE_MECHANIC | `migrations`, `cmd`, `internal/data/pgtest`, `internal/data/schema` |
| `github.com/sethvargo/go-retry` | INFRASTRUCTURE_MECHANIC | `internal/operations`, `internal/connectivity`, `internal/data` |
| `github.com/xi2/xz` | DEV_TEST_ONLY | `test`, `tools` |
| `golang.org/x/exp/typeparams` | DEV_TEST_ONLY | `tools` |
| `golang.org/x/mod` | DEV_TEST_ONLY | `tools` |
| `golang.org/x/net` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `tools/uxqual/forms`, `tools/uxqual/qual` |
| `golang.org/x/sync` | INFRASTRUCTURE_MECHANIC | `internal/platform`, `internal/operations`, `internal/workflow` |
| `golang.org/x/text` | INFRASTRUCTURE_MECHANIC | `internal/engines/wire/canonical`, `internal/kernel/values`, `internal/intent` |
| `golang.org/x/tools` | DEV_TEST_ONLY | `tools` |
| `google.golang.org/genproto/googleapis/rpc` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `gen` |
| `connectrpc.com/connect` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `cmd` |
| `github.com/monstercameron/GoGRPCBridge` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `cmd`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm` |
| `github.com/monstercameron/GoWebComponents/v5` | INFRASTRUCTURE_MECHANIC | `tools/uxqual` |
| `github.com/monstercameron/schemaflux` | DEV_TEST_ONLY | `tools/gen` |
| `go.opentelemetry.io/otel/sdk/metric` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw` |
| `google.golang.org/grpc` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `gen`, `tools/gen`, `cmd`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm` |
| `google.golang.org/grpc/cmd/protoc-gen-go-grpc` | DEV_TEST_ONLY | `tools` |
| `google.golang.org/protobuf` | INFRASTRUCTURE_MECHANIC | `gen`, `internal/transport`, `internal/intent/protomap`, `internal/engines/wire`, `tools/gen`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm`, `tools/quality/bufprotovalidatekit` |
| `gopkg.in/yaml.v3` | INFRASTRUCTURE_MECHANIC | `tools`, `internal/data/tenancy/storagedisposition`, `internal/platform/telemetry` |
| `honnef.co/go/tools` | DEV_TEST_ONLY | `tools` |

## Package inventory by declared root

### `cmd`

Composition-root command binaries. Every second-level directory name must be an approved command from approved_commands.initial.

- `github.com/monstercameron/hcm-next/cmd/frontenddev`
- `github.com/monstercameron/hcm-next/cmd/hcmctl`
- `github.com/monstercameron/hcm-next/cmd/hcmnext`
- `github.com/monstercameron/hcm-next/cmd/migrate`
- `github.com/monstercameron/hcm-next/cmd/projector`
- `github.com/monstercameron/hcm-next/cmd/scheduler`
- `github.com/monstercameron/hcm-next/cmd/worker`

### `api`

Reserved for API/service composition surfaces. May be empty until a phase requires a distinct package here; internal/transport is the current home for transport adaptation.

_No Go packages currently scanned._

### `definitions`

Machine-readable architecture, planning and policy manifests. Never executable; consumed by tools/policy and tools/planning.

_No Go packages currently scanned._

### `internal`

Semantic package roots. Every second-level directory name must be a declared root in internal_package_roots.

- `github.com/monstercameron/hcm-next/internal/a11y`
- `github.com/monstercameron/hcm-next/internal/agentsecurity`
- `github.com/monstercameron/hcm-next/internal/application`
- `github.com/monstercameron/hcm-next/internal/authn`
- `github.com/monstercameron/hcm-next/internal/authn/federation`
- `github.com/monstercameron/hcm-next/internal/authn/issuerregistry`
- `github.com/monstercameron/hcm-next/internal/authn/oidc`
- `github.com/monstercameron/hcm-next/internal/authn/subjectlink`
- `github.com/monstercameron/hcm-next/internal/capability`
- `github.com/monstercameron/hcm-next/internal/capability/binding`
- `github.com/monstercameron/hcm-next/internal/commercial`
- `github.com/monstercameron/hcm-next/internal/conformance/selectedjurisdiction`
- `github.com/monstercameron/hcm-next/internal/connectivity`
- `github.com/monstercameron/hcm-next/internal/connectivity/adapter`
- `github.com/monstercameron/hcm-next/internal/connectivity/adapter/connrt001`
- `github.com/monstercameron/hcm-next/internal/connectivity/diagnostics`
- `github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent`
- `github.com/monstercameron/hcm-next/internal/connectivity/mapping`
- `github.com/monstercameron/hcm-next/internal/connectivity/mapping/execute`
- `github.com/monstercameron/hcm-next/internal/connectivity/mappingprofile`
- `github.com/monstercameron/hcm-next/internal/connectivity/mft`
- `github.com/monstercameron/hcm-next/internal/connectivity/observe`
- `github.com/monstercameron/hcm-next/internal/connectivity/observe/adapters/postgres`
- `github.com/monstercameron/hcm-next/internal/connectivity/onboarding`
- `github.com/monstercameron/hcm-next/internal/connectivity/operation`
- `github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot`
- `github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot/adapters/postgres`
- `github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot/diff`
- `github.com/monstercameron/hcm-next/internal/connectivity/spi`
- `github.com/monstercameron/hcm-next/internal/connectivity/spi/spiconform`
- `github.com/monstercameron/hcm-next/internal/connectivity/transport`
- `github.com/monstercameron/hcm-next/internal/contractarchive`
- `github.com/monstercameron/hcm-next/internal/cryptoagility`
- `github.com/monstercameron/hcm-next/internal/customobject`
- `github.com/monstercameron/hcm-next/internal/data`
- `github.com/monstercameron/hcm-next/internal/data/accessstore`
- `github.com/monstercameron/hcm-next/internal/data/aggregates`
- `github.com/monstercameron/hcm-next/internal/data/analytics`
- `github.com/monstercameron/hcm-next/internal/data/artifacts`
- `github.com/monstercameron/hcm-next/internal/data/assetstore`
- `github.com/monstercameron/hcm-next/internal/data/assurancemeta`
- `github.com/monstercameron/hcm-next/internal/data/attestationstore`
- `github.com/monstercameron/hcm-next/internal/data/balancestore`
- `github.com/monstercameron/hcm-next/internal/data/benefitsstore`
- `github.com/monstercameron/hcm-next/internal/data/bitemporal`
- `github.com/monstercameron/hcm-next/internal/data/budgetstore`
- `github.com/monstercameron/hcm-next/internal/data/careerstore`
- `github.com/monstercameron/hcm-next/internal/data/cbastore`
- `github.com/monstercameron/hcm-next/internal/data/commercialstore`
- `github.com/monstercameron/hcm-next/internal/data/configregistry`
- `github.com/monstercameron/hcm-next/internal/data/contactstore`
- `github.com/monstercameron/hcm-next/internal/data/contentregistrystore`
- `github.com/monstercameron/hcm-next/internal/data/crmstore`
- `github.com/monstercameron/hcm-next/internal/data/customstore`
- `github.com/monstercameron/hcm-next/internal/data/dbport`
- `github.com/monstercameron/hcm-next/internal/data/demoworkforce`
- `github.com/monstercameron/hcm-next/internal/data/documentmeta`
- `github.com/monstercameron/hcm-next/internal/data/employeerelationsstore`
- `github.com/monstercameron/hcm-next/internal/data/equitystore`
- `github.com/monstercameron/hcm-next/internal/data/fxstore`
- `github.com/monstercameron/hcm-next/internal/data/governance`
- `github.com/monstercameron/hcm-next/internal/data/health`
- `github.com/monstercameron/hcm-next/internal/data/hrcasestore`
- `github.com/monstercameron/hcm-next/internal/data/identityprivacystore`
- `github.com/monstercameron/hcm-next/internal/data/inboundmsg`
- `github.com/monstercameron/hcm-next/internal/data/inbox`
- `github.com/monstercameron/hcm-next/internal/data/incentivestore`
- `github.com/monstercameron/hcm-next/internal/data/integration`
- `github.com/monstercameron/hcm-next/internal/data/integrationmeta`
- `github.com/monstercameron/hcm-next/internal/data/intentcontrol`
- `github.com/monstercameron/hcm-next/internal/data/jobarchstore`
- `github.com/monstercameron/hcm-next/internal/data/jobs`
- `github.com/monstercameron/hcm-next/internal/data/ledger`
- `github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint`
- `github.com/monstercameron/hcm-next/internal/data/ledger/commit`
- `github.com/monstercameron/hcm-next/internal/data/ledger/evidence`
- `github.com/monstercameron/hcm-next/internal/data/ledger/hashchain`
- `github.com/monstercameron/hcm-next/internal/data/ledger/lineage`
- `github.com/monstercameron/hcm-next/internal/data/ledger/partition`
- `github.com/monstercameron/hcm-next/internal/data/ledger/temporal`
- `github.com/monstercameron/hcm-next/internal/data/legalevidencestore`
- `github.com/monstercameron/hcm-next/internal/data/locationstore`
- `github.com/monstercameron/hcm-next/internal/data/meritstore`
- `github.com/monstercameron/hcm-next/internal/data/messagingmeta`
- `github.com/monstercameron/hcm-next/internal/data/mobilitystore`
- `github.com/monstercameron/hcm-next/internal/data/opsmeta`
- `github.com/monstercameron/hcm-next/internal/data/outbox`
- `github.com/monstercameron/hcm-next/internal/data/partition`
- `github.com/monstercameron/hcm-next/internal/data/payglstore`
- `github.com/monstercameron/hcm-next/internal/data/payinputstore`
- `github.com/monstercameron/hcm-next/internal/data/paymethodstore`
- `github.com/monstercameron/hcm-next/internal/data/payrollstore`
- `github.com/monstercameron/hcm-next/internal/data/performancestore`
- `github.com/monstercameron/hcm-next/internal/data/pgtest`
- `github.com/monstercameron/hcm-next/internal/data/pgxadapter`
- `github.com/monstercameron/hcm-next/internal/data/planningstore`
- `github.com/monstercameron/hcm-next/internal/data/positionstore`
- `github.com/monstercameron/hcm-next/internal/data/privacymeta`
- `github.com/monstercameron/hcm-next/internal/data/projection`
- `github.com/monstercameron/hcm-next/internal/data/projection/critical`
- `github.com/monstercameron/hcm-next/internal/data/promotioncommit`
- `github.com/monstercameron/hcm-next/internal/data/provenance`
- `github.com/monstercameron/hcm-next/internal/data/queryplans`
- `github.com/monstercameron/hcm-next/internal/data/rebuild`
- `github.com/monstercameron/hcm-next/internal/data/recordsmeta`
- `github.com/monstercameron/hcm-next/internal/data/refdata`
- `github.com/monstercameron/hcm-next/internal/data/runtimestate`
- `github.com/monstercameron/hcm-next/internal/data/safetystore`
- `github.com/monstercameron/hcm-next/internal/data/schedulingstore`
- `github.com/monstercameron/hcm-next/internal/data/schema`
- `github.com/monstercameron/hcm-next/internal/data/search`
- `github.com/monstercameron/hcm-next/internal/data/seed`
- `github.com/monstercameron/hcm-next/internal/data/skillstore`
- `github.com/monstercameron/hcm-next/internal/data/store`
- `github.com/monstercameron/hcm-next/internal/data/subscriptionstore`
- `github.com/monstercameron/hcm-next/internal/data/successionstore`
- `github.com/monstercameron/hcm-next/internal/data/surveystore`
- `github.com/monstercameron/hcm-next/internal/data/taxprofilestore`
- `github.com/monstercameron/hcm-next/internal/data/tenancy`
- `github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition`
- `github.com/monstercameron/hcm-next/internal/data/tenantstore`
- `github.com/monstercameron/hcm-next/internal/data/truststore`
- `github.com/monstercameron/hcm-next/internal/data/uow`
- `github.com/monstercameron/hcm-next/internal/data/wakeup`
- `github.com/monstercameron/hcm-next/internal/data/workforce`
- `github.com/monstercameron/hcm-next/internal/documentextract`
- `github.com/monstercameron/hcm-next/internal/documentredact`
- `github.com/monstercameron/hcm-next/internal/documents/evidence`
- `github.com/monstercameron/hcm-next/internal/documents/intake`
- `github.com/monstercameron/hcm-next/internal/documents/template`
- `github.com/monstercameron/hcm-next/internal/documentsecurity`
- `github.com/monstercameron/hcm-next/internal/domains/access`
- `github.com/monstercameron/hcm-next/internal/domains/appointment`
- `github.com/monstercameron/hcm-next/internal/domains/asset`
- `github.com/monstercameron/hcm-next/internal/domains/asset/quarantine`
- `github.com/monstercameron/hcm-next/internal/domains/attendance`
- `github.com/monstercameron/hcm-next/internal/domains/attestation`
- `github.com/monstercameron/hcm-next/internal/domains/audience`
- `github.com/monstercameron/hcm-next/internal/domains/availability`
- `github.com/monstercameron/hcm-next/internal/domains/balance`
- `github.com/monstercameron/hcm-next/internal/domains/benefits`
- `github.com/monstercameron/hcm-next/internal/domains/budget`
- `github.com/monstercameron/hcm-next/internal/domains/career`
- `github.com/monstercameron/hcm-next/internal/domains/cba`
- `github.com/monstercameron/hcm-next/internal/domains/clock`
- `github.com/monstercameron/hcm-next/internal/domains/compensation`
- `github.com/monstercameron/hcm-next/internal/domains/contact`
- `github.com/monstercameron/hcm-next/internal/domains/crm`
- `github.com/monstercameron/hcm-next/internal/domains/custom`
- `github.com/monstercameron/hcm-next/internal/domains/dataops`
- `github.com/monstercameron/hcm-next/internal/domains/dataops/importing`
- `github.com/monstercameron/hcm-next/internal/domains/demand`
- `github.com/monstercameron/hcm-next/internal/domains/employeerelations`
- `github.com/monstercameron/hcm-next/internal/domains/equity`
- `github.com/monstercameron/hcm-next/internal/domains/evidence`
- `github.com/monstercameron/hcm-next/internal/domains/fixtures`
- `github.com/monstercameron/hcm-next/internal/domains/fx`
- `github.com/monstercameron/hcm-next/internal/domains/hrcase`
- `github.com/monstercameron/hcm-next/internal/domains/incentive`
- `github.com/monstercameron/hcm-next/internal/domains/industrypack`
- `github.com/monstercameron/hcm-next/internal/domains/intelligence`
- `github.com/monstercameron/hcm-next/internal/domains/jobarch`
- `github.com/monstercameron/hcm-next/internal/domains/knowledge`
- `github.com/monstercameron/hcm-next/internal/domains/labor`
- `github.com/monstercameron/hcm-next/internal/domains/leave`
- `github.com/monstercameron/hcm-next/internal/domains/location`
- `github.com/monstercameron/hcm-next/internal/domains/matching`
- `github.com/monstercameron/hcm-next/internal/domains/merit`
- `github.com/monstercameron/hcm-next/internal/domains/mobility`
- `github.com/monstercameron/hcm-next/internal/domains/org`
- `github.com/monstercameron/hcm-next/internal/domains/organization`
- `github.com/monstercameron/hcm-next/internal/domains/partnerapp`
- `github.com/monstercameron/hcm-next/internal/domains/paygl`
- `github.com/monstercameron/hcm-next/internal/domains/payinput`
- `github.com/monstercameron/hcm-next/internal/domains/paymethod`
- `github.com/monstercameron/hcm-next/internal/domains/paymethod/achrisk`
- `github.com/monstercameron/hcm-next/internal/domains/payroll`
- `github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack`
- `github.com/monstercameron/hcm-next/internal/domains/payroll/calcpolicy`
- `github.com/monstercameron/hcm-next/internal/domains/payroll/filing`
- `github.com/monstercameron/hcm-next/internal/domains/payroll/paymentprofile`
- `github.com/monstercameron/hcm-next/internal/domains/people`
- `github.com/monstercameron/hcm-next/internal/domains/performance`
- `github.com/monstercameron/hcm-next/internal/domains/position`
- `github.com/monstercameron/hcm-next/internal/domains/privacy/dsr`
- `github.com/monstercameron/hcm-next/internal/domains/promotion`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/commit`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/localcommit`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/simassign`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/simcomp`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/simcontract`
- `github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot`
- `github.com/monstercameron/hcm-next/internal/domains/proofing`
- `github.com/monstercameron/hcm-next/internal/domains/pseudonym`
- `github.com/monstercameron/hcm-next/internal/domains/qualification`
- `github.com/monstercameron/hcm-next/internal/domains/refdata`
- `github.com/monstercameron/hcm-next/internal/domains/repair`
- `github.com/monstercameron/hcm-next/internal/domains/rewards`
- `github.com/monstercameron/hcm-next/internal/domains/safety`
- `github.com/monstercameron/hcm-next/internal/domains/scenario`
- `github.com/monstercameron/hcm-next/internal/domains/schedopt`
- `github.com/monstercameron/hcm-next/internal/domains/service`
- `github.com/monstercameron/hcm-next/internal/domains/settlement`
- `github.com/monstercameron/hcm-next/internal/domains/skill`
- `github.com/monstercameron/hcm-next/internal/domains/subscription`
- `github.com/monstercameron/hcm-next/internal/domains/succession`
- `github.com/monstercameron/hcm-next/internal/domains/survey`
- `github.com/monstercameron/hcm-next/internal/domains/taxprofile`
- `github.com/monstercameron/hcm-next/internal/domains/tenant`
- `github.com/monstercameron/hcm-next/internal/domains/tenant/govauth`
- `github.com/monstercameron/hcm-next/internal/effectgraph`
- `github.com/monstercameron/hcm-next/internal/engines/abuse`
- `github.com/monstercameron/hcm-next/internal/engines/abuse/anomaly002`
- `github.com/monstercameron/hcm-next/internal/engines/canonicalbytes`
- `github.com/monstercameron/hcm-next/internal/engines/cycle`
- `github.com/monstercameron/hcm-next/internal/engines/docextract`
- `github.com/monstercameron/hcm-next/internal/engines/docredact`
- `github.com/monstercameron/hcm-next/internal/engines/effectivedate`
- `github.com/monstercameron/hcm-next/internal/engines/eligibility`
- `github.com/monstercameron/hcm-next/internal/engines/fielddiff`
- `github.com/monstercameron/hcm-next/internal/engines/messagetemplate`
- `github.com/monstercameron/hcm-next/internal/engines/payband`
- `github.com/monstercameron/hcm-next/internal/engines/popscale`
- `github.com/monstercameron/hcm-next/internal/engines/population`
- `github.com/monstercameron/hcm-next/internal/engines/replan`
- `github.com/monstercameron/hcm-next/internal/engines/rules`
- `github.com/monstercameron/hcm-next/internal/engines/schedule`
- `github.com/monstercameron/hcm-next/internal/engines/snapshot`
- `github.com/monstercameron/hcm-next/internal/engines/transformation`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/adapters`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/conformance`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/exec`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/ir`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/lineage`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/propagation`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/runtime`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/taint`
- `github.com/monstercameron/hcm-next/internal/engines/transformation/version`
- `github.com/monstercameron/hcm-next/internal/engines/wire/canonical`
- `github.com/monstercameron/hcm-next/internal/engines/wire/digest`
- `github.com/monstercameron/hcm-next/internal/experience/adoption`
- `github.com/monstercameron/hcm-next/internal/experience/channelparity`
- `github.com/monstercameron/hcm-next/internal/experience/continuity`
- `github.com/monstercameron/hcm-next/internal/experience/disposition`
- `github.com/monstercameron/hcm-next/internal/experience/draftflow`
- `github.com/monstercameron/hcm-next/internal/experience/flowmigration`
- `github.com/monstercameron/hcm-next/internal/experience/i18n`
- `github.com/monstercameron/hcm-next/internal/experience/i18nparity`
- `github.com/monstercameron/hcm-next/internal/experience/localize`
- `github.com/monstercameron/hcm-next/internal/experience/outcome`
- `github.com/monstercameron/hcm-next/internal/experience/participants`
- `github.com/monstercameron/hcm-next/internal/experience/presentation`
- `github.com/monstercameron/hcm-next/internal/experience/recovery`
- `github.com/monstercameron/hcm-next/internal/experience/reporting`
- `github.com/monstercameron/hcm-next/internal/experience/reportrender`
- `github.com/monstercameron/hcm-next/internal/experience/reportschedule`
- `github.com/monstercameron/hcm-next/internal/experience/status`
- `github.com/monstercameron/hcm-next/internal/experience/userflow`
- `github.com/monstercameron/hcm-next/internal/flow`
- `github.com/monstercameron/hcm-next/internal/forms/drafts`
- `github.com/monstercameron/hcm-next/internal/generated/schemaflux`
- `github.com/monstercameron/hcm-next/internal/governance`
- `github.com/monstercameron/hcm-next/internal/governance/decision`
- `github.com/monstercameron/hcm-next/internal/governance/legal`
- `github.com/monstercameron/hcm-next/internal/governance/legal/attribution`
- `github.com/monstercameron/hcm-next/internal/governance/legal/carveouts`
- `github.com/monstercameron/hcm-next/internal/governance/legal/extract`
- `github.com/monstercameron/hcm-next/internal/governance/legal/indexation`
- `github.com/monstercameron/hcm-next/internal/governance/legal/payrules`
- `github.com/monstercameron/hcm-next/internal/governance/legal/pipeline`
- `github.com/monstercameron/hcm-next/internal/governance/legal/reciprocity`
- `github.com/monstercameron/hcm-next/internal/governance/legal/researchgaps`
- `github.com/monstercameron/hcm-next/internal/governance/legal/stateparams`
- `github.com/monstercameron/hcm-next/internal/governance/legalhold`
- `github.com/monstercameron/hcm-next/internal/governance/privacy`
- `github.com/monstercameron/hcm-next/internal/governance/privacy/inventory`
- `github.com/monstercameron/hcm-next/internal/governance/revalidate`
- `github.com/monstercameron/hcm-next/internal/humanwork`
- `github.com/monstercameron/hcm-next/internal/humanwork/formcontinuity`
- `github.com/monstercameron/hcm-next/internal/humanwork/formdraft`
- `github.com/monstercameron/hcm-next/internal/humanwork/productui`
- `github.com/monstercameron/hcm-next/internal/humanwork/profilephoto`
- `github.com/monstercameron/hcm-next/internal/humanwork/sla`
- `github.com/monstercameron/hcm-next/internal/humanwork/uicomponents`
- `github.com/monstercameron/hcm-next/internal/humanwork/workitem`
- `github.com/monstercameron/hcm-next/internal/humanwork/workspace`
- `github.com/monstercameron/hcm-next/internal/i18n`
- `github.com/monstercameron/hcm-next/internal/intent`
- `github.com/monstercameron/hcm-next/internal/intent/app`
- `github.com/monstercameron/hcm-next/internal/intent/app/pgstore`
- `github.com/monstercameron/hcm-next/internal/intent/approval`
- `github.com/monstercameron/hcm-next/internal/intent/definitions`
- `github.com/monstercameron/hcm-next/internal/intent/evolution`
- `github.com/monstercameron/hcm-next/internal/intent/lifecycle`
- `github.com/monstercameron/hcm-next/internal/intent/model`
- `github.com/monstercameron/hcm-next/internal/intent/model/deferred`
- `github.com/monstercameron/hcm-next/internal/intent/modelbinding`
- `github.com/monstercameron/hcm-next/internal/intent/protomap`
- `github.com/monstercameron/hcm-next/internal/kernel/values`
- `github.com/monstercameron/hcm-next/internal/ledger`
- `github.com/monstercameron/hcm-next/internal/messaging`
- `github.com/monstercameron/hcm-next/internal/operations/accessdrift`
- `github.com/monstercameron/hcm-next/internal/operations/admin`
- `github.com/monstercameron/hcm-next/internal/operations/admincenter/connectorconfig`
- `github.com/monstercameron/hcm-next/internal/operations/admincenter/diagnosticsession`
- `github.com/monstercameron/hcm-next/internal/operations/admincenter/evidenceexport`
- `github.com/monstercameron/hcm-next/internal/operations/admincenter/incidentrepair`
- `github.com/monstercameron/hcm-next/internal/operations/admission`
- `github.com/monstercameron/hcm-next/internal/operations/assurance`
- `github.com/monstercameron/hcm-next/internal/operations/authzsim`
- `github.com/monstercameron/hcm-next/internal/operations/explorer`
- `github.com/monstercameron/hcm-next/internal/operations/inspector`
- `github.com/monstercameron/hcm-next/internal/operations/internal/viewdigest`
- `github.com/monstercameron/hcm-next/internal/operations/reconcile`
- `github.com/monstercameron/hcm-next/internal/operations/reliability`
- `github.com/monstercameron/hcm-next/internal/operations/repair`
- `github.com/monstercameron/hcm-next/internal/performance`
- `github.com/monstercameron/hcm-next/internal/platform/bootstrap`
- `github.com/monstercameron/hcm-next/internal/platform/buildinfo`
- `github.com/monstercameron/hcm-next/internal/platform/cache`
- `github.com/monstercameron/hcm-next/internal/platform/config`
- `github.com/monstercameron/hcm-next/internal/platform/config/promotion`
- `github.com/monstercameron/hcm-next/internal/platform/configbundle`
- `github.com/monstercameron/hcm-next/internal/platform/configregistry`
- `github.com/monstercameron/hcm-next/internal/platform/diagnostics`
- `github.com/monstercameron/hcm-next/internal/platform/execution`
- `github.com/monstercameron/hcm-next/internal/platform/execution/promotionsteps`
- `github.com/monstercameron/hcm-next/internal/platform/execution/promotionterminal`
- `github.com/monstercameron/hcm-next/internal/platform/execution/scheduler`
- `github.com/monstercameron/hcm-next/internal/platform/logging`
- `github.com/monstercameron/hcm-next/internal/platform/observability`
- `github.com/monstercameron/hcm-next/internal/platform/sandbox`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry/boundary`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry/otel`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry/otel/testexport`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry/securityevidence`
- `github.com/monstercameron/hcm-next/internal/platform/telemetry/testexport`
- `github.com/monstercameron/hcm-next/internal/platform/timeauth`
- `github.com/monstercameron/hcm-next/internal/platform/versionexplain`
- `github.com/monstercameron/hcm-next/internal/replan`
- `github.com/monstercameron/hcm-next/internal/resource/reservation`
- `github.com/monstercameron/hcm-next/internal/store/object`
- `github.com/monstercameron/hcm-next/internal/transaction`
- `github.com/monstercameron/hcm-next/internal/transaction/cancel`
- `github.com/monstercameron/hcm-next/internal/transaction/commit`
- `github.com/monstercameron/hcm-next/internal/transaction/conflict`
- `github.com/monstercameron/hcm-next/internal/transaction/conformance`
- `github.com/monstercameron/hcm-next/internal/transaction/coordinator`
- `github.com/monstercameron/hcm-next/internal/transaction/correction`
- `github.com/monstercameron/hcm-next/internal/transaction/idempotency`
- `github.com/monstercameron/hcm-next/internal/transaction/plan`
- `github.com/monstercameron/hcm-next/internal/transaction/recovery`
- `github.com/monstercameron/hcm-next/internal/transport`
- `github.com/monstercameron/hcm-next/internal/transport/admin`
- `github.com/monstercameron/hcm-next/internal/transport/admin/hcmctl`
- `github.com/monstercameron/hcm-next/internal/transport/cell`
- `github.com/monstercameron/hcm-next/internal/transport/clients`
- `github.com/monstercameron/hcm-next/internal/transport/eastwest`
- `github.com/monstercameron/hcm-next/internal/transport/edge`
- `github.com/monstercameron/hcm-next/internal/transport/envelope`
- `github.com/monstercameron/hcm-next/internal/transport/grpcserver`
- `github.com/monstercameron/hcm-next/internal/transport/journey`
- `github.com/monstercameron/hcm-next/internal/transport/list`
- `github.com/monstercameron/hcm-next/internal/transport/manifest`
- `github.com/monstercameron/hcm-next/internal/transport/otelmw`
- `github.com/monstercameron/hcm-next/internal/transport/streaming`
- `github.com/monstercameron/hcm-next/internal/transport/transporttest`
- `github.com/monstercameron/hcm-next/internal/trust`
- `github.com/monstercameron/hcm-next/internal/trust/accessreview`
- `github.com/monstercameron/hcm-next/internal/trust/adversarial`
- `github.com/monstercameron/hcm-next/internal/trust/attest`
- `github.com/monstercameron/hcm-next/internal/trust/authz`
- `github.com/monstercameron/hcm-next/internal/trust/breakglass`
- `github.com/monstercameron/hcm-next/internal/trust/bundle`
- `github.com/monstercameron/hcm-next/internal/trust/confidentialactor`
- `github.com/monstercameron/hcm-next/internal/trust/content`
- `github.com/monstercameron/hcm-next/internal/trust/cryptoagile`
- `github.com/monstercameron/hcm-next/internal/trust/custody`
- `github.com/monstercameron/hcm-next/internal/trust/custody/kmsadapter`
- `github.com/monstercameron/hcm-next/internal/trust/dataclass`
- `github.com/monstercameron/hcm-next/internal/trust/dlp`
- `github.com/monstercameron/hcm-next/internal/trust/envelope`
- `github.com/monstercameron/hcm-next/internal/trust/federation`
- `github.com/monstercameron/hcm-next/internal/trust/jit`
- `github.com/monstercameron/hcm-next/internal/trust/lease`
- `github.com/monstercameron/hcm-next/internal/trust/outage`
- `github.com/monstercameron/hcm-next/internal/trust/outbound`
- `github.com/monstercameron/hcm-next/internal/trust/pseudonym`
- `github.com/monstercameron/hcm-next/internal/trust/secrets`
- `github.com/monstercameron/hcm-next/internal/trust/session`
- `github.com/monstercameron/hcm-next/internal/trust/session/pgstore`
- `github.com/monstercameron/hcm-next/internal/trust/sod`
- `github.com/monstercameron/hcm-next/internal/trust/stepup`
- `github.com/monstercameron/hcm-next/internal/trust/workload`
- `github.com/monstercameron/hcm-next/internal/workflow`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/benefits`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/hrcase`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/learning`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/leave`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/managerchange`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/mobility`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/payroll`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/talent`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/termination`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/time`
- `github.com/monstercameron/hcm-next/internal/workflow/conformance/transfer`
- `github.com/monstercameron/hcm-next/internal/workflow/execute`
- `github.com/monstercameron/hcm-next/internal/workflow/execute/effects`
- `github.com/monstercameron/hcm-next/internal/workflow/frontier`
- `github.com/monstercameron/hcm-next/internal/workflow/inspect`
- `github.com/monstercameron/hcm-next/internal/workflow/intervention`
- `github.com/monstercameron/hcm-next/internal/workflow/lease`
- `github.com/monstercameron/hcm-next/internal/workflow/migrate`
- `github.com/monstercameron/hcm-next/internal/workflow/migrate/artifacts`
- `github.com/monstercameron/hcm-next/internal/workflow/migrationpreview`
- `github.com/monstercameron/hcm-next/internal/workflow/promotionexec`
- `github.com/monstercameron/hcm-next/internal/workflow/prototype`
- `github.com/monstercameron/hcm-next/internal/workflow/quarantine`
- `github.com/monstercameron/hcm-next/internal/workflow/recover`
- `github.com/monstercameron/hcm-next/internal/workflow/replay`
- `github.com/monstercameron/hcm-next/internal/workflow/runtime`
- `github.com/monstercameron/hcm-next/internal/workflow/shadow`
- `github.com/monstercameron/hcm-next/internal/workflow/simulate`
- `github.com/monstercameron/hcm-next/internal/workflow/steps/approval`
- `github.com/monstercameron/hcm-next/internal/workflow/steps/signal`
- `github.com/monstercameron/hcm-next/internal/workflow/steps/task`
- `github.com/monstercameron/hcm-next/internal/workflow/steps/wait`
- `github.com/monstercameron/hcm-next/internal/workflow/timer`
- `github.com/monstercameron/hcm-next/internal/workflow/version`

### `migrations`

Authoritative Goose migration tree (owned outside tools/policy).

- `github.com/monstercameron/hcm-next/migrations`

### `test`

Cross-package conformance, integration and browser test suites that do not belong to a single internal package.

- `github.com/monstercameron/hcm-next/test/acceptance`
- `github.com/monstercameron/hcm-next/test/bootstrap`
- `github.com/monstercameron/hcm-next/test/edge`
- `github.com/monstercameron/hcm-next/test/serve`
- `github.com/monstercameron/hcm-next/test/tenant`
- `github.com/monstercameron/hcm-next/test/trustabuse`
- `github.com/monstercameron/hcm-next/test/tunnel`
- `github.com/monstercameron/hcm-next/test/workflow`
- `github.com/monstercameron/hcm-next/test/workspace`

### `tools`

Developer and CI tooling. Excluded from the release image; internal structure is not constrained by this manifest beyond being under tools/.

- `github.com/monstercameron/hcm-next/tools/conformance`
- `github.com/monstercameron/hcm-next/tools/conformance/checks`
- `github.com/monstercameron/hcm-next/tools/conformance/discover`
- `github.com/monstercameron/hcm-next/tools/conformance/intentdefinitions`
- `github.com/monstercameron/hcm-next/tools/conformance/internal/reporoot`
- `github.com/monstercameron/hcm-next/tools/conformance/model`
- `github.com/monstercameron/hcm-next/tools/conformance/parse`
- `github.com/monstercameron/hcm-next/tools/conformance/recruit`
- `github.com/monstercameron/hcm-next/tools/conformance/report`
- `github.com/monstercameron/hcm-next/tools/conformance/runner`
- `github.com/monstercameron/hcm-next/tools/conformance/transformationvectors`
- `github.com/monstercameron/hcm-next/tools/conformance/vocab`
- `github.com/monstercameron/hcm-next/tools/gen`
- `github.com/monstercameron/hcm-next/tools/gen/archdoc`
- `github.com/monstercameron/hcm-next/tools/gen/archdoc/cmd/archdoc`
- `github.com/monstercameron/hcm-next/tools/gen/architecturedoc`
- `github.com/monstercameron/hcm-next/tools/gen/clients`
- `github.com/monstercameron/hcm-next/tools/gen/clients/cmd/generateclients`
- `github.com/monstercameron/hcm-next/tools/gen/compatibility`
- `github.com/monstercameron/hcm-next/tools/gen/connectorsdk`
- `github.com/monstercameron/hcm-next/tools/gen/contracts`
- `github.com/monstercameron/hcm-next/tools/gen/dbdeferred`
- `github.com/monstercameron/hcm-next/tools/gen/deferredschema`
- `github.com/monstercameron/hcm-next/tools/gen/librarystrategy`
- `github.com/monstercameron/hcm-next/tools/gen/librarystrategy/cmd/generatelibrarystrategy`
- `github.com/monstercameron/hcm-next/tools/gen/modelgen`
- `github.com/monstercameron/hcm-next/tools/gen/modelgen/cmd/modelgen`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux/bindingcheck`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux/cmd/modelgen`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux/drift`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux/modelgen`
- `github.com/monstercameron/hcm-next/tools/gen/schemaflux/sources`
- `github.com/monstercameron/hcm-next/tools/gen/schemafluxsql`
- `github.com/monstercameron/hcm-next/tools/gen/storagemanifest`
- `github.com/monstercameron/hcm-next/tools/planning/atomicity`
- `github.com/monstercameron/hcm-next/tools/planning/authoritygate`
- `github.com/monstercameron/hcm-next/tools/planning/boundarytests`
- `github.com/monstercameron/hcm-next/tools/planning/cmd/featurecoverage`
- `github.com/monstercameron/hcm-next/tools/planning/cmd/plancheck`
- `github.com/monstercameron/hcm-next/tools/planning/cmd/productslice`
- `github.com/monstercameron/hcm-next/tools/planning/cmd/todogovernance`
- `github.com/monstercameron/hcm-next/tools/planning/cmd/todoregistry`
- `github.com/monstercameron/hcm-next/tools/planning/controlcrosswalk`
- `github.com/monstercameron/hcm-next/tools/planning/controlcrosswalk/cmd/controlcrosswalk`
- `github.com/monstercameron/hcm-next/tools/planning/corpus`
- `github.com/monstercameron/hcm-next/tools/planning/corpus/cmd/corpus`
- `github.com/monstercameron/hcm-next/tools/planning/coveragematrix`
- `github.com/monstercameron/hcm-next/tools/planning/dbcoverage`
- `github.com/monstercameron/hcm-next/tools/planning/deferredimports`
- `github.com/monstercameron/hcm-next/tools/planning/dependencygraph`
- `github.com/monstercameron/hcm-next/tools/planning/depthvocab`
- `github.com/monstercameron/hcm-next/tools/planning/docintegrity`
- `github.com/monstercameron/hcm-next/tools/planning/evidence`
- `github.com/monstercameron/hcm-next/tools/planning/federalbaseline`
- `github.com/monstercameron/hcm-next/tools/planning/gateevidence`
- `github.com/monstercameron/hcm-next/tools/planning/intentcoverage`
- `github.com/monstercameron/hcm-next/tools/planning/intentcoverage/cmd/intentcoverage`
- `github.com/monstercameron/hcm-next/tools/planning/intentmanifests`
- `github.com/monstercameron/hcm-next/tools/planning/legalmatrix`
- `github.com/monstercameron/hcm-next/tools/planning/links`
- `github.com/monstercameron/hcm-next/tools/planning/manifest`
- `github.com/monstercameron/hcm-next/tools/planning/obligations`
- `github.com/monstercameron/hcm-next/tools/planning/obligations/cmd/obligations`
- `github.com/monstercameron/hcm-next/tools/planning/operations`
- `github.com/monstercameron/hcm-next/tools/planning/plancontradiction`
- `github.com/monstercameron/hcm-next/tools/planning/productslice`
- `github.com/monstercameron/hcm-next/tools/planning/progress`
- `github.com/monstercameron/hcm-next/tools/planning/researchquestions`
- `github.com/monstercameron/hcm-next/tools/planning/riskbinding`
- `github.com/monstercameron/hcm-next/tools/planning/riskbinding/cmd/riskbinding`
- `github.com/monstercameron/hcm-next/tools/planning/scopeexchange`
- `github.com/monstercameron/hcm-next/tools/planning/securebydesign`
- `github.com/monstercameron/hcm-next/tools/planning/tddcontract`
- `github.com/monstercameron/hcm-next/tools/planning/terminology`
- `github.com/monstercameron/hcm-next/tools/planning/threatmodel`
- `github.com/monstercameron/hcm-next/tools/planning/threatmodel/cmd/threatmodel`
- `github.com/monstercameron/hcm-next/tools/planning/todogovernance`
- `github.com/monstercameron/hcm-next/tools/planning/todoregistry`
- `github.com/monstercameron/hcm-next/tools/planning/traceability`
- `github.com/monstercameron/hcm-next/tools/planning/userflowgaps`
- `github.com/monstercameron/hcm-next/tools/policy/apigate`
- `github.com/monstercameron/hcm-next/tools/policy/apigate/cmd/apigate`
- `github.com/monstercameron/hcm-next/tools/policy/archrules`
- `github.com/monstercameron/hcm-next/tools/policy/cleancheckout`
- `github.com/monstercameron/hcm-next/tools/policy/cleancheckout/cmd/cleancheckout`
- `github.com/monstercameron/hcm-next/tools/policy/crosscut`
- `github.com/monstercameron/hcm-next/tools/policy/depadmission`
- `github.com/monstercameron/hcm-next/tools/policy/depadmission/cmd/depadmission`
- `github.com/monstercameron/hcm-next/tools/policy/depedge`
- `github.com/monstercameron/hcm-next/tools/policy/depmanifest`
- `github.com/monstercameron/hcm-next/tools/policy/dispositioncoverage`
- `github.com/monstercameron/hcm-next/tools/policy/dispositioncoverage/cmd/dispositioncoverage`
- `github.com/monstercameron/hcm-next/tools/policy/dispositionrebuild`
- `github.com/monstercameron/hcm-next/tools/policy/docintegrity`
- `github.com/monstercameron/hcm-next/tools/policy/docintegrity/cmd/docintegrity`
- `github.com/monstercameron/hcm-next/tools/policy/driftgate`
- `github.com/monstercameron/hcm-next/tools/policy/driftgate/cmd/driftgate`
- `github.com/monstercameron/hcm-next/tools/policy/endpointmanifest`
- `github.com/monstercameron/hcm-next/tools/policy/endpointmanifest/cmd/endpointmanifest`
- `github.com/monstercameron/hcm-next/tools/policy/enginecoverage`
- `github.com/monstercameron/hcm-next/tools/policy/enginecoverage/cmd/enginecoverage`
- `github.com/monstercameron/hcm-next/tools/policy/garbagedrawer`
- `github.com/monstercameron/hcm-next/tools/policy/gensources`
- `github.com/monstercameron/hcm-next/tools/policy/importgraph`
- `github.com/monstercameron/hcm-next/tools/policy/internal/repopath`
- `github.com/monstercameron/hcm-next/tools/policy/invocationpath`
- `github.com/monstercameron/hcm-next/tools/policy/layout`
- `github.com/monstercameron/hcm-next/tools/policy/libfirewall`
- `github.com/monstercameron/hcm-next/tools/policy/libqualification`
- `github.com/monstercameron/hcm-next/tools/policy/mutationpolicy`
- `github.com/monstercameron/hcm-next/tools/policy/mutationpolicy/cmd/mutationpolicy`
- `github.com/monstercameron/hcm-next/tools/policy/oraclestrength`
- `github.com/monstercameron/hcm-next/tools/policy/oraclestrength/cmd/oraclestrength`
- `github.com/monstercameron/hcm-next/tools/policy/phaseone`
- `github.com/monstercameron/hcm-next/tools/policy/phaseonegate`
- `github.com/monstercameron/hcm-next/tools/policy/placementbindings`
- `github.com/monstercameron/hcm-next/tools/policy/processroles`
- `github.com/monstercameron/hcm-next/tools/policy/prohibitedframework`
- `github.com/monstercameron/hcm-next/tools/policy/provenance`
- `github.com/monstercameron/hcm-next/tools/policy/provenance/cmd/provgen`
- `github.com/monstercameron/hcm-next/tools/policy/racepolicy`
- `github.com/monstercameron/hcm-next/tools/policy/racepolicy/cmd/racepolicy`
- `github.com/monstercameron/hcm-next/tools/policy/release`
- `github.com/monstercameron/hcm-next/tools/policy/release/cmd/release`
- `github.com/monstercameron/hcm-next/tools/policy/releaseadmission`
- `github.com/monstercameron/hcm-next/tools/policy/runtimedecision`
- `github.com/monstercameron/hcm-next/tools/policy/sbom`
- `github.com/monstercameron/hcm-next/tools/policy/sbom/cmd/sbomgen`
- `github.com/monstercameron/hcm-next/tools/policy/storeboundaries`
- `github.com/monstercameron/hcm-next/tools/policy/substratecoverage`
- `github.com/monstercameron/hcm-next/tools/policy/substratecoverage/cmd/substratecoverage`
- `github.com/monstercameron/hcm-next/tools/policy/tableinventory`
- `github.com/monstercameron/hcm-next/tools/policy/tableownership`
- `github.com/monstercameron/hcm-next/tools/policy/testhygiene`
- `github.com/monstercameron/hcm-next/tools/policy/testlayout`
- `github.com/monstercameron/hcm-next/tools/policy/vulnimpact`
- `github.com/monstercameron/hcm-next/tools/policy/webdelivery`
- `github.com/monstercameron/hcm-next/tools/policy/workspace`
- `github.com/monstercameron/hcm-next/tools/quality`
- `github.com/monstercameron/hcm-next/tools/quality/bufprotovalidatekit`
- `github.com/monstercameron/hcm-next/tools/quality/buildverify`
- `github.com/monstercameron/hcm-next/tools/quality/celqual`
- `github.com/monstercameron/hcm-next/tools/quality/cicd`
- `github.com/monstercameron/hcm-next/tools/quality/compositionroot`
- `github.com/monstercameron/hcm-next/tools/quality/configboundaries`
- `github.com/monstercameron/hcm-next/tools/quality/cosignkit`
- `github.com/monstercameron/hcm-next/tools/quality/decomposition`
- `github.com/monstercameron/hcm-next/tools/quality/definitionscontract`
- `github.com/monstercameron/hcm-next/tools/quality/ephemeralenv`
- `github.com/monstercameron/hcm-next/tools/quality/fuzzkit`
- `github.com/monstercameron/hcm-next/tools/quality/integrationboundaries`
- `github.com/monstercameron/hcm-next/tools/quality/oidckit`
- `github.com/monstercameron/hcm-next/tools/quality/ownershipboundaries`
- `github.com/monstercameron/hcm-next/tools/quality/phaseonepackages`
- `github.com/monstercameron/hcm-next/tools/quality/provenance`
- `github.com/monstercameron/hcm-next/tools/quality/racecheck`
- `github.com/monstercameron/hcm-next/tools/quality/rapidkit`
- `github.com/monstercameron/hcm-next/tools/quality/releaseboundary`
- `github.com/monstercameron/hcm-next/tools/quality/sbom`
- `github.com/monstercameron/hcm-next/tools/quality/sqlckit`
- `github.com/monstercameron/hcm-next/tools/quality/storagearch`
- `github.com/monstercameron/hcm-next/tools/quality/synctestkit`
- `github.com/monstercameron/hcm-next/tools/quality/testcontainerskit`
- `github.com/monstercameron/hcm-next/tools/quality/thintransport`
- `github.com/monstercameron/hcm-next/tools/quality/toolinventory`
- `github.com/monstercameron/hcm-next/tools/quality/toxiproxykit`
- `github.com/monstercameron/hcm-next/tools/quality/xtextkit`
- `github.com/monstercameron/hcm-next/tools/uxqual/cmd/genfixtures`
- `github.com/monstercameron/hcm-next/tools/uxqual/cmd/journeywasm`
- `github.com/monstercameron/hcm-next/tools/uxqual/cmd/uxqualwasm`
- `github.com/monstercameron/hcm-next/tools/uxqual/contract`
- `github.com/monstercameron/hcm-next/tools/uxqual/floorplan`
- `github.com/monstercameron/hcm-next/tools/uxqual/forms`
- `github.com/monstercameron/hcm-next/tools/uxqual/i18n`
- `github.com/monstercameron/hcm-next/tools/uxqual/journeyclient`
- `github.com/monstercameron/hcm-next/tools/uxqual/pagedef`
- `github.com/monstercameron/hcm-next/tools/uxqual/presentation`
- `github.com/monstercameron/hcm-next/tools/uxqual/productclient`
- `github.com/monstercameron/hcm-next/tools/uxqual/qual`
- `github.com/monstercameron/hcm-next/tools/uxqual/render/gwc`
- `github.com/monstercameron/hcm-next/tools/uxqual/render/journey`
- `github.com/monstercameron/hcm-next/tools/uxqual/render/page`
- `github.com/monstercameron/hcm-next/tools/uxqual/render/ssr`
- `github.com/monstercameron/hcm-next/tools/uxqual/render/workspace`
- `github.com/monstercameron/hcm-next/tools/uxqual/ssrshell`
- `github.com/monstercameron/hcm-next/tools/uxqual/tokens`
- `github.com/monstercameron/hcm-next/tools/uxqual/wcag`
- `github.com/monstercameron/hcm-next/tools/uxqual/wcagtest`
- `github.com/monstercameron/hcm-next/tools/uxqual/widgetreg`

### `gen`

Generated Protobuf/Go artifacts. Content is owned by the generator (TOOL-002/TOOL-004), never hand-edited; internal structure is not constrained by this manifest.

- `github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/dataops/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/evidence/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/humanwork/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/integration/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/model`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1`
- `github.com/monstercameron/hcm-next/gen/go/hcmnext/workflow/v1`
- `github.com/monstercameron/hcm-next/gen/wire`

## Document digest

`8d136d6be33a7c11a070126872eae87f663e17eef6eb010fd83450f939021e79`
