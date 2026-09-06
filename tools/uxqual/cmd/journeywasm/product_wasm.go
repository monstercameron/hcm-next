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
	journeyStore := journey.NewStore(journey.Page{})
	journeyApp := journeyclient.New(cfg, service, journeyStore, time.Now)
	journeyApp.Locate = func(fragment string) {
		router.Navigate(productclient.ProductJourneyHref(fragment, currentQuery()))
	}
	journeyApp.NavigateProduct = router.Navigate
	journeys := &productJourneyBridge{ctx: ctx, app: journeyApp}
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	for _, definition := range productui.PageDefinitions() {
		definition := definition
		productRouter.Register(definition.Route, productRouteComponent, router.Options{
			Title: definition.Title + " · HCM Next",
			Loader: func(loadCtx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
				// A route change discards an unsaved preview and reapplies the last
				// customer selection before the next component tree is mounted.
				appearance.Apply(appearance.Saved())
				state, parseErr := productclient.ParseState(routeContext.Path, routeContext.Query.Encode())
				if parseErr != nil {
					return nil, parseErr
				}
				view, loadErr := productclient.Load(loadCtx, liveService, session, state)
				view.Navigate = router.Navigate
				view.Appearance = appearance.Saved()
				view.PreviewTheme = appearance.Preview
				view.SaveTheme = appearance.Save
				view.ResetTheme = appearance.Reset
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
			Loading: productui.Build(productui.NewView(definition.ID, session.Tenant, session.Principal, session.Scope)),
		})
	}
	clearRoot()
	productRouter.Mount(rootSelector)
	return nil
}

func productRouteComponent(_ router.Attrs) *router.Element {
	data := router.UseRouteData()
	view, ok := data[productViewKey].(productui.View)
	if !ok {
		view = productui.NewView(productui.PageHome, "", "", "")
		view.LoadError = "The requested view did not produce route data."
		view.Navigate = router.Navigate
	}
	applyThemeDocumentIdentity(view.Title, view.Appearance)
	applyLocaleDocumentIdentity(view.Locale)
	if view.Page == productui.PageJourneys {
		if store, storeOK := data[productJourneyStoreKey].(*journey.Store); storeOK {
			return productui.BuildEmbedded(view, journey.LiveContentComponent(store))
		}
	}
	return productui.Build(view)
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
