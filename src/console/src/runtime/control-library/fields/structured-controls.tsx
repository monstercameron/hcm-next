import { useMemo } from "react";
import {
  controlClassNames,
  controlStyleProps,
  displayValue,
  eventInputValue,
  objectFieldValue,
  optionDescription,
  optionLabel,
  optionValue,
  recordValue,
  recordsValue,
  scalarInputValue,
  stringArrayValue,
  updateIndexedRecord,
  updateObjectField,
} from "./utils";
import type { FieldControlProps } from "./types";

export const RepeatingListControl = (props: FieldControlProps) => {
  const values = stringArrayValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-repeating-list")}
      style={controlStyleProps(props)}
    >
      {values.map((item, index) => (
        <label className="field-library-inline-row" key={`${item}-${index}`}>
          <span>{index + 1}</span>
          <input
            aria-label={`${props.config.label} item ${index + 1}`}
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                values.map((currentItem, itemIndex) =>
                  itemIndex === index ? eventInputValue(event) : currentItem,
                ),
              )
            }
            value={item}
          />
          <button
            onClick={() =>
              props.onChange(values.filter((_, itemIndex) => itemIndex !== index))
            }
            type="button"
          >
            Remove
          </button>
        </label>
      ))}
      <button onClick={() => props.onChange([...values, ""])} type="button">
        Add item
      </button>
    </div>
  );
};

export const TableEditorControl = (props: FieldControlProps) => {
  const fallbackColumns = [
    { label: "Field", value: "field" },
    { label: "Value", value: "value" },
  ];
  const columns =
    props.columns?.length === 0 ? fallbackColumns : (props.columns ?? fallbackColumns);
  const rows = recordsValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-table-editor")}
      role="table"
      style={controlStyleProps(props)}
    >
      <div className="field-library-table-row" role="row">
        {columns.map((column) => (
          <strong key={optionValue(column)} role="columnheader">
            {optionLabel(column)}
          </strong>
        ))}
      </div>
      {rows.map((row, rowIndex) => (
        <div className="field-library-table-row" key={rowIndex} role="row">
          {columns.map((column) => {
            const key = optionValue(column);

            return (
              <input
                aria-label={`${props.config.label} ${optionLabel(column)} ${rowIndex + 1}`}
                className="field-library-input"
                key={key}
                onChange={(event) =>
                  props.onChange(
                    updateIndexedRecord(rows, rowIndex, key, eventInputValue(event)),
                  )
                }
                value={displayValue(row[key], "")}
              />
            );
          })}
        </div>
      ))}
      <button
        onClick={() =>
          props.onChange([
            ...rows,
            Object.fromEntries(columns.map((column) => [optionValue(column), ""])),
          ])
        }
        type="button"
      >
        Add row
      </button>
    </div>
  );
};

export const MatrixControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-matrix")}
      role="table"
      style={controlStyleProps(props)}
    >
      {(props.rows ?? []).map((row) => {
        const rowValue = optionValue(row);

        return (
          <fieldset className="field-library-matrix-row" key={rowValue}>
            <legend>{optionLabel(row)}</legend>
            {(props.columns ?? []).map((column) => {
              const columnValue = optionValue(column);

              return (
                <label key={columnValue}>
                  <input
                    checked={currentValue[rowValue] === columnValue}
                    name={`${props.config.id}-${rowValue}`}
                    onChange={() =>
                      props.onChange(
                        updateObjectField(props.value, rowValue, columnValue),
                      )
                    }
                    type="radio"
                  />
                  {optionLabel(column)}
                </label>
              );
            })}
          </fieldset>
        );
      })}
    </div>
  );
};

export const ClusterBoardControl = (props: FieldControlProps) => {
  const items = props.items?.length === 0 ? (props.options ?? []) : (props.items ?? []);
  const configuredGroups = props.groups?.length === 0 ? [] : (props.groups ?? []);
  const groups = useMemo(
    () =>
      configuredGroups.length > 0
        ? configuredGroups
        : items.map((item) => ({ label: optionLabel(item), value: optionValue(item) })),
    [configuredGroups, items],
  );

  return (
    <div
      className={controlClassNames(props, "field-library-cluster-board")}
      style={controlStyleProps(props)}
    >
      {items.map((item) => {
        const itemValue = optionValue(item);
        const selectedGroup = objectFieldValue(props.value, itemValue);

        return (
          <label className="field-library-cluster-card" key={itemValue}>
            <span>
              {optionLabel(item)}
              <small>{optionDescription(item)}</small>
            </span>
            <select
              onChange={(event) =>
                props.onChange(
                  updateObjectField(props.value, itemValue, eventInputValue(event)),
                )
              }
              value={selectedGroup}
            >
              <option value="">Unassigned</option>
              {groups.map((group) => (
                <option key={optionValue(group)} value={optionValue(group)}>
                  {optionLabel(group)}
                </option>
              ))}
            </select>
          </label>
        );
      })}
    </div>
  );
};

export const StructuredControl = (props: FieldControlProps) => {
  if (props.config.type === "repeating_list") {
    return <RepeatingListControl {...props} />;
  }

  if (props.config.type === "matrix") {
    return <MatrixControl {...props} />;
  }

  if (
    props.config.type === "cluster_board" ||
    props.config.type === "drag_drop_clusters"
  ) {
    return <ClusterBoardControl {...props} />;
  }

  if (props.config.type === "bulk_grid_editor") {
    return <TableEditorControl {...props} value={recordsValue(props.value)} />;
  }

  return <TableEditorControl {...props} value={recordsValue(props.value)} />;
};

export const StaticPreviewControl = (props: FieldControlProps) => (
  <output
    className={controlClassNames(props, "field-library-static-preview")}
    style={controlStyleProps(props)}
  >
    {scalarInputValue(props.value) || props.config.placeholder}
  </output>
);
