//go:build js && wasm

package main

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"net/url"
	"strings"
	"syscall/js"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/tools/uxqual/productclient"
)

const (
	productHistoryIDField    = "hcmProductHistoryID"
	productHistoryIndexField = "hcmProductHistoryIndex"
)

var productHistory *browserProductHistoryController

// browserProductHistoryController annotates entries created by the software
// router. The standard History API can move in both directions but cannot
// report forward availability, so the controller retains the high-water mark
// in session storage. A ledger ID in history.state keeps stale sessions apart.
type browserProductHistoryController struct {
	id                        string
	index                     int
	maxIndex                  int
	pendingSoftwareNavigation string
}

func newBrowserProductHistoryController() *browserProductHistoryController {
	controller := &browserProductHistoryController{}
	history := js.Global().Get("history")
	if history.Type() != js.TypeObject {
		return controller
	}
	state := history.Get("state")
	if id, index, ok := productHistoryState(state); ok {
		controller.id = id
		controller.index = index
		controller.maxIndex = maxInt(index, readProductHistoryMax(id))
		return controller
	}
	if controller.id = newProductHistoryLedgerID(); controller.id == "" {
		// A browser without a secure random source cannot safely mint the
		// ledger namespace. Keep the router usable; only forward controls that
		// require this non-authoritative hint are disabled.
		return controller
	}
	controller.replaceCurrentState(0)
	controller.writeMax()
	return controller
}

func newProductHistoryLedgerID() string {
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(randomID[:])
}

// Navigate delegates to the production router, then annotates the entry it
// synchronously pushed. A new push discards the browser's forward branch.
func (controller *browserProductHistoryController) Navigate(navigate func(string), href string) {
	if navigate == nil {
		return
	}
	if controller != nil {
		controller.pendingSoftwareNavigation = normalizedProductHistoryHref(href)
	}
	navigate(href)
	if controller == nil || controller.id == "" {
		return
	}
	if id, _, ok := productHistoryState(js.Global().Get("history").Get("state")); ok && id == controller.id {
		return
	}
	controller.index++
	controller.maxIndex = controller.index
	controller.replaceCurrentState(controller.index)
	controller.writeMax()
}

// ClaimSoftwareNavigation distinguishes a user-initiated software push from
// cold rendering and browser popstate resumption. Route loaders may persist
// presentation preferences only for the former; a history read must never
// replay a preference or workflow-use mutation.
func (controller *browserProductHistoryController) ClaimSoftwareNavigation(path, encodedQuery string) bool {
	if controller == nil {
		return false
	}
	pending := controller.pendingSoftwareNavigation
	controller.pendingSoftwareNavigation = ""
	current := path
	if encodedQuery != "" {
		current += "?" + encodedQuery
	}
	return pending != "" && pending == current
}

func normalizedProductHistoryHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil || parsed.Path == "" {
		return ""
	}
	normalized := parsed.Path
	if query := parsed.Query().Encode(); query != "" {
		normalized += "?" + query
	}
	return normalized
}

func (controller *browserProductHistoryController) Props(locale productui.LocaleContext) productui.HistoryNavigationProps {
	props := productui.HistoryNavigationProps{I18nProps: productui.I18nProps{Locale: locale}}
	if controller == nil || controller.id == "" {
		return props
	}
	if id, index, ok := productHistoryState(js.Global().Get("history").Get("state")); ok && id == controller.id {
		controller.index = index
	}
	props.CanGoBack = controller.index > 0
	props.CanGoForward = controller.index < controller.maxIndex
	props.GoBack = func() { controller.Go(-1) }
	props.GoForward = func() { controller.Go(1) }
	return props
}

func (controller *browserProductHistoryController) Go(offset int) {
	if controller == nil || (offset != -1 && offset != 1) {
		return
	}
	if offset < 0 && controller.index <= 0 || offset > 0 && controller.index >= controller.maxIndex {
		return
	}
	js.Global().Get("history").Call("go", offset)
}

func (controller *browserProductHistoryController) replaceCurrentState(index int) {
	history := js.Global().Get("history")
	if history.Type() != js.TypeObject || history.Get("replaceState").Type() != js.TypeFunction {
		return
	}
	state := js.Global().Get("Object").New()
	state.Set(productHistoryIDField, controller.id)
	state.Set(productHistoryIndexField, index)
	history.Call("replaceState", state, "", js.Global().Get("location").Get("href"))
}

func (controller *browserProductHistoryController) writeMax() {
	key := productclient.HistoryStorageKey(controller.id)
	value, ok := productclient.EncodeHistoryIndex(controller.maxIndex)
	storage, storageOK := browserSessionStorage()
	if storageOK && ok && productclient.ValidateBrowserStorageWrite(key, value) == nil {
		browserStorageSet(storage, key, value)
	}
}

func readProductHistoryMax(id string) int {
	key := productclient.HistoryStorageKey(id)
	if key == "" {
		return 0
	}
	storage, storageOK := browserSessionStorage()
	if !storageOK {
		return 0
	}
	raw, ok := browserStorageGet(storage, key)
	if !ok {
		return 0
	}
	value, ok := productclient.DecodeHistoryIndex(raw)
	if !ok {
		return 0
	}
	return value
}

func productHistoryState(state js.Value) (string, int, bool) {
	if state.Type() != js.TypeObject {
		return "", 0, false
	}
	idValue := state.Get(productHistoryIDField)
	indexValue := state.Get(productHistoryIndexField)
	if idValue.Type() != js.TypeString || indexValue.Type() != js.TypeNumber {
		return "", 0, false
	}
	id := strings.TrimSpace(idValue.String())
	indexFloat := indexValue.Float()
	// history.state is browser-owned input. Do not let NaN, infinity,
	// fractions, or an unbounded value turn the history controls into a
	// surprising history.go request (or a platform-dependent int overflow).
	if !safeProductHistoryID(id) || math.IsNaN(indexFloat) || math.IsInf(indexFloat, 0) || indexFloat < 0 || math.Trunc(indexFloat) != indexFloat || indexFloat > float64(productclient.MaxHistoryIndex) {
		return "", 0, false
	}
	return id, int(indexFloat), true
}

func safeProductHistoryID(id string) bool {
	return productclient.ValidHistoryLedgerID(id)
}

// Web Storage is an optional, user-controlled capability. Private browsing,
// disabled cookies, quota exhaustion and sandboxed iframes can make either
// operation throw a DOMException. A storage failure must never tear down the
// Go event loop or turn an optional hint into a hard navigation failure.
func browserStorageSet(storage js.Value, key, value string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if storage.Type() != js.TypeObject || key == "" || productclient.ValidateBrowserStorageWrite(key, value) != nil {
		return false
	}
	storage.Call("setItem", key, value)
	return true
}

func browserSessionStorage() (storage js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			storage = js.Undefined()
			ok = false
		}
	}()
	storage = js.Global().Get("sessionStorage")
	if storage.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return storage, true
}

func browserStorageGet(storage js.Value, key string) (value string, ok bool) {
	defer func() {
		if recover() != nil {
			value = ""
			ok = false
		}
	}()
	if storage.Type() != js.TypeObject || key == "" || productclient.ValidateHistoryStorageEntry(key, "0") != nil {
		return "", false
	}
	raw := storage.Call("getItem", key)
	if raw.Type() != js.TypeString {
		return "", false
	}
	return raw.String(), true
}

func applyBrowserHistoryNavigation(view *productui.View) {
	if view != nil && productHistory != nil {
		view.HistoryNavigation = productHistory.Props(view.Locale)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
