// Package workspace serves the Promotion workspace: the first user-visible
// surface of a running P1A cell.
//
// # What it is
//
// tools/uxqual already owns the finished, frozen half of this surface: the
// renderer-independent contract (tools/uxqual/contract), the shared design
// tokens, the Go SSR renderer, the FORM-004 form/intent equivalence proof,
// and the UX-QUAL-001 qualification fixture. What it deliberately does not
// own is data: its fixture is hand-authored, and it is forbidden from
// importing the application layer.
//
// This package is the missing half. It reads a live cell through a narrow
// port ([Cell]), projects the answers into that frozen contract with the
// caller's own authorization decision already applied, renders the result
// with the frozen SSR renderer, and publishes it on the cell's HTTP edge
// under the same bearer credential the API uses.
//
// # Zero effects
//
// Every route is a read. The governed capabilities behind them
// (explain_worker_state, evaluate_pay_band_position, simulate_compensation,
// promote_worker preflight and simulate) are all published READ_ONLY, the
// capability gateway refuses any handler that is not, and no route reaches a
// write RPC: SubmitIntent, CancelIntent and SupersedeIntent are not called
// from here at all. The zero-effect receipt the domain mints
// (promotion.SimulationResult.Receipt) is rendered as evidence rather than
// asserted in prose.
//
// # Masking
//
// Field and action visibility is derived from what the live cell actually
// disclosed, never from a local guess. A worker-state field the governed
// read refused (people.Explanation reports it as DENIED) and every
// compensation field, when the caller's purpose carries no compensation
// grant, are dropped by contract.NewWorkspaceContract before any renderer
// sees them: they are absent from the rendered document rather than blanked
// in it. [MaskedNeedles] returns exactly the strings a masked field or
// action would have contributed, which is what the conformance test greps
// the live HTML for.
//
// # Progressive enhancement
//
// The page is fully functional with no JavaScript: the request form is a
// normal full-page POST and every control is a native element. The GWC/WASM
// renderer is offered on top of that baseline only when its build output has
// been placed in this package's embedded asset directory (assets/uxqual.wasm
// plus the matching assets/wasm_exec.js):
//
//	GOOS=js GOARCH=wasm go build -o internal/humanwork/workspace/assets/uxqual.wasm ./tools/uxqual/cmd/uxqualwasm
//	cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" internal/humanwork/workspace/assets/wasm_exec.js
//
// No bundle is committed, so a stock build of this repository serves the SSR
// document only and [Handler] answers the asset route with 404.
//
// That is deliberate, and there is a second reason beyond the size of the
// artifact. definitions/ux/workspace-renderer-decision.yaml selects GWC as
// the browser renderer, but the entrypoint it qualified
// (tools/uxqual/cmd/uxqualwasm) mounts the frozen UX-QUAL-001 fixture into
// "#app" - hand-authored data, not this cell's. Serving that bundle over a
// live page would replace real worker state with fixture state, which is
// worse than no enhancement at all. The asset route and the loader exist for
// the entrypoint that reads the live contract; until that entrypoint is
// written, the honest answer is the server-rendered document, which is the
// same qualified markup either renderer produces.
package workspace
