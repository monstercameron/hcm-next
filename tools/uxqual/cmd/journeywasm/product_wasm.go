//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/router"
	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
	"github.com/monstercameron/hcm-next/tools/uxqual/productclient"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/journey"
	"google.golang.org/grpc/status"
)

const productPathPrefix = "/workspace/app/"

const productViewKey = "product-view"
const productJourneyStoreKey = "product-journey-store"

var lastFocusedProductRoute string
var productNavigationGroups *browserNavigationGroupController

func isProductPath(path string) bool { return strings.HasPrefix(path, productPathPrefix) }

func startProduct(ctx context.Context, cfg journeyclient.Config, service journeyclient.Service) error {
	liveService := productclient.Service{
		ListJourneys: func(ctx context.Context, request *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return service.ListJourneys(ctx, request)
		},
		ListWorkers: func(ctx context.Context, request *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return service.ListWorkers(ctx, request)
		},
	}
	session := productclient.Session{Tenant: cfg.Tenant, Principal: cfg.Subject, Scope: cfg.Purpose}
	appearance := newBrowserThemeController(cfg.Tenant)
	appearance.Apply(appearance.Saved())
	accessibility := newBrowserAccessibilityController(cfg.Tenant, cfg.Subject)
	accessibility.Apply(accessibility.Saved())
	productNavigationGroups = newBrowserNavigationGroupController(cfg.Tenant)
	productNavigationGroups.Bind()
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	// Menu filtering is local component state. Debounce only its shareable URL
	// state so typing never reruns page loaders or refetches workforce data.
	navigationDebounce := newNavigationDebouncerWithScheduler(browserReplaceURL, browserDebounceScheduler)
	journeyStore := journey.NewStore(journey.Page{})
	journeyApp := journeyclient.New(cfg, service, journeyStore, time.Now)
	journeyApp.Locate = func(fragment string) {
		productRouter.Navigate(productclient.ProductJourneyHref(fragment, currentQuery()))
	}
	journeyApp.NavigateProduct = productRouter.Navigate
	journeys := &productJourneyBridge{ctx: ctx, app: journeyApp}
	for _, definition := range productui.PageDefinitions() {
		definition := definition
		productRouter.Register(definition.Route, productRouteComponent, router.Options{
			Title: definition.Title + " · HCM Next",
			Loader: func(loadCtx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
				navigationDebounce.Cancel()
				// A route change discards an unsaved preview and reapplies the last
				// customer selection before the next component tree is mounted.
				appearance.Apply(appearance.Saved())
				accessibility.Apply(accessibility.Saved())
				state, parseErr := productclient.ParseState(routeContext.Path, routeContext.Query.Encode())
				if parseErr != nil {
					return nil, parseErr
				}
				view, loadErr := productclient.Load(loadCtx, liveService, session, state)
				view.Navigate = productRouter.Navigate
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
				view.Appearance = appearance.Saved()
				view.PreviewTheme = appearance.Preview
				view.SaveTheme = appearance.Save
				view.ResetTheme = appearance.Reset
				view.Accessibility = accessibility.Saved()
				view.PreviewAccessibility = accessibility.Preview
				view.SaveAccessibility = accessibility.Save
				view.ResetAccessibility = accessibility.Reset
				attrs := router.Attrs{productViewKey: view}
				if state.Page == productui.PageJourneys {
					journeys.Load(productclient.JourneyFragment(state.Request))
					attrs[productJourneyStoreKey] = journeyStore
				}
				if loadErr != nil {
					view.LoadError = "The live Journey service could not complete this view (" + status.Code(loadErr).String() + ")."
					attrs[productViewKey] = view
				}
				return attrs, nil
			},
			Loading: func(_ router.Attrs) *router.Element {
				state, stateErr := productclient.ParseState(currentPath(), currentQuery())
				if stateErr != nil {
					state = productclient.State{Page: definition.ID, Request: productui.PageRequest{Page: definition.ID}}
				}
				view := productclient.LoadingView(session, state)
				view.Navigate = productRouter.Navigate
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
				view.Appearance = appearance.Saved()
				view.Accessibility = accessibility.Saved()
				if productNavigationGroups != nil {
					view.NavigationGroupOpen = make(map[productui.PageID]bool)
					for page, open := range productNavigationGroups.State() {
						view.NavigationGroupOpen[productui.PageID(page)] = open
					}
				}
				applyThemeDocumentIdentity(view.Title, view.Appearance)
				applyLocaleDocumentIdentity(view.Locale)
				return productui.BuildLoading(view)
			},
		})
	}
	clearRoot()
	productRouter.Mount(rootSelector)
	return nil
}

