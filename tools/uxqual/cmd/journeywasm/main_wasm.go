//go:build js && wasm

// Command journeywasm is the Promotion journey page's client:
// `go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets`
// builds this file into assets/journey.wasm, and
// internal/humanwork/workspace's journey shell loads it.
//
// Everything it does is in main below, and there is very little of it, which
// is the point: the configuration, the routing, the projection and the state
// machine are all in tools/uxqual/journeyclient, which has no syscall/js in
// it and is therefore tested by ordinary `go test`. What is left here is the
// four things only a browser can do -- read the island out of the document,
// dial the tunnel, mount the renderer, and follow the address bar.
package main

import (
	"context"
	"errors"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/wasm/dialer"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/journey"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// The shell's half of the contract (internal/humanwork/workspace's
// journey_shell.go). These are restated rather than imported because a
// tools/ command that imported the serving package would drag the whole
// server into a wasm binary; internal/humanwork/workspace's own test asserts
// the same two identifiers.
const (
	configElementID = "journey-config"
	rootElementID   = "app"
	rootSelector    = "#" + rootElementID
)

func main() {
	if err := start(); err != nil {
		mountStartupFailure(err)
	}
	// The page's work happens on the browser's event loop from here: every
	// RPC answer, every keystroke and every hash change arrives as a
	// callback. main must not return, or the Go runtime exits and takes all
	// of them with it.
	select {}
}

// start brings the client up. It returns an error only for the two failures
// that leave nothing working at all -- an unreadable island and a client
// that cannot be constructed -- because every later failure is an RPC
// refusal, which the page itself is the right place to show.
func start() error {
	raw, err := readIsland(configElementID)
	if err != nil {
		return err
	}
	cfg, err := journeyclient.ParseConfig([]byte(raw))
	if err != nil {
		return err
	}
	conn, err := dial(cfg)
	if err != nil {
		return err
	}
	service := journeyclient.NewGRPCService(conn, cfg.Bearer)
	if isProductPath(currentPath()) {
		return startProduct(context.Background(), cfg, service)
	}

	store := journey.NewStore(journey.Page{})
	app := journeyclient.New(cfg, service, store, time.Now)
	// Navigation goes through the address bar rather than straight into the
	// state machine, so the fragment and the page can never disagree and the
	// browser's own Back button works.
	app.Locate = setHash

	mount(store)
	js.Global().Set("onhashchange", js.FuncOf(func(js.Value, []js.Value) any {
		app.OnHashChange(currentHash())
		return nil
	}))
	app.Start(context.Background(), currentHash())
	return nil
}

// dial builds the gRPC client over the browser's WebSocket.
//
// Three things here are deliberate. The dial target is a passthrough address
// (see Config.DialTarget) because grpc.NewClient's default resolver is DNS,
// which a browser cannot run. The dialer is handed the tunnel's own ws:// or
// wss:// URL, which is what it actually opens; the target is ignored by it.
// And the credentials are insecure at the gRPC layer on purpose: transport
// security is the browser's wss:// socket, which gRPC inside a WASM page
// cannot see and must not try to negotiate.
func dial(cfg journeyclient.Config) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		cfg.DialTarget(),
		dialer.New(cfg.TunnelURL),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// mount clears the shell's no-script fallback and renders the live client in
// its place.
//
// The clearing is this command's job rather than the renderer's: GWC's
// reconciler mounts its tree into the container it is given and leaves any
// DOM already there alone, so without this the fallback paragraph would sit
// above a working page telling its reader the page does not work.
// textContent is used rather than innerHTML because it is not an injection
// sink, and clearing a node needs nothing that is.
func mount(store *journey.Store) {
	if root := js.Global().Get("document").Call("getElementById", rootElementID); root.Truthy() {
		root.Set("textContent", "")
	}
	journey.MountLive(store, rootSelector)
	bindActionableNoticeFocus(store)
}

// bindActionableNoticeFocus brings a newly rendered refusal into view. The
// promotion form can be taller than the viewport, so leaving its summary at
// the top of the page makes a failed submit look like a dead button.
func bindActionableNoticeFocus(store *journey.Store) {
	lastNotice := ""
	store.Subscribe(func() {
		var callback js.Func
		callback = js.FuncOf(func(js.Value, []js.Value) any {
			defer callback.Release()
			notice := js.Global().Get("document").Call("querySelector", `.jn-notice[data-tone="warning"],.jn-notice[data-tone="danger"]`)
			if !notice.Truthy() {
				lastNotice = ""
				return nil
			}
			text := notice.Get("textContent").String()
			if text == "" || text == lastNotice {
				return nil
			}
			lastNotice = text
			notice.Call("setAttribute", "tabindex", "-1")
			notice.Call("focus")
			notice.Call("scrollIntoView", map[string]any{"behavior": "smooth", "block": "center"})
			return nil
		})
		js.Global().Call("requestAnimationFrame", callback)
	})
}

// errNoIsland is the one document-shaped failure: the shell always writes
// the island, so its absence means this bundle is loaded by something that
// is not the journey shell.
var errNoIsland = errors.New("journeywasm: the document carries no journey-config island")

// readIsland reads the JSON island's text.
func readIsland(id string) (string, error) {
	element := js.Global().Get("document").Call("getElementById", id)
	if !element.Truthy() {
		return "", errNoIsland
	}
	text := element.Get("textContent")
	if text.Type() != js.TypeString {
		return "", errNoIsland
	}
	return text.String(), nil
}

// currentHash is the address bar's fragment.
func currentHash() string {
	hash := js.Global().Get("location").Get("hash")
	if hash.Type() != js.TypeString {
		return ""
	}
	return hash.String()
}

// setHash changes the address bar, which comes back as a hashchange event.
func setHash(href string) {
	js.Global().Get("location").Set("hash", href)
}

// mountStartupFailure renders the one thing a client that cannot start can
// still say honestly.
//
// It goes through the same renderer as everything else, so the page a reader
// sees on failure is the page they know, with the reason in the notice
// rather than a blank document or a console message they will never open.
func mountStartupFailure(err error) {
	mount(journey.NewStore(startupFailurePage(err)))
}

// startupFailurePage is the failure page as a value, so it can be asserted
// without a DOM.
func startupFailurePage(err error) journey.Page {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return journey.Page{
		Title: "Promotion journey · " + journeyclient.Brand,
		Brand: journeyclient.Brand,
		Nav: []journey.NavLink{
			{Label: "Workspace", Href: journeyclient.WorkspacePath},
		},
		Notice: &journey.Notice{
			Tone:   "danger",
			Title:  "This page could not start",
			Detail: detail,
		},
		Footer: journey.Footer{
			Lines: []string{
				"The Promotion journey page is a live client: it reads and acts through this cell's gRPC services over the WebSocket tunnel, and there is no server-rendered equivalent of it to fall back to.",
			},
		},
	}
}
