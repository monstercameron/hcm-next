import { useId } from "react";

import {
  controlClassNames,
  controlStyleProps,
  eventInputValue,
  objectFieldValue,
  recordValue,
  stringArrayValue,
  updateObjectField,
} from "./utils";
import type { FieldControlProps } from "./types";
import { fieldControlSourceType } from "../shared/field-types";

const fileNamesFromList = (fileList: FileList | null): readonly string[] =>
  fileList === null ? [] : Array.from(fileList).map((file) => file.name);

export const FileUploadControl = (props: FieldControlProps) => {
  const files = stringArrayValue(props.value);
  const inputId = useId();

  return (
    <div
      className={controlClassNames(props, "field-library-file-upload")}
      style={controlStyleProps(props)}
    >
      <input
        aria-label={props.config.label}
        className="field-library-file-input"
        id={inputId}
        multiple
        onChange={(event) =>
          props.onChange(fileNamesFromList(event.currentTarget.files))
        }
        type="file"
      />
      <label className="field-library-file-picker" htmlFor={inputId}>
        <span>Choose files</span>
        <small>
          {files.length > 0
            ? `${files.length} file${files.length === 1 ? "" : "s"} selected`
            : "No files selected"}
        </small>
      </label>
      {files.length > 0 ? (
        <ul>
          {files.map((fileName) => (
            <li key={fileName}>{fileName}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
};

export const EvidenceUploadControl = (props: FieldControlProps) => (
  <FileUploadControl
    {...props}
    className={`${props.className ?? ""} field-library-evidence-upload`}
  />
);

export const PolicyAcknowledgementControl = (props: FieldControlProps) => (
  <label
    className={controlClassNames(props, "field-library-policy-acknowledgement")}
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

export const SignatureInputControl = (props: FieldControlProps) => {
  if (fieldControlSourceType(props.config) === "signature_capture") {
    return <SignatureCaptureControl {...props} />;
  }

  return (
    <div
      className={controlClassNames(props, "field-library-signature")}
      style={controlStyleProps(props)}
    >
      <input
        aria-label={`${props.config.label} typed signature`}
        className="field-library-input"
        onChange={(event) => props.onChange(eventInputValue(event))}
        placeholder="Type full legal name"
        value={typeof props.value === "string" ? props.value : ""}
      />
      <small>Typed signature is stored as the attestation value.</small>
    </div>
  );
};

export const SignatureControl = SignatureInputControl;

export const SignatureCaptureControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);
  const method = objectFieldValue(currentValue, "method", "typed");
  const typedName = objectFieldValue(currentValue, "typedName");

  return (
    <div
      className={controlClassNames(props, "field-library-signature-capture")}
      style={controlStyleProps(props)}
    >
      <div className="field-library-segmented-control" role="group">
        {["typed", "drawn", "uploaded"].map((signatureMethod) => (
          <button
            aria-pressed={method === signatureMethod}
            key={signatureMethod}
            onClick={() =>
              props.onChange(updateObjectField(currentValue, "method", signatureMethod))
            }
            type="button"
          >
            {signatureMethod}
          </button>
        ))}
      </div>
      <input
        aria-label={`${props.config.label} typed name`}
        className="field-library-input"
        onChange={(event) =>
          props.onChange(
            updateObjectField(currentValue, "typedName", eventInputValue(event)),
          )
        }
        placeholder="Type full legal name"
        value={typedName}
      />
    </div>
  );
};

export const FileSignatureControl = (props: FieldControlProps) => {
  if (fieldControlSourceType(props.config) === "evidence_upload") {
    return <EvidenceUploadControl {...props} />;
  }

  if (fieldControlSourceType(props.config) === "policy_acknowledgement") {
    return <PolicyAcknowledgementControl {...props} />;
  }

  if (props.config.type === "signature") {
    return <SignatureInputControl {...props} />;
  }

  return <FileUploadControl {...props} />;
};
