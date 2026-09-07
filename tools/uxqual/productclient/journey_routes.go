package productclient

import (
	"net/url"
	"strings"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
)

// JourneyFragment translates a first-class product route into the address
// understood by the existing live journey state machine.
func JourneyFragment(request productui.PageRequest) string {
	if id := strings.TrimSpace(request.JourneyID); id != "" {
		return journeyclient.DetailHref(id)
	}
	worker := strings.TrimSpace(request.JourneyWorker)
	if strings.EqualFold(strings.TrimSpace(request.JourneyMode), "new") {
		return journeyclient.ProposalHref(worker)
	}
	if worker != "" {
		return journeyclient.WorkerHref(worker)
	}
	return journeyclient.ListHref()
}

// ProductJourneyHref translates a navigation emitted by the live journey
// client back into a history-router URL. Only shell preferences survive the
// transition; stale journey-specific fields are always replaced.
func ProductJourneyHref(fragment, currentQuery string) string {
	if len(currentQuery) > maxRouteQueryBytes {
		currentQuery = ""
	}
	current, err := url.ParseQuery(currentQuery)
	if err != nil {
		// A malformed current address is not a reason to block a safe
		// navigation, but none of its partially parsed values are trusted.
		current = url.Values{}
	}
	values := url.Values{}
	if entries := current["nav"]; len(entries) == 1 && entries[0] == "collapsed" {
		values.Set("nav", "collapsed")
	}
	for _, key := range []string{"locale", "menu_q"} {
		if entries := current[key]; len(entries) == 1 && safeRouteValue(entries[0]) {
			if value := strings.TrimSpace(entries[0]); value != "" {
				values.Set(key, value)
			}
		}
	}
	if entries := current["favorites"]; len(entries) == 1 && safeRouteValue(entries[0]) {
		favorites := parseFavoritePages(entries[0])
		if len(favorites) > 0 {
			serialized := make([]string, 0, len(favorites))
			for _, page := range favorites {
				serialized = append(serialized, string(page))
			}
			values.Set("favorites", strings.Join(serialized, ","))
		}
	}
	route := journeyclient.Parse(fragment)
	switch route.Kind {
	case journeyclient.RouteDetail:
		values.Set("journey", route.IntentID)
	case journeyclient.RouteProposal:
		values.Set("mode", "new")
		if route.WorkerRef != "" {
			values.Set("worker", route.WorkerRef)
		}
	default:
		if route.WorkerRef != "" {
			values.Set("worker", route.WorkerRef)
		}
	}
	href := productui.Path(productui.PageJourneys)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}
