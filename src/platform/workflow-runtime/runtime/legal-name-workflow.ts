import {
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_TRANSITIONS,
  type WorkflowState,
  type WorkflowTransition,
} from "../constants";
import type { WorkflowDefinitionVersion } from "../domain";
import { legalNameInputInteraction } from "./interactions";

export const LEGAL_NAME_WORKFLOW_DEFINITION: WorkflowDefinitionVersion = {
  workflowDefinitionId: "workflow_definition_legal_name_change",
  workflowVersionId: "workflow_version_legal_name_change_v1",
  intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
  versionNumber: 1,
  status: "published",
  inputSchema: legalNameInputInteraction().jsonSchema ?? {},
  graphDefinition: {
    states: Object.values(WORKFLOW_STATES),
    transitions: {
      [WORKFLOW_STATES.COLLECTING_INPUT]: {
        [WORKFLOW_TRANSITIONS.SUBMIT_INPUT]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REQUEST,
          next: WORKFLOW_STATES.COLLECTING_EVIDENCE,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
      [WORKFLOW_STATES.COLLECTING_EVIDENCE]: {
        [WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE,
          next: WORKFLOW_STATES.WAITING_APPROVAL,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
      [WORKFLOW_STATES.WAITING_APPROVAL]: {
        [WORKFLOW_TRANSITIONS.APPROVE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_APPROVE,
          next: WORKFLOW_STATES.APPROVED,
        },
        [WORKFLOW_TRANSITIONS.REJECT]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
          next: WORKFLOW_STATES.REJECTED,
        },
        [WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
          next: WORKFLOW_STATES.COLLECTING_EVIDENCE,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
      [WORKFLOW_STATES.APPROVED]: {
        [WORKFLOW_TRANSITIONS.EXECUTE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_EXECUTE,
          next: WORKFLOW_STATES.EXECUTED,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
    },
  },
};

export const LEGAL_NAME_TRANSITION_MAP: Record<WorkflowState, WorkflowTransition[]> = {
  [WORKFLOW_STATES.COLLECTING_INPUT]: [
    WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
    WORKFLOW_TRANSITIONS.CANCEL,
  ],
  [WORKFLOW_STATES.COLLECTING_EVIDENCE]: [
    WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE,
    WORKFLOW_TRANSITIONS.CANCEL,
  ],
  [WORKFLOW_STATES.WAITING_APPROVAL]: [
    WORKFLOW_TRANSITIONS.APPROVE,
    WORKFLOW_TRANSITIONS.REJECT,
    WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
    WORKFLOW_TRANSITIONS.CANCEL,
  ],
  [WORKFLOW_STATES.APPROVED]: [
    WORKFLOW_TRANSITIONS.EXECUTE,
    WORKFLOW_TRANSITIONS.CANCEL,
  ],
  [WORKFLOW_STATES.EXECUTING]: [],
  [WORKFLOW_STATES.EXECUTED]: [],
  [WORKFLOW_STATES.REJECTED]: [],
  [WORKFLOW_STATES.CANCELED]: [],
  [WORKFLOW_STATES.FAILED]: [],
  [WORKFLOW_STATES.WAITING_REPAIR]: [],
};
