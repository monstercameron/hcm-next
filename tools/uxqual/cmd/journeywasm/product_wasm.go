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
	"github.com/monstercameron/hcm-next/tools/uxqual/taskmux"
	"google.golang.org/grpc/status"
)

const productPathPrefix = "/workspace/app/"

const productViewKey = "product-view"
const productJourneyStoreKey = "product-journey-store"

var lastFocusedProductRoute string
var productNavigationGroups *browserNavigationGroupController
var productTransientPopovers *browserTransientPopoverController
var lastResolvedProductView *productui.View

func isProductPath(path string) bool { return strings.HasPrefix(path, productPathPrefix) }

func startProduct(ctx context.Context, cfg journeyclient.Config, service journeyclient.Service) error {
	// Finite RPC work shares a bounded lane. WatchJourney subscriptions stay
	// outside it, so an open detail page cannot reduce navigation/click
	// capacity. Four slots allow the independent projection reads and a user
	// action to overlap without permitting an unbounded goroutine burst.
	frontendTasks := taskmux.New(taskmux.Options{MaxRunning: 4, MaxQueued: 64, PriorityBurst: 8})
	liveService := productclient.Service{
		ListJourneys: func(ctx context.Context, request *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return service.ListJourneys(ctx, request)
		},
		ListWorkers: func(ctx context.Context, request *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return service.ListWorkers(ctx, request)
		},
	}
	if preferenceService, ok := service.(journeyclient.PreferenceService); ok {
		liveService.GetPreferences = preferenceService.GetProductPreferences
		liveService.GetWorkerIDPolicy = preferenceService.GetWorkerIDPolicy
		liveService.GetRoleAccess = preferenceService.GetRoleAccess
	}
	pagePermissions := make([]productui.RolePagePermission, 0, len(cfg.PagePermissions))
	for _, permission := range cfg.PagePermissions {
		pagePermissions = append(pagePermissions, productui.RolePagePermission{
			Version: permission.Version, RoleID: permission.RoleID, Page: productui.PageID(permission.PageID),
			View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete,
		})
	}
	session := productclient.Session{Tenant: cfg.Tenant, Principal: cfg.Subject, Scope: cfg.Purpose, Roles: cfg.Roles, Permissions: pagePermissions, EnforceRoleVisibility: true, LogoutHref: cfg.LogoutPath}
	preferences := newServerPreferenceController(ctx, service)
	appearance := newBrowserThemeController(preferences.SaveTheme)
	appearance.Apply(appearance.Saved())
	accessibility := newBrowserAccessibilityController(preferences.SaveAccessibility)
	accessibility.Apply(accessibility.Saved())
	productNavigationGroups = newBrowserNavigationGroupController(preferences.SaveNavigationGroups)
	productNavigationGroups.Bind()
	productTransientPopovers = newBrowserTransientPopoverController()
	productTransientPopovers.Bind()
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	productHistory = newBrowserProductHistoryController()
	navigateProduct := func(href string) { productHistory.Navigate(productRouter.Navigate, href) }
	// Menu filtering is local component state. Debounce only its shareable URL
	// state so typing never reruns page loaders or refetches workforce data.
	navigationDebounce := newNavigationDebouncerWithScheduler(browserReplaceURL, browserDebounceScheduler)
	journeyStore := journey.NewStore(journey.Page{})
	bindActionableNoticeFocus(journeyStore)
	journeyApp := journeyclient.New(cfg, service, journeyStore, time.Now)
	journeyApp.Tasks = frontendTasks
	journeyApp.Locate = func(fragment string) {
		navigateProduct(productclient.ProductJourneyHref(fragment, currentQuery()))
	}
	journeyApp.NavigateProduct = navigateProduct
	journeys := &productJourneyBridge{ctx: ctx, app: journeyApp}
	for _, definition := range productui.PageDefinitions() {
		if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction(string(definition.ID), "view") || len(cfg.PagePermissions) == 0 && !productui.PageVisible(definition.ID, cfg.Roles) {
			continue
		}
		definition := definition
		productRouter.Register(definition.Route, productRouteComponent, router.Options{
			Title: definition.Title + " · HCM Next",
			Loader: func(loadCtx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
				navigationDebounce.Cancel()
				// A route change discards an unsaved preview and reapplies the last
				// customer selection before the next component tree is mounted.
				appearance.Apply(appearance.Saved())
				accessibility.Apply(accessibility.Saved())
				// The history router's RouteContext can lag the address bar query
				// during same-page navigation. The browser URL is the canonical
				// presentation state: using the stale context here made a sort click
				// reload the previously persisted column and direction.
				state, parseErr := productclient.ParseState(routeContext.Path, currentQuery())
				if parseErr != nil {
					return nil, parseErr
				}
				var view productui.View
				var loadErr error
				handle, scheduleErr := frontendTasks.Submit(loadCtx, taskmux.Spec{
					Key: "product:route-projection", Priority: taskmux.UserVisible, Duplicate: taskmux.ReplaceExisting,
				}, func(taskCtx context.Context) error {
					view, loadErr = productclient.Load(taskCtx, liveService, session, state)
					return nil
				})
				if scheduleErr != nil {
					return nil, scheduleErr
				}
				select {
				case <-handle.Done():
					result := handle.Result()
					if result.Err != nil {
						return nil, result.Err
					}
				case <-loadCtx.Done():
					handle.Cancel()
					return nil, loadCtx.Err()
				}
				view.Navigate = navigateProduct
				applyBrowserHistoryNavigation(&view)
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
				preferences.Adopt(view)
				appearance.Load(view.Appearance)
				accessibility.Load(view.Accessibility)
				groups := make(map[string]bool, len(view.NavigationGroupOpen))
				for page, open := range view.NavigationGroupOpen {
					groups[string(page)] = open
				}
				productNavigationGroups.Load(groups)
				preferences.PersistView(view)
				if state.Page == productui.PageJourneys && state.Request.JourneyMode == "new" && state.Request.JourneyWorker != "" {
					preferences.RecordWorkflowUse("promotion", state.Request.JourneyWorker)
				} else {
					preferences.ResetWorkflowUseMarker()
				}
				view.Appearance = appearance.Saved()
				view.PreviewTheme = appearance.Preview
				view.SaveTheme = appearance.Save
				view.ResetTheme = appearance.Reset
				view.SaveWorkerIDPolicy = func(policy productui.WorkerIDPolicy) {
					preferences.SaveWorkerIDPolicy(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "worker-id-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "Could not save rules: "+status.Code(err).String())
								return
							}
							statusNode.Set("textContent", "Worker ID rules saved for this organization.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveOrganizationVisibility = func(policy productui.OrganizationVisibilityPolicy) {
					preferences.SaveOrganizationVisibility(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "organization-visibility-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "Could not save visibility: "+status.Code(err).String())
								return
							}
							statusNode.Set("textContent", "Organization visibility saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveAccessRole = func(role productui.AccessRole) {
					preferences.SaveAccessRole(role, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "role-access-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "Could not save role: "+status.Code(err).String())
								return
							}
							statusNode.Set("textContent", "Role created.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveWorkerRoleAssignment = func(assignment productui.WorkerRoleAssignment) {
					preferences.SaveWorkerRoleAssignment(assignment, func(err error) {
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveRoleVisibility = func(policy productui.OrganizationVisibilityPolicy) {
					preferences.SaveRoleVisibility(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "organization-visibility-status-"+policy.RoleID)
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "Could not save visibility: "+status.Code(err).String())
								return
							}
							statusNode.Set("textContent", "Role visibility saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveRolePagePermission = func(permission productui.RolePagePermission) {
					preferences.SaveRolePagePermission(permission, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "role-page-permission-status-"+permission.RoleID+"-"+string(permission.Page))
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "Could not save page access: "+status.Code(err).String())
								return
							}
							statusNode.Set("textContent", "Page access saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.UpdatePeopleDirectory = func(change productui.PeopleDirectoryChange) {
					browserReplaceURL(change.Href)
					if lastResolvedProductView == nil || lastResolvedProductView.Page != productui.PagePeople {
						return
					}
					updated := *lastResolvedProductView
					updated.PeoplePage = 1
					updated.PeopleSort = change.Sort
					updated.PeopleDirection = "asc"
					if change.Descending {
						updated.PeopleDirection = "desc"
					}
					updated.UpdatePeopleDirectory = view.UpdatePeopleDirectory
					lastResolvedProductView = &updated
					preferences.PersistView(updated)
				}
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
				warmRefresh := lastResolvedProductView != nil && lastResolvedProductView.Page == state.Page && state.Page != productui.PageJourneys
				if warmRefresh {
					// Same-page network effects retain the last authorized projection.
					// This avoids a skeleton flash for fast filters, sorts and paging;
					// the resolved response still replaces the tree atomically.
					view = *lastResolvedProductView
					view.Refreshing = true
					currentRoute := currentPath() + "?" + currentQuery()
					if peopleDirectoryOnlyRouteChange(lastFocusedProductRoute, currentRoute) {
						// Sorting, filtering and paging affect only the directory surface.
						// Keep the shell and page heading mounted and scope busy/progress
						// semantics to the table-plus-pagination component.
						view.RefreshingRegion = productui.RefreshRegionPeopleDirectory
					}
				}
				view.Navigate = navigateProduct
				applyBrowserHistoryNavigation(&view)
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
				if warmRefresh {
					return productui.BuildRefreshing(view)
				}
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
		history.Call("replaceState", history.Get("state"), "", href)
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
	resolved := view
	resolved.Loading = false
	resolved.Refreshing = false
	lastResolvedProductView = &resolved
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
	if navigationCollapsedRouteChange(previousRoute, route) {
		resetCollapsedNavigationScroll()
	}
	selector, caretAtEnd := productRouteFocusTarget(previousRoute, route)
	if selector == "" {
		return
	}
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

// resetCollapsedNavigationScroll prevents an expanded menu's independent
// scroll position from stranding the compact icon rail halfway down the list.
func resetCollapsedNavigationScroll() {
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		navigation := js.Global().Get("document").Call("querySelector", ".primary-nav")
		if navigation.Truthy() {
			navigation.Set("scrollTop", 0)
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
