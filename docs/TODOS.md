# Atomic Component TODO Inventory

Generated from the previous component and control inventory on 2026-05-15.
The previous file mixed atomic components, aliases, renderer internals, examples,
compound HCM editors, and broad domain catalog widgets. This version keeps only
the reusable building blocks that a workflow config or agent should compose into
larger interactions.

Each checkbox means:

- Verify the standardized config shape and hydration path.
- Verify semantic markup, accessible names, keyboard behavior, and focus states.
- Verify full runtime branding through brand tokens, CSS variables, and style props.
- Verify preview data and generated UI compatibility.
- Verify responsive quality with Playwright screenshots.
- Add focused unit, integration, or E2E coverage where the item has behavior.

## Review Rules

- Keep components that do one durable UI job.
- Collapse aliases into one canonical component with semantic props.
- Collapse domain-specific controls into generic field, selector, table, diff,
  timeline, checklist, and action primitives.
- Collapse catalog-only widgets into examples or recipes, not atomic components.
- Remove showcase helper components from the readiness checklist.
- Keep compatibility aliases only in registry/adapters, not as first-class TODOs.

## Sources Read

- `src/console/src/app/App.tsx`
- `src/console/src/brand/BrandTokenProvider.tsx`
- `src/console/src/features/workflows/WorkflowPageRoute.tsx`
- `src/console/src/runtime/WorkflowPageRenderer.tsx`
- `src/console/src/runtime/control-library/fields/*.tsx`
- `src/console/src/runtime/control-library/fields/registry.tsx`
- `src/console/src/runtime/control-library/widgets/*.tsx`
- `src/console/src/runtime/control-library/widgets/index.tsx`
- `src/console/src/runtime/style-lab/*.tsx`
- `src/platform/ui-contracts/*.ts`
- `src/platform/ui-runtime/*.ts`

## Atomic Runtime And Branding

- [ ] `BrandTokenProvider`
- [ ] `WorkflowPageRenderer`
- [ ] `WidgetShell`
- [ ] `FieldShell`
- [ ] `FieldControlFactory`
- [ ] `WidgetFactoryRegistry`
- [ ] `StyleLabPreviewScope`
- [ ] `StatusBadge`
- [ ] `EmptyState`

## Atomic Field Components

### Scalar Inputs

- [ ] `TextInputControl`
- [ ] `TextareaControl`
- [ ] `NumberInputControl`
- [ ] `DateInputControl`
- [ ] `TimeInputControl`
- [ ] `ReadOnlyValueControl`

### Choice Inputs

- [ ] `SelectControl`
- [ ] `ComboboxControl`
- [ ] `MultiSelectControl`
- [ ] `RadioGroupControl`
- [ ] `CheckboxControl`
- [ ] `ToggleControl`
- [ ] `SliderControl`

### Structured Inputs

- [ ] `RepeaterControl`
- [ ] `TableInputControl`
- [ ] `MatrixInputControl`
- [ ] `EntityPickerControl`
- [ ] `TreePickerControl`
- [ ] `FileUploadControl`
- [ ] `SignatureInputControl`
- [ ] `SensitiveRevealControl`

## Atomic Display Components

### Text And Content

- [ ] `TextContentWidget`
- [ ] `MarkdownContentWidget`
- [ ] `HtmlContentWidget`
- [ ] `CalloutWidget`
- [ ] `LinkListWidget`
- [ ] `AccordionWidget`

### Data And Status

- [ ] `MetricTileWidget`
- [ ] `ProgressWidget`
- [ ] `LabelValueListWidget`
- [ ] `RecordSummaryWidget`
- [ ] `ChecklistWidget`
- [ ] `QueueListWidget`
- [ ] `DataTableWidget`

### Review And Audit

- [ ] `DiffViewerWidget`
- [ ] `TimelineWidget`
- [ ] `ActionBarWidget`
- [ ] `ReasonCaptureWidget`

### Visualization And Media

