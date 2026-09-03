import { useMemo } from "react";
import {
  accessibleMatrixStatus,
  summarizeAccessibilityMatrix,
  validateAccessibilityMatrixCase,
  type AccessibilityMatrixCase,
} from "./a11y-compatibility.js";

export function AccessibilityMatrixPanel({
  cases,
}: {
  cases: readonly AccessibilityMatrixCase[];
}): JSX.Element {
  const summary = useMemo(() => summarizeAccessibilityMatrix(cases), [cases]);

  return (
    <section aria-labelledby="accessibility-matrix-title">
      <header>
        <p className="eyebrow">A11Y-001 · Gate A</p>
        <h2 id="accessibility-matrix-title">Compatibility matrix</h2>
        <p>
          Critical flows are evaluated by assistive technology, browser, locale, input
          mode and zoom. Unsupported combinations retain a safe continuity path and
          never weaken identity, privacy or deadline semantics.
        </p>
      </header>
      <dl aria-label="Compatibility summary">
        <div>
          <dt>Total cases</dt>
          <dd>{summary.total}</dd>
        </div>
        <div>
          <dt>Supported</dt>
          <dd>{summary.supported}</dd>
        </div>
        <div>
          <dt>Continuity</dt>
          <dd>{summary.continuity}</dd>
        </div>
        <div>
          <dt>Critical failures</dt>
          <dd>{summary.criticalFailures}</dd>
        </div>
      </dl>
      <div role="table" aria-label="Assistive technology compatibility cases">
        {cases.map((item) => {
          const errors = validateAccessibilityMatrixCase(item);
          return (
            <article
              key={item.id}
              role="row"
              aria-label={`${item.flow}: ${accessibleMatrixStatus(item)}`}
            >
              <strong>{item.flow}</strong>
              <span>
                {item.assistiveTechnology} · {item.browser} · {item.locale} ·{" "}
                {item.zoomPercent}%
              </span>
              <span>{item.inputMode}</span>
              <span>{accessibleMatrixStatus(item)}</span>
              {errors.length > 0 ? (
                <span role="alert">Incomplete evidence: {errors.join(", ")}</span>
              ) : null}
            </article>
          );
        })}
      </div>
    </section>
  );
}
