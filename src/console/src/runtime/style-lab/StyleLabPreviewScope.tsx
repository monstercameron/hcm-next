import type { CSSProperties } from "react";
import { mergeStyleLabCssVariables } from "./style-lab-vars";
import type { StyleLabPreviewScopeProps, StyleLabWorkbenchProps } from "./types";
import { StyleLabRail } from "./StyleLabRail";

const previewScopeBaseStyle: CSSProperties = {
  minWidth: 0,
};

const workbenchStyle: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "minmax(0, 1fr) 340px",
  minHeight: "100%",
};

/** Provides a brand-aware CSS variable scope for live control previews. */
export function StyleLabPreviewScope({
  brand,
  styleConfig,
  children,
  className,
  style,
}: StyleLabPreviewScopeProps): JSX.Element {
  const cssVariables = mergeStyleLabCssVariables({
    brand,
    styleConfig,
  });

  return (
    <div
      className={className}
      style={{
        ...previewScopeBaseStyle,
        ...cssVariables,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

/** Layout helper that places a live preview beside the right-hand style rail. */
export function StyleLabWorkbench({
  brand,
  value,
  onChange,
  children,
  presets,
  title,
  description,
  className,
  style,
}: StyleLabWorkbenchProps): JSX.Element {
  const railProps = {
    brand,
    onChange,
    value,
    ...(description === undefined ? {} : { description }),
    ...(presets === undefined ? {} : { presets }),
    ...(title === undefined ? {} : { title }),
  };

  return (
    <div className={className} style={{ ...workbenchStyle, ...style }}>
      <StyleLabPreviewScope brand={brand} styleConfig={value}>
        {children}
      </StyleLabPreviewScope>
      <StyleLabRail {...railProps} />
    </div>
  );
}