- [ ] `ChartWidget`
- [ ] `GraphWidget`
- [ ] `BoardWidget`
- [ ] `MediaViewerWidget`
- [ ] `DocumentPreviewWidget`

## Atomic Layout And Surface Components

- [ ] `SectionLayout`
- [ ] `StackLayout`
- [ ] `GridLayout`
- [ ] `TabsWidget`
- [ ] `StepperWidget`
- [ ] `ModalDrawerWidget`
- [ ] `ToastCenterWidget`

## Canonical Generated Field Type IDs

These are the only field type IDs that should be first-class for generated UI.
Older IDs can remain as compatibility aliases that normalize into these types.

- [ ] `text`
- [ ] `textarea`
- [ ] `number`
- [ ] `date`
- [ ] `time`
- [ ] `select`
- [ ] `combobox`
- [ ] `multi_select`
- [ ] `radio_group`
- [ ] `checkbox`
- [ ] `toggle`
- [ ] `slider`
- [ ] `repeater`
- [ ] `table`
- [ ] `matrix`
- [ ] `entity_picker`
- [ ] `tree_picker`
- [ ] `file_upload`
- [ ] `signature`
- [ ] `sensitive_reveal`
- [ ] `readonly`

## Canonical Generated Widget Type IDs

These are the widget type IDs that should stay unique and reusable. Domain,
workflow, and visualization aliases should normalize into these IDs with props.

- [ ] `layout.section`
- [ ] `layout.stack`
- [ ] `layout.grid`
- [ ] `content.text`
- [ ] `content.markdown`
- [ ] `content.html`
- [ ] `content.callout`
- [ ] `content.linkList`
- [ ] `content.accordion`
- [ ] `data.metricTile`
- [ ] `data.progress`
- [ ] `data.labelValueList`
- [ ] `data.recordSummary`
- [ ] `data.checklist`
- [ ] `data.queueList`
- [ ] `data.table`
- [ ] `review.diff`
- [ ] `review.timeline`
- [ ] `workflow.actionBar`
- [ ] `workflow.reasonCapture`
- [ ] `viz.chart`
- [ ] `viz.graph`
- [ ] `ui.board`
- [ ] `media.viewer`
- [ ] `document.preview`
- [ ] `ui.tabs`
- [ ] `ui.stepper`
- [ ] `ui.modalDrawer`
- [ ] `ui.toastCenter`

## Consolidation Map

### App And Renderer Items

- `App`, `WorkflowPageRoute`, `StyleLabRail`, and `StyleLabWorkbench` are app/admin
  surfaces, not generated workflow primitives.
- Inline `WorkflowPageRenderer` components should be deleted or converted into
  registry-backed atomic components.
- `WidgetBody` should become a thin registry dispatcher, not a component family.

### Field Type Aliases

- `email`, `phone`, and `url` map to `text` with `inputType`, `inputMode`,
  `pattern`, and transform props.
- `money` and `percent` map to `number` with `format`, `prefix`, `suffix`,
  `currency`, `min`, `max`, and `step` props.
- `date_range` maps to two `date` controls composed by the workflow layout.
- `dropdown`, `grouped_select`, `dropdown_group`, `filterable_select`, and
  `filterable_dropdown` map to `select` or `combobox` with `searchEnabled`,
  `groupPath`, and data-source props.
- `toggle_group`, `slider_group`, and `metric_slider_group` map to repeated
  `toggle` or `slider` controls.
- `repeating_list`, `table_editor`, `cluster_board`, and `drag_drop_clusters`
  map to `repeater`, `table`, or `ui.board`.
- `file`, `evidence_upload`, and policy evidence upload variants map to
  `file_upload` plus validation and accepted-file props.
- `e_signature` and `signature_capture` map to `signature`.

### HCM And Governance Controls

- `employee_picker`, `manager_picker`, `org_unit_picker`, `department_picker`,
  `legal_entity_picker`, `cost_center_picker`, `location_picker`,
  `job_profile_picker`, `position_picker`, `pay_band_picker`,
  `payroll_cutoff_picker`, and `permission_entity_picker` map to
  `entity_picker` with `entityType` and binding metadata.
