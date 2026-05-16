import { useMemo, useState } from "react";
import type { LabelValueItem, WidgetComponentProps, WidgetRecord } from "./types";
import { WidgetRoot } from "./primitives";
import {
  numberValue,
  objectValue,
  optionLabel,
  optionValue,
  recordsValue,
  resolveWidgetStyleProps,
  stringValue,
  valueToText,
} from "./utils";

export type MetricTileConfig = {
  label: string;
  value: unknown;
  detail?: string;
};

export type ProgressConfig = {
  value: number;
  label?: string;
  max?: number;
};

export type MetricGraphPoint = {
  label: string;
  value: number;
  target?: number;
};

export type MetricGraphConfig = {
  points: readonly MetricGraphPoint[];
  variant?: "bar" | "line";
};

export type GraphChartSeries = {
  id: string;
  label: string;
  points: readonly MetricGraphPoint[];
};

export type GraphChartConfig = {
  series: readonly GraphChartSeries[];
};

export type ChartConfig = {
  points?: readonly MetricGraphPoint[];
  series?: readonly GraphChartSeries[];
  variant?: "bar" | "line" | "area";
  emptyLabel?: string;
};

export type TableColumnConfig = {
  id: string;
  label: string;
};

export type DataTableConfig = {
  columns: readonly TableColumnConfig[];
  rows: readonly WidgetRecord[];
  filterable?: boolean;
  riskFilterField?: string;
};

export type FilterableTableConfig = DataTableConfig & {
  riskFilterField?: string;
};

export type MatrixConfig = {
  rows: readonly LabelValueItem[];
  columns: readonly LabelValueItem[];
  values: WidgetRecord;
};

export type ClusterBoardItem = {
  id: string;
  label: string;
  group: string;
  description?: string;
};

export type ClusterBoardGroup = {
  id: string;
  label: string;
};

export type ClusterBoardConfig = {
  groups: readonly ClusterBoardGroup[];
  items: readonly ClusterBoardItem[];
  values?: Readonly<Record<string, readonly string[]>>;
};

export type BoardConfig = ClusterBoardConfig;

export const metricPointsFromRecords = (value: unknown): readonly MetricGraphPoint[] =>
  recordsValue(value).map((point) => {
    const target = typeof point.target === "number" ? { target: point.target } : {};

    return {
      label: stringValue(point.label, "Metric"),
      value: numberValue(point.value, 0),
      ...target,
    };
  });

export const tableColumnsFromRecords = (value: unknown): readonly TableColumnConfig[] =>
  recordsValue(value).map((column) => ({
    id: stringValue(column.id, stringValue(column.value)),
    label: stringValue(column.label, stringValue(column.id)),
  }));

const metricPathPoints = (points: readonly MetricGraphPoint[]): string => {
  const maxValue = Math.max(
    1,
    ...points.flatMap((point) => [point.value, point.target ?? 0]),
  );

  return points
    .map((point, index) => {
      const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
      const y = 96 - (point.value / maxValue) * 86;

      return `${x},${y}`;
    })
    .join(" ");
};

const createClusterState = (
  groups: readonly ClusterBoardGroup[],
  items: readonly ClusterBoardItem[],
): Record<string, string[]> => {
  const fallbackGroupId = groups[0]?.id ?? "Backlog";
  const clusters = Object.fromEntries(groups.map((group) => [group.id, []])) as Record<
    string,
    string[]
  >;

  for (const item of items) {
    const targetGroup =
      clusters[item.group] === undefined ? fallbackGroupId : item.group;
    clusters[targetGroup] = [...(clusters[targetGroup] ?? []), item.id];
  }

  return clusters;
};

export const clusterBoardItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly ClusterBoardItem[] =>
  records.map((record) => ({
    id: optionValue(record),
    label: optionLabel(record),
    group: stringValue(record.group, "Backlog"),
    description: stringValue(record.description),
  }));

export const clusterBoardGroupsFromRecords = (
  records: readonly WidgetRecord[],
): readonly ClusterBoardGroup[] =>
  records.map((record) => ({
    id: optionValue(record),
    label: optionLabel(record),
  }));

export function MetricTileWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<MetricTileConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="metric-tile" styleProps={resolvedStyleProps}>
      <span>{config.label}</span>
      <strong>{valueToText(config.value)}</strong>
      {config.detail !== undefined ? <small>{config.detail}</small> : null}
    </WidgetRoot>
  );
}

export function ProgressWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ProgressConfig>): JSX.Element {
  const max = config.max ?? 100;
  const value = Math.min(max, Math.max(0, config.value));
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="progress-widget" styleProps={resolvedStyleProps}>
      <progress max={max} value={value} />
      <span>
        {config.label !== undefined ? `${config.label}: ` : ""}
        {value}%
      </span>
    </WidgetRoot>
  );
}

