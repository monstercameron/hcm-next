export type WorkflowConfigJsonSchema = Record<string, unknown>;

/**
 * JSON Schema-shaped contract used by Workflow Admin editors for completion,
 * validation hints, and import/export checks.
 */
export const canonicalWorkflowConfigJsonSchema: WorkflowConfigJsonSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: "https://hcm-next.local/schemas/workflow-config-v0.4.json",
  title: "HCM Next Workflow Config",
  type: "object",
  additionalProperties: true,
  required: [
    "schemaVersion",
    "intent",
    "subjectType",
    "selfServiceStart",
    "states",
    "graph",
    "interactions",
    "submit",
    "approval",
    "plan",
    "ledger",
    "projection",
    "timeline",
  ],
  properties: {
    schemaVersion: {
      type: "string",
      enum: ["v0.3", "v0.4"],
    },
    name: {
      type: "string",
      minLength: 1,
    },
    description: {
      type: "string",
      minLength: 1,
    },
    metadata: {
      type: "object",
      additionalProperties: true,
      properties: {
        title: { type: "string", minLength: 1 },
        description: { type: "string", minLength: 1 },
        domain: { type: "string", minLength: 1 },
        owner: { type: "string", minLength: 1 },
      },
    },
    intent: {
      type: "string",
      minLength: 1,
      pattern: "^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)+$",
    },
    subjectType: {
      type: "string",
      minLength: 1,
    },
    selfServiceStart: {
      type: "boolean",
    },
    states: {
      type: "object",
      additionalProperties: {
        type: "object",
        required: ["actions"],
        properties: {
          actions: {
            type: "array",
            items: {
              type: "object",
              required: ["transition", "label", "actor", "handler"],
            },
          },
        },
      },
    },
    graph: {
      type: "object",
      required: ["startNodeId", "nodes"],
      properties: {
        startNodeId: { type: "string", minLength: 1 },
        nodes: {
          type: "array",
          items: {
            type: "object",
            required: ["nodeId", "type", "title"],
          },
        },
        edges: {
          type: "array",
          items: {
            type: "object",
            required: ["fromNodeId", "routeKey"],
          },
        },
      },
    },
    interactions: {
      type: "object",
      additionalProperties: {
        type: "object",
        required: ["type", "title"],
      },
    },
    permissions: {
      type: "object",
      additionalProperties: true,
    },
    ledger: {
      type: "object",
      additionalProperties: true,
    },
  },
} as const;
