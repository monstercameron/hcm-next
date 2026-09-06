package main

import "net/url"

const (
	productPageFocusSelector = "#page-title, #main-content h1"
	menuFilterFocusSelector  = "#menu-filter"
)

// productRouteFocusTarget keeps focus in the menu filter when the debounce
// changes only menu_q. Other software-navigation changes retain the normal
// SPA behavior of announcing and focusing the new page heading.
func productRouteFocusTarget(previous, current string) (selector string, caretAtEnd bool) {
	if menuFilterOnlyRouteChange(previous, current) {
		return menuFilterFocusSelector, true
	}
	return productPageFocusSelector, false
}

func menuFilterOnlyRouteChange(previous, current string) bool {
	before, beforeErr := url.Parse(previous)
	after, afterErr := url.Parse(current)
	if beforeErr != nil || afterErr != nil || before.Path != after.Path {
		return false
	}
	beforeQuery, afterQuery := before.Query(), after.Query()
	beforeFilter, afterFilter := beforeQuery.Get("menu_q"), afterQuery.Get("menu_q")
	if beforeFilter == afterFilter {
		return false
	}
	beforeQuery.Del("menu_q")
	afterQuery.Del("menu_q")
	return beforeQuery.Encode() == afterQuery.Encode()
}
