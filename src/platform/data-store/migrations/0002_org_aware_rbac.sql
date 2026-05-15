CREATE TABLE organization_units (
  org_unit_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  unit_key text NOT NULL,
  type text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  country text,
  jurisdiction text,
  effective_start timestamptz NOT NULL DEFAULT now(),
  effective_end timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT organization_units_status_check
    CHECK (status IN ('proposed', 'pending_approval', 'active', 'suspended', 'expired', 'revoked', 'superseded', 'inactive')),
  CONSTRAINT organization_units_effective_range_check
    CHECK (effective_end IS NULL OR effective_end > effective_start),
  CONSTRAINT organization_units_key_unique
    UNIQUE (tenant_id, unit_key)
);

CREATE INDEX organization_units_tenant_type_idx
  ON organization_units (tenant_id, type, status);

CREATE INDEX organization_units_metadata_gin_idx
  ON organization_units USING gin (metadata);

CREATE TABLE organization_relationships (
  organization_relationship_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  from_org_unit_id uuid NOT NULL REFERENCES organization_units(org_unit_id),
  to_org_unit_id uuid NOT NULL REFERENCES organization_units(org_unit_id),
  relationship_type text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  effective_start timestamptz NOT NULL DEFAULT now(),
  effective_end timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT organization_relationships_status_check
    CHECK (status IN ('proposed', 'pending_approval', 'active', 'suspended', 'expired', 'revoked', 'superseded', 'inactive')),
  CONSTRAINT organization_relationships_effective_range_check
    CHECK (effective_end IS NULL OR effective_end > effective_start),
  CONSTRAINT organization_relationships_self_check
    CHECK (from_org_unit_id <> to_org_unit_id),
  CONSTRAINT organization_relationships_unique
    UNIQUE (tenant_id, from_org_unit_id, to_org_unit_id, relationship_type, effective_start)
);

CREATE INDEX organization_relationships_from_idx
  ON organization_relationships (tenant_id, from_org_unit_id, relationship_type, status);

CREATE INDEX organization_relationships_to_idx
  ON organization_relationships (tenant_id, to_org_unit_id, relationship_type, status);

CREATE TABLE worker_assignments (
  worker_assignment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  employee_id text NOT NULL,
  org_unit_id uuid NOT NULL REFERENCES organization_units(org_unit_id),
  assignment_type text NOT NULL,
  role_type text,
  manager_employee_id text,
  allocation_percent numeric(5,2) NOT NULL DEFAULT 100,
  status text NOT NULL DEFAULT 'active',
  effective_start timestamptz NOT NULL DEFAULT now(),
  effective_end timestamptz,
  source_workflow_instance_id uuid REFERENCES workflow_instances(workflow_instance_id),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT worker_assignments_status_check
    CHECK (status IN ('proposed', 'pending_approval', 'active', 'suspended', 'expired', 'revoked', 'superseded')),
  CONSTRAINT worker_assignments_allocation_check
    CHECK (allocation_percent > 0 AND allocation_percent <= 100),
  CONSTRAINT worker_assignments_effective_range_check
    CHECK (effective_end IS NULL OR effective_end > effective_start)
);

CREATE INDEX worker_assignments_employee_idx
  ON worker_assignments (tenant_id, employee_id, assignment_type, status);

CREATE INDEX worker_assignments_org_unit_idx
  ON worker_assignments (tenant_id, org_unit_id, assignment_type, status);

CREATE INDEX worker_assignments_manager_idx
  ON worker_assignments (tenant_id, manager_employee_id, status)
  WHERE manager_employee_id IS NOT NULL;

CREATE INDEX worker_assignments_metadata_gin_idx
  ON worker_assignments USING gin (metadata);

CREATE TABLE role_bindings (
  role_binding_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  actor_id uuid NOT NULL REFERENCES actors(actor_id),
  role_key text NOT NULL,
  scope_type text NOT NULL,
  scope_org_unit_id uuid REFERENCES organization_units(org_unit_id),
  scope_value text,
  relationship_type text,
  status text NOT NULL DEFAULT 'active',
  effective_start timestamptz NOT NULL DEFAULT now(),
  effective_end timestamptz,
  source_workflow_instance_id uuid REFERENCES workflow_instances(workflow_instance_id),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT role_bindings_status_check
    CHECK (status IN ('proposed', 'pending_approval', 'active', 'suspended', 'expired', 'revoked', 'superseded')),
  CONSTRAINT role_bindings_scope_type_check
    CHECK (scope_type IN ('self', 'direct_reports', 'manager_chain', 'org_unit', 'org_unit_descendants', 'legal_entity', 'location', 'country', 'cost_center', 'project', 'assignment', 'workflow_instance', 'global')),
  CONSTRAINT role_bindings_effective_range_check
    CHECK (effective_end IS NULL OR effective_end > effective_start)
);

CREATE INDEX role_bindings_actor_idx
  ON role_bindings (tenant_id, actor_id, status, effective_start DESC);

CREATE INDEX role_bindings_role_idx
  ON role_bindings (tenant_id, role_key, status);

CREATE INDEX role_bindings_scope_org_unit_idx
  ON role_bindings (tenant_id, scope_org_unit_id, status)
  WHERE scope_org_unit_id IS NOT NULL;

CREATE INDEX role_bindings_metadata_gin_idx
  ON role_bindings USING gin (metadata);
