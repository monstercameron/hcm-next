import {
  controlClassNames,
  controlStyleProps,
  eventInputValue,
  numberValue,
  optionDescription,
  optionLabel,
  optionValue,
  rawNumber,
  recordValue,
  scalarInputValue,
  updateObjectField,
} from "./utils";
import type { FieldControlProps } from "./types";

export const ToggleControl = (props: FieldControlProps) => (
  <label
    className={controlClassNames(props, "field-library-toggle")}
    style={controlStyleProps(props)}
  >
    <input
      checked={props.value === true}
      onChange={(event) => props.onChange(event.currentTarget.checked)}
      type="checkbox"
    />
    <span aria-hidden="true" className="field-library-toggle-track">
      <span className="field-library-toggle-thumb" />
    </span>
    <span>{props.config.placeholder || props.config.label}</span>
  </label>
);

export const ToggleGroupControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <fieldset
      className={controlClassNames(props, "field-library-toggle-group")}
      style={controlStyleProps(props)}
    >
      {(props.options ?? []).map((option) => {
        const value = optionValue(option);
        const isChecked = currentValue[value] === true;

        return (
          <label className="field-library-toggle-row" key={value}>
            <input
              checked={isChecked}
              onChange={(event) =>
                props.onChange(
                  updateObjectField(props.value, value, event.currentTarget.checked),
                )
              }
              type="checkbox"
            />
            <span>
              {optionLabel(option)}
              <small>{optionDescription(option)}</small>
            </span>
          </label>
        );
      })}
    </fieldset>
  );
};

export const SliderControl = (props: FieldControlProps) => {
  const min = rawNumber(props.config, "min", 0);
  const max = rawNumber(props.config, "max", 100);
  const step = rawNumber(props.config, "step", 1);
  const suffix = scalarInputValue(props.config.raw.suffix) || "%";
  const currentValue = numberValue(props.value, min);

  return (
    <div
      className={controlClassNames(props, "field-library-slider")}
      style={controlStyleProps(props)}
    >
      <input
        aria-label={props.config.label}
        max={max}
        min={min}
        onChange={(event) => props.onChange(Number(eventInputValue(event)))}
        step={step}
        type="range"
        value={currentValue}
      />
      <output>
        {currentValue}
        {suffix}
      </output>
    </div>
  );
};

export const SliderGroupControl = (props: FieldControlProps) => {
  const min = rawNumber(props.config, "min", 0);
  const max = rawNumber(props.config, "max", 100);
  const step = rawNumber(props.config, "step", 1);
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-slider-group")}
      style={controlStyleProps(props)}
    >
      {(props.options ?? []).map((option) => {
        const value = optionValue(option);
        const numericValue = numberValue(currentValue[value], 0);

        return (
          <label className="field-library-slider-row" key={value}>
            <span>
              {optionLabel(option)}
              <small>{optionDescription(option)}</small>
            </span>
            <input
              max={max}
              min={min}
              onChange={(event) =>
                props.onChange(
                  updateObjectField(props.value, value, Number(eventInputValue(event))),
                )
              }
              step={step}
              type="range"
              value={numericValue}
            />
            <output>{numericValue}%</output>
          </label>
        );
      })}
    </div>
  );
};

export const ToggleSliderControl = (props: FieldControlProps) => {
  if (props.config.type === "toggle") {
    return <ToggleControl {...props} />;
  }

  if (props.config.type === "toggle_group") {
    return <ToggleGroupControl {...props} />;
  }

  if (
    props.config.type === "slider_group" ||
    props.config.type === "metric_slider_group"
  ) {
    return <SliderGroupControl {...props} />;
  }

  return <SliderControl {...props} />;
};
