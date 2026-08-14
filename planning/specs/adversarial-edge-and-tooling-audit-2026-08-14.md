# Adversarial Edge-Case and Tooling Audit — 2026-08-14

## Scope and decision rule

This audit compares the planned Go/PostgreSQL execution substrate with failure
semantics documented by the upstream projects most likely to provide its
infrastructure mechanics. It does not change the product thesis or authorize a
library merely because it is named.

The governing rule remains:

> HCM Next owns HCM semantics; third-party libraries and tools may provide
> replaceable infrastructure mechanics behind owned contracts.

Every candidate below must be pinned, classified in the dependency-role
manifest, isolated to approved import or tooling roots, tested against an owned
conformance contract, and given a removal path. A tool that captures workflow,
intent, governance, transaction, ledger, authority, or domain meaning is
rejected even if it is technically capable.

## Material gaps found

| Area                      | Adversarial edge                                                                                                      | Required contract                                                                                                                                                 |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL notification   | `LISTEN` begins only at commit and has an initial setup race; notifications are not durable work                      | Treat notification as a wake-up hint only; commit subscription, read authoritative table state, then consume hints with periodic polling/recovery                 |
| PostgreSQL serialization  | `40001` requires the whole transaction to restart; a retry may still encounter a uniqueness conflict                  | Retry the complete side-effect-free transaction closure from a new snapshot; never retry only one statement or an ambiguous commit                                |
| PostgreSQL advisory locks | Session locks survive rollback and shared lock memory is finite                                                       | Prefer transaction-scoped locks, own a collision-free key namespace, budget lock count, and prove pool/session cleanup                                            |
| gRPC retries              | Transparent and configured retries may replay an RPC; no deadline exists by default                                   | Classify every method by idempotency/effect boundary, require deadlines, and ensure retry cannot duplicate intent, transaction, or external effect                |
| Protobuf evolution        | Older descriptors or transformations can silently discard unknown fields; unrestricted `Any` admits unowned types     | Preserve or explicitly reject unknowns, allowlist `Any` type URLs, and prove old/new relay behavior with golden bytes                                             |
| Unicode                   | Go strings are not normalized and Unicode security skeletons are detection aids, not canonical identity               | Pin Unicode data, define normalization per field, preserve original text, and never merge identities from confusable skeletons alone                              |
| Virtual time              | Sleep-based timer/retry tests are slow and flaky; fake clocks alone do not prove goroutine quiescence                 | Qualify `testing/synctest` for in-process time/concurrency and retain real I/O fault tests for database/network boundaries                                        |
| Stateful invariants       | Example tests under-sample workflow, balance, temporal, and lifecycle state machines                                  | Qualify a shrinking property/state-machine library for test code only; minimized counterexamples become checked-in regressions                                    |
| Network faults            | Provider fakes rarely reproduce latency, half-close, reset, truncation, and connection loss                           | Add a test-only network proxy boundary with deterministic toxic schedules and exact durable-state oracles                                                         |
| SQL generation            | Handwritten scan plumbing creates drift, while an ORM would hide the correctness-bearing SQL                          | Evaluate `sqlc` only for typed adapter mechanics; SQL, transaction boundaries, temporal predicates, locks, and semantic repositories remain authored and reviewed |
| Object storage            | Key names are not immutable artifact identities; multipart completion, versioning, retention, and legal hold interact | Bind artifact identity to provider version and checksum, use conditional completion, abort orphan uploads, and address legal hold per immutable version           |
| Telemetry                 | Baggage propagates downstream without integrity guarantees and can leak sensitive values                              | Allowlist bounded non-sensitive propagation fields, never authorize from baggage, strip at third-party egress, and test collector redaction/cardinality limits    |
| Spreadsheet export        | Untrusted fields can become executable formulas after CSV parsing or spreadsheet re-save                              | Publish separate human-spreadsheet and machine-data export profiles with formula-neutralization, exact round-trip behavior, and DLP evidence                      |
| Supply chain              | Producing an SBOM or attestation is not verification                                                                  | Verify subject digest, builder identity, issuer, source, policy, and SBOM linkage at deployment admission; pin verification tooling and retain bundles            |
| Vulnerability scanning    | Module-version matching alone produces noisy or irrelevant results                                                    | Run symbol-aware `govulncheck`, retain the analyzed module graph/tool version, and govern reachable/unreachable findings with expiry-bound evidence               |

## Candidate disposition

| Candidate                 | Disposition       | Allowed role                                              | Explicit boundary                                                                                       |
| ------------------------- | ----------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Buf CLI                   | `QUALIFY`         | build/dev compatibility and lint mechanics                | no runtime or HCM semantic authority                                                                    |
| Protovalidate             | `QUALIFY`         | structural request/definition validation                  | business, legal, authorization, temporal, and cross-aggregate rules remain owned capabilities/engines   |
| `sqlc`                    | `EVALUATE`        | generated PostgreSQL adapter plumbing                     | no ORM, repository ownership, transaction-plan generation, or hidden SQL                                |
| `testing/synctest`        | `QUALIFY`         | Go standard-library deterministic time/concurrency tests  | not evidence for PostgreSQL, network, provider, or process-crash behavior                               |
| Rapid                     | `EVALUATE`        | test-only property and state-machine generation/shrinking | zero production/runtime dependency; license and maintenance review required                             |
| Toxiproxy                 | `QUALIFY`         | test-only transport fault injection                       | never a production proxy or correctness mechanism                                                       |
| `golang.org/x/text`       | `QUALIFY`         | Unicode normalization, language and locale mechanics      | owned field policies and identity semantics select operations and versions                              |
| AWS SDK for Go v2         | `QUALIFY`         | S3-compatible provider adapter                            | provider types cannot escape the object-store adapter; provider-neutral artifact identity remains owned |
| Cosign/Sigstore tooling   | `QUALIFY`         | release signing/attestation verification                  | no runtime business dependency; admission policy remains HCM Next-owned                                 |
| `govulncheck`             | `ADOPT_TOOL`      | symbol-aware Go vulnerability evidence                    | does not replace SBOM, exploitability review, dependency ownership, or patch SLA                        |
| ClamAV or another scanner | `DEFER_SELECTION` | hostile-content scanning adapter                          | scanner signatures and verdicts are observations; quarantine policy remains owned and fail-closed       |