export function MetricGraphWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<MetricGraphConfig>): JSX.Element {
  const configuredVariant = config.variant ?? "bar";
  const [variant, setVariant] = useState<"bar" | "line">(configuredVariant);
  const maxValue = Math.max(
    1,
    ...config.points.flatMap((point) => [point.value, point.target ?? 0]),
  );
  const pathPoints = metricPathPoints(config.points);
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="metric-graph-widget" styleProps={resolvedStyleProps}>
      <div className="graph-toolbar" role="group" aria-label="Metric graph type">
        {[
          { label: "Bars", value: "bar" as const },
          { label: "Line", value: "line" as const },
        ].map((option) => (
          <button
            aria-pressed={variant === option.value}
            className={variant === option.value ? "segment-active" : ""}
            key={option.value}
            onClick={() => setVariant(option.value)}
            type="button"
          >
            {option.label}
          </button>
        ))}
      </div>
      {variant === "bar" ? (
        <div className="metric-bar-chart">
          {config.points.map((point) => (
            <div className="metric-bar-column" key={point.label}>
              <div className="metric-bar-rail">
                {point.target === undefined ? null : (
                  <span
                    className="metric-target-line"
                    style={{ bottom: `${(point.target / maxValue) * 100}%` }}
                  />
                )}
                <span
                  className="metric-bar"
                  style={{ height: `${(point.value / maxValue) * 100}%` }}
                >
                  {point.value}
                </span>
              </div>
              <small>{point.label}</small>
            </div>
          ))}
        </div>
      ) : (
        <div className="metric-line-chart">
          <svg viewBox="0 0 100 100" preserveAspectRatio="none">
            <g className="chart-grid-lines" aria-hidden="true">
              {[28, 52, 76].map((y) => (
                <line key={y} x1="0" x2="100" y1={y} y2={y} />
              ))}
            </g>
            <polyline points={pathPoints} />
            {config.points.map((point, index) => {
              const x =
                config.points.length <= 1
                  ? 50
                  : (index / (config.points.length - 1)) * 100;
              const y = 96 - (point.value / maxValue) * 86;

              return <circle cx={x} cy={y} key={point.label} r="1.7" />;
            })}
          </svg>
          <div className="metric-line-labels">
            {config.points.map((point) => (
              <span key={point.label}>
                <strong>{point.value}</strong>
                <small>{point.label}</small>
              </span>
            ))}
          </div>
        </div>
      )}
    </WidgetRoot>
  );
}

export function GraphChartWidget({
  config,
  styleProps,
}: WidgetComponentProps<GraphChartConfig>): JSX.Element {
  const [activeSeriesId, setActiveSeriesId] = useState(config.series[0]?.id ?? "");
  const activeSeries =
    config.series.find((series) => series.id === activeSeriesId) ?? config.series[0];
  const points = activeSeries?.points ?? [];
  const maxValue = Math.max(1, ...points.map((point) => point.value));
  const pathPoints = points
    .map((point, index) => {
      const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
      const y = 94 - (point.value / maxValue) * 84;

      return `${x},${y}`;
    })
    .join(" ");

  return (
    <WidgetRoot className="graph-chart-widget" styleProps={styleProps}>
      <div className="graph-toolbar" role="group" aria-label="Graph chart series">
        {config.series.map((series) => (
          <button
            aria-pressed={activeSeriesId === series.id}
            className={activeSeriesId === series.id ? "segment-active" : ""}
            key={series.id}
            onClick={() => setActiveSeriesId(series.id)}
            type="button"
          >
            {series.label}
          </button>
        ))}
      </div>
      <div className="graph-chart-canvas">
        <svg viewBox="0 0 100 100" preserveAspectRatio="none">
          <g className="chart-grid-lines" aria-hidden="true">
            {[28, 52, 76].map((y) => (
              <line key={y} x1="0" x2="100" y1={y} y2={y} />
            ))}
          </g>
          <polygon points={`0,100 ${pathPoints} 100,100`} />
          <polyline points={pathPoints} />
          {points.map((point, index) => {
            const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
            const y = 94 - (point.value / maxValue) * 84;

            return <circle cx={x} cy={y} key={point.label} r="1.7" />;
          })}
        </svg>
        <div className="metric-line-labels">
          {points.map((point) => (
            <span key={point.label}>
              <strong>{point.value}</strong>
              <small>{point.label}</small>
            </span>
          ))}
        </div>
      </div>
    </WidgetRoot>
  );
}

export function ChartWidget({
  config,
  styleProps,
}: WidgetComponentProps<ChartConfig>): JSX.Element {
  const series = config.series ?? [];

  if (series.length > 0) {
    return <GraphChartWidget config={{ series }} styleProps={styleProps} />;
  }

  return (
    <MetricGraphWidget
      config={{
        points: config.points ?? [],
        variant: config.variant === "line" ? "line" : "bar",
      }}
      styleProps={styleProps}
    />
  );
}

