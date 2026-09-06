-- DRAFT MIGRATION FOR REVIEW. NOT numbered into migrations/ and not applied
-- by goose. Companion to planning/research/organization-structure-maximal-2026.md,
-- which explains every design choice below in prose.
--
-- Scope: the maximal (worst-case) organizational shape a customer of this
-- HCM/payroll platform can have -- a platform-tenant HR service provider
-- (PEO/ASO/EOR/staffing/subcontracted HR) serving many client organizations,
-- each owning many businesses, legal entities, shells, holding companies,
-- joint ventures, franchises, brands and trade names, across countries and
-- states, with co-employment, shared services, matrix reporting, benefits
-- and payroll grouping that cross entities, and billing that does not equal
-- usage.
--
-- Relationship to existing tables. migrations/00012_organization_aggregates.sql
-- already defines a flat, tenant-scoped `legal_entity` and a single-parent
-- `organization_unit` tree; these are the P1A minimal aggregates and are not
-- edited here. This draft adds a fuller structural layer beside them:
--   - legal_entity_structure_revision et al. carry the corporate/legal facts
--     00012's legal_entity does not (entity type, jurisdiction, dissolution,
--     ownership DAG, tax registrations) and are keyed by their own
--     legal_entity_id rather than 00012's aggregate_entity.entity_id, because
--     the maximal case includes entities (shells, JVs, franchise units, trade
--     names) that may never need a full people/position aggregate presence.
--     Reconciling the two identity spaces is left as an open question in the
--     companion document rather than decided unilaterally here.
--   - management_unit_revision and cost_center_revision split 00012's single
--     organization_unit tree (which mixes BUSINESS_UNIT/DIVISION/DEPARTMENT/
--     TEAM/COST_CENTER in one parent-pointer tree) into two independent
--     trees, per this task's design principle that the management dimension
--     and the cost/financial dimension are different questions that happen
--     to often draw the same lines.
--
-- House conventions followed throughout (see migrations/00002, 00008, 00011,
-- 00012, 00050, 00061..00064, 00072, 00076, 00078):
--   - tenant_ref, semantic_key, cas_version, digest_algorithm, content_digest
--     domains are reused as-is (defined in 00002_tenant_primitives.sql).
--   - Every tenant-scoped table carries tenant_id NOT NULL, has
--     ENABLE/FORCE ROW LEVEL SECURITY and a `tenant_isolation` policy against
--     NULLIF(current_setting('app.tenant_id', true), '')::uuid, and grants
--     hcmnext_app only the statements it needs -- never DELETE, matching
--     00008's "this data plane has no delete semantics, only append and
--     state transition."
--   - Structural facts are append-only revision chains: one row per
--     revision, revision cas_version, parent_revision/parent_digest lineage,
--     effective_from/effective_to half-open business time, recorded_at
--     system time, a content_digest, and a forbid_mutation trigger (defined
--     once, in migrations/00001_platform_control.sql, and reused here by
--     name -- it is not redefined).
--   - Two tables (tenant_relationship_revision, cross_tenant_identity_link_
--     revision) genuinely span two tenants and cannot carry one tenant_id
--     column; their RLS policy and this fact are called out explicitly at
--     the point of definition and in the companion document's "hardest
--     decisions" section.
--   - Deep trees (legal entity ownership, management hierarchy, cost
--     centers) each get a companion closure table for O(1) ancestor/
--     descendant reads, and a companion "current" pointer table mirroring
--     migrations/00003_definition_registry.sql's definition_active_pointer:
--     REBUILDABLE, "a convenience that can never invent a version."
--   - Relationship kinds are closed CHECK vocabularies, not free text.

-- +goose Up

-- =============================================================================
-- SECTION 1: Legal entity tree (corporate/statutory structure)
-- =============================================================================

-- legal_entity_structure_revision: the corporate/statutory facts about one
-- legal entity across its life -- what kind of entity it is, where it is
-- incorporated, and how/when it stopped existing. legal_entity_id is a new,
-- freestanding identity (see header note); it is not the same id space as
-- 00012's legal_entity.entity_id.
CREATE TABLE IF NOT EXISTS legal_entity_structure_revision (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    legal_entity_id  uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    registered_name  text           NOT NULL,
    entity_type      text           NOT NULL,
    jurisdiction_country    text    NOT NULL,
    jurisdiction_subdivision text,
    incorporation_date      date,
    dissolution_date        date,
    lifecycle_state  text           NOT NULL,
    successor_entity_id     uuid,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_structure_revision_identity
        UNIQUE (tenant_id, legal_entity_id, revision),
    CONSTRAINT legal_entity_structure_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT legal_entity_structure_revision_entity_type_allowed CHECK (
        entity_type IN (
            'ENTERPRISE_GROUP', 'HOLDING_COMPANY', 'OPERATING_COMPANY',
            'SUBSIDIARY', 'SHELL_COMPANY', 'JOINT_VENTURE',
            'FRANCHISE_UNIT', 'BRANCH', 'PROFESSIONAL_EMPLOYER_ORGANIZATION',
            'STAFFING_AGENCY', 'NONPROFIT_AFFILIATE'
        )
    ),
    CONSTRAINT legal_entity_structure_revision_lifecycle_allowed CHECK (
        lifecycle_state IN ('ACTIVE', 'DORMANT', 'DIVESTED', 'MERGED_OUT', 'DISSOLVED')
    ),
    CONSTRAINT legal_entity_structure_revision_dissolution_requires_state CHECK (
        (dissolution_date IS NULL) OR (lifecycle_state IN ('DIVESTED', 'MERGED_OUT', 'DISSOLVED'))
    ),
    CONSTRAINT legal_entity_structure_revision_successor_requires_exit CHECK (
        (successor_entity_id IS NULL) OR (lifecycle_state IN ('MERGED_OUT', 'DISSOLVED'))
    ),
    CONSTRAINT legal_entity_structure_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS legal_entity_structure_revision_history
    ON legal_entity_structure_revision (tenant_id, legal_entity_id, revision DESC);

CREATE OR REPLACE TRIGGER legal_entity_structure_revision_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_structure_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_structure_revision FROM PUBLIC;

ALTER TABLE legal_entity_structure_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_structure_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_structure_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_structure_revision TO hcmnext_app;

-- legal_entity_structure_pointer: REBUILDABLE convenience naming the current
-- (latest, live) revision per legal_entity_id, exactly like
-- definition_active_pointer -- "a convenience that can never invent a
-- version." rebuild_source is legal_entity_structure_revision.
CREATE TABLE IF NOT EXISTS legal_entity_structure_pointer (
    tenant_id           tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    legal_entity_id     uuid       NOT NULL,
    current_row_id      uuid       NOT NULL,
    current_revision    cas_version NOT NULL,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, legal_entity_id),
    FOREIGN KEY (tenant_id, current_row_id) REFERENCES legal_entity_structure_revision (tenant_id, row_id)
);
ALTER TABLE legal_entity_structure_pointer ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_structure_pointer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_structure_pointer
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON legal_entity_structure_pointer TO hcmnext_app;

