import type { CurrentInteraction, JsonObject, LegalName } from "../domain";

export const LEGAL_NAME_FORM_SCHEMA_VERSION = "2026-05-15.1";

export function legalNameInputInteraction(errors?: JsonObject): CurrentInteraction {
  const interaction: CurrentInteraction = {
    type: "form",
    schemaVersion: LEGAL_NAME_FORM_SCHEMA_VERSION,
    title: "Request legal name change",
    description: "Enter the legal name that should be used in HR records.",
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
        businessReason: { type: "string", minLength: 1 },
      },
    },
  };

  if (errors !== undefined) {
    interaction.errors = errors;
  }

  return interaction;
}

export function evidenceUploadInteraction(): CurrentInteraction {
  return {
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
}

export function waitingApprovalInteraction(): CurrentInteraction {
  return {
    type: "waiting",
    title: "Waiting for HR approval",
    description: "Your legal name change request has been submitted.",
  };
}

export function approvedInteraction(): CurrentInteraction {
  return {
    type: "ready_to_execute",
    title: "Legal name change approved",
    description: "The approved change is ready to execute.",
  };
}

export function rejectedInteraction(reason?: string): CurrentInteraction {
  return {
    type: "confirmation",
    title: "Legal name change rejected",
    description: reason ?? "The legal name change request was rejected.",
  };
}

export function canceledInteraction(): CurrentInteraction {
  return {
    type: "confirmation",
    title: "Legal name change canceled",
    description: "The legal name change request was canceled.",
  };
}

export function executedInteraction(
  previousLegalName: LegalName,
  newLegalName: LegalName,
): CurrentInteraction {
  return {
    type: "confirmation",
    title: "Legal name change completed",
    description: `${formatLegalName(previousLegalName)} is now ${formatLegalName(
      newLegalName,
    )}.`,
  };
}

export function failedInteraction(): CurrentInteraction {
  return {
    type: "failure",
    title: "Workflow failed",
    description: "The workflow needs operational review before it can continue.",
  };
}

function formatLegalName(legalName: LegalName): string {
  return [legalName.first, legalName.middle, legalName.last].filter(Boolean).join(" ");
}