Existing rejections remain unchanged: no ORM for the authoritative kernel, no
generic workflow product as the HCM business model, no general scripting runtime,
and no mandatory event broker in Phase 1.

## Required TDD vectors

The backlog items derived from this audit must include these concrete vectors:

1. A worker commits an outbox row while every notification is lost; polling still
   claims the row exactly once.
2. A listener starts during a concurrent commit; the post-subscription table scan
   plus duplicate hint produces one logical claim.
3. A serializable closure receives `40001`, rereads a changed baseline, and either
   commits the recomputed result once or returns the new typed conflict.
4. The connection drops after commit acknowledgement becomes unknowable; no local
   retry is attempted and idempotent resolution determines the durable result.
5. A pooled connection is returned after tenant context, advisory locks, temp
   state, and session settings were changed; the next tenant observes none of them.
6. A gRPC call is transparently retried around headers; one client request ID maps
   to one intent and one effect ledger.
7. An older relay receives a message containing a newer unknown field; it either
   preserves the exact bytes/semantics or rejects with the declared compatibility
   error—never silently approves a reduced request.
8. A confusable worker name and canonically equivalent text do not cause identity
   merge, digest drift, or search authorization leakage.
9. Virtual-time retry/timer tests reach quiescence without wall-clock sleeps, while
   a real proxy reset still proves the durable retry/ambiguity state.
10. Two concurrent multipart uploads target the same logical artifact; only the
    conditionally authorized version/checksum is bound and the loser is cleaned up.
11. A medical classification and attacker-controlled baggage key reach no trace,
    metric label, log, or third-party request.
12. CSV cells beginning with ASCII/full-width formula initiators, separators,
    quotes, tabs, CR, and LF remain inert in the declared human-viewing profile;
    machine export preserves exact values under a different content type/profile.
13. A signed image with a mismatched subject digest, untrusted workflow identity,
    stale policy, or detached SBOM is denied before deployment.

## Primary upstream evidence

- PostgreSQL documents transaction retry and isolation behavior in
  [Transaction Isolation](https://www.postgresql.org/docs/15/transaction-iso.html),
  advisory-lock lifetime and capacity in
  [Explicit Locking](https://www.postgresql.org/docs/16/explicit-locking.html),
  and the listener startup race in
  [LISTEN](https://www.postgresql.org/docs/17/sql-listen.html).
- gRPC documents transparent/configured replay behavior in
  [Retry](https://grpc.io/docs/guides/retry/) and notes that calls have no deadline
  by default in [Deadlines](https://grpc.io/docs/guides/deadlines/).
- Buf documents linting of Protovalidate constraints in
  [Lint rules](https://buf.build/docs/lint/rules/), `Any` allow/deny lists in
  [Protovalidate Any rules](https://buf.build/docs/reference/protovalidate/rules/any_rules/),
  and older-schema field loss risks in
  [Reflection and schema transformation](https://buf.build/docs/bsr/reflection/).
- Go documents Unicode normalization in
  [Text normalization in Go](https://go.dev/blog/normalization), fuzzing in
  [Go Fuzzing](https://go.dev/doc/security/fuzz/), virtualized time/concurrency in
  [Testing Time](https://go.dev/blog/testing-time), and vulnerability analysis in
  [Vulnerability Management for Go](https://go.dev/doc/security/vuln/).
- Unicode defines confusable detection and its limits in
  [UTS #39: Unicode Security Mechanisms](https://www.unicode.org/reports/tr39/).
- OpenTelemetry warns that baggage can expose sensitive information and lacks
  integrity guarantees in [Baggage](https://opentelemetry.io/docs/concepts/signals/baggage/).
- AWS documents multipart concurrency and completion behavior in
  [Multipart upload overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)
  and version-specific retention/holds in
  [S3 Object Lock](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html).
- OWASP documents spreadsheet formula execution hazards in
  [CSV Injection](https://owasp.org/www-community/attacks/CSV_Injection).
- The candidate tool projects describe their mechanics in the official repositories:
  [Buf](https://github.com/bufbuild/buf), [sqlc](https://github.com/sqlc-dev/sqlc),
  [Rapid](https://github.com/flyingmutant/rapid),
  [Toxiproxy](https://github.com/shopify/toxiproxy),
  [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2), and
  [Cosign](https://github.com/sigstore/cosign).

## Phase boundary

This audit creates qualification and correctness obligations. It does not pull
later HCM domains into Phase 1, authorize a cloud-specific semantic dependency,
or imply that every evaluated candidate will be adopted. A rejected candidate
is a successful outcome when the owned contract can be implemented more safely
with the standard library or an existing qualified dependency.
