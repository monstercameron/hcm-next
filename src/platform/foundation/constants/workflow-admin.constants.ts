import type { ValueOf } from "../domain";

export const WORKFLOW_ADMIN_FAMILY_STATUSES = {
  ACTIVE: "active",
  ARCHIVED: "archived",
} as const;

export type WorkflowAdminFamilyStatus = ValueOf<typeof WORKFLOW_ADMIN_FAMILY_STATUSES>;

export const WORKFLOW_ADMIN_DRAFT_STATUSES = {
  DRAFT: "draft",
  READY_FOR_REVIEW: "ready_for_review",
  REJECTED: "rejected",
  PUBLISHED: "published",
} as const;

export type WorkflowAdminDraftStatus = ValueOf<typeof WORKFLOW_ADMIN_DRAFT_STATUSES>;

export const WORKFLOW_ADMIN_VERSION_STATUSES = {
  PUBLISHED: "published",
  INACTIVE: "inactive",
} as const;

export type WorkflowAdminVersionStatus = ValueOf<
  typeof WORKFLOW_ADMIN_VERSION_STATUSES
>;

export const WORKFLOW_ADMIN_TEMPLATE_STATUSES = {
  ACTIVE: "active",
  ARCHIVED: "archived",
} as const;

export type WorkflowAdminTemplateStatus = ValueOf<
  typeof WORKFLOW_ADMIN_TEMPLATE_STATUSES
>;

export const WORKFLOW_ADMIN_PUBLISH_ACTIONS = {
  PUBLISHED: "published",
  ROLLED_BACK: "rolled_back",
} as const;

export type WorkflowAdminPublishAction = ValueOf<typeof WORKFLOW_ADMIN_PUBLISH_ACTIONS>;

export const WORKFLOW_CONFIG_SOURCES = {
  DATABASE: "database",
  FILESYSTEM: "filesystem",
  TEMPLATE: "template",
  PUBLISHED_VERSION: "published_version",
  API: "api",
} as const;

export type WorkflowConfigSource = ValueOf<typeof WORKFLOW_CONFIG_SOURCES>;
