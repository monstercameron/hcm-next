import type { CSSProperties } from "react";
import {
  applyStyleLabShadowPreset,
  createDefaultStyleLabConfig,
  styleLabControlHeightForDensity,
} from "./style-lab-vars";
import type {
  StyleLabControlStyle,
  StyleLabDensity,
  StyleLabRailProps,
  StyleLabShadowPreset,
} from "./types";

type FieldShellProps = Readonly<{
  label: string;
  valueLabel?: string;
  children: JSX.Element;
}>;

const railStyle: CSSProperties = {
  alignSelf: "stretch",
  background: "var(--surface-raised, #ffffff)",
  borderLeft: "1px solid var(--border-default, #d8dee8)",
  color: "var(--text-primary, #17202f)",
  display: "grid",
  gap: 18,
  gridTemplateRows: "auto 1fr",
  minWidth: 320,
  padding: 18,
  position: "sticky",
  right: 0,
  top: 0,
};

const sectionStyle: CSSProperties = {
  display: "grid",
  gap: 12,
};

const fieldStyle: CSSProperties = {
  display: "grid",
  gap: 7,
};

const fieldHeaderStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  gap: 8,
  justifyContent: "space-between",
};

const labelStyle: CSSProperties = {
  color: "var(--text-secondary, #42526a)",
  fontSize: 12,
  fontWeight: 700,
};

const valueLabelStyle: CSSProperties = {
  color: "var(--text-muted, #6b778c)",
  fontFamily: "var(--font-mono, ui-monospace)",
  fontSize: 11,
};

const inputStyle: CSSProperties = {
  background: "var(--ui-control-surface, #ffffff)",
  border: "1px solid var(--ui-control-border, #d8dee8)",
  borderRadius: "var(--ui-radius-control, 8px)",
  color: "var(--text-primary, #17202f)",
  minHeight: 34,
  padding: "7px 9px",
  width: "100%",
};

const colorInputStyle: CSSProperties = {
  ...inputStyle,
  minHeight: 38,
  padding: 4,
};

const rangeStyle: CSSProperties = {
  accentColor: "var(--action-primary-background, #2456d6)",
  width: "100%",
};

const titleStyle: CSSProperties = {
  fontSize: 16,
  fontWeight: 800,
  margin: 0,
};

const descriptionStyle: CSSProperties = {
  color: "var(--text-muted, #6b778c)",
  fontSize: 13,
  lineHeight: 1.45,
  margin: 0,
};

const colorFields: readonly {
  key:
    | "pageSurface"
    | "raisedSurface"
    | "mutedSurface"
    | "controlSurface"
    | "activeSurface"
    | "border"
    | "strongBorder"
    | "textPrimary"
    | "textSecondary"
    | "textMuted"
    | "accent";
  label: string;
}[] = [
  { key: "pageSurface", label: "Page surface" },
  { key: "raisedSurface", label: "Raised surface" },
  { key: "mutedSurface", label: "Muted surface" },
  { key: "controlSurface", label: "Control surface" },
  { key: "activeSurface", label: "Active surface" },
  { key: "border", label: "Border" },
  { key: "strongBorder", label: "Strong border" },
  { key: "textPrimary", label: "Primary text" },
  { key: "textSecondary", label: "Secondary text" },
  { key: "textMuted", label: "Muted text" },
  { key: "accent", label: "Accent" },
];

const densityOptions: readonly StyleLabDensity[] = [
  "compact",
  "default",
  "comfortable",
];

const shadowOptions: readonly StyleLabShadowPreset[] = [
  "none",
  "soft",
  "raised",
  "strong",
];

const fieldValue = (value: number, suffix = ""): string => `${value}${suffix}`;

const opacityValue = (value: number): string => value.toFixed(2);

function FieldShell({ label, valueLabel, children }: FieldShellProps): JSX.Element {
  return (
    <label style={fieldStyle}>
      <span style={fieldHeaderStyle}>
        <span style={labelStyle}>{label}</span>
        {valueLabel === undefined ? null : (
          <span style={valueLabelStyle}>{valueLabel}</span>
        )}
      </span>
      {children}
    </label>
  );
}

const updateStyleValue = <Key extends keyof StyleLabControlStyle>(
  currentValue: StyleLabControlStyle,
  key: Key,
  value: StyleLabControlStyle[Key],
): StyleLabControlStyle => ({
  ...currentValue,
  [key]: value,
});

