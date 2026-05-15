import {
  controlClassNames,
  controlStyleProps,
  displayValue,
  eventInputValue,
  objectFieldValue,
  optionDescription,
  optionLabel,
  optionStatus,
  optionValue,
  recordValue,
  updateObjectField,
} from "./utils";
import type { FieldControlProps } from "./types";

export const ApprovalChainEditorControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-approval-chain")}
      style={controlStyleProps(props)}
    >
      {(props.items ?? []).map((item, index) => {
        const itemValue = optionValue(item);
        const status = objectFieldValue(
          currentValue,
          itemValue,
          displayValue(item.status, "Pending"),
        );

        return (
          <div className="field-library-approval-step" key={itemValue}>
            <span className="field-library-step-index">{index + 1}</span>
            <span>
              {optionLabel(item)}
              <small>
                {displayValue(item.approver, "Unassigned")} -{" "}
                {displayValue(item.rule, "")}
              </small>
            </span>
            <select
              onChange={(event) =>
                props.onChange(
                  updateObjectField(currentValue, itemValue, eventInputValue(event)),
                )
              }
              value={status}
            >
              {["Pending", "Approved", "Rejected", "Skipped"].map((statusOption) => (
                <option key={statusOption} value={statusOption}>
                  {statusOption}
                </option>
              ))}
            </select>
          </div>
        );
      })}
    </div>
  );
};

export const PolicyEvidenceChecklistControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <fieldset
      className={controlClassNames(props, "field-library-policy-evidence")}
      style={controlStyleProps(props)}
    >
      {(props.items ?? []).map((item) => {
        const itemValue = optionValue(item);
        const isChecked =
          currentValue[itemValue] === true || item.defaultValue === true;

        return (
          <label
            className={`field-library-check-row status-${optionStatus(item)}`}
            key={itemValue}
          >
            <input
              checked={isChecked}
              onChange={(event) =>
                props.onChange(
                  updateObjectField(
                    currentValue,
                    itemValue,
                    event.currentTarget.checked,
                  ),
                )
              }
              type="checkbox"
            />
            <span>
              {optionLabel(item)}
              <small>{optionDescription(item)}</small>
            </span>
          </label>
        );
      })}
    </fieldset>
  );
};

export const AiReviewPanelControl = (props: FieldControlProps) => (
  <div
    className={controlClassNames(props, "field-library-ai-review")}
    style={controlStyleProps(props)}
  >
    {(props.items ?? []).map((item) => (
      <article
        className={`field-library-ai-review-item status-${optionStatus(item)}`}
        key={optionValue(item)}
      >
        <strong>{optionLabel(item)}</strong>
        <p>{optionDescription(item)}</p>
        <button
          onClick={() =>
            props.onChange(
              updateObjectField(
                recordValue(props.value),
                optionValue(item),
                "accepted",
              ),
            )
          }
          type="button"
        >
          Accept
        </button>
      </article>
    ))}
  </div>
);

export const SensitiveFieldRevealControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);
  const isRevealed = currentValue.revealed === true;

  return (
    <div
      className={controlClassNames(props, "field-library-sensitive-reveal")}
      style={controlStyleProps(props)}
    >
      <div className="field-library-sensitive-value">
        {isRevealed ? props.config.placeholder : "Restricted value hidden"}
      </div>
      <label className="field-library-stack-row">
        <span>Reason for access</span>
        <input
          className="field-library-input"
          onChange={(event) =>
            props.onChange(
              updateObjectField(currentValue, "reason", eventInputValue(event)),
            )
          }
          value={objectFieldValue(currentValue, "reason")}
        />
      </label>
      <button
        onClick={() =>
          props.onChange(updateObjectField(currentValue, "revealed", !isRevealed))
        }
        type="button"
      >
        {isRevealed ? "Hide" : "Reveal"}
      </button>
    </div>
  );
};

export const GovernanceControl = (props: FieldControlProps) => {
  if (props.config.type === "approval_chain_editor") {
    return <ApprovalChainEditorControl {...props} />;
  }

  if (props.config.type === "policy_evidence_checklist") {
    return <PolicyEvidenceChecklistControl {...props} />;
  }

  if (props.config.type === "sensitive_field_reveal") {
    return <SensitiveFieldRevealControl {...props} />;
  }

  return <AiReviewPanelControl {...props} />;
};
