# Design review notes

Review date: September 5, 2026. All boards were visually inspected. Generated
boards are concept references, not pixel-perfect specifications or verified
production states. The written visual and interaction contracts are authoritative.

## Reference priority

1. [Customer branding and layout](customer-branding-and-layout.md): customer-controlled identity and organization.
2. [Visual language](visual-language.md), boards 18–21: visual craft and theme/layout adaptability.
3. [Interaction corpus](interaction-design-corpus.md): exact behavior, access and scenario facts.
4. Boards 01–17: floorplan, state and feature explorations.

## Corrected before selection

Targeted image edits removed medical leave detail from manager and composition
examples; reconciled the worker-profile manager; corrected the draft requester;
removed false autosave/offline promises; clarified payroll as an unissued draft
preview; and removed authenticated content from public sign-in.

## Remaining raster limitations

| Boards | Do not copy literally into implementation                                                                                                                                                                                                                                                                |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01,18  | Illustrative font sizes, faint control borders, generated focus hues and mottled fills differ from the exact visual tokens. Use the written flat fills, control boundary and two-layer focus. The specimen roster is not a canonical fixture.                                                            |
| 02     | Authorized navigation must be filtered; the composite shell is not proof every manager has Admin access.                                                                                                                                                                                                 |
| 03–07  | These are different role/stage snapshots. Some raster dates, requester/approver labels, revision wording and active nav still differ. Use Alex as requester, Maya as HR reviewer, exact revision3, and a later tracking snapshot; never copy an approval chronology from the bitmap.                     |
| 08     | Read-only last-loaded data remains subject to session and data-retention rules; an expired session cannot use the neighboring connected-state examples to retain protected information.                                                                                                                  |
| 09     | The raster mixes employee/reviewer personas and a provisional PTO balance. Use the role contracts. Use Approve / Return for changes / Reject; only a true modal contains focus, not a full-page review.                                                                                                  |
| 10     | The editor retains duplicate scope labels and tiny fixture inconsistencies. One resolved scope is authoritative. The corrected synthetic-preview banner does not itself grant access.                                                                                                                    |
| 11     | Use Noah's canonical payroll role and manager Maya. Grid date range should be Sep13–19; the gap is Tue Sep15. An unsubmitted PTO request is Draft, not Pending.                                                                                                                                          |
| 12     | Use authorized recruiter context, Maya as hiring manager, safe example.test contact data, and consistent stage filtering; retain the correct Eastern/Central interview conversion.                                                                                                                       |
| 13     | The corrected board identifies the statement as a draft preview, not payment evidence. Generated exception-row job subtitles are not worker facts.                                                                                                                                                       |
| 14     | Draft workflow definitions and synthetic simulations must not appear as live production execution history. A payroll employee identity does not imply workflow-admin access.                                                                                                                             |
| 15     | Status-count report fields must be Transaction status / Transaction count, grouped by status. Do not derive a population-wide recipient list, personal risk judgment, small-cohort disclosure or prior-period trend from the raster.                                                                     |
| 16     | Invitation role is HR Operations Lead, candidate contact is example.test. OP204 differences are annual base pay and effective date. Panels require separate authorized scopes; use review-before-send for an AI draft.                                                                                   |
| 17     | Verify pending step-up and saved-draft behavior against server evidence; illustrative timestamps are not a persistence guarantee.                                                                                                                                                                        |
| 19     | This board prioritizes visual hierarchy and contains invented dates, names and pay figures. It is not the Promotion scenario fixture. Normalize icons, status color and type to the visual-language spec.                                                                                                |
| 20,21  | Demonstrate extensibility, not a final locked-region map or arbitrary CSS support. Only policy-required regions are locked; customer content/optional layout remains editable. The studio's generated contrast-success copy is an example validator state, not evidence that every component meets WCAG. |

These limitations do not block reviewing colors, shapes, layouts and feature
placement. They do block treating the raster as a complete build specification.
No implementation TODOs are marked complete by this design delivery.

## Validation record

Color-token pairs were calculated directly from sRGB values and recorded in
the visual specification. Local gallery checks cover its navigation/state logic,
asset existence and document links. This is not an accessibility audit or a
usability study. Representative user testing and a semantic browser prototype
remain required before production sign-off.
