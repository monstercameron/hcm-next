# Product-styled workspace slice

Open [the demo](../hcm-brandable-workspace.html) through the local design server.
This is a standalone product surface, not a conversation iframe.

- `workspace.css`: shared tokens, component styles, control states and responsive layouts.
- `workspace.js`: local route, overlay, loading, notification and fictional workflow state.
- `workspace.test.cjs`: static contract checks plus a headless browser interaction test;
  run with Node from the repository.

Customer controls support company identity, an arbitrary accent, shape, density,
navigation grouping, Home section order and light/deep-green navigation. Semantic
status colors are independent of brand colors. Primary, hover and selected text
colors are derived with contrast checks rather than replacing green blindly.

This is a bounded UI slice with Home, My Work, People, Organization, Insights,
Admin, Help and Settings views. Buttons demonstrate delayed route loading, pending
actions, notifications, request decisions, lightweight forms, report receipts and
dialog focus behavior. The customizer includes fast, typical and slow network
simulation.
It has no authentication, HR writes, persistence, payroll execution or report
engine. Lucide is the existing pinned icon dependency; labels and interactions
remain usable if it is unavailable.

The authenticated screen boards and the corrected interaction boards in
`../mockups/interaction-v1/` are the visual references for this slice. Where an
early image conflicts with a later safety or domain correction, the later board
wins. This slice's typography, component geometry, focus states and responsive CSS
are the current implemented design example. It is not a production-readiness or
accessibility-conformance certification.
