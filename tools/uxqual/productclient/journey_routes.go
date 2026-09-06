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
	current, _ := url.ParseQuery(currentQuery)
	values := url.Values{}
	for _, key := range []string{"nav", "menu_q", "favorites"} {
		if value := current.Get(key); value != "" {
			values.Set(key, value)
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