- `manager_tree_picker` and `org_tree_picker` map to `tree_picker`.
- `compensation_editor`, `job_change_editor`, `manager_org_change_editor`,
  `worker_assignment_editor`, `role_binding_editor`,
  `effective_dated_fact_editor`, `effective_dated_change`,
  `before_after_field_editor`, `compensation_package_editor`,
  `schedule_time_control`, and `international_contact` are compound workflow
  recipes composed from scalar inputs, selectors, tables, diffs, and layouts.
- `approval_chain_editor`, `policy_evidence_checklist`, and `ai_review_panel`
  are compound workflow recipes composed from checklist, timeline, action, and
  content primitives.
- `bulk_grid_editor`, `conflict_resolver`, `integration_repair_control`, and
  `transaction_simulation_viewer` are compound recipes composed from table,
  diff, checklist, timeline, and action primitives.

### Widget Aliases

- `queue.requestList` maps to `data.queueList`.
- `employee.summary` maps to `data.recordSummary`.
- `change.diff` maps to `review.diff`.
- `approval.decisionPanel` maps to `workflow.actionBar` plus
  `workflow.reasonCapture`.
- `simulation.resultPanel` maps to `data.checklist` plus `review.timeline`.
- `audit.timeline` maps to `review.timeline`.
- `content.faq` maps to `content.accordion`.
- `data.filterableTable` maps to `data.table` with filter props.
- `data.metricGraph`, `data.graphChart`, and all `viz.*` chart variants map to
  `viz.chart` with a `variant` prop.
- `data.nodeGraph` and `data.orgChart` map to `viz.graph` with `graphType`.
- `data.clusterBoard`, `ui.kanbanBoard`, and drag/drop board variants map to
  `ui.board`.
- `media.image`, `media.audio`, `media.video`, and `media.pdf` map to
  `media.viewer` with `mediaType`.
- Document packet, generated PDF, OCR, redaction, attachment, and signature
  packet widgets map to `document.preview` plus primitive subcomponents.

### Catalog Families Removed From Atomic Inventory

The following catalog families are useful as page recipes, examples, or
compatibility IDs, but they are not atomic components:

- `ai.*`
- `hcm.*`
- `workflow.*`
- `document.*`
- `collaboration.*`
- `integration.*`
- `recruiting.*`
- `onboarding.*`
- `offboarding.*`
- `performance.*`
- `talent.*`
- `workforce.*`
- `scheduling.*`
- `leave.*`
- `benefits.*`
- `payroll.*`
- `employeeRelations.*`
- `compliance.*`
- `experience.*`

### Showcase Helpers Removed From Atomic Inventory

These are implementation details or demos and should not be first-class
generated UI components:

- `ShowcaseMetrics`
- `ShowcaseItems`
- `ShowcaseMessages`
- `ShowcaseDiff`
- `ShowcasePointBars`
- `ShowcaseRows`
- `GenericShowcaseWidget`
- `LineChartPreview`
- `BulletChartPreview`
- `StackedBarChartPreview`
- `ProportionalChartPreview`
- `ScatterChartPreview`
- `RangeChartPreview`
- `TimelineChartPreview`
- `VisualizationShowcaseWidget`
- `TimeControls`
- `TimeRing`
- `TimeShowcaseWidget`
- `UiBlockShowcaseWidget`

## Completion Criteria

- [ ] Atomic registries expose only canonical first-class type IDs.
- [ ] Compatibility aliases normalize into canonical IDs before rendering.
- [ ] Removed compound/domain components are represented as recipes or examples.
- [ ] Every retained atomic component accepts standardized style and brand props.
- [ ] Every retained atomic component has preview data for generated UI demos.
- [ ] Playwright covers desktop and mobile rendering of every retained category.
- [ ] Playwright screenshots verify runtime rebranding does not break layout.
- [ ] `WorkflowPageRenderer.tsx` delegates component rendering to registries.
