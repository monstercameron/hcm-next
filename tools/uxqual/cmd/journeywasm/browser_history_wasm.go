//go:build js && wasm

package main

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

const (
	productHistoryIDField    = "hcmProductHistoryID"
	productHistoryIndexField = "hcmProductHistoryIndex"
	productHistoryStoreKey   = "hcm-next.product-history."
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
	controller.id = strconv.FormatInt(time.Now().UnixNano(), 36)
	controller.replaceCurrentState(0)
	controller.writeMax()
	return controller
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
	current := history.Get("state")
	if current.Type() == js.TypeObject {
		state = js.Global().Get("Object").Call("assign", state, current)
	}
	state.Set(productHistoryIDField, controller.id)
	state.Set(productHistoryIndexField, index)
	history.Call("replaceState", state, "", js.Global().Get("location").Get("href"))
}

func (controller *browserProductHistoryController) writeMax() {
	storage := js.Global().Get("sessionStorage")
	if storage.Type() == js.TypeObject {
		storage.Call("setItem", productHistoryStoreKey+controller.id, strconv.Itoa(controller.maxIndex))
	}
}

func readProductHistoryMax(id string) int {
	storage := js.Global().Get("sessionStorage")
	if storage.Type() != js.TypeObject {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(storage.Call("getItem", productHistoryStoreKey+id).String()))
	if err != nil || value < 0 {
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
	if !safeProductHistoryID(id) || math.IsNaN(indexFloat) || math.IsInf(indexFloat, 0) || indexFloat < 0 || math.Trunc(indexFloat) != indexFloat || indexFloat > float64(maxIntValue()) {
		return "", 0, false
	}
	return id, int(indexFloat), true
}

func safeProductHistoryID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'z' {
				return false
			}
		}
	}
	return true
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
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
