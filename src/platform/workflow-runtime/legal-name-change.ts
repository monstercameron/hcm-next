import {
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_TRANSITIONS,
  type WorkflowState,
  type WorkflowTransition,
} from "@hcm-next/foundation";
import { z } from "zod";

export const legalNameInputSchema = z.object({
  newLegalName: z.object({
    first: z.string().min(1),
    middle: z.string().nullable().optional(),
    last: z.string().min(1),
  }),
  effectiveAt: z.string().min(1),
  businessReason: z.enum([
    "marriage",
    "divorce",
    "legal_name_change",
    "correction",
    "other",
  ]),
});

export const provideEvidenceSchema = z.object({
  documentId: z.string().min(1),
});

export const approveSchema = z.object({
  approvalTaskId: z.string().min(1),
  comment: z.string().optional(),
});

export const rejectSchema = z.object({
  approvalTaskId: z.string().min(1),
  reason: z.string().min(1),
});

export const requestMoreInfoSchema = z.object({
  approvalTaskId: z.string().min(1),
  comment: z.string().min(1),
});

export type LegalNameInput = z.infer<typeof legalNameInputSchema>;
export type ProvideEvidenceInput = z.infer<typeof provideEvidenceSchema>;
export type ApproveInput = z.infer<typeof approveSchema>;
export type RejectInput = z.infer<typeof rejectSchema>;
export type RequestMoreInfoInput = z.infer<typeof requestMoreInfoSchema>;

export type WorkflowTransitionDefinition = {
  transition: WorkflowTransition;
  requiredPermission?: string;
  nextState?: WorkflowState;
};

export const legalNameTransitionMap: Record<
  WorkflowState,
  WorkflowTransitionDefinition[]
> = {
  [WORKFLOW_STATES.COLLECTING_INPUT]: [
    {
      transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_REQUEST,
      nextState: WORKFLOW_STATES.COLLECTING_EVIDENCE,
    },
    {
      transition: WORKFLOW_TRANSITIONS.CANCEL,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
      nextState: WORKFLOW_STATES.CANCELED,
    },
  ],
  [WORKFLOW_STATES.COLLECTING_EVIDENCE]: [
    {
      transition: WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE,
      nextState: WORKFLOW_STATES.WAITING_APPROVAL,
    },
    {
      transition: WORKFLOW_TRANSITIONS.CANCEL,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
      nextState: WORKFLOW_STATES.CANCELED,
    },
  ],
  [WORKFLOW_STATES.WAITING_APPROVAL]: [
    {
      transition: WORKFLOW_TRANSITIONS.APPROVE,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_APPROVE,
      nextState: WORKFLOW_STATES.APPROVED,
    },
    {
      transition: WORKFLOW_TRANSITIONS.REJECT,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
      nextState: WORKFLOW_STATES.REJECTED,
    },
    {
      transition: WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_APPROVE,
      nextState: WORKFLOW_STATES.COLLECTING_EVIDENCE,
    },
  ],
  [WORKFLOW_STATES.APPROVED]: [
    {
      transition: WORKFLOW_TRANSITIONS.EXECUTE,
      requiredPermission: PERMISSION_KEYS.LEGAL_NAME_EXECUTE,
      nextState: WORKFLOW_STATES.EXECUTED,
    },
  ],
  [WORKFLOW_STATES.EXECUTING]: [],
  [WORKFLOW_STATES.EXECUTED]: [],
  [WORKFLOW_STATES.REJECTED]: [],
  [WORKFLOW_STATES.CANCELED]: [],
  [WORKFLOW_STATES.FAILED]: [],
  [WORKFLOW_STATES.WAITING_REPAIR]: [],
};

export function createLegalNameFormInteraction(currentLegalName: unknown) {
  return {
    type: "form",
    schemaVersion: "v0.1",
    title: "Request legal name change",
    currentLegalName,
    jsonSchema: {
      type: "object",
      required: ["newLegalName", "effectiveAt", "businessReason"],
      properties: {
        newLegalName: {
          type: "object",
          required: ["first", "last"],
          properties: {
            first: { type: "string", minLength: 1 },
            middle: { type: ["string", "null"] },
            last: { type: "string", minLength: 1 },
          },
        },
        effectiveAt: { type: "string", format: "date" },
        businessReason: {
          type: "string",
          enum: ["marriage", "divorce", "legal_name_change", "correction", "other"],
        },
      },
    },
    uiSchema: {
      layout: "wizard",
      submitLabel: "Continue",
    },
  };
}

export const legalNameEvidenceInteraction = {
  type: "evidence_upload",
  title: "Upload legal name change evidence",
  description:
    "Upload a marriage certificate, court order, updated government ID, or other approved legal document.",
  acceptedDocumentTypes: [
    "marriage_certificate",
    "court_order",
    "government_id",
    "other_legal_document",
  ],
  maxFiles: 1,
};

export const legalNameWaitingInteraction = {
  type: "waiting",
  title: "Waiting for HR approval",
  description: "Your legal name change request has been submitted.",
};

export const legalNameReadyToExecuteInteraction = {
  type: "ready_to_execute",
  title: "Ready to execute legal name change",
  description: "The legal name change has been approved and is ready to execute.",
};

export const legalNameWorkflowDefinition = {
  workflowType: "employee_data_change",
  intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
  version: 1,
  initialState: WORKFLOW_STATES.COLLECTING_INPUT,
  transitions: legalNameTransitionMap,
};
