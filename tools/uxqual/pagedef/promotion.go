package pagedef

// PromotionListPageDefinition and PromotionDetailPageDefinition are WEB-002's
// projection proof: the real Promotion journey pages (tools/uxqual/render/
// journey.Page's ListView and DetailView, projected by
// tools/uxqual/journeyclient) expressed as PageDefinitions. This file does
// not import the renderer or the client -- pagedef owns none of that code --
// it only names the same regions, widgets, bindings, and actions those
// packages already build by hand, in the versioned contract shape WEB-002
// defines. Every RPC named below is read off hcmnext.journey.v1.JourneyService's
// own generated ServiceDesc (see rpcregistry.go and JourneyServiceName), so a
// rename of a method on that service breaks this file's compile-time string
// nowhere -- it breaks TestPromotionPagesProjectOntoJourneyService, which
// checks every one of these refs still resolves.

// PromotionListPageDefinition is the journeys overview: the workforce panel
// (tools/uxqual/render/journey.PeopleView) and the journeys panel
// (tools/uxqual/render/journey.ListView.Journeys plus its ProposalForm), in
// the order the real page renders them.
func PromotionListPageDefinition() PageDefinition {
	return PageDefinition{
		PageID:       "promotion.journeys.list",
		Version:      1,
		FloorplanRef: "floorplan.launch.v1",
		Regions: []Region{
			{
				ID:   "shell",
				Kind: RegionShell,
			},
			{
				ID:      "page-identity",
				Kind:    RegionPageIdentity,
				Heading: &Heading{Level: 1, Text: "Promotion journeys"},
			},
			{
				ID:      "workforce",
				Kind:    RegionPrimary,
				Heading: &Heading{Level: 2, Text: "Workforce"},
				Widgets: []WidgetSlot{
					{ID: "workforce-table", WidgetRef: "widget.table.workforce.v1"},
					{ID: "create-worker-form", WidgetRef: "widget.form.create-worker.v1"},
				},
				Bindings: []DataBinding{
					{ID: "workers", RPC: RPCRef(JourneyServiceName, "ListWorkers")},
				},
				Actions: []ActionRef{
					{ID: "create-worker", RPC: RPCRef(JourneyServiceName, "CreateWorker"), RequiredRole: "hr.workforce.editor"},
				},
			},
			{
				ID:      "journeys",
				Kind:    RegionSupporting,
				Heading: &Heading{Level: 2, Text: "Promotion journeys"},
				Widgets: []WidgetSlot{
					{ID: "journeys-list", WidgetRef: "widget.list.journeys.v1"},
					{ID: "proposal-form", WidgetRef: "widget.form.propose-journey.v1"},
				},
				Bindings: []DataBinding{
					{ID: "journeys", RPC: RPCRef(JourneyServiceName, "ListJourneys")},
				},
				Actions: []ActionRef{
					{ID: "propose", RPC: RPCRef(JourneyServiceName, "ProposeJourney"), RequiredRole: "manager"},
				},
			},
		},
		Accessibility: Accessibility{
			Landmarks:  []string{"banner", "main", "contentinfo"},
			LiveRegion: LiveRegionPolite,
		},
		BrandTokens: []string{
			"brand.color.primary",
			"brand.typography.heading",
			"brand.spacing.md",
		},
	}
}

// PromotionDetailPageDefinition is one journey's detail page: the stepper,
// the comparison/simulation region, the engine/work-item region, and the
// stage-dependent decision actions (tools/uxqual/render/journey.DetailView).
func PromotionDetailPageDefinition() PageDefinition {
	return PageDefinition{
		PageID:       "promotion.journeys.detail",
		Version:      1,
		FloorplanRef: "floorplan.intent_workspace.v1",
		Regions: []Region{
			{
				ID:   "shell",
				Kind: RegionShell,
			},
			{
				ID:      "page-identity",
				Kind:    RegionPageIdentity,
				Heading: &Heading{Level: 1, Text: "Promotion journey"},
				Bindings: []DataBinding{
					{ID: "journey", RPC: RPCRef(JourneyServiceName, "InspectJourney")},
				},
			},
			{
				ID:      "steps",
				Kind:    RegionLocalNavigation,
				Widgets: []WidgetSlot{{ID: "stepper", WidgetRef: "widget.stepper.journey-stage.v1"}},
			},
			{
				ID:      "comparison",
				Kind:    RegionPrimary,
				Heading: &Heading{Level: 2, Text: "Comparison"},
				Widgets: []WidgetSlot{
					{ID: "comparison-table", WidgetRef: "widget.table.comparison.v1"},
					{ID: "pay-band-gauge", WidgetRef: "widget.gauge.pay-band.v1"},
					{ID: "budget-gauge", WidgetRef: "widget.gauge.budget.v1"},
				},
			},
			{
				ID:      "engine",
				Kind:    RegionSupporting,
				Heading: &Heading{Level: 2, Text: "Engine and work items"},
				Widgets: []WidgetSlot{
					{ID: "engine-facts", WidgetRef: "widget.factlist.v1"},
					{ID: "work-items", WidgetRef: "widget.table.work-items.v1"},
					{ID: "timeline", WidgetRef: "widget.timeline.v1"},
				},
			},
			{
				ID:      "decision",
				Kind:    RegionCompletion,
				Heading: &Heading{Level: 2, Text: "Decision"},
				Actions: []ActionRef{
					{ID: "execute", RPC: RPCRef(JourneyServiceName, "ExecuteJourney"), RequiredRole: "manager"},
					{ID: "approve", RPC: RPCRef(JourneyServiceName, "DecideJourney"), RequiredRole: "compensation.approver"},
					{ID: "reject", RPC: RPCRef(JourneyServiceName, "DecideJourney"), RequiredRole: "compensation.approver"},
				},
			},
		},
		Accessibility: Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "contentinfo"},
			LiveRegion: LiveRegionPolite,
		},
		BrandTokens: []string{
			"brand.color.primary",
			"brand.color.status",
			"brand.typography.heading",
			"brand.spacing.md",
		},
	}
}
