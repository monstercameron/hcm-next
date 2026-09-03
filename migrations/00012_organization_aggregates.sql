-- Owner: data plane. Phase: P1A.
-- DB-009: organization, legal-entity, job, position and headcount aggregates.
--
-- Same bitemporal-fact-log shape as migrations/00011_people_aggregates.sql:
-- one row per version, effective_from/effective_to for business time,
-- recorded_at/superseded_at for system time, aggregate_forbid_inplace_update
-- (defined in 00011) as the only allowed UPDATE, tenant_id + FORCE ROW LEVEL
-- SECURITY + a tenant_isolation policy identical to 00008's, hcmnext_app
-- granted SELECT/INSERT/UPDATE (never DELETE) on every table. See 00011's
-- header comment for the full rationale; it is not repeated here.
--
-- Two invariants from DB-009's RED list are cheap enough to enforce as
-- triggers at this layer and are added below:
--   - organization_unit_forbid_cycle: an organization's parent chain can
--     never loop back to itself (RED: "manager/org cycle").
--   - position_occupancy_forbid_overcommit: the sum of live occupancy
--     allocations against a position can never exceed that position's own
--     live capacity_fte (RED: "reservation beyond FTE commits").
-- The remaining DB-009 RED cases (duplicate parent, closed-node assignment)
-- belong to the promotion/org domain layer that consumes this adapter, not
-- to the storage layer itself, and are out of this todo's scope.

-- +goose Up

