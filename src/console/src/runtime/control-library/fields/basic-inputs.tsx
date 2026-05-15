import {
  controlClassNames,
  controlStyleProps,
  eventInputValue,
  inputConstraintAttributes,
  objectFieldValue,
  rawString,
  scalarInputValue,
  stringValue,
  textareaConstraintAttributes,
  updateObjectField,
} from "./utils";
import type { FieldControlProps } from "./types";

const textInputTypeByControlType: Readonly<Record<string, string>> = {
  email: "email",
  phone: "tel",
  url: "url",
  date: "date",
  time: "time",
  number: "number",
  money: "number",
  percent: "number",
  text: "text",
};

const nextScalarValue = (props: FieldControlProps, nextValue: string): unknown => {
  if (
    props.config.type === "number" ||
    props.config.type === "money" ||
    props.config.type === "percent"
  ) {
    return nextValue.length === 0 ? "" : Number(nextValue);
  }

  return nextValue;
};

export const TextInputControl = (props: FieldControlProps) => {
  const inputType = textInputTypeByControlType[props.config.type] ?? "text";
  const prefix = rawString(props.config, "prefix");
  const suffix = rawString(props.config, "suffix");

  return (
    <div
      className={controlClassNames(props, "field-library-text-input")}
      style={controlStyleProps(props)}
    >
      {prefix.length > 0 ? <span className="field-library-affix">{prefix}</span> : null}
      <input
        {...inputConstraintAttributes(props.config)}
        aria-label={props.config.label}
        className="field-library-input"
        onChange={(event) =>
          props.onChange(nextScalarValue(props, eventInputValue(event)))
        }
        placeholder={props.config.placeholder}
        type={inputType}
        value={scalarInputValue(props.value)}
      />
      {suffix.length > 0 ? <span className="field-library-affix">{suffix}</span> : null}
    </div>
  );
};

export const TextareaControl = (props: FieldControlProps) => (
  <textarea
    {...textareaConstraintAttributes(props.config)}
    aria-label={props.config.label}
    className={controlClassNames(props, "field-library-textarea")}
    onChange={(event) => props.onChange(eventInputValue(event))}
    placeholder={props.config.placeholder}
    style={controlStyleProps(props)}
    value={scalarInputValue(props.value)}
  />
);

export const DateRangeControl = (props: FieldControlProps) => {
  const startDate = objectFieldValue(props.value, "start");
  const endDate = objectFieldValue(props.value, "end");

  return (
    <div
      className={controlClassNames(props, "field-library-date-range")}
      style={controlStyleProps(props)}
    >
      <input
        aria-label={`${props.config.label} start`}
        className="field-library-input"
        onChange={(event) =>
          props.onChange(
            updateObjectField(props.value, "start", eventInputValue(event)),
          )
        }
        type="date"
        value={startDate}
      />
      <span className="field-library-range-separator">to</span>
      <input
        aria-label={`${props.config.label} end`}
        className="field-library-input"
        onChange={(event) =>
          props.onChange(updateObjectField(props.value, "end", eventInputValue(event)))
        }
        type="date"
        value={endDate}
      />
    </div>
  );
};

export const BasicInputControl = (props: FieldControlProps) => {
  if (props.config.type === "textarea") {
    return <TextareaControl {...props} />;
  }

  if (props.config.type === "date_range") {
    return <DateRangeControl {...props} />;
  }

  return <TextInputControl {...props} />;
};

export const ReadOnlyValueControl = (props: FieldControlProps) => (
  <output
    aria-label={props.config.label}
    className={controlClassNames(props, "field-library-readonly")}
    style={controlStyleProps(props)}
  >
    {stringValue(props.value, props.config.placeholder)}
  </output>
);