-- legal_entity_trade_name_revision: a DBA/brand/trade name asserted by one
-- legal entity, scoped to the jurisdiction where it is registered/used. A
-- brand is very often not its own legal entity; this is how "brand" is
-- represented without inventing a fictitious entity for it.
CREATE TABLE IF NOT EXISTS legal_entity_trade_name_revision (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid           NOT NULL,
    trade_name_id         uuid           NOT NULL,
    legal_entity_id       uuid           NOT NULL,
    revision              cas_version    NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    trade_name            text           NOT NULL,
    jurisdiction_country  text           NOT NULL,
    jurisdiction_subdivision text,
    effective_from        timestamptz    NOT NULL,
    effective_to          timestamptz,
    recorded_at           timestamptz    NOT NULL DEFAULT now(),
    digest                content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_trade_name_revision_identity
        UNIQUE (tenant_id, trade_name_id, revision),
    CONSTRAINT legal_entity_trade_name_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT legal_entity_trade_name_revision_not_blank CHECK (btrim(trade_name) <> ''),
    CONSTRAINT legal_entity_trade_name_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS legal_entity_trade_name_revision_by_entity
    ON legal_entity_trade_name_revision (tenant_id, legal_entity_id);

CREATE OR REPLACE TRIGGER legal_entity_trade_name_revision_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_trade_name_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_trade_name_revision FROM PUBLIC;

ALTER TABLE legal_entity_trade_name_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_trade_name_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_trade_name_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_trade_name_revision TO hcmnext_app;

-- legal_entity_ownership_edge: the corporate ownership/control DAG. Unlike
-- the management tree below, a legal entity can have more than one live
-- parent edge at once (a joint venture is owned by two parents; a franchise
-- unit is both locally owned and franchise-bound to a franchisor), so this
-- is a graph, not a single-parent tree, and gets a cycle-detecting trigger
-- that walks edges rather than a parent column.
CREATE TABLE IF NOT EXISTS legal_entity_ownership_edge (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid           NOT NULL,
    edge_id             uuid           NOT NULL,
    revision            cas_version    NOT NULL,
    parent_revision     cas_version,
    parent_digest       content_digest,
    parent_entity_id    uuid           NOT NULL,
    child_entity_id     uuid           NOT NULL,
    relationship_kind   text           NOT NULL,
    ownership_percentage numeric(6, 3),
    effective_from      timestamptz    NOT NULL,
    effective_to        timestamptz,
    recorded_at         timestamptz    NOT NULL DEFAULT now(),
    digest              content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_ownership_edge_identity
        UNIQUE (tenant_id, edge_id, revision),
    CONSTRAINT legal_entity_ownership_edge_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT legal_entity_ownership_edge_not_self CHECK (parent_entity_id <> child_entity_id),
    CONSTRAINT legal_entity_ownership_edge_kind_allowed CHECK (
        relationship_kind IN (
            'WHOLLY_OWNED_SUBSIDIARY', 'MAJORITY_OWNED_SUBSIDIARY', 'MINORITY_INVESTMENT',
            'JOINT_VENTURE_PARTNER', 'FRANCHISE_AGREEMENT', 'BRAND_LICENSE',
            'MANAGEMENT_AGREEMENT', 'HOLDING_STRUCTURE'
        )
    ),
    CONSTRAINT legal_entity_ownership_edge_percentage_range CHECK (
        ownership_percentage IS NULL OR (ownership_percentage >= 0 AND ownership_percentage <= 100)
    ),
    CONSTRAINT legal_entity_ownership_edge_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS legal_entity_ownership_edge_parent
    ON legal_entity_ownership_edge (tenant_id, parent_entity_id) WHERE effective_to IS NULL;
CREATE INDEX IF NOT EXISTS legal_entity_ownership_edge_child
    ON legal_entity_ownership_edge (tenant_id, child_entity_id) WHERE effective_to IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION legal_entity_ownership_edge_forbid_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    hit boolean;
BEGIN
    IF NEW.effective_to IS NOT NULL THEN
        RETURN NEW;
    END IF;
    -- Walk forward from the new child; if any live path reaches back to the
    -- new parent, this edge would close a cycle in the ownership DAG.
    WITH RECURSIVE reachable(entity_id, hops) AS (
        SELECT NEW.child_entity_id, 0
        UNION ALL
        SELECT e.child_entity_id, r.hops + 1
        FROM legal_entity_ownership_edge e
        JOIN reachable r ON e.parent_entity_id = r.entity_id
        WHERE e.tenant_id = NEW.tenant_id AND e.effective_to IS NULL AND r.hops < 100000
    )
    SELECT true INTO hit FROM reachable WHERE entity_id = NEW.parent_entity_id LIMIT 1;

    IF hit THEN
        RAISE EXCEPTION
            'legal_entity_ownership_edge %: adding % -> % would close a cycle in the ownership graph',
            NEW.edge_id, NEW.parent_entity_id, NEW.child_entity_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
COMMENT ON FUNCTION legal_entity_ownership_edge_forbid_cycle() IS
    'The corporate ownership graph is a DAG (a joint venture may have two live parents); no live path may loop back to an ancestor.';

CREATE OR REPLACE TRIGGER legal_entity_ownership_edge_forbid_cycle_trigger
    BEFORE INSERT ON legal_entity_ownership_edge
    FOR EACH ROW EXECUTE FUNCTION legal_entity_ownership_edge_forbid_cycle();
CREATE OR REPLACE TRIGGER legal_entity_ownership_edge_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_ownership_edge
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_ownership_edge FROM PUBLIC;

ALTER TABLE legal_entity_ownership_edge ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_ownership_edge FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_ownership_edge
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_ownership_edge TO hcmnext_app;

-- legal_entity_closure: REBUILDABLE ancestor/descendant closure over live
-- legal_entity_ownership_edge rows, so "every entity under Holding Co X" is
-- an index lookup rather than a recursive query at read time.
-- rebuild_source: legal_entity_ownership_edge.
CREATE TABLE IF NOT EXISTS legal_entity_closure (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    ancestor_id     uuid       NOT NULL,
    descendant_id   uuid       NOT NULL,
    depth           int        NOT NULL,
    computed_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, ancestor_id, descendant_id),
    CONSTRAINT legal_entity_closure_depth_nonnegative CHECK (depth >= 0)
);
CREATE INDEX IF NOT EXISTS legal_entity_closure_descendant
    ON legal_entity_closure (tenant_id, descendant_id);
ALTER TABLE legal_entity_closure ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_closure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_closure
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON legal_entity_closure TO hcmnext_app;

-- legal_entity_event: the typed lifecycle log for events that are more than
-- "a new revision exists" -- incorporation, rename, reparenting, merger,
-- divestiture, dissolution and reinstatement each carry their own evidence
-- and are queried as a timeline independent of the revision chain.
CREATE TABLE IF NOT EXISTS legal_entity_event (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    legal_entity_id    uuid       NOT NULL,
    event_kind         text       NOT NULL,
    event_sequence     bigint     NOT NULL,
    occurred_at        timestamptz NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    actor_principal_id text,
    reason             text,
    evidence_ref       text,
    related_entity_id  uuid,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_event_sequence_unique UNIQUE (tenant_id, legal_entity_id, event_sequence),
    CONSTRAINT legal_entity_event_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT legal_entity_event_kind_allowed CHECK (
        event_kind IN (
            'INCORPORATED', 'RENAMED', 'REPARENTED', 'MERGED_INTO',
            'ACQUIRED', 'DIVESTED', 'DISSOLVED', 'REINSTATED'
        )
    )
);
CREATE OR REPLACE TRIGGER legal_entity_event_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_event FROM PUBLIC;

ALTER TABLE legal_entity_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_event TO hcmnext_app;

-- legal_entity_tax_registration_revision: one governed tax registration for
-- one legal entity in one jurisdiction (federal EIN, state withholding,
-- state unemployment insurance account, local tax, VAT/GST, PAYE reference,
-- social insurance number). Following internal/domains/taxprofile's
-- convention, the raw registration number is never stored here -- only a
-- governed reference to it, plus the pay agent responsible for filing.
CREATE TABLE IF NOT EXISTS legal_entity_tax_registration_revision (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id               uuid           NOT NULL,
    registration_id      uuid           NOT NULL,
    legal_entity_id      uuid           NOT NULL,
    revision             cas_version    NOT NULL,
    parent_revision      cas_version,
    parent_digest        content_digest,
    registration_kind    text           NOT NULL,
    jurisdiction_country  text          NOT NULL,
    jurisdiction_subdivision text,
    registration_id_ref  text           NOT NULL,
    pay_agent_id         uuid,
    status               text           NOT NULL,
    effective_from       timestamptz    NOT NULL,
    effective_to         timestamptz,
    recorded_at          timestamptz    NOT NULL DEFAULT now(),
    digest               content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_tax_registration_revision_identity
        UNIQUE (tenant_id, registration_id, revision),
    CONSTRAINT legal_entity_tax_registration_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT legal_entity_tax_registration_revision_kind_allowed CHECK (
        registration_kind IN (
            'FEDERAL_EIN', 'STATE_WITHHOLDING', 'STATE_UNEMPLOYMENT_INSURANCE',
            'LOCAL_TAX', 'VAT_GST', 'PAYE_REFERENCE', 'SOCIAL_INSURANCE', 'OTHER_STATUTORY'
        )
    ),
    CONSTRAINT legal_entity_tax_registration_revision_status_allowed CHECK (
        status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'CLOSED')
    ),
    CONSTRAINT legal_entity_tax_registration_revision_ref_not_blank CHECK (btrim(registration_id_ref) <> ''),
    CONSTRAINT legal_entity_tax_registration_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS legal_entity_tax_registration_revision_by_entity
    ON legal_entity_tax_registration_revision (tenant_id, legal_entity_id, jurisdiction_country, jurisdiction_subdivision);

CREATE OR REPLACE TRIGGER legal_entity_tax_registration_revision_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_tax_registration_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_tax_registration_revision FROM PUBLIC;

ALTER TABLE legal_entity_tax_registration_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_tax_registration_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_tax_registration_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_tax_registration_revision TO hcmnext_app;

-- legal_entity_data_residency_revision: the privacy/controller-processor
-- posture and required storage region for one legal entity's workforce data
-- (GDPR controller/joint-controller/processor roles; a subsidiary's data may
-- need to reside in-region even though the platform tenant is global).
CREATE TABLE IF NOT EXISTS legal_entity_data_residency_revision (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid           NOT NULL,
    residency_id        uuid           NOT NULL,
    legal_entity_id     uuid           NOT NULL,
    revision            cas_version    NOT NULL,
    parent_revision     cas_version,
    parent_digest       content_digest,
    required_region     text           NOT NULL,
    controller_role     text           NOT NULL,
    controller_party_id uuid,
    effective_from      timestamptz    NOT NULL,
    effective_to        timestamptz,
    recorded_at         timestamptz    NOT NULL DEFAULT now(),
    digest              content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_data_residency_revision_identity
        UNIQUE (tenant_id, residency_id, revision),
    CONSTRAINT legal_entity_data_residency_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT legal_entity_data_residency_revision_role_allowed CHECK (
        controller_role IN ('CONTROLLER', 'JOINT_CONTROLLER', 'PROCESSOR')
    ),
    CONSTRAINT legal_entity_data_residency_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE OR REPLACE TRIGGER legal_entity_data_residency_revision_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_data_residency_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_data_residency_revision FROM PUBLIC;

ALTER TABLE legal_entity_data_residency_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_data_residency_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_data_residency_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_data_residency_revision TO hcmnext_app;

-- legal_entity_worksite_assignment: which physical worksites
-- (location.worksite_revision) a legal entity operates out of, effective-
-- dated. Backs ACA ALE aggregation and state-unemployment-insurance
-- worksite reporting, which count locations under a controlled group across
-- entity lines.
CREATE TABLE IF NOT EXISTS legal_entity_worksite_assignment (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    assignment_id    uuid           NOT NULL,
    legal_entity_id  uuid           NOT NULL,
    worksite_id      text           NOT NULL,
    revision         cas_version    NOT NULL,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_entity_worksite_assignment_identity
        UNIQUE (tenant_id, assignment_id, revision),
    CONSTRAINT legal_entity_worksite_assignment_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS legal_entity_worksite_assignment_by_worksite
    ON legal_entity_worksite_assignment (tenant_id, worksite_id);

CREATE OR REPLACE TRIGGER legal_entity_worksite_assignment_append_only
    BEFORE UPDATE OR DELETE ON legal_entity_worksite_assignment
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_entity_worksite_assignment FROM PUBLIC;

ALTER TABLE legal_entity_worksite_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_worksite_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity_worksite_assignment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_entity_worksite_assignment TO hcmnext_app;

-- =============================================================================
-- SECTION 2: Pay agents and payroll grouping (crosses legal entities)
-- =============================================================================

-- pay_agent_revision: who actually remits withheld tax and files on behalf
-- of a legal entity in a jurisdiction -- itself, a common paymaster
-- affiliate (IRC 3121(s)), a PEO acting as pay agent, or a third-party
-- payroll provider. Referenced by legal_entity_tax_registration_revision
-- and payroll_group_revision.
CREATE TABLE IF NOT EXISTS pay_agent_revision (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    pay_agent_id     uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    agent_kind       text           NOT NULL,
    agent_legal_entity_id uuid,
    external_provider_ref text,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT pay_agent_revision_identity UNIQUE (tenant_id, pay_agent_id, revision),
    CONSTRAINT pay_agent_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT pay_agent_revision_kind_allowed CHECK (
        agent_kind IN (
            'SELF', 'COMMON_PAYMASTER', 'PEO_PAY_AGENT',
            'THIRD_PARTY_PAYROLL_PROVIDER', 'REPORTING_AGENT'
        )
    ),
    CONSTRAINT pay_agent_revision_self_needs_entity CHECK (
        (agent_kind <> 'SELF') OR (agent_legal_entity_id IS NOT NULL)
    ),
    CONSTRAINT pay_agent_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE OR REPLACE TRIGGER pay_agent_revision_append_only
    BEFORE UPDATE OR DELETE ON pay_agent_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON pay_agent_revision FROM PUBLIC;

ALTER TABLE pay_agent_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE pay_agent_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pay_agent_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON pay_agent_revision TO hcmnext_app;

-- payroll_group_revision: a pay-frequency/process grouping (e.g. "US
-- biweekly hourly") that a payroll run executes against. A payroll group
-- can and often does span more than one legal entity (a common paymaster
-- arrangement runs several affiliates through one group).
CREATE TABLE IF NOT EXISTS payroll_group_revision (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    payroll_group_id uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    name             text           NOT NULL,
    pay_frequency    text           NOT NULL,
    primary_pay_agent_id uuid,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payroll_group_revision_identity UNIQUE (tenant_id, payroll_group_id, revision),
    CONSTRAINT payroll_group_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT payroll_group_revision_frequency_allowed CHECK (
        pay_frequency IN ('WEEKLY', 'BIWEEKLY', 'SEMIMONTHLY', 'MONTHLY', 'FOUR_WEEKLY', 'IRREGULAR')
    ),
    CONSTRAINT payroll_group_revision_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT payroll_group_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE OR REPLACE TRIGGER payroll_group_revision_append_only
    BEFORE UPDATE OR DELETE ON payroll_group_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON payroll_group_revision FROM PUBLIC;

ALTER TABLE payroll_group_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll_group_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payroll_group_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON payroll_group_revision TO hcmnext_app;

-- payroll_group_legal_entity: which legal entities one payroll group covers,
-- effective-dated (an entity can join or leave a shared payroll group, e.g.
-- when a common-paymaster designation starts or ends).
CREATE TABLE IF NOT EXISTS payroll_group_legal_entity (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    membership_id    uuid           NOT NULL,
    payroll_group_id uuid           NOT NULL,
    legal_entity_id  uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payroll_group_legal_entity_identity UNIQUE (tenant_id, membership_id, revision),
    CONSTRAINT payroll_group_legal_entity_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS payroll_group_legal_entity_by_entity
    ON payroll_group_legal_entity (tenant_id, legal_entity_id);
CREATE INDEX IF NOT EXISTS payroll_group_legal_entity_by_group
    ON payroll_group_legal_entity (tenant_id, payroll_group_id);

CREATE OR REPLACE TRIGGER payroll_group_legal_entity_append_only
    BEFORE UPDATE OR DELETE ON payroll_group_legal_entity
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON payroll_group_legal_entity FROM PUBLIC;

ALTER TABLE payroll_group_legal_entity ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll_group_legal_entity FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payroll_group_legal_entity
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON payroll_group_legal_entity TO hcmnext_app;

-- =============================================================================
-- SECTION 3: Management (reporting/business) tree -- separate from legal
-- structure and from the cost/financial dimension.
-- =============================================================================

CREATE TABLE IF NOT EXISTS management_unit_revision (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid           NOT NULL,
    management_unit_id uuid          NOT NULL,
    revision          cas_version    NOT NULL,
    parent_revision   cas_version,
    parent_digest     content_digest,
    name              text           NOT NULL,
    unit_type         text           NOT NULL,
    home_legal_entity_id uuid,
    parent_unit_id    uuid,
    lifecycle_state   text           NOT NULL,
    effective_from    timestamptz    NOT NULL,
    effective_to      timestamptz,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),
    digest            content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT management_unit_revision_identity UNIQUE (tenant_id, management_unit_id, revision),
    CONSTRAINT management_unit_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT management_unit_revision_type_allowed CHECK (
        unit_type IN (
            'BUSINESS_UNIT', 'DIVISION', 'DEPARTMENT', 'TEAM',
            'SHARED_SERVICE_CENTER', 'PROGRAM', 'PROJECT'
        )
    ),
    CONSTRAINT management_unit_revision_lifecycle_allowed CHECK (
        lifecycle_state IN ('ACTIVE', 'FROZEN', 'CLOSED')
    ),
    CONSTRAINT management_unit_revision_not_own_parent CHECK (
        parent_unit_id IS NULL OR parent_unit_id <> management_unit_id
    ),
    CONSTRAINT management_unit_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS management_unit_revision_parent
    ON management_unit_revision (tenant_id, parent_unit_id) WHERE effective_to IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION management_unit_revision_forbid_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    ancestor uuid;
    hops     integer := 0;
BEGIN
    IF NEW.parent_unit_id IS NULL THEN
        RETURN NEW;
    END IF;
    ancestor := NEW.parent_unit_id;
    LOOP
        hops := hops + 1;
        IF ancestor = NEW.management_unit_id THEN
            RAISE EXCEPTION
                'management_unit %: parent chain loops back to itself through %',
                NEW.management_unit_id, ancestor
                USING ERRCODE = '23514';
        END IF;
        IF hops > 100000 THEN
            RAISE EXCEPTION
                'management_unit %: parent chain did not terminate within % hops',
                NEW.management_unit_id, hops
                USING ERRCODE = '23514';
        END IF;
        SELECT parent_unit_id INTO ancestor
        FROM management_unit_revision
        WHERE tenant_id = NEW.tenant_id AND management_unit_id = ancestor AND effective_to IS NULL
        LIMIT 1;
        EXIT WHEN ancestor IS NULL;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
COMMENT ON FUNCTION management_unit_revision_forbid_cycle() IS
    'The management (reporting) tree is single-parent per live instant; a parent chain can never loop back to itself.';

CREATE OR REPLACE TRIGGER management_unit_revision_forbid_cycle_trigger
    BEFORE INSERT ON management_unit_revision
    FOR EACH ROW EXECUTE FUNCTION management_unit_revision_forbid_cycle();
CREATE OR REPLACE TRIGGER management_unit_revision_append_only
    BEFORE UPDATE OR DELETE ON management_unit_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON management_unit_revision FROM PUBLIC;

ALTER TABLE management_unit_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE management_unit_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON management_unit_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON management_unit_revision TO hcmnext_app;

CREATE TABLE IF NOT EXISTS management_unit_closure (
    tenant_id     tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    ancestor_id   uuid       NOT NULL,
    descendant_id uuid       NOT NULL,
    depth         int        NOT NULL,
    computed_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, ancestor_id, descendant_id),
    CONSTRAINT management_unit_closure_depth_nonnegative CHECK (depth >= 0)
);
CREATE INDEX IF NOT EXISTS management_unit_closure_descendant
    ON management_unit_closure (tenant_id, descendant_id);
ALTER TABLE management_unit_closure ENABLE ROW LEVEL SECURITY;
ALTER TABLE management_unit_closure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON management_unit_closure
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON management_unit_closure TO hcmnext_app;

CREATE TABLE IF NOT EXISTS management_unit_pointer (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    management_unit_id uuid       NOT NULL,
    current_row_id     uuid       NOT NULL,
    current_revision   cas_version NOT NULL,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, management_unit_id),
    FOREIGN KEY (tenant_id, current_row_id) REFERENCES management_unit_revision (tenant_id, row_id)
);
ALTER TABLE management_unit_pointer ENABLE ROW LEVEL SECURITY;
ALTER TABLE management_unit_pointer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON management_unit_pointer
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON management_unit_pointer TO hcmnext_app;

CREATE TABLE IF NOT EXISTS management_unit_event (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    management_unit_id uuid       NOT NULL,
    event_kind         text       NOT NULL,
    event_sequence     bigint     NOT NULL,
    occurred_at        timestamptz NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    actor_principal_id text,
    reason             text,
    related_unit_id    uuid,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT management_unit_event_sequence_unique UNIQUE (tenant_id, management_unit_id, event_sequence),
    CONSTRAINT management_unit_event_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT management_unit_event_kind_allowed CHECK (
        event_kind IN ('CREATED', 'RENAMED', 'REPARENTED', 'MERGED', 'SPLIT', 'FROZEN', 'CLOSED')
    )
);
CREATE OR REPLACE TRIGGER management_unit_event_append_only
    BEFORE UPDATE OR DELETE ON management_unit_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON management_unit_event FROM PUBLIC;

ALTER TABLE management_unit_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE management_unit_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON management_unit_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON management_unit_event TO hcmnext_app;

-- =============================================================================
-- SECTION 4: Cost/financial dimension tree -- distinct from the management
-- tree; a management unit and a cost center very often draw the same lines
-- but are allowed to diverge (shared services, allocated overhead).
-- =============================================================================

CREATE TABLE IF NOT EXISTS cost_center_revision (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    cost_center_id   uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    code             semantic_key   NOT NULL,
    name             text           NOT NULL,
    home_legal_entity_id uuid,
    parent_cost_center_id uuid,
    lifecycle_state  text           NOT NULL,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cost_center_revision_identity UNIQUE (tenant_id, cost_center_id, revision),
    CONSTRAINT cost_center_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT cost_center_revision_lifecycle_allowed CHECK (
        lifecycle_state IN ('ACTIVE', 'FROZEN', 'CLOSED')
    ),
    CONSTRAINT cost_center_revision_not_own_parent CHECK (
        parent_cost_center_id IS NULL OR parent_cost_center_id <> cost_center_id
    ),
    CONSTRAINT cost_center_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS cost_center_revision_parent
    ON cost_center_revision (tenant_id, parent_cost_center_id) WHERE effective_to IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cost_center_revision_forbid_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    ancestor uuid;
    hops     integer := 0;
BEGIN
    IF NEW.parent_cost_center_id IS NULL THEN
        RETURN NEW;
    END IF;
    ancestor := NEW.parent_cost_center_id;
    LOOP
        hops := hops + 1;
        IF ancestor = NEW.cost_center_id THEN
            RAISE EXCEPTION
                'cost_center %: parent chain loops back to itself through %', NEW.cost_center_id, ancestor
                USING ERRCODE = '23514';
        END IF;
        IF hops > 100000 THEN
            RAISE EXCEPTION
                'cost_center %: parent chain did not terminate within % hops', NEW.cost_center_id, hops
                USING ERRCODE = '23514';
        END IF;
        SELECT parent_cost_center_id INTO ancestor
        FROM cost_center_revision
        WHERE tenant_id = NEW.tenant_id AND cost_center_id = ancestor AND effective_to IS NULL
        LIMIT 1;
        EXIT WHEN ancestor IS NULL;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
COMMENT ON FUNCTION cost_center_revision_forbid_cycle() IS
    'The cost-center rollup tree is single-parent per live instant; a parent chain can never loop back to itself.';

CREATE OR REPLACE TRIGGER cost_center_revision_forbid_cycle_trigger
    BEFORE INSERT ON cost_center_revision
    FOR EACH ROW EXECUTE FUNCTION cost_center_revision_forbid_cycle();
CREATE OR REPLACE TRIGGER cost_center_revision_append_only
    BEFORE UPDATE OR DELETE ON cost_center_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON cost_center_revision FROM PUBLIC;

ALTER TABLE cost_center_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_center_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cost_center_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON cost_center_revision TO hcmnext_app;

CREATE TABLE IF NOT EXISTS cost_center_closure (
    tenant_id     tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    ancestor_id   uuid       NOT NULL,
    descendant_id uuid       NOT NULL,
    depth         int        NOT NULL,
    computed_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, ancestor_id, descendant_id),
    CONSTRAINT cost_center_closure_depth_nonnegative CHECK (depth >= 0)
);
CREATE INDEX IF NOT EXISTS cost_center_closure_descendant
    ON cost_center_closure (tenant_id, descendant_id);
ALTER TABLE cost_center_closure ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_center_closure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cost_center_closure
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON cost_center_closure TO hcmnext_app;

CREATE TABLE IF NOT EXISTS cost_center_pointer (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    cost_center_id   uuid       NOT NULL,
    current_row_id   uuid       NOT NULL,
    current_revision cas_version NOT NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, cost_center_id),
    FOREIGN KEY (tenant_id, current_row_id) REFERENCES cost_center_revision (tenant_id, row_id)
);
ALTER TABLE cost_center_pointer ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_center_pointer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cost_center_pointer
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON cost_center_pointer TO hcmnext_app;

-- cost_center_allocation: how a management_unit's cost is split across one
-- or more cost centers, effective-dated (a shared-service department may
-- allocate 40% to Company A, 60% to Company B).
CREATE TABLE IF NOT EXISTS cost_center_allocation (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    allocation_id      uuid           NOT NULL,
    management_unit_id uuid           NOT NULL,
    cost_center_id     uuid           NOT NULL,
    revision           cas_version    NOT NULL,
    allocation_percentage numeric(6, 3) NOT NULL,
    effective_from     timestamptz    NOT NULL,
    effective_to       timestamptz,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),
    digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cost_center_allocation_identity UNIQUE (tenant_id, allocation_id, revision),
    CONSTRAINT cost_center_allocation_percentage_range CHECK (
        allocation_percentage > 0 AND allocation_percentage <= 100
    ),
    CONSTRAINT cost_center_allocation_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS cost_center_allocation_by_unit
    ON cost_center_allocation (tenant_id, management_unit_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER cost_center_allocation_append_only
    BEFORE UPDATE OR DELETE ON cost_center_allocation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON cost_center_allocation FROM PUBLIC;

ALTER TABLE cost_center_allocation ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_center_allocation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cost_center_allocation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON cost_center_allocation TO hcmnext_app;

-- =============================================================================
-- SECTION 5: Service-relationship graph -- who serves whom, who employs whom.
-- This is the PEO/ASO/EOR/staffing/shared-services/subcontracted-HR layer.
-- =============================================================================

-- service_agreement_revision: the commercial/legal agreement under which a
-- provider entity (which may sit in this tenant or in a provider tenant --
-- see tenant_relationship_revision below) delivers HR, payroll, benefits or
-- tax-filing services to a client entity.
CREATE TABLE IF NOT EXISTS service_agreement_revision (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid           NOT NULL,
    agreement_id        uuid           NOT NULL,
    revision            cas_version    NOT NULL,
    parent_revision     cas_version,
    parent_digest       content_digest,
    provider_legal_entity_id uuid,
    provider_tenant_id  tenant_ref,
    client_legal_entity_id   uuid       NOT NULL,
    agreement_kind       text          NOT NULL,
    scope_of_services    jsonb         NOT NULL,
    billing_party_id     uuid,
    status               text          NOT NULL,
    effective_from       timestamptz   NOT NULL,
    effective_to         timestamptz,
    recorded_at          timestamptz   NOT NULL DEFAULT now(),
    digest               content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT service_agreement_revision_identity UNIQUE (tenant_id, agreement_id, revision),
    CONSTRAINT service_agreement_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT service_agreement_revision_kind_allowed CHECK (
        agreement_kind IN (
            'PEO_CLIENT_SERVICE_AGREEMENT', 'ASO_SERVICE_AGREEMENT', 'EOR_SERVICE_AGREEMENT',
            'STAFFING_SUPPLY_AGREEMENT', 'PAYROLL_PROCESSING_AGREEMENT',
            'SUBCONTRACTED_HR_AGREEMENT', 'SHARED_SERVICE_AGREEMENT'
        )
    ),
    CONSTRAINT service_agreement_revision_status_allowed CHECK (
        status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'TERMINATED')
    ),
    CONSTRAINT service_agreement_revision_one_provider CHECK (
        (provider_legal_entity_id IS NOT NULL) OR (provider_tenant_id IS NOT NULL)
    ),
    CONSTRAINT service_agreement_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS service_agreement_revision_by_client
    ON service_agreement_revision (tenant_id, client_legal_entity_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER service_agreement_revision_append_only
    BEFORE UPDATE OR DELETE ON service_agreement_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON service_agreement_revision FROM PUBLIC;

ALTER TABLE service_agreement_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_agreement_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON service_agreement_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON service_agreement_revision TO hcmnext_app;

-- service_relationship_revision: the entity-level typed relationship
-- instance created under a service_agreement -- e.g. "Acme PEO LLC is the
-- ASO administrator for Client Bakery Inc, in scope: payroll and benefits
-- administration, not tax filing." Separate from the agreement so a single
-- agreement can spawn several typed relationships as scope changes.
CREATE TABLE IF NOT EXISTS service_relationship_revision (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid           NOT NULL,
    relationship_id       uuid           NOT NULL,
    agreement_id          uuid           NOT NULL,
    revision              cas_version    NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    relationship_kind     text           NOT NULL,
    provider_legal_entity_id uuid,
    provider_tenant_id    tenant_ref,
    client_legal_entity_id   uuid        NOT NULL,
    functions_in_scope    jsonb          NOT NULL,
    effective_from        timestamptz    NOT NULL,
    effective_to          timestamptz,
    recorded_at           timestamptz    NOT NULL DEFAULT now(),
    digest                content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT service_relationship_revision_identity UNIQUE (tenant_id, relationship_id, revision),
    CONSTRAINT service_relationship_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT service_relationship_revision_kind_allowed CHECK (
        relationship_kind IN (
            'PEO_CO_EMPLOYER', 'ASO_ADMINISTRATOR', 'EOR_EMPLOYER_OF_RECORD',
            'STAFFING_WORKER_OF_RECORD', 'WORKSITE_EMPLOYER',
            'SHARED_SERVICE_PROVIDER', 'PAYROLL_AGENT_PROVIDER', 'SUBCONTRACTED_HR_PROVIDER'
        )
    ),
    CONSTRAINT service_relationship_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS service_relationship_revision_by_client
    ON service_relationship_revision (tenant_id, client_legal_entity_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER service_relationship_revision_append_only
    BEFORE UPDATE OR DELETE ON service_relationship_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON service_relationship_revision FROM PUBLIC;

ALTER TABLE service_relationship_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_relationship_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON service_relationship_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON service_relationship_revision TO hcmnext_app;

-- co_employment_relationship_revision: the worker-level co-employment fact.
-- Employment (internal/domains/people) already carries one legal_entity;
-- this table is what makes that legal_entity meaningful when it is not the
-- worksite the worker actually reports to -- a PEO/EOR arrangement, or a
-- staffing placement -- by naming both the employer of record and the
-- worksite employer for the same worker.
CREATE TABLE IF NOT EXISTS co_employment_relationship_revision (
    tenant_id                 tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                    uuid           NOT NULL,
    co_employment_id          uuid           NOT NULL,
    worker_id                 text           NOT NULL,
    employment_id             text           NOT NULL,
    revision                  cas_version    NOT NULL,
    parent_revision           cas_version,
    parent_digest             content_digest,
    relationship_kind         text           NOT NULL,
    employer_of_record_entity_id uuid        NOT NULL,
    worksite_employer_entity_id  uuid        NOT NULL,
    service_relationship_id   uuid,
    effective_from            timestamptz    NOT NULL,
    effective_to              timestamptz,
    recorded_at               timestamptz    NOT NULL DEFAULT now(),
    digest                    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT co_employment_relationship_revision_identity UNIQUE (tenant_id, co_employment_id, revision),
    CONSTRAINT co_employment_relationship_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT co_employment_relationship_revision_kind_allowed CHECK (
        relationship_kind IN (
            'PEO_CO_EMPLOYMENT', 'EOR_ARRANGEMENT', 'STAFFING_ASSIGNMENT', 'WORKER_OF_RECORD'
        )
    ),
    CONSTRAINT co_employment_relationship_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS co_employment_relationship_revision_by_worker
    ON co_employment_relationship_revision (tenant_id, worker_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER co_employment_relationship_revision_append_only
    BEFORE UPDATE OR DELETE ON co_employment_relationship_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON co_employment_relationship_revision FROM PUBLIC;

ALTER TABLE co_employment_relationship_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE co_employment_relationship_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON co_employment_relationship_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON co_employment_relationship_revision TO hcmnext_app;

-- shared_service_center_scope: which legal entities a shared-service-center
-- management_unit (payroll, IT, recruiting, finance run once for many
-- businesses) actually serves, and for which functions.
CREATE TABLE IF NOT EXISTS shared_service_center_scope (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    scope_id           uuid           NOT NULL,
    management_unit_id uuid           NOT NULL,
    served_legal_entity_id uuid       NOT NULL,
    functions_served   jsonb          NOT NULL,
    revision           cas_version    NOT NULL,
    effective_from     timestamptz    NOT NULL,
    effective_to       timestamptz,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),
    digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT shared_service_center_scope_identity UNIQUE (tenant_id, scope_id, revision),
    CONSTRAINT shared_service_center_scope_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS shared_service_center_scope_by_entity
    ON shared_service_center_scope (tenant_id, served_legal_entity_id);

CREATE OR REPLACE TRIGGER shared_service_center_scope_append_only
    BEFORE UPDATE OR DELETE ON shared_service_center_scope
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON shared_service_center_scope FROM PUBLIC;

ALTER TABLE shared_service_center_scope ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_service_center_scope FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shared_service_center_scope
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON shared_service_center_scope TO hcmnext_app;

-- =============================================================================
-- SECTION 6: Cross-entity benefits and bargaining coverage.
-- =============================================================================

-- benefit_plan_adoption_revision: legal entities other than a plan's own
-- sponsor_ref (migrations/00072_benefits.sql) that have adopted the plan for
-- their own workers, under their own adoption agreement.
CREATE TABLE IF NOT EXISTS benefit_plan_adoption_revision (
    tenant_id              tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                 uuid           NOT NULL,
    adoption_id            uuid           NOT NULL,
    plan_id                uuid           NOT NULL,
    adopting_legal_entity_id uuid         NOT NULL,
    revision               cas_version    NOT NULL,
    parent_revision        cas_version,
    parent_digest          content_digest,
    adoption_agreement_ref text,
    effective_from         timestamptz    NOT NULL,
    effective_to           timestamptz,
    recorded_at            timestamptz    NOT NULL DEFAULT now(),
    digest                 content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT benefit_plan_adoption_revision_identity UNIQUE (tenant_id, adoption_id, revision),
    CONSTRAINT benefit_plan_adoption_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT benefit_plan_adoption_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS benefit_plan_adoption_revision_by_plan
    ON benefit_plan_adoption_revision (tenant_id, plan_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER benefit_plan_adoption_revision_append_only
    BEFORE UPDATE OR DELETE ON benefit_plan_adoption_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON benefit_plan_adoption_revision FROM PUBLIC;

ALTER TABLE benefit_plan_adoption_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE benefit_plan_adoption_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON benefit_plan_adoption_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON benefit_plan_adoption_revision TO hcmnext_app;

-- cba_bargaining_unit_legal_entity: legal entities covered by one bargaining
-- unit (migrations/00076_cba.sql's cba_bargaining_unit_revision.unit_id),
-- for a multi-employer bargaining unit that spans separately incorporated
-- entities under one master agreement.
CREATE TABLE IF NOT EXISTS cba_bargaining_unit_legal_entity (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    coverage_id      uuid           NOT NULL,
    bargaining_unit_id text         NOT NULL,
    legal_entity_id  uuid           NOT NULL,
    revision         cas_version    NOT NULL,
    effective_from   timestamptz    NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz    NOT NULL DEFAULT now(),
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cba_bargaining_unit_legal_entity_identity UNIQUE (tenant_id, coverage_id, revision),
    CONSTRAINT cba_bargaining_unit_legal_entity_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS cba_bargaining_unit_legal_entity_by_entity
    ON cba_bargaining_unit_legal_entity (tenant_id, legal_entity_id);

CREATE OR REPLACE TRIGGER cba_bargaining_unit_legal_entity_append_only
    BEFORE UPDATE OR DELETE ON cba_bargaining_unit_legal_entity
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON cba_bargaining_unit_legal_entity FROM PUBLIC;

ALTER TABLE cba_bargaining_unit_legal_entity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cba_bargaining_unit_legal_entity FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cba_bargaining_unit_legal_entity
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON cba_bargaining_unit_legal_entity TO hcmnext_app;

-- =============================================================================
-- SECTION 7: Billing and commercial scoping (who pays for whom).
-- =============================================================================

-- billing_party_revision: the payer identity. A billing party is usually a
-- legal entity, but a consolidated corporate payer that is itself not a
-- workforce legal entity (e.g. a pure holding-company AP function) is
-- possible, so the entity link is optional and the party carries its own
-- name/reference.
CREATE TABLE IF NOT EXISTS billing_party_revision (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid           NOT NULL,
    billing_party_id  uuid           NOT NULL,
    revision          cas_version    NOT NULL,
    parent_revision   cas_version,
    parent_digest     content_digest,
    display_name      text           NOT NULL,
    linked_legal_entity_id uuid,
    contract_id       text,
    status            text           NOT NULL,
    effective_from    timestamptz    NOT NULL,
    effective_to      timestamptz,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),
    digest            content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT billing_party_revision_identity UNIQUE (tenant_id, billing_party_id, revision),
    CONSTRAINT billing_party_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT billing_party_revision_status_allowed CHECK (status IN ('ACTIVE', 'SUSPENDED', 'CLOSED')),
    CONSTRAINT billing_party_revision_name_not_blank CHECK (btrim(display_name) <> ''),
    CONSTRAINT billing_party_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE OR REPLACE TRIGGER billing_party_revision_append_only
    BEFORE UPDATE OR DELETE ON billing_party_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON billing_party_revision FROM PUBLIC;

ALTER TABLE billing_party_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_party_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_party_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON billing_party_revision TO hcmnext_app;

-- billing_scope_assignment_revision: maps a usage scope (the legal entity or
-- management unit whose activity generated usage) to a payer scope (the
-- billing_party who is charged for it), with an allocation percentage so a
-- shared cost can be split. usage_scope_kind/id let the same table cover
-- both dimensions without a foreign key to two different tables.
CREATE TABLE IF NOT EXISTS billing_scope_assignment_revision (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid           NOT NULL,
    assignment_id       uuid           NOT NULL,
    usage_scope_kind    text           NOT NULL,
    usage_scope_id      uuid           NOT NULL,
    billing_party_id    uuid           NOT NULL,
    allocation_percentage numeric(6, 3) NOT NULL,
    revision            cas_version    NOT NULL,
    parent_revision     cas_version,
    parent_digest       content_digest,
    effective_from      timestamptz    NOT NULL,
    effective_to        timestamptz,
    recorded_at         timestamptz    NOT NULL DEFAULT now(),
    digest              content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT billing_scope_assignment_revision_identity UNIQUE (tenant_id, assignment_id, revision),
    CONSTRAINT billing_scope_assignment_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT billing_scope_assignment_revision_kind_allowed CHECK (
        usage_scope_kind IN ('LEGAL_ENTITY', 'MANAGEMENT_UNIT', 'TENANT')
    ),
    CONSTRAINT billing_scope_assignment_revision_percentage_range CHECK (
        allocation_percentage > 0 AND allocation_percentage <= 100
    ),
    CONSTRAINT billing_scope_assignment_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS billing_scope_assignment_revision_by_scope
    ON billing_scope_assignment_revision (tenant_id, usage_scope_kind, usage_scope_id) WHERE effective_to IS NULL;
CREATE INDEX IF NOT EXISTS billing_scope_assignment_revision_by_payer
    ON billing_scope_assignment_revision (tenant_id, billing_party_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER billing_scope_assignment_revision_append_only
    BEFORE UPDATE OR DELETE ON billing_scope_assignment_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON billing_scope_assignment_revision FROM PUBLIC;

ALTER TABLE billing_scope_assignment_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_scope_assignment_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_scope_assignment_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON billing_scope_assignment_revision TO hcmnext_app;

-- =============================================================================
-- SECTION 8: Tenant-of-tenant, reseller/white-label, and cross-tenant
-- identity. These two tables are the deliberate exception to "one tenant_id
-- column, one RLS predicate": each row genuinely belongs to two tenants at
-- once, so the isolation policy checks both columns. See the companion
-- document's "hardest modelling decisions" section for the reasoning.
-- =============================================================================

-- tenant_relationship_revision: a governed relationship between two tenants
-- -- a platform-level PEO/ASO/EOR/payroll-bureau provider tenant and a
-- client tenant, or a reseller/white-label distributor tenant and the
-- tenants it resells to. This is distinct from service_relationship_revision
-- (which relates entities that can sit inside one tenant) precisely for the
-- case where the client is not merely a "company" inside the provider's
-- tenant but is its own fully isolated tenant.
CREATE TABLE IF NOT EXISTS tenant_relationship_revision (
    row_id               uuid           NOT NULL PRIMARY KEY,
    relationship_id      uuid           NOT NULL,
    revision             cas_version    NOT NULL,
    parent_revision      cas_version,
    parent_digest        content_digest,
    provider_tenant_id   tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    client_tenant_id     tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    relationship_kind    text           NOT NULL,
    billing_responsibility text         NOT NULL,
    status               text           NOT NULL,
    effective_from       timestamptz    NOT NULL,
    effective_to         timestamptz,
    recorded_at          timestamptz    NOT NULL DEFAULT now(),
    digest               content_digest NOT NULL,

    CONSTRAINT tenant_relationship_revision_identity UNIQUE (relationship_id, revision),
    CONSTRAINT tenant_relationship_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT tenant_relationship_revision_not_self CHECK (provider_tenant_id <> client_tenant_id),
    CONSTRAINT tenant_relationship_revision_kind_allowed CHECK (
        relationship_kind IN (
            'RESELLER', 'WHITE_LABEL', 'PEO_PLATFORM_PROVIDER', 'ASO_PLATFORM_PROVIDER',
            'EOR_PLATFORM_PROVIDER', 'PAYROLL_BUREAU', 'SUBCONTRACTED_HR_PLATFORM_PROVIDER'
        )
    ),
    CONSTRAINT tenant_relationship_revision_billing_allowed CHECK (
        billing_responsibility IN ('PROVIDER_BILLS_CLIENT', 'PLATFORM_BILLS_CLIENT_DIRECT', 'PLATFORM_BILLS_PROVIDER_CONSOLIDATED')
    ),
    CONSTRAINT tenant_relationship_revision_status_allowed CHECK (
        status IN ('ACTIVE', 'SUSPENDED', 'TERMINATED')
    ),
    CONSTRAINT tenant_relationship_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS tenant_relationship_revision_by_provider
    ON tenant_relationship_revision (provider_tenant_id) WHERE effective_to IS NULL;
CREATE INDEX IF NOT EXISTS tenant_relationship_revision_by_client
    ON tenant_relationship_revision (client_tenant_id) WHERE effective_to IS NULL;

CREATE OR REPLACE TRIGGER tenant_relationship_revision_append_only
    BEFORE UPDATE OR DELETE ON tenant_relationship_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON tenant_relationship_revision FROM PUBLIC;

-- No single tenant_id column exists on this table, so the usual one-column
-- predicate does not apply. A session may see a relationship row only when
-- its own tenant is one of the two named parties; this still fails closed
-- when app.tenant_id is unset, because NULLIF(...)::uuid is then NULL and
-- NULL never equals either column.
ALTER TABLE tenant_relationship_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_relationship_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY cross_tenant_party_isolation ON tenant_relationship_revision
    USING (
        provider_tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        OR client_tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    )
    WITH CHECK (
        provider_tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        OR client_tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    );
GRANT SELECT, INSERT ON tenant_relationship_revision TO hcmnext_app;

-- cross_tenant_identity_link_revision: evidence that one human is linked
-- across two different tenants (a gig worker or contractor engaged, under
-- two unrelated staffing tenants, by two unrelated end clients; or a
-- reseller's client migrating between provider tenants). It stores no
-- person facts of its own, only the linkage, its evidence, its declared
-- purpose, and consent -- the same discipline
-- internal/domains/people/person_worker.go already applies to identity
-- resolution claims within one tenant.
CREATE TABLE IF NOT EXISTS cross_tenant_identity_link_revision (
    row_id            uuid           NOT NULL PRIMARY KEY,
    link_id           uuid           NOT NULL,
    revision          cas_version    NOT NULL,
    parent_revision   cas_version,
    parent_digest     content_digest,
    tenant_a_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    person_a_ref      text           NOT NULL,
    tenant_b_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    person_b_ref      text           NOT NULL,
    purpose           text           NOT NULL,
    consent_evidence_ref text        NOT NULL,
    status            text           NOT NULL,
    effective_from    timestamptz    NOT NULL,
    effective_to      timestamptz,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),
    digest            content_digest NOT NULL,

    CONSTRAINT cross_tenant_identity_link_revision_identity UNIQUE (link_id, revision),
    CONSTRAINT cross_tenant_identity_link_revision_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision IS NOT NULL AND parent_revision < revision AND parent_digest IS NOT NULL)
    ),
    CONSTRAINT cross_tenant_identity_link_revision_not_self CHECK (
        tenant_a_id <> tenant_b_id OR person_a_ref <> person_b_ref
    ),
    CONSTRAINT cross_tenant_identity_link_revision_status_allowed CHECK (
        status IN ('PROPOSED', 'CONFIRMED', 'REVOKED')
    ),
    CONSTRAINT cross_tenant_identity_link_revision_consent_not_blank CHECK (btrim(consent_evidence_ref) <> ''),
    CONSTRAINT cross_tenant_identity_link_revision_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);
CREATE INDEX IF NOT EXISTS cross_tenant_identity_link_revision_by_a
    ON cross_tenant_identity_link_revision (tenant_a_id, person_a_ref);
CREATE INDEX IF NOT EXISTS cross_tenant_identity_link_revision_by_b
    ON cross_tenant_identity_link_revision (tenant_b_id, person_b_ref);

CREATE OR REPLACE TRIGGER cross_tenant_identity_link_revision_append_only
    BEFORE UPDATE OR DELETE ON cross_tenant_identity_link_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON cross_tenant_identity_link_revision FROM PUBLIC;

ALTER TABLE cross_tenant_identity_link_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cross_tenant_identity_link_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY cross_tenant_party_isolation ON cross_tenant_identity_link_revision
    USING (
        tenant_a_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        OR tenant_b_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    )
    WITH CHECK (
        tenant_a_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        OR tenant_b_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    );
GRANT SELECT, INSERT ON cross_tenant_identity_link_revision TO hcmnext_app;

-- +goose Down

DROP POLICY cross_tenant_party_isolation ON cross_tenant_identity_link_revision;
REVOKE ALL ON cross_tenant_identity_link_revision FROM hcmnext_app;
ALTER TABLE cross_tenant_identity_link_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cross_tenant_identity_link_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE cross_tenant_identity_link_revision;

DROP POLICY cross_tenant_party_isolation ON tenant_relationship_revision;
REVOKE ALL ON tenant_relationship_revision FROM hcmnext_app;
ALTER TABLE tenant_relationship_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_relationship_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE tenant_relationship_revision;

DROP POLICY tenant_isolation ON billing_scope_assignment_revision;
REVOKE ALL ON billing_scope_assignment_revision FROM hcmnext_app;
ALTER TABLE billing_scope_assignment_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE billing_scope_assignment_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE billing_scope_assignment_revision;

DROP POLICY tenant_isolation ON billing_party_revision;
REVOKE ALL ON billing_party_revision FROM hcmnext_app;
ALTER TABLE billing_party_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE billing_party_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE billing_party_revision;

DROP POLICY tenant_isolation ON cba_bargaining_unit_legal_entity;
REVOKE ALL ON cba_bargaining_unit_legal_entity FROM hcmnext_app;
ALTER TABLE cba_bargaining_unit_legal_entity NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cba_bargaining_unit_legal_entity DISABLE ROW LEVEL SECURITY;
DROP TABLE cba_bargaining_unit_legal_entity;

DROP POLICY tenant_isolation ON benefit_plan_adoption_revision;
REVOKE ALL ON benefit_plan_adoption_revision FROM hcmnext_app;
ALTER TABLE benefit_plan_adoption_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE benefit_plan_adoption_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE benefit_plan_adoption_revision;

DROP POLICY tenant_isolation ON shared_service_center_scope;
REVOKE ALL ON shared_service_center_scope FROM hcmnext_app;
ALTER TABLE shared_service_center_scope NO FORCE ROW LEVEL SECURITY;
ALTER TABLE shared_service_center_scope DISABLE ROW LEVEL SECURITY;
DROP TABLE shared_service_center_scope;

DROP POLICY tenant_isolation ON co_employment_relationship_revision;
REVOKE ALL ON co_employment_relationship_revision FROM hcmnext_app;
ALTER TABLE co_employment_relationship_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE co_employment_relationship_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE co_employment_relationship_revision;

DROP POLICY tenant_isolation ON service_relationship_revision;
REVOKE ALL ON service_relationship_revision FROM hcmnext_app;
ALTER TABLE service_relationship_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE service_relationship_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE service_relationship_revision;

DROP POLICY tenant_isolation ON service_agreement_revision;
REVOKE ALL ON service_agreement_revision FROM hcmnext_app;
ALTER TABLE service_agreement_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE service_agreement_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE service_agreement_revision;

DROP POLICY tenant_isolation ON cost_center_allocation;
REVOKE ALL ON cost_center_allocation FROM hcmnext_app;
ALTER TABLE cost_center_allocation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cost_center_allocation DISABLE ROW LEVEL SECURITY;
DROP TABLE cost_center_allocation;

DROP POLICY tenant_isolation ON cost_center_pointer;
REVOKE ALL ON cost_center_pointer FROM hcmnext_app;
ALTER TABLE cost_center_pointer NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cost_center_pointer DISABLE ROW LEVEL SECURITY;
DROP TABLE cost_center_pointer;

DROP POLICY tenant_isolation ON cost_center_closure;
REVOKE ALL ON cost_center_closure FROM hcmnext_app;
ALTER TABLE cost_center_closure NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cost_center_closure DISABLE ROW LEVEL SECURITY;
DROP TABLE cost_center_closure;

DROP TRIGGER cost_center_revision_forbid_cycle_trigger ON cost_center_revision;
DROP POLICY tenant_isolation ON cost_center_revision;
REVOKE ALL ON cost_center_revision FROM hcmnext_app;
ALTER TABLE cost_center_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cost_center_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE cost_center_revision;
DROP FUNCTION cost_center_revision_forbid_cycle();

DROP POLICY tenant_isolation ON management_unit_event;
REVOKE ALL ON management_unit_event FROM hcmnext_app;
ALTER TABLE management_unit_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE management_unit_event DISABLE ROW LEVEL SECURITY;
DROP TABLE management_unit_event;

DROP POLICY tenant_isolation ON management_unit_pointer;
REVOKE ALL ON management_unit_pointer FROM hcmnext_app;
ALTER TABLE management_unit_pointer NO FORCE ROW LEVEL SECURITY;
ALTER TABLE management_unit_pointer DISABLE ROW LEVEL SECURITY;
DROP TABLE management_unit_pointer;

DROP POLICY tenant_isolation ON management_unit_closure;
REVOKE ALL ON management_unit_closure FROM hcmnext_app;
ALTER TABLE management_unit_closure NO FORCE ROW LEVEL SECURITY;
ALTER TABLE management_unit_closure DISABLE ROW LEVEL SECURITY;
DROP TABLE management_unit_closure;

DROP TRIGGER management_unit_revision_forbid_cycle_trigger ON management_unit_revision;
DROP POLICY tenant_isolation ON management_unit_revision;
REVOKE ALL ON management_unit_revision FROM hcmnext_app;
ALTER TABLE management_unit_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE management_unit_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE management_unit_revision;
DROP FUNCTION management_unit_revision_forbid_cycle();

DROP POLICY tenant_isolation ON payroll_group_legal_entity;
REVOKE ALL ON payroll_group_legal_entity FROM hcmnext_app;
ALTER TABLE payroll_group_legal_entity NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payroll_group_legal_entity DISABLE ROW LEVEL SECURITY;
DROP TABLE payroll_group_legal_entity;

DROP POLICY tenant_isolation ON payroll_group_revision;
REVOKE ALL ON payroll_group_revision FROM hcmnext_app;
ALTER TABLE payroll_group_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payroll_group_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE payroll_group_revision;

DROP POLICY tenant_isolation ON pay_agent_revision;
REVOKE ALL ON pay_agent_revision FROM hcmnext_app;
ALTER TABLE pay_agent_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE pay_agent_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE pay_agent_revision;

DROP POLICY tenant_isolation ON legal_entity_worksite_assignment;
REVOKE ALL ON legal_entity_worksite_assignment FROM hcmnext_app;
ALTER TABLE legal_entity_worksite_assignment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_worksite_assignment DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_worksite_assignment;

DROP POLICY tenant_isolation ON legal_entity_data_residency_revision;
REVOKE ALL ON legal_entity_data_residency_revision FROM hcmnext_app;
ALTER TABLE legal_entity_data_residency_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_data_residency_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_data_residency_revision;

DROP POLICY tenant_isolation ON legal_entity_tax_registration_revision;
REVOKE ALL ON legal_entity_tax_registration_revision FROM hcmnext_app;
ALTER TABLE legal_entity_tax_registration_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_tax_registration_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_tax_registration_revision;

DROP POLICY tenant_isolation ON legal_entity_event;
REVOKE ALL ON legal_entity_event FROM hcmnext_app;
ALTER TABLE legal_entity_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_event DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_event;

DROP POLICY tenant_isolation ON legal_entity_closure;
REVOKE ALL ON legal_entity_closure FROM hcmnext_app;
ALTER TABLE legal_entity_closure NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_closure DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_closure;

DROP TRIGGER legal_entity_ownership_edge_forbid_cycle_trigger ON legal_entity_ownership_edge;
DROP POLICY tenant_isolation ON legal_entity_ownership_edge;
REVOKE ALL ON legal_entity_ownership_edge FROM hcmnext_app;
ALTER TABLE legal_entity_ownership_edge NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_ownership_edge DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_ownership_edge;
DROP FUNCTION legal_entity_ownership_edge_forbid_cycle();

DROP POLICY tenant_isolation ON legal_entity_trade_name_revision;
REVOKE ALL ON legal_entity_trade_name_revision FROM hcmnext_app;
ALTER TABLE legal_entity_trade_name_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_trade_name_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_trade_name_revision;

DROP POLICY tenant_isolation ON legal_entity_structure_pointer;
REVOKE ALL ON legal_entity_structure_pointer FROM hcmnext_app;
ALTER TABLE legal_entity_structure_pointer NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_structure_pointer DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_structure_pointer;

DROP POLICY tenant_isolation ON legal_entity_structure_revision;
REVOKE ALL ON legal_entity_structure_revision FROM hcmnext_app;
ALTER TABLE legal_entity_structure_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_entity_structure_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_entity_structure_revision;
