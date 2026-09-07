//go:build js && wasm

// This file is the product entrypoint. It only builds under
// GOOS=js GOARCH=wasm because ui.Render mounts into the real DOM via
// syscall/js: the journey page ships as a GoWebComponents client that the
// server's shell loads, and everything below it happens in the browser.
//
// It mirrors tools/uxqual/render/gwc/mount_wasm.go in shape and adds the
// one thing a live surface needs that a static render does not: a mount
// that re-renders when the client's state changes, so an RPC answer
// redraws through GWC's reconciler instead of replacing the document.
package journey

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Mount renders one immutable Page into the given CSS selector. It is the
// simplest possible entrypoint: useful for a shell, a static preview or a
// smoke test, and correct only while nothing changes.
//
// Anything that answers an RPC wants MountLive.
func Mount(p Page, selector string) error {
	return MountLive(NewStore(p), selector)
}

// MountLive renders the store's page and keeps it rendered: every Set,
// Update or SetValue on the store re-renders through GWC's reconciler, so
// the DOM is patched rather than rebuilt and focus, scroll position and
// text selection survive an engine answer arriving mid-typing.
//
// The client's loop is: mount once, then call store.Set with each new Page
// the gRPC stream produces. Nothing here knows about the transport.
func MountLive(store *Store, selector string) error {
	if store == nil {
		return ErrMountFailed
	}
	return defaultMount.Mount(selector, func() error {
		injectStylesheet()
		ui.Render(LiveComponent(store), selector)
		return nil
	}, func() {
		// Rendering an empty root is GWC v5's public unmount path. It runs
		// effect cleanups (including LiveComponent's Store unsubscribe) and
		// removes the owned DOM tree without reaching into runtime internals.
		ui.Render(nil, selector)
	})
}

var defaultMount = NewMountLifecycle()

// StopMount releases the process-wide mount. It is primarily used by host
// teardown and tests; repeated calls are safe.
func StopMount() { defaultMount.Stop() }

// injectStylesheet puts the one hashed stylesheet into document.head via
// textContent, never innerHTML: Stylesheet() is our own static constant, so
// this is not an injection sink, and textContent cannot become one if that
// ever stops being true.
//
// It is idempotent by marker attribute rather than by a package-level bool,
// because the shell may already have inlined the same sheet (its sha256 is
// what the content-security-policy pins) and a second copy would be dead
// weight in the cascade.
func injectStylesheet() {
	document := js.Global().Get("document")
	if existing := document.Call("querySelector", "style[data-journey-stylesheet]"); existing.Truthy() {
		return
	}
	style := document.Call("createElement", "style")
	style.Call("setAttribute", "data-journey-stylesheet", "1")
	style.Set("textContent", Stylesheet())
	document.Get("head").Call("appendChild", style)
}
