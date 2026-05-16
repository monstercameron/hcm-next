import { useMemo, useState } from "react";
import type { WidgetComponentProps, WidgetRecord } from "./types";
import { StatusBadge, WidgetRoot } from "./primitives";
import {
  numberValue,
  optionGroup,
  optionLabel,
  optionValue,
  recordsValue,
  resolveWidgetStyleProps,
  stringValue,
} from "./utils";

export type GraphNodeConfig = {
  id: string;
  label: string;
  description: string;
  group: string;
  status: string;
  x: number;
  y: number;
  parentId?: string;
};

export type GraphEdgeConfig = {
  id: string;
  source: string;
  target: string;
  label: string;
  status: string;
};

export type OrgChartConfig = {
  nodes: readonly GraphNodeConfig[];
  selectedLabel?: string;
};

export type NodeGraphConfig = {
  nodes: readonly GraphNodeConfig[];
  edges: readonly GraphEdgeConfig[];
  label?: string;
  selectedLabel?: string;
};

export type GraphConfig = NodeGraphConfig & {
  graphType?: string;
};

export const graphNodesFromRecords = (value: unknown): readonly GraphNodeConfig[] =>
  recordsValue(value).map((node) => {
    const id = stringValue(node.id, optionValue(node));
    const parentId = stringValue(node.parentId, stringValue(node.parent));
    const parentConfig = parentId.length > 0 ? { parentId } : {};

    return {
      id,
      label: stringValue(node.label, id),
      description: stringValue(node.description, optionGroup(node)),
      group: optionGroup(node),
      status: stringValue(node.status, "info"),
      x: numberValue(node.x, 50),
      y: numberValue(node.y, 50),
      ...parentConfig,
    };
  });

export const graphEdgesFromRecords = (value: unknown): readonly GraphEdgeConfig[] =>
  recordsValue(value).map((edge, index) => {
    const source = stringValue(edge.source);
    const target = stringValue(edge.target);

    return {
      id: stringValue(edge.id, `${source}-${target}-${index}`),
      source,
      target,
      label: stringValue(edge.label),
      status: stringValue(edge.status, "info"),
    };
  });

export const orgChartNodesFromRecords = (
  records: readonly WidgetRecord[],
): readonly GraphNodeConfig[] =>
  records.map((record, index) => {
    const parentId = stringValue(record.parentId, stringValue(record.parent));
    const parentConfig = parentId.length > 0 ? { parentId } : {};

    return {
      id: optionValue(record) || `node-${index}`,
      label: optionLabel(record),
      description: stringValue(record.description, optionGroup(record)),
      group: optionGroup(record),
      status: stringValue(record.status, "info"),
      x: numberValue(record.x, 50),
      y: numberValue(record.y, 50),
      ...parentConfig,
    };
  });

export function OrgChartWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<OrgChartConfig>): JSX.Element {
  const rootNodes = config.nodes.filter((node) => node.parentId === undefined);
  const [selectedNodeId, setSelectedNodeId] = useState(rootNodes[0]?.id ?? "");
  const selectedNode = config.nodes.find((node) => node.id === selectedNodeId);
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  const childrenByParent = useMemo(() => {
    const groups = new Map<string, GraphNodeConfig[]>();

    for (const node of config.nodes) {
      if (node.parentId !== undefined) {
        groups.set(node.parentId, [...(groups.get(node.parentId) ?? []), node]);
      }
    }

    return groups;
  }, [config.nodes]);

  const renderOrgNode = (node: GraphNodeConfig): JSX.Element => {
    const children = childrenByParent.get(node.id) ?? [];
    const selected = selectedNodeId === node.id;

    return (
      <li key={node.id}>
        <button
          aria-pressed={selected}
          className={
            selected ? "org-chart-node org-chart-node-selected" : "org-chart-node"
          }
          onClick={() => setSelectedNodeId(node.id)}
          type="button"
        >
          <span>
            <strong>{node.label}</strong>
            <small>{node.description}</small>
          </span>
          <StatusBadge status={node.status} styleProps={resolvedStyleProps} />
        </button>
        {children.length > 0 ? <ul>{children.map(renderOrgNode)}</ul> : null}
      </li>
    );
  };

  return (
    <WidgetRoot className="org-chart-widget" styleProps={resolvedStyleProps}>
      <ul>{rootNodes.map(renderOrgNode)}</ul>
      <div className="selected-pill">
        {config.selectedLabel ?? "Selected"}: {selectedNode?.label ?? "None"}
      </div>
    </WidgetRoot>
  );
}

export function NodeGraphWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<NodeGraphConfig>): JSX.Element {
  const [selectedNodeId, setSelectedNodeId] = useState(config.nodes[0]?.id ?? "");
  const nodeMap = new Map(config.nodes.map((node) => [node.id, node]));
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="node-graph-widget" styleProps={resolvedStyleProps}>
      <div className="node-graph-canvas" role="img" aria-label={config.label}>
        <svg viewBox="0 0 100 100" preserveAspectRatio="none">
          {config.edges.map((edge) => {
            const source = nodeMap.get(edge.source);
            const target = nodeMap.get(edge.target);

            if (source === undefined || target === undefined) {
              return null;
            }

            return (
              <g key={edge.id}>
                <line x1={source.x} y1={source.y} x2={target.x} y2={target.y} />
                {edge.label.length > 0 ? (
                  <text x={(source.x + target.x) / 2} y={(source.y + target.y) / 2}>
                    {edge.label}
                  </text>
                ) : null}
              </g>
            );
          })}
        </svg>
        {config.nodes.map((node) => (
          <button
            aria-pressed={selectedNodeId === node.id}
            className={
              selectedNodeId === node.id
                ? "graph-node graph-node-selected"
                : "graph-node"
            }
            key={node.id}
            onClick={() => setSelectedNodeId(node.id)}
            style={{ left: `${node.x}%`, top: `${node.y}%` }}
            type="button"
          >
            <strong>{node.label}</strong>
            <small>{node.group}</small>
          </button>
        ))}
      </div>
      <div className="selected-pill">
        {config.selectedLabel ?? "Selected node"}:{" "}
        {nodeMap.get(selectedNodeId)?.label ?? "None"}
      </div>
    </WidgetRoot>
  );
}

export function GraphWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<GraphConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  if (config.graphType === "org" || config.graphType === "orgChart") {
    const selectedLabelConfig =
      config.selectedLabel === undefined ? {} : { selectedLabel: config.selectedLabel };

    return (
      <OrgChartWidget
        config={{
          nodes: config.nodes,
          ...selectedLabelConfig,
        }}
        styleProps={resolvedStyleProps}
      />
    );
  }

  return <NodeGraphWidget config={config} styleProps={resolvedStyleProps} />;
}
