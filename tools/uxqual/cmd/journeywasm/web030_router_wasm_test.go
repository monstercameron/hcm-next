//go:build js && wasm

package main

import (
	"context"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/tools/uxqual/productclient"
)

// TestTodo_WEB_030_Browser runs as a real js/wasm Go test under Node. It uses
// GWC v5's production HistoryRouter rather than reimplementing its loader
// cache: pushState, popstate, loader contexts, cancellation, stale-answer
// suppression, the persistent layout stack, and the product history controls
// all execute through the dependency used by startProduct.
func TestTodo_WEB_030_Browser(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/people?q=cold&page=2&action=approve&credential=secret")
	canonicalizeCurrentProductLocation()
	if got := browser.path(); got != "/workspace/app/people?page=2&q=cold" {
		t.Fatalf("cold browser URL was not canonicalized before router construction: %q", got)
	}

	var coldAttempts atomic.Int32
	started := make(chan string, 8)
	completed := make(chan string, 8)
	writeEligible := make(chan bool, 8)
	coldCancelled := make(chan struct{})
	releaseCold := make(chan struct{})
	rendered := make(chan string, 8)

	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	productRouter.SetFocusManagement(false)
	productRouter.Register("/workspace/app", func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "persistent-product-shell"}, router.GetOutlet())
	}, router.Options{Layout: true})
	productRouter.Register(productui.Path(productui.PagePeople), func(attrs router.Attrs) *router.Element {
		if answer, ok := attrs["answer"].(string); ok {
			if source, sourceOK := attrs[productSourceHrefKey].(string); sourceOK && currentProductHref() == source {
				if resolved, resolvedOK := attrs[productResolvedHrefKey].(string); resolvedOK && resolved != source {
					browserReplaceURL(resolved)
				}
			}
			rendered <- answer
		}
		return html.Div(html.Props{ID: "people-route"}, ui.Text("people"))
	}, router.Options{
		Loader: func(ctx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
			query := routeContext.Query.Encode()
			writeEligible <- productHistory.ClaimSoftwareNavigation(routeContext.Path, query)
			started <- routeContext.Path + "?" + query
			if routeContext.Query.Get("q") == "cold" && coldAttempts.Add(1) == 1 {
				<-ctx.Done()
				close(coldCancelled)
				// Deliberately finish after cancellation. The real router must
				// suppress this stale answer rather than trusting cooperation.
				<-releaseCold
			}
			state, err := productclient.ParseState(routeContext.Path, query)
			if err != nil {
				return nil, err
			}
			resolved := productui.NewView(productui.PagePeople, "Tenant", "Principal", "Scope")
			resolved.People = []productui.Person{{ID: "worker-1", Name: "Rafael"}}
			resolved = productui.ApplyRequest(resolved, state.Request)
			sourceHref := productclient.CanonicalHref(state)
			completed <- query
			return router.Attrs{
				"answer": query, productSourceHrefKey: sourceHref,
				productResolvedHrefKey: productclient.ResolvedCanonicalHref(state, resolved),
			}, nil
		},
		Loading: func(router.Attrs) *router.Element {
			return html.Div(html.Props{ID: "people-route-loading"}, ui.Text("loading"))
		},
	})

	productHistory = newBrowserProductHistoryController()
	navigate := func(href string) { productHistory.Navigate(productRouter.Navigate, href) }
	if productRouter.Current() == nil {
		t.Fatal("cold deep link produced no route element")
	}
	wantEvent(t, started, "/workspace/app/people?page=2&q=cold")
	wantBoolEvent(t, writeEligible, false, "cold route")

	navigate("/workspace/app/people?page=3&q=fresh")
	wantEvent(t, started, "/workspace/app/people?page=3&q=fresh")
	wantBoolEvent(t, writeEligible, true, "software navigation")
	if browserRouteGenerationMatches(productui.Path(productui.PagePeople), url.Values{"page": {"2"}, "q": {"cold"}}.Encode()) {
		t.Fatal("superseded same-path query generation could canonicalize the newer address")
	}
	wantSignal(t, coldCancelled, "superseded GWC loader context was not cancelled")
	close(releaseCold)
	wantEvent(t, completed, "page=3&q=fresh")
	wantEvent(t, completed, "page=2&q=cold")
	wantRenderedRoute(t, productRouter, rendered, "page=3&q=fresh")
	if browser.path() != "/workspace/app/people?page=1&q=fresh" || len(browser.entries) != 2 {
		t.Fatalf("resolved push address/history = %q entries=%d", browser.path(), len(browser.entries))
	}

	inspection := router.InspectCurrentRoute()
	if inspection.Path != productui.Path(productui.PagePeople) || len(inspection.Stack) != 2 ||
		inspection.Stack[0].Path != "/workspace/app" || inspection.Stack[1].Path != productui.Path(productui.PagePeople) {
		t.Fatalf("production route stack = %+v", inspection)
	}
	props := productHistory.Props(productui.ResolveProductLocale("de-DE"))
	if !props.CanGoBack || props.CanGoForward || props.GoBack == nil {
		t.Fatalf("fresh history controls = %+v", props)
	}

	props.GoBack()
	wantEvent(t, started, "/workspace/app/people?page=2&q=cold")
	wantBoolEvent(t, writeEligible, false, "back popstate")
	wantEvent(t, completed, "page=2&q=cold")
	wantRenderedRoute(t, productRouter, rendered, "page=2&q=cold")
	if browser.path() != "/workspace/app/people?page=1&q=cold" || len(browser.entries) != 2 {
		t.Fatalf("resolved back address/history = %q entries=%d", browser.path(), len(browser.entries))
	}
	props = productHistory.Props(productui.ResolveProductLocale("de-DE"))
	if props.CanGoBack || !props.CanGoForward || props.GoForward == nil {
		t.Fatalf("back-resumed history controls = %+v", props)
	}

	props.GoForward()
	wantEvent(t, started, "/workspace/app/people?page=1&q=fresh")
	wantBoolEvent(t, writeEligible, false, "forward popstate")
	wantEvent(t, completed, "page=1&q=fresh")
	wantRenderedRoute(t, productRouter, rendered, "page=1&q=fresh")
	if browser.path() != "/workspace/app/people?page=1&q=fresh" || len(browser.entries) != 2 {
		t.Fatalf("forward address = %q", browser.path())
	}
}