/** Right-hand rail for live tuning the CSS variables consumed by generated controls. */
export function StyleLabRail({
  value,
  onChange,
  brand,
  presets = [],
  title = "Brand styling",
  description = "Tune the live CSS variables used by generated workflow components.",
  className,
  style,
}: StyleLabRailProps): JSX.Element {
  const defaultStyle = createDefaultStyleLabConfig(brand?.tokens);
  const mergedRailStyle = {
    ...railStyle,
    ...style,
  };

  const setDensity = (density: StyleLabDensity): void => {
    onChange({
      ...value,
      density,
      controlHeightPx: styleLabControlHeightForDensity(density),
    });
  };

  return (
    <aside
      aria-label="Live style controls"
      className={className}
      style={mergedRailStyle}
    >
      <header style={sectionStyle}>
        <div>
          <p style={titleStyle}>{title}</p>
          <p style={descriptionStyle}>{description}</p>
        </div>
        <button onClick={() => onChange(defaultStyle)} style={inputStyle} type="button">
          Reset to brand defaults
        </button>
      </header>

      <div style={sectionStyle}>
        {presets.length > 0 ? (
          <FieldShell label="Preset">
            <select
              onChange={(event) => {
                const selectedPreset = presets.find(
                  (preset) => preset.id === event.currentTarget.value,
                );

                if (selectedPreset !== undefined) {
                  onChange(selectedPreset.style);
                }
              }}
              style={inputStyle}
              value=""
            >
              <option value="">Choose preset</option>
              {presets.map((preset) => (
                <option key={preset.id} value={preset.id}>
                  {preset.label}
                </option>
              ))}
            </select>
          </FieldShell>
        ) : null}

        {colorFields.map((field) => (
          <FieldShell key={field.key} label={field.label} valueLabel={value[field.key]}>
            <input
              onChange={(event) =>
                onChange(updateStyleValue(value, field.key, event.currentTarget.value))
              }
              style={colorInputStyle}
              type="color"
              value={value[field.key]}
            />
          </FieldShell>
        ))}

        <FieldShell label="Density">
          <select
            onChange={(event) =>
              setDensity(event.currentTarget.value as StyleLabDensity)
            }
            style={inputStyle}
            value={value.density}
          >
            {densityOptions.map((density) => (
              <option key={density} value={density}>
                {density}
              </option>
            ))}
          </select>
        </FieldShell>

        <FieldShell
          label="Control height"
          valueLabel={fieldValue(value.controlHeightPx, "px")}
        >
          <input
            max={60}
            min={30}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "controlHeightPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.controlHeightPx}
          />
        </FieldShell>

        <FieldShell label="Base gap" valueLabel={fieldValue(value.gapPx, "px")}>
          <input
            max={28}
            min={4}
            onChange={(event) =>
              onChange(
                updateStyleValue(value, "gapPx", Number(event.currentTarget.value)),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.gapPx}
          />
        </FieldShell>

        <FieldShell
          label="Section gap"
          valueLabel={fieldValue(value.sectionGapPx, "px")}
        >
          <input
            max={42}
            min={8}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "sectionGapPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.sectionGapPx}
          />
        </FieldShell>

        <FieldShell
          label="Panel padding"
          valueLabel={fieldValue(value.panelPaddingPx, "px")}
        >
          <input
            max={42}
            min={8}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "panelPaddingPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.panelPaddingPx}
          />
        </FieldShell>

        <FieldShell
          label="Card padding"
          valueLabel={fieldValue(value.cardPaddingPx, "px")}
        >
          <input
            max={36}
            min={8}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "cardPaddingPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.cardPaddingPx}
          />
        </FieldShell>

        <FieldShell
          label="Control padding X"
          valueLabel={fieldValue(value.controlPaddingXPx, "px")}
        >
          <input
            max={24}
            min={6}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "controlPaddingXPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.controlPaddingXPx}
          />
        </FieldShell>

        <FieldShell
          label="Control padding Y"
          valueLabel={fieldValue(value.controlPaddingYPx, "px")}
        >
          <input
            max={18}
            min={4}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "controlPaddingYPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.controlPaddingYPx}
          />
        </FieldShell>

        <FieldShell label="Radius" valueLabel={fieldValue(value.radiusPx, "px")}>
          <input
            max={24}
            min={0}
            onChange={(event) =>
              onChange(
                updateStyleValue(value, "radiusPx", Number(event.currentTarget.value)),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.radiusPx}
          />
        </FieldShell>

        <FieldShell
          label="Border width"
          valueLabel={fieldValue(value.borderWidthPx, "px")}
        >
          <input
            max={4}
            min={0}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "borderWidthPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={0.5}
            style={rangeStyle}
            type="range"
            value={value.borderWidthPx}
          />
        </FieldShell>

        <FieldShell
          label="Motion speed"
          valueLabel={fieldValue(value.motionSpeedMs, "ms")}
        >
          <input
            max={500}
            min={0}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "motionSpeedMs",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={10}
            style={rangeStyle}
            type="range"
            value={value.motionSpeedMs}
          />
        </FieldShell>

        <FieldShell label="Hover lift" valueLabel={fieldValue(value.hoverLiftPx, "px")}>
          <input
            max={0}
            min={-8}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "hoverLiftPx",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={1}
            style={rangeStyle}
            type="range"
            value={value.hoverLiftPx}
          />
        </FieldShell>

        <FieldShell label="Hover scale" valueLabel={value.hoverScale.toFixed(3)}>
          <input
            max={1.04}
            min={1}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "hoverScale",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={0.001}
            style={rangeStyle}
            type="range"
            value={value.hoverScale}
          />
        </FieldShell>

        <FieldShell label="Hover opacity" valueLabel={opacityValue(value.hoverOpacity)}>
          <input
            max={1}
            min={0.7}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "hoverOpacity",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={0.01}
            style={rangeStyle}
            type="range"
            value={value.hoverOpacity}
          />
        </FieldShell>

        <FieldShell
          label="Disabled opacity"
          valueLabel={opacityValue(value.disabledOpacity)}
        >
          <input
            max={0.9}
            min={0.25}
            onChange={(event) =>
              onChange(
                updateStyleValue(
                  value,
                  "disabledOpacity",
                  Number(event.currentTarget.value),
                ),
              )
            }
            step={0.01}
            style={rangeStyle}
            type="range"
            value={value.disabledOpacity}
          />
        </FieldShell>

        <FieldShell label="Shadow">
          <select
            onChange={(event) =>
              onChange(
                applyStyleLabShadowPreset(
                  value,
                  event.currentTarget.value as StyleLabShadowPreset,
                ),
              )
            }
            style={inputStyle}
            value={value.shadowPreset}
          >
            {shadowOptions.map((shadowPreset) => (
              <option key={shadowPreset} value={shadowPreset}>
                {shadowPreset}
              </option>
            ))}
          </select>
        </FieldShell>
      </div>
    </aside>
  );
}