type browserDebounceTimer struct {
	id       js.Value
	callback js.Func
	active   bool
}

func browserDebounceScheduler(delay time.Duration, fire func()) debounceTimer {
	timer := &browserDebounceTimer{active: true}
	timer.callback = js.FuncOf(func(js.Value, []js.Value) any {
		if !timer.active {
			return nil
		}
		timer.active = false
		fire()
		timer.callback.Release()
		return nil
	})
	timer.id = js.Global().Call("setTimeout", timer.callback, delay.Milliseconds())
	return timer
}

func browserReplaceURL(href string) {
	history := js.Global().Get("history")
	if history.Truthy() && history.Get("replaceState").Truthy() {
		history.Call("replaceState", nil, "", href)
	}
}

func (t *browserDebounceTimer) Stop() bool {
	if t == nil || !t.active {
		return false
	}
	t.active = false
	js.Global().Call("clearTimeout", t.id)
	t.callback.Release()
	return true
}

func productRouteComponent(_ router.Attrs) *router.Element {
	data := router.UseRouteData()
	view, ok := data[productViewKey].(productui.View)
	if !ok {
		view = productui.NewView(productui.PageHome, "", "", "")
		view.LoadError = "The requested view did not produce route data."
		view.Navigate = router.Navigate
	}
	if productNavigationGroups != nil {
		view.NavigationGroupOpen = make(map[productui.PageID]bool)
		for page, open := range productNavigationGroups.State() {
			view.NavigationGroupOpen[productui.PageID(page)] = open
		}
	}
	applyThemeDocumentIdentity(view.Title, view.Appearance)
	applyLocaleDocumentIdentity(view.Locale)
	focusProductRouteAfterNavigation()
	var result *router.Element
	if view.Page == productui.PageJourneys {
		if store, storeOK := data[productJourneyStoreKey].(*journey.Store); storeOK {
			result = productui.BuildEmbedded(view, journey.LiveContentComponent(store))
		}
	}
	if result == nil {
		result = productui.Build(view)
	}
	return result
}

// focusProductRouteAfterNavigation restores the missing browser behavior of
// an SPA route change. Initial page load keeps the browser's natural focus
// order (including the skip link); later routes focus their page heading.
func focusProductRouteAfterNavigation() {
	route := currentPath() + "?" + currentQuery()
	if lastFocusedProductRoute == "" {
		lastFocusedProductRoute = route
		return
	}
	if route == lastFocusedProductRoute {
		return
	}
	previousRoute := lastFocusedProductRoute
	lastFocusedProductRoute = route
	selector, caretAtEnd := productRouteFocusTarget(previousRoute, route)
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		document := js.Global().Get("document")
		target := document.Call("querySelector", selector)
		if target.Truthy() {
			if !caretAtEnd && !target.Call("hasAttribute", "tabindex").Bool() {
				target.Call("setAttribute", "tabindex", "-1")
			}
			target.Call("focus")
			if caretAtEnd {
				length := target.Get("value").Get("length").Int()
				target.Call("setSelectionRange", length, length)
			}
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

type productJourneyBridge struct {
	ctx      context.Context
	app      *journeyclient.App
	started  bool
	fragment string
}

func (b *productJourneyBridge) Load(fragment string) {
	if b == nil || b.app == nil || fragment == b.fragment && b.started {
		return
	}
	b.fragment = fragment
	if !b.started {
		b.started = true
		b.app.Start(b.ctx, fragment)
		return
	}
	b.app.OnHashChange(fragment)
}

func clearRoot() {
	if root := js.Global().Get("document").Call("getElementById", rootElementID); root.Truthy() {
		root.Set("textContent", "")
	}
}

func currentPath() string {
	path := js.Global().Get("location").Get("pathname")
	if path.Type() != js.TypeString {
		return ""
	}
	return path.String()
}

func currentQuery() string {
	search := js.Global().Get("location").Get("search")
	if search.Type() != js.TypeString {
		return ""
	}
	return strings.TrimPrefix(search.String(), "?")
}
