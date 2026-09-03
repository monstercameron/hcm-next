// Package wcag owns the UX-003 WCAG 2.2 AA pilot release gate.
//
// The gate deliberately consumes the UX-QUAL-001/FORM-004 fixture and
// renderers; it does not change the workspace contract or runtime. Its
// scorecard is release evidence: each automated criterion has a stable name,
// while zoom, reduced-motion, and assistive-auth checks are named manual
// scenarios recorded in definitions/ux/wcag/ux-003-evidence.yaml.
package wcag
