import type { Pool } from "pg";

import {
  fromPromise,
  mapUnknownToDatabaseError,
  ok,
  type Result,
} from "@hcm-next/foundation";

import { executeInTransaction, type DatabaseClient } from "../client";
import {
  DEMO_ACTORS,
  DEMO_EMPLOYEE_PROJECTION,
  DEMO_LEGAL_NAME_INPUT_SCHEMA,
  DEMO_PERMISSION_POLICIES,
  DEMO_SEED_IDS,
  DEMO_WORKFLOW_GRAPH,
  DEMO_WORKFLOW_OUTPUT_SCHEMA,
} from "./demo-seed-data";
import { DEMO_SEED_ALIASES } from "@hcm-next/foundation";

export type SeedSummary = {
  statementsApplied: number;
  tenantSlug: string;
  environmentName: string;
  employeeActorAlias: string;
  hrActorAlias: string;
  systemActorAlias: string;
  employeeId: string;
};

async function executeSeedStatement(
  database: DatabaseClient,
  sql: string,
  values: readonly unknown[],
): Promise<Result<void>> {
  const result = await fromPromise(
    async () => database.query(sql, values),
    mapUnknownToDatabaseError,
  );

  if (!result.ok) {
    return result;
  }

  return ok(undefined);
}

async function applyDemoSeed(database: DatabaseClient): Promise<Result<number>> {
  let statementsApplied = 0;

  const tenantResult = await executeSeedStatement(
    database,
    `
      INSERT INTO tenants (
        tenant_id,
        name,
        slug,
        status,
        default_timezone,
        default_locale,
        data_region,
        settings
      )
      VALUES ($1, $2, $3, 'active', 'America/New_York', 'en-US', 'US', $4::jsonb)
      ON CONFLICT (tenant_id) DO UPDATE
      SET name = EXCLUDED.name,
          slug = EXCLUDED.slug,
          status = EXCLUDED.status,
          default_timezone = EXCLUDED.default_timezone,
          default_locale = EXCLUDED.default_locale,
          data_region = EXCLUDED.data_region,
          settings = EXCLUDED.settings,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.TENANT_ID,
      "HCM Next Demo",
      DEMO_SEED_ALIASES.TENANT,
      {
        demo: true,
      },
    ],
  );

  if (!tenantResult.ok) {
    return tenantResult;
  }

  statementsApplied += 1;

  const environmentResult = await executeSeedStatement(
    database,
    `
      INSERT INTO environments (
        environment_id,
        tenant_id,
        name,
        type,
        status
      )
      VALUES ($1, $2, $3, 'development', 'active')
      ON CONFLICT (environment_id) DO UPDATE
      SET name = EXCLUDED.name,
          type = EXCLUDED.type,
          status = EXCLUDED.status,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.ENVIRONMENT_ID,
      DEMO_SEED_IDS.TENANT_ID,
      DEMO_SEED_ALIASES.ENVIRONMENT,
    ],
  );

  if (!environmentResult.ok) {
    return environmentResult;
  }

  statementsApplied += 1;

  for (const actor of DEMO_ACTORS) {
    const actorResult = await executeSeedStatement(
      database,
      `
        INSERT INTO actors (
          actor_id,
          tenant_id,
          actor_type,
          linked_worker_id,
          email,
          display_name,
          status,
          roles,
          groups,
          auth_provider,
          external_subject,
          metadata
        )
        VALUES (
          $1,
          $2,
          $3,
          $4,
          $5,
          $6,
          'active',
          $7::jsonb,
          '[]'::jsonb,
          'demo',
          $8,
          $9::jsonb
        )
        ON CONFLICT (actor_id) DO UPDATE
        SET actor_type = EXCLUDED.actor_type,
            linked_worker_id = EXCLUDED.linked_worker_id,
            email = EXCLUDED.email,
            display_name = EXCLUDED.display_name,
            status = EXCLUDED.status,
            roles = EXCLUDED.roles,
            groups = EXCLUDED.groups,
            auth_provider = EXCLUDED.auth_provider,
            external_subject = EXCLUDED.external_subject,
            metadata = EXCLUDED.metadata,
            updated_at = now()
      `,
      [
        actor.actorId,
        DEMO_SEED_IDS.TENANT_ID,
        actor.actorType,
        actor.linkedWorkerId,
        actor.email,
        actor.displayName,
        actor.roles,
        actor.externalSubject,
        {
          demoAlias: actor.externalSubject,
        },
      ],
    );

    if (!actorResult.ok) {
      return actorResult;
    }

    statementsApplied += 1;
  }

  const workflowDefinitionResult = await executeSeedStatement(
    database,
    `
      INSERT INTO workflow_definitions (
        workflow_definition_id,
        tenant_id,
        name,
        workflow_type,
        status,
        metadata
      )
      VALUES ($1, $2, $3, 'employee_data_change', 'active', $4::jsonb)
      ON CONFLICT (workflow_definition_id) DO UPDATE
      SET name = EXCLUDED.name,
          workflow_type = EXCLUDED.workflow_type,
          status = EXCLUDED.status,
          metadata = EXCLUDED.metadata,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.WORKFLOW_DEFINITION_ID,
      DEMO_SEED_IDS.TENANT_ID,
      DEMO_WORKFLOW_GRAPH.intent,
      {
        demo: true,
      },
    ],
  );

  if (!workflowDefinitionResult.ok) {
    return workflowDefinitionResult;
  }

  statementsApplied += 1;

  const workflowVersionResult = await executeSeedStatement(
    database,
    `
      INSERT INTO workflow_versions (
        workflow_version_id,
        tenant_id,
        workflow_definition_id,
        version_number,
        graph_definition,
        input_schema,
        output_schema,
        validation_rules,
        approval_rules,
        ai_review_scope,
        status,
        published_by,
        published_at
      )
      VALUES (
        $1,
        $2,
        $3,
        1,
        $4::jsonb,
        $5::jsonb,
        $6::jsonb,
        $7::jsonb,
        $8::jsonb,
        $9::jsonb,
        'published',
        $10,
        now()
      )
      ON CONFLICT (workflow_definition_id, version_number) DO UPDATE
      SET graph_definition = EXCLUDED.graph_definition,
          input_schema = EXCLUDED.input_schema,
          output_schema = EXCLUDED.output_schema,
          validation_rules = EXCLUDED.validation_rules,
          approval_rules = EXCLUDED.approval_rules,
          ai_review_scope = EXCLUDED.ai_review_scope,
          status = EXCLUDED.status,
          published_by = EXCLUDED.published_by,
          published_at = EXCLUDED.published_at,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.WORKFLOW_VERSION_ID,
      DEMO_SEED_IDS.TENANT_ID,
      DEMO_SEED_IDS.WORKFLOW_DEFINITION_ID,
      DEMO_WORKFLOW_GRAPH,
      DEMO_LEGAL_NAME_INPUT_SCHEMA,
      DEMO_WORKFLOW_OUTPUT_SCHEMA,
      [
        {
          blockName: "system.employee_data.legal_name.preflight",
          blockVersion: "1.0.0",
        },
      ],
      [
        {
          assigneeRole: "hr_admin",
          status: "pending",
        },
      ],
      {
        enabled: false,
      },
      DEMO_SEED_IDS.SYSTEM_ACTOR_ID,
    ],
  );

  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  statementsApplied += 1;

  const currentVersionResult = await executeSeedStatement(
    database,
    `
      UPDATE workflow_definitions
      SET current_version_id = $3,
          updated_at = now()
      WHERE tenant_id = $1
        AND workflow_definition_id = $2
    `,
    [
      DEMO_SEED_IDS.TENANT_ID,
      DEMO_SEED_IDS.WORKFLOW_DEFINITION_ID,
      DEMO_SEED_IDS.WORKFLOW_VERSION_ID,
    ],
  );

  if (!currentVersionResult.ok) {
    return currentVersionResult;
  }

  statementsApplied += 1;

  const employeeProjectionResult = await executeSeedStatement(
    database,
    `
      INSERT INTO employee_projection (
        tenant_id,
        employee_id,
        projection_version,
        document,
        indexed_fields
      )
      VALUES ($1, $2, 1, $3::jsonb, $4::jsonb)
      ON CONFLICT (tenant_id, employee_id) DO UPDATE
      SET projection_version = EXCLUDED.projection_version,
          document = EXCLUDED.document,
          indexed_fields = EXCLUDED.indexed_fields,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.TENANT_ID,
      DEMO_SEED_ALIASES.EMPLOYEE,
      DEMO_EMPLOYEE_PROJECTION,
      {
        displayName: DEMO_EMPLOYEE_PROJECTION.person.displayName,
        employmentStatus: DEMO_EMPLOYEE_PROJECTION.employment.status,
        legalEntity: DEMO_EMPLOYEE_PROJECTION.employment.legalEntity,
      },
    ],
  );

  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  statementsApplied += 1;

  for (const policy of DEMO_PERMISSION_POLICIES) {
    const policyResult = await executeSeedStatement(
      database,
      `
        INSERT INTO permission_policies (
          permission_policy_id,
          tenant_id,
          name,
          subject_scope,
          resource_scope,
          actions,
          field_permissions,
          effect,
          priority,
          status
        )
        VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, 'active')
        ON CONFLICT (permission_policy_id) DO UPDATE
        SET name = EXCLUDED.name,
            subject_scope = EXCLUDED.subject_scope,
            resource_scope = EXCLUDED.resource_scope,
            actions = EXCLUDED.actions,
            field_permissions = EXCLUDED.field_permissions,
            effect = EXCLUDED.effect,
            priority = EXCLUDED.priority,
            status = EXCLUDED.status,
            updated_at = now()
      `,
      [
        policy.permissionPolicyId,
        DEMO_SEED_IDS.TENANT_ID,
        policy.name,
        policy.subjectScope,
        policy.resourceScope,
        policy.actions,
        policy.fieldPermissions,
        policy.effect,
        policy.priority,
      ],
    );

    if (!policyResult.ok) {
      return policyResult;
    }

    statementsApplied += 1;
  }

  const legalNameFieldResult = await executeSeedStatement(
    database,
    `
      INSERT INTO metadata_field_definitions (
        metadata_field_id,
        tenant_id,
        object_type,
        field_key,
        label,
        data_type,
        required,
        sensitive,
        searchable,
        effective_dated,
        permission_tags,
        status
      )
      VALUES ($1, $2, 'person', 'legalName', 'Legal name', 'object', true, true, true, true, $3::jsonb, 'active')
      ON CONFLICT (tenant_id, object_type, field_key) DO UPDATE
      SET label = EXCLUDED.label,
          data_type = EXCLUDED.data_type,
          required = EXCLUDED.required,
          sensitive = EXCLUDED.sensitive,
          searchable = EXCLUDED.searchable,
          effective_dated = EXCLUDED.effective_dated,
          permission_tags = EXCLUDED.permission_tags,
          status = EXCLUDED.status,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.LEGAL_NAME_FIELD_ID,
      DEMO_SEED_IDS.TENANT_ID,
      ["person_identity", "legal_name"],
    ],
  );

  if (!legalNameFieldResult.ok) {
    return legalNameFieldResult;
  }

  statementsApplied += 1;

  const evidenceFieldResult = await executeSeedStatement(
    database,
    `
      INSERT INTO metadata_field_definitions (
        metadata_field_id,
        tenant_id,
        object_type,
        field_key,
        label,
        data_type,
        required,
        sensitive,
        searchable,
        effective_dated,
        permission_tags,
        status
      )
      VALUES ($1, $2, 'document', 'legalNameEvidence', 'Legal name evidence', 'reference', true, true, false, false, $3::jsonb, 'active')
      ON CONFLICT (tenant_id, object_type, field_key) DO UPDATE
      SET label = EXCLUDED.label,
          data_type = EXCLUDED.data_type,
          required = EXCLUDED.required,
          sensitive = EXCLUDED.sensitive,
          searchable = EXCLUDED.searchable,
          effective_dated = EXCLUDED.effective_dated,
          permission_tags = EXCLUDED.permission_tags,
          status = EXCLUDED.status,
          updated_at = now()
    `,
    [
      DEMO_SEED_IDS.EVIDENCE_FIELD_ID,
      DEMO_SEED_IDS.TENANT_ID,
      ["person_identity", "evidence"],
    ],
  );

  if (!evidenceFieldResult.ok) {
    return evidenceFieldResult;
  }

  statementsApplied += 1;

  return ok(statementsApplied);
}

/**
 * Seeds deterministic demo tenant, actors, workflow config, permissions, and employee projection.
 */
export async function runDemoSeed(pool: Pool): Promise<Result<SeedSummary>> {
  const transactionResult = await executeInTransaction(pool, applyDemoSeed);

  if (!transactionResult.ok) {
    return transactionResult;
  }

  return ok({
    statementsApplied: transactionResult.value,
    tenantSlug: DEMO_SEED_ALIASES.TENANT,
    environmentName: DEMO_SEED_ALIASES.ENVIRONMENT,
    employeeActorAlias: DEMO_SEED_ALIASES.EMPLOYEE_ACTOR,
    hrActorAlias: DEMO_SEED_ALIASES.HR_ACTOR,
    systemActorAlias: DEMO_SEED_ALIASES.SYSTEM_ACTOR,
    employeeId: DEMO_SEED_ALIASES.EMPLOYEE,
  });
}
