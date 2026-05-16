import { useMemo, useState } from "react";
import {
  controlClassNames,
  controlStyleProps,
  eventInputValue,
  groupedOptions,
  nextCheckedStringArray,
  optionDescription,
  optionGroup,
  optionLabel,
  optionMatchesQuery,
  optionValue,
  rawBoolean,
  scalarInputValue,
  selectedStringSet,
} from "./utils";
import type { FieldControlProps } from "./types";

const OptionBody = ({ option }: { option: Readonly<Record<string, unknown>> }) => {
  const description = optionDescription(option);

  return (
    <>
      <span>{optionLabel(option)}</span>
      {description.length > 0 ? <small>{description}</small> : null}
    </>
  );
};

export const SelectControl = (props: FieldControlProps) => {
  const groupedRecords = groupedOptions(props.options ?? []);
  const shouldGroupOptions =
    rawBoolean(props.config, "grouped") || groupedRecords.length > 1;

  return (
    <select
      aria-label={props.config.label}
      className={controlClassNames(props, "field-library-select")}
      onChange={(event) => props.onChange(eventInputValue(event))}
      style={controlStyleProps(props)}
      value={scalarInputValue(props.value)}
    >
      <option value="">{props.config.placeholder}</option>
      {shouldGroupOptions
        ? groupedRecords.map((group) => (
            <optgroup key={group.group} label={group.group}>
              {group.options.map((option) => (
                <option key={optionValue(option)} value={optionValue(option)}>
                  {optionLabel(option)}
                </option>
              ))}
            </optgroup>
          ))
        : (props.options ?? []).map((option) => (
            <option key={optionValue(option)} value={optionValue(option)}>
              {optionLabel(option)}
            </option>
          ))}
    </select>
  );
};

export const ComboboxControl = (props: FieldControlProps) => {
  const [query, setQuery] = useState("");
  const filteredOptions = useMemo(
    () => (props.options ?? []).filter((option) => optionMatchesQuery(option, query)),
    [props.options, query],
  );

  return (
    <div
      className={controlClassNames(props, "field-library-filterable-select")}
      style={controlStyleProps(props)}
    >
      <input
        aria-label={`${props.config.label} filter`}
        className="field-library-input"
        onChange={(event) => setQuery(eventInputValue(event))}
        placeholder="Filter options"
        value={query}
      />
      <div className="field-library-option-list" role="listbox">
        {filteredOptions.map((option) => {
          const value = optionValue(option);
          const isSelected = scalarInputValue(props.value) === value;

          return (
            <button
              aria-selected={isSelected}
              className="field-library-option-card"
              key={value}
              onClick={() => props.onChange(value)}
              type="button"
            >
              <OptionBody option={option} />
              <small>{optionGroup(option)}</small>
            </button>
          );
        })}
      </div>
    </div>
  );
};

export const FilterableSelectControl = ComboboxControl;

export const MultiSelectControl = (props: FieldControlProps) => {
  const selectedValues = selectedStringSet(props.value);

  return (
    <fieldset
      className={controlClassNames(props, "field-library-multi-select")}
      style={controlStyleProps(props)}
    >
      {(props.options ?? []).map((option) => {
        const value = optionValue(option);

        return (
          <label className="field-library-check-row" key={value}>
            <input
              checked={selectedValues.has(value)}
              onChange={(event) =>
                props.onChange(
                  nextCheckedStringArray(
                    props.value,
                    value,
                    event.currentTarget.checked,
                  ),
                )
              }
              type="checkbox"
            />
            <OptionBody option={option} />
          </label>
        );
      })}
    </fieldset>
  );
};

export const RadioGroupControl = (props: FieldControlProps) => (
  <fieldset
    className={controlClassNames(props, "field-library-radio-group")}
    style={controlStyleProps(props)}
  >
    {(props.options ?? []).map((option) => {
      const value = optionValue(option);

      return (
        <label className="field-library-choice-card" key={value}>
          <input
            checked={scalarInputValue(props.value) === value}
            name={props.config.id}
            onChange={() => props.onChange(value)}
            type="radio"
          />
          <OptionBody option={option} />
        </label>
      );
    })}
  </fieldset>
);

export const CheckboxControl = (props: FieldControlProps) => (
  <label
    className={controlClassNames(props, "field-library-checkbox")}
    style={controlStyleProps(props)}
  >
    <input
      checked={props.value === true}
      onChange={(event) => props.onChange(event.currentTarget.checked)}
      type="checkbox"
    />
    <span>{props.config.placeholder || props.config.label}</span>
  </label>
);

export const ChoiceControl = (props: FieldControlProps) => {
  if (props.config.type === "combobox") {
    return <ComboboxControl {...props} />;
  }

  if (props.config.type === "multi_select") {
    return <MultiSelectControl {...props} />;
  }

  if (props.config.type === "radio_group") {
    return <RadioGroupControl {...props} />;
  }

  if (props.config.type === "checkbox") {
    return <CheckboxControl {...props} />;
  }

  return <SelectControl {...props} />;
};
