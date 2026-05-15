import {
  controlClassNames,
  controlStyleProps,
  displayValue,
  eventInputValue,
  optionDescription,
  optionLabel,
  optionStatus,
  optionValue,
  recordValue,
  recordsValue,
  updateObjectField,
} from "./utils";
import { TableEditorControl } from "./structured-controls";
import type { FieldControlProps } from "./types";

export const ConflictResolverControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-conflict-resolver")}
      style={controlStyleProps(props)}
    >
      {(props.items ?? []).map((item) => {
        const itemValue = optionValue(item);
        const selectedResolution =
          displayValue(currentValue[itemValue], "") ||
          displayValue(item.defaultResolution, "review");

        return (
          <article className="field-library-repair-card" key={itemValue}>
            <strong>{optionLabel(item)}</strong>
            <p>{optionDescription(item)}</p>
            <select
              onChange={(event) =>
                props.onChange(
                  updateObjectField(currentValue, itemValue, eventInputValue(event)),
                )
              }
              value={selectedResolution}
            >
              {["review", "use_current", "use_proposed", "manual_repair"].map(
                (resolution) => (
                  <option key={resolution} value={resolution}>
                    {resolution}
                  </option>
                ),
              )}
            </select>
          </article>
        );
      })}
    </div>
  );
};

export const IntegrationRepairControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-integration-repair")}
      style={controlStyleProps(props)}
    >
      {(props.items ?? []).map((item) => {
        const itemValue = optionValue(item);
        const selectedAction =
          displayValue(currentValue[itemValue], "") ||
          displayValue(item.defaultAction, "retry");

        return (
          <article
            className={`field-library-repair-card status-${optionStatus(item)}`}
            key={itemValue}
          >
            <strong>{optionLabel(item)}</strong>
            <p>{optionDescription(item)}</p>
            <select
              onChange={(event) =>
                props.onChange(
                  updateObjectField(currentValue, itemValue, eventInputValue(event)),
                )
              }
              value={selectedAction}
            >
              {["retry", "skip", "rollback", "manual_repair"].map((action) => (
                <option key={action} value={action}>
                  {action}
                </option>
              ))}
            </select>
          </article>
        );
      })}
    </div>
  );
};

export const TransactionSimulationViewerControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-transaction-simulation")}
      style={controlStyleProps(props)}
    >
      {(props.items ?? []).map((item, index) => {
        const itemValue = optionValue(item);

        return (
          <article
            className={`field-library-simulation-step status-${optionStatus(item)}`}
            key={itemValue}
          >
            <span className="field-library-step-index">{index + 1}</span>
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>{optionDescription(item)}</small>
            </span>
            <label>
              <input
                checked={currentValue[itemValue] === true}
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
              Confirm
            </label>
          </article>
        );
      })}
    </div>
  );
};

export const BulkGridEditorControl = (props: FieldControlProps) => (
  <TableEditorControl {...props} value={recordsValue(props.value)} />
);

export const SimulationRepairControl = (props: FieldControlProps) => {
  if (props.config.type === "conflict_resolver") {
    return <ConflictResolverControl {...props} />;
  }

  if (props.config.type === "integration_repair_control") {
    return <IntegrationRepairControl {...props} />;
  }

  if (props.config.type === "transaction_simulation_viewer") {
    return <TransactionSimulationViewerControl {...props} />;
  }

  return <BulkGridEditorControl {...props} />;
};
