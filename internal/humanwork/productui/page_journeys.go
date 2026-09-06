package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// journeysPage is the non-browser fallback for the registered route. The
// WASM composition replaces this body with the live journey state machine;
// keeping an honest fallback means SSR and architecture checks never invent
// workflow data.
func journeysPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       "Connecting to Journeys",
		Description: "Loading the governed workflow service and your authorized journey records.",
		Role:        "status",
	})
}
