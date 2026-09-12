# 2026-09-11 — Public origin serve configuration (EDGE-011)

## What

`hcmnext serve` gained `-public-origin` / `HCMNEXT_PUBLIC_ORIGIN`: the
absolute http(s) origin browsers reach the cell at. It exists for the
deployment shape that plain same-origin serving cannot express — a
TLS-terminating or Host-rewriting proxy in front of the cell — and leaves
localhost and direct-VPS serving on the unchanged same-origin default.

Declared once at the composition root
(`internal/application`, canonicalized to `scheme://host`), then bound at
the two boundaries that consume it:

- `internal/transport/cell`: the declared origin replaces the browser
  origin allowlist, `https` marks normalized cookies `Secure`, and the
  gRPC tunnel admits upgrades whose `Origin` host matches the public
  authority.
- `internal/humanwork/workspace`: journey and product shells emit the
  tunnel URL as `ws(s)://<public authority>/workspace/grpc` and bind CSP
  `connect-src` to it; receipt/promotion documents also bind it.

Each boundary revalidates fail-closed so a caller that skips the root
validation still refuses a malformed value.

## Verified

- `go test -count=1 ./internal/application/ ./internal/humanwork/workspace/ ./internal/transport/cell/ ./internal/intent/app/` PASS on linux/amd64 (Go 1.26.8); `go vet`, `gofmt -l` clean.
- Live: cell served with `-public-origin=https://<preview host>` behind a
  Host/Origin-rewriting proxy — persona login 303, session cookie `Secure`,
  journey shell emits `wss://<public host>/workspace/grpc`, websocket
  upgrade 101.

## Left partial

- `BrowserPolicyOptions.AllowedHosts` deliberately unset: it applies to all
  state-changing requests and breaks Host-rewriting proxies and non-browser
  clients.
- With a public origin declared, other origins (including direct
  `localhost` form posts) are rejected — the declared origin is the
  browser boundary by design.