-- ---------------------------------------------------------------------------
-- LegalEntity
-- ---------------------------------------------------------------------------
CREATE TABLE legal_entity (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    registered_name  text             NOT NULL,
    lifecycle_state  text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT legal_entity_lifecycle_state_allowed CHECK (lifecycle_state IN ('ACTIVE', 'INACTIVE')),
    CONSTRAINT legal_entity_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT legal_entity_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX legal_entity_history ON legal_entity (tenant_id, entity_id, recorded_at);
CREATE INDEX legal_entity_live ON legal_entity (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE TRIGGER legal_entity_forbid_inplace_update BEFORE UPDATE ON legal_entity
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE legal_entity ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_entity FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_entity
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON legal_entity TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- OrganizationUnit. parent_organization_ref names another organization_unit's
-- entity_id; organization_unit_forbid_cycle walks that chain on every INSERT
-- (an UPDATE can never change it -- aggregate_forbid_inplace_update already
-- refuses any update but the superseded_at transition).
-- ---------------------------------------------------------------------------
CREATE TABLE organization_unit (
    row_id                  uuid             NOT NULL,
    tenant_id               tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id               uuid             NOT NULL,
    canonical_id            semantic_key     NOT NULL,
    org_type                text             NOT NULL,
    code                    semantic_key     NOT NULL,
    name                    text             NOT NULL,
    legal_entity_ref        uuid,
    parent_organization_ref uuid,
    lifecycle_state         text             NOT NULL,
    effective_from          timestamptz      NOT NULL,
    effective_to            timestamptz,
    recorded_at             timestamptz      NOT NULL DEFAULT now(),
    superseded_at           timestamptz,
    digest_algorithm        digest_algorithm NOT NULL,
    digest                  content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, legal_entity_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, parent_organization_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT organization_unit_org_type_allowed CHECK (
        org_type IN ('BUSINESS_UNIT', 'DIVISION', 'DEPARTMENT', 'TEAM', 'COST_CENTER')
    ),
    CONSTRAINT organization_unit_lifecycle_state_allowed CHECK (lifecycle_state IN ('ACTIVE', 'INACTIVE')),
    CONSTRAINT organization_unit_not_own_parent CHECK (
        parent_organization_ref IS NULL OR parent_organization_ref <> entity_id
    ),
    CONSTRAINT organization_unit_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT organization_unit_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX organization_unit_history ON organization_unit (tenant_id, entity_id, recorded_at);
CREATE INDEX organization_unit_live ON organization_unit (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE INDEX organization_unit_parent ON organization_unit (tenant_id, parent_organization_ref) WHERE superseded_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION organization_unit_forbid_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    ancestor uuid;
    hops     integer := 0;
BEGIN
    IF NEW.parent_organization_ref IS NULL THEN
        RETURN NEW;
    END IF;

    ancestor := NEW.parent_organization_ref;
    LOOP
        hops := hops + 1;
        IF ancestor = NEW.entity_id THEN
            RAISE EXCEPTION
                'organization_unit %: parent chain loops back to itself through %',
                NEW.entity_id, ancestor
                USING ERRCODE = '23514';
        END IF;
        -- A live tenant can never have more organization_unit rows than this;
        -- a chain that has not terminated by then is itself proof of a cycle
        -- that does not happen to pass back through NEW.entity_id directly.
        IF hops > 100000 THEN
            RAISE EXCEPTION
                'organization_unit %: parent chain did not terminate within % hops',
                NEW.entity_id, hops
                USING ERRCODE = '23514';
        END IF;

        SELECT parent_organization_ref INTO ancestor
        FROM organization_unit
        WHERE tenant_id = NEW.tenant_id
          AND entity_id = ancestor
          AND superseded_at IS NULL
        LIMIT 1;

        EXIT WHEN ancestor IS NULL;
    END LOOP;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION organization_unit_forbid_cycle() IS
    'DB-009 RED: an organization_unit parent chain can never loop back to itself.';

CREATE TRIGGER organization_unit_forbid_cycle_trigger BEFORE INSERT ON organization_unit
    FOR EACH ROW EXECUTE FUNCTION organization_unit_forbid_cycle();
CREATE TRIGGER organization_unit_forbid_inplace_update BEFORE UPDATE ON organization_unit
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE organization_unit ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_unit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_unit
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON organization_unit TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Job
-- ---------------------------------------------------------------------------
CREATE TABLE job (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    code             semantic_key     NOT NULL,
    title            text             NOT NULL,
    job_family       semantic_key,
    grade            semantic_key,
    exempt_status    text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT job_exempt_status_allowed CHECK (exempt_status IN ('EXEMPT', 'NON_EXEMPT')),
    CONSTRAINT job_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT job_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX job_history ON job (tenant_id, entity_id, recorded_at);
CREATE INDEX job_live ON job (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE TRIGGER job_forbid_inplace_update BEFORE UPDATE ON job
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE job ENABLE ROW LEVEL SECURITY;
ALTER TABLE job FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON job TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Position: capacity_fte is the position's own ceiling; position_occupancy
-- rows reserve against it.
-- ---------------------------------------------------------------------------
CREATE TABLE job_position (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    position_code    semantic_key     NOT NULL,
    job_ref          uuid             NOT NULL,
    organization_ref uuid             NOT NULL,
    legal_entity_ref uuid,
    location         text,
    capacity_fte     numeric(9, 4)    NOT NULL,
    lifecycle_state  text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, job_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, organization_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, legal_entity_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT position_capacity_positive CHECK (capacity_fte > 0),
    CONSTRAINT position_lifecycle_state_allowed CHECK (
        lifecycle_state IN ('OPEN', 'FILLED', 'FROZEN', 'CLOSED')
    ),
    CONSTRAINT position_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT position_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX job_position_history ON job_position (tenant_id, entity_id, recorded_at);
CREATE INDEX job_position_live ON job_position (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE TRIGGER job_position_forbid_inplace_update BEFORE UPDATE ON job_position
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE job_position ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_position FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_position
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON job_position TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- PositionOccupancy: allocation_fte reserved against one position by one
-- assignment/worker. position_occupancy_forbid_overcommit sums every other
-- live occupancy row for the same position and refuses an insert that would
-- push the total past the position's own live capacity_fte.
-- ---------------------------------------------------------------------------
CREATE TABLE position_occupancy (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    position_ref     uuid             NOT NULL,
    assignment_ref   uuid,
    worker_ref       uuid,
    allocation_fte   numeric(9, 4)    NOT NULL,
    primary_flag     boolean          NOT NULL DEFAULT true,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, position_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, assignment_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, worker_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT position_occupancy_allocation_positive CHECK (allocation_fte > 0),
    CONSTRAINT position_occupancy_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT position_occupancy_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX position_occupancy_history ON position_occupancy (tenant_id, entity_id, recorded_at);
CREATE INDEX position_occupancy_position ON position_occupancy (tenant_id, position_ref) WHERE superseded_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION position_occupancy_forbid_overcommit() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    capacity  numeric(9, 4);
    committed numeric(9, 4);
BEGIN
    SELECT capacity_fte INTO capacity
    FROM job_position
    WHERE tenant_id = NEW.tenant_id AND entity_id = NEW.position_ref AND superseded_at IS NULL;

    IF capacity IS NULL THEN
        RAISE EXCEPTION
            'position_occupancy %: position % has no live capacity row',
            NEW.entity_id, NEW.position_ref
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(allocation_fte), 0) INTO committed
    FROM position_occupancy
    WHERE tenant_id = NEW.tenant_id
      AND position_ref = NEW.position_ref
      AND superseded_at IS NULL
      AND entity_id <> NEW.entity_id;

    IF committed + NEW.allocation_fte > capacity THEN
        RAISE EXCEPTION
            'position_occupancy %: allocation % plus already-committed % would exceed position % capacity %',
            NEW.entity_id, NEW.allocation_fte, committed, NEW.position_ref, capacity
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION position_occupancy_forbid_overcommit() IS
    'DB-009 RED: live position_occupancy allocations can never exceed the position''s own live capacity_fte.';

CREATE TRIGGER position_occupancy_forbid_overcommit_trigger BEFORE INSERT ON position_occupancy
    FOR EACH ROW EXECUTE FUNCTION position_occupancy_forbid_overcommit();
CREATE TRIGGER position_occupancy_forbid_inplace_update BEFORE UPDATE ON position_occupancy
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE position_occupancy ENABLE ROW LEVEL SECURITY;
ALTER TABLE position_occupancy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON position_occupancy
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON position_occupancy TO hcmnext_app;

-- +goose Down
DROP POLICY tenant_isolation ON position_occupancy;
REVOKE ALL ON position_occupancy FROM hcmnext_app;
DROP TABLE position_occupancy;
DROP FUNCTION position_occupancy_forbid_overcommit();

DROP POLICY tenant_isolation ON job_position;
REVOKE ALL ON job_position FROM hcmnext_app;
DROP TABLE job_position;

DROP POLICY tenant_isolation ON job;
REVOKE ALL ON job FROM hcmnext_app;
DROP TABLE job;

DROP POLICY tenant_isolation ON organization_unit;
REVOKE ALL ON organization_unit FROM hcmnext_app;
DROP TABLE organization_unit;
DROP FUNCTION organization_unit_forbid_cycle();

DROP POLICY tenant_isolation ON legal_entity;
REVOKE ALL ON legal_entity FROM hcmnext_app;
DROP TABLE legal_entity;