export function DataTableWidget({
  config,
  styleProps,
}: WidgetComponentProps<DataTableConfig>): JSX.Element {
  const riskField = config.riskFilterField ?? "risk";
  const [query, setQuery] = useState("");
  const [riskFilter, setRiskFilter] = useState("all");
  const filteredRows = useMemo(
    () =>
      config.filterable === true
        ? config.rows.filter((row) => {
            const queryMatch =
              query.trim().length === 0 ||
              Object.values(row).some((value) =>
                valueToText(value).toLowerCase().includes(query.toLowerCase()),
              );
            const rowRisk = stringValue(row[riskField]).toLowerCase();
            const riskMatch = riskFilter === "all" || rowRisk === riskFilter;

            return queryMatch && riskMatch;
          })
        : config.rows,
    [config.filterable, config.rows, query, riskField, riskFilter],
  );

  return (
    <WidgetRoot
      className={config.filterable === true ? "filterable-table" : "data-table-wrap"}
      styleProps={styleProps}
    >
      {config.filterable === true ? (
        <div className="table-filters">
          <input
            aria-label="Filter table rows"
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="Filter rows"
            value={query}
          />
          <select
            aria-label="Risk filter"
            onChange={(event) => setRiskFilter(event.currentTarget.value)}
            value={riskFilter}
          >
            <option value="all">All risk levels</option>
            <option value="low">Low</option>
            <option value="medium">Medium</option>
            <option value="high">High</option>
          </select>
        </div>
      ) : null}
      <table className="data-table">
        <thead>
          <tr>
            {config.columns.map((column) => (
              <th key={column.id}>{column.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {filteredRows.map((row, rowIndex) => (
            <tr key={`${stringValue(row.id, "row")}-${rowIndex}`}>
              {config.columns.map((column) => (
                <td key={column.id}>{valueToText(row[column.id])}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {config.filterable === true ? (
        <div className="selected-pill">
          Showing {filteredRows.length} of {config.rows.length} rows
        </div>
      ) : null}
    </WidgetRoot>
  );
}

export function FilterableTableWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<FilterableTableConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <DataTableWidget
      config={{
        ...config,
        filterable: true,
      }}
      styleProps={resolvedStyleProps}
    />
  );
}

export function MatrixWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<MatrixConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="matrix-display" styleProps={resolvedStyleProps}>
      <strong>Criterion</strong>
      {config.columns.map((column) => (
        <strong key={column.label}>{column.label}</strong>
      ))}
      {config.rows.map((row) => (
        <div className="matrix-row-fragment" key={row.label}>
          <span>{row.label}</span>
          {config.columns.map((column) => {
            const active = stringValue(config.values[row.label]) === column.label;

            return (
              <span
                className={active ? "matrix-cell matrix-cell-active" : "matrix-cell"}
                key={column.label}
              >
                {active ? "Selected" : ""}
              </span>
            );
          })}
        </div>
      ))}
    </WidgetRoot>
  );
}

export function ClusterBoardWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ClusterBoardConfig>): JSX.Element {
  const initialClusters =
    config.values ?? createClusterState(config.groups, config.items);
  const [selectedItemId, setSelectedItemId] = useState(config.items[0]?.id ?? "");
  const itemMap = new Map(config.items.map((item) => [item.id, item]));
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="cluster-board" styleProps={resolvedStyleProps}>
      {Object.entries(initialClusters).map(([groupId, itemIds]) => {
        const group = config.groups.find((candidate) => candidate.id === groupId);

        return (
          <section className="cluster-column" key={groupId}>
            <h3>{group?.label ?? groupId}</h3>
            {itemIds.map((itemId) => {
              const item = itemMap.get(itemId);

              if (item === undefined) {
                return null;
              }

              return (
                <button
                  aria-pressed={selectedItemId === item.id}
                  className={
                    selectedItemId === item.id
                      ? "cluster-card cluster-card-selected"
                      : "cluster-card"
                  }
                  key={item.id}
                  onClick={() => setSelectedItemId(item.id)}
                  type="button"
                >
                  <strong>{item.label}</strong>
                  {item.description !== undefined ? (
                    <small>{item.description}</small>
                  ) : null}
                </button>
              );
            })}
          </section>
        );
      })}
    </WidgetRoot>
  );
}

export const BoardWidget = ClusterBoardWidget;

export const matrixItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly LabelValueItem[] =>
  records.map((record) => ({
    label: optionLabel(record),
    value: optionValue(record),
  }));

export const matrixValuesFromConfig = (value: unknown): WidgetRecord =>
  objectValue(value);