func TestTodo_WEB_030_Conformance(t *testing.T) {
	object := js.Global().Get("Object")
	for name, test := range map[string]struct {
		id    string
		index any
	}{
		"fraction":         {id: "0123456789abcdef0123456789abcdef", index: 1.5},
		"negative":         {id: "0123456789abcdef0123456789abcdef", index: -1},
		"not finite":       {id: "0123456789abcdef0123456789abcdef", index: js.Global().Get("Infinity")},
		"unsafe id":        {id: "ledger.tenant-a", index: 1},
		"oversized id":     {id: strings.Repeat("a", 129), index: 1},
		"wrong index type": {id: "0123456789abcdef0123456789abcdef", index: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			state := object.New()
			state.Set(productHistoryIDField, test.id)
			state.Set(productHistoryIndexField, test.index)
			if _, _, ok := productHistoryState(state); ok {
				t.Fatalf("accepted malformed history state id=%q index=%v", test.id, test.index)
			}
		})
	}
	state := object.New()
	state.Set(productHistoryIDField, "0123456789abcdef0123456789abcdef")
	state.Set(productHistoryIndexField, 17)
	if id, index, ok := productHistoryState(state); !ok || id != "0123456789abcdef0123456789abcdef" || index != 17 {
		t.Fatalf("valid history state = %q %d %v", id, index, ok)
	}

	for name, transition := range map[string]struct {
		before, after string
		selector      string
		caret         bool
	}{
		"collection control retains focus": {
			before: "/workspace/app/people?page=2", after: "/workspace/app/people?page=3",
		},
		"shell control retains focus": {
			before: "/workspace/app/people", after: "/workspace/app/people?nav=collapsed",
		},
		"menu filter retains caret": {
			before: "/workspace/app/home", after: "/workspace/app/home?menu_q=peo",
			selector: menuFilterFocusSelector, caret: true,
		},
		"destination announces heading": {
			before: "/workspace/app/people", after: "/workspace/app/person?person=worker-1",
			selector: productPageFocusSelector,
		},
	} {
		t.Run(name, func(t *testing.T) {
			selector, caret := productRouteFocusTarget(transition.before, transition.after)
			if selector != transition.selector || caret != transition.caret {
				t.Fatalf("focus target = (%q, %v), want (%q, %v)", selector, caret, transition.selector, transition.caret)
			}
		})
	}
}

func wantRenderedRoute(t *testing.T, productRouter *router.Router, rendered <-chan string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		productRouter.Current()
		select {
		case got := <-rendered:
			if got == want {
				return
			}
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("route %q was not rendered from the current GWC loader generation", want)
}

func wantEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("router event = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for router event %q", want)
	}
}

func wantSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func wantBoolEvent(t *testing.T, events <-chan bool, want bool, description string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("%s write eligibility = %v, want %v", description, got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s write eligibility", description)
	}
}

type wasmHistoryEntry struct {
	href  string
	state js.Value
}

type wasmHistoryHarness struct {
	t          *testing.T
	window     js.Value
	location   js.Value
	history    js.Value
	entries    []wasmHistoryEntry
	index      int
	listeners  map[string]js.Value
	storage    map[string]string
	callbacks  []js.Func
	oldWindow  js.Value
	oldHistory js.Value
	oldLoc     js.Value
	oldStorage js.Value
}

func installWASMHistory(t *testing.T, initialHref string) *wasmHistoryHarness {
	t.Helper()
	object := js.Global().Get("Object")
	h := &wasmHistoryHarness{
		t: t, window: object.New(), location: object.New(), history: object.New(),
		entries: []wasmHistoryEntry{{href: initialHref, state: js.Null()}}, listeners: map[string]js.Value{}, storage: map[string]string{},
		oldWindow: js.Global().Get("window"), oldHistory: js.Global().Get("history"), oldLoc: js.Global().Get("location"), oldStorage: js.Global().Get("sessionStorage"),
	}
	h.applyHref(initialHref)
	h.addCallback("addEventListener", func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			h.listeners[args[0].String()] = args[1]
		}
		return nil
	}, h.window)
	h.addCallback("removeEventListener", func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			delete(h.listeners, args[0].String())
		}
		return nil
	}, h.window)
	h.addCallback("pushState", func(_ js.Value, args []js.Value) any {
		href := args[2].String()
		h.entries = append(append([]wasmHistoryEntry(nil), h.entries[:h.index+1]...), wasmHistoryEntry{href: href, state: args[0]})
		h.index++
		h.applyHref(href)
		h.syncState()
		return nil
	}, h.history)
	h.addCallback("replaceState", func(_ js.Value, args []js.Value) any {
		href := args[2].String()
		h.entries[h.index] = wasmHistoryEntry{href: href, state: args[0]}
		h.applyHref(href)
		h.syncState()
		return nil
	}, h.history)
	h.addCallback("go", func(_ js.Value, args []js.Value) any {
		next := h.index + args[0].Int()
		if next < 0 || next >= len(h.entries) {
			return nil
		}
		h.index = next
		h.applyHref(h.entries[h.index].href)
		h.syncState()
		if listener := h.listeners["popstate"]; listener.Truthy() {
			listener.Invoke()
		}
		return nil
	}, h.history)
	storage := object.New()
	h.addCallback("getItem", func(_ js.Value, args []js.Value) any {
		if value, ok := h.storage[args[0].String()]; ok {
			return value
		}
		return nil
	}, storage)
	h.addCallback("setItem", func(_ js.Value, args []js.Value) any {
		h.storage[args[0].String()] = args[1].String()
		return nil
	}, storage)
	h.window.Set("location", h.location)
	h.window.Set("history", h.history)
	js.Global().Set("window", h.window)
	js.Global().Set("location", h.location)
	js.Global().Set("history", h.history)
	js.Global().Set("sessionStorage", storage)
	h.syncState()
	t.Cleanup(func() {
		js.Global().Set("window", h.oldWindow)
		js.Global().Set("history", h.oldHistory)
		js.Global().Set("location", h.oldLoc)
		js.Global().Set("sessionStorage", h.oldStorage)
		for _, callback := range h.callbacks {
			callback.Release()
		}
	})
	return h
}

func (h *wasmHistoryHarness) addCallback(name string, callback func(js.Value, []js.Value) any, target js.Value) {
	fn := js.FuncOf(callback)
	h.callbacks = append(h.callbacks, fn)
	target.Set(name, fn)
}

func (h *wasmHistoryHarness) applyHref(href string) {
	parsed, err := url.Parse(href)
	if err != nil {
		h.t.Fatal(err)
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	h.location.Set("pathname", path)
	search := ""
	if parsed.RawQuery != "" {
		search = "?" + parsed.RawQuery
	}
	h.location.Set("search", search)
	h.location.Set("hash", parsed.Fragment)
	h.location.Set("href", href)
}

func (h *wasmHistoryHarness) syncState() {
	h.history.Set("state", h.entries[h.index].state)
}

func (h *wasmHistoryHarness) path() string {
	return h.location.Get("pathname").String() + h.location.Get("search").String()
}
