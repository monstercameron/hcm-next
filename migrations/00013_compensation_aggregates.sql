-- Owner: data plane. Phase: P1A.
-- DB-010: compensation and workforce-budget data needed by Promotion.
--
-- Same bitemporal-fact-log shape as migrations/00011_people_aggregates.sql
-- and migrations/00012_organization_aggregates.sql; see 00011's header for
-- the full rationale (not repeated here). Money is exact decimal everywhere
-- -- numeric(19, 4), never float -- matching planning/data/models/
-- modeling-conventions.md's "Material financial values never use binary
-- floating point" and DB-010's own RED bullet.
--
-- Two invariants enforced as triggers here (mirroring 00012's pattern):
--   - compensation_component_forbid_currency_mismatch: a component's
--     currency must match its package's currency (RED: "currency mismatch").
--   - budget_reservation_forbid_overcommit: the sum of live HELD/COMMITTED
--     reservations against a budget can never exceed that budget's own live
--     available_quantity, and a reservation's currency must match its
--     budget's (RED: "over-reserved budget commits" / "currency mismatch").
-- compensation_band's minimum <= midpoint <= maximum is a plain CHECK (RED:
-- "invalid band range"). "Floating-point money" is refused structurally: the
-- amount columns are numeric(19,4), so a caller cannot even bind a float
-- without the driver already having rounded/converted it to text/numeric
-- first -- see internal/data/aggregates' use of values.Money/Decimal, which
-- never round-trips through float64.

-- +goose Up

-- ---------------------------------------------------------------------------
-- CompensationPackage
-- ---------------------------------------------------------------------------
CREATE TABLE compensation_package (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    worker_ref       uuid             NOT NULL,
    employment_ref   uuid,
    assignment_ref   uuid,
    currency         text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, worker_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, employment_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, assignment_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT compensation_package_currency_iso CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT compensation_package_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT compensation_package_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX compensation_package_history ON compensation_package (tenant_id, entity_id, recorded_at);
CREATE INDEX compensation_package_worker ON compensation_package (tenant_id, worker_ref) WHERE superseded_at IS NULL;
CREATE TRIGGER compensation_package_forbid_inplace_update BEFORE UPDATE ON compensation_package
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE compensation_package ENABLE ROW LEVEL SECURITY;
ALTER TABLE compensation_package FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compensation_package
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON compensation_package TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- CompensationComponent
-- ---------------------------------------------------------------------------
CREATE TABLE compensation_component (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    package_ref      uuid             NOT NULL,
    component_type   text             NOT NULL,
    amount           numeric(19, 4)   NOT NULL,
    currency         text             NOT NULL,
    frequency        text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, package_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT compensation_component_type_allowed CHECK (
        component_type IN (
            'BASE_PAY', 'BONUS_TARGET', 'ALLOWANCE', 'COMMISSION', 'EQUITY',
            'DIFFERENTIAL', 'STIPEND', 'ONE_TIME'
        )
    ),
    CONSTRAINT compensation_component_frequency_allowed CHECK (
        frequency IN ('ANNUAL', 'MONTHLY', 'BIWEEKLY', 'WEEKLY', 'HOURLY', 'ONE_TIME')
    ),
    CONSTRAINT compensation_component_currency_iso CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT compensation_component_amount_non_negative CHECK (amount >= 0),
    CONSTRAINT compensation_component_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT compensation_component_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX compensation_component_history ON compensation_component (tenant_id, entity_id, recorded_at);
CREATE INDEX compensation_component_package ON compensation_component (tenant_id, package_ref) WHERE superseded_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION compensation_component_forbid_currency_mismatch() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    package_currency text;
BEGIN
    SELECT currency INTO package_currency
    FROM compensation_package
    WHERE tenant_id = NEW.tenant_id AND entity_id = NEW.package_ref AND superseded_at IS NULL;

    IF package_currency IS NULL THEN
        RAISE EXCEPTION
            'compensation_component %: package % has no live row',
            NEW.entity_id, NEW.package_ref
            USING ERRCODE = '23514';
    END IF;

    IF package_currency IS DISTINCT FROM NEW.currency THEN
        RAISE EXCEPTION
            'compensation_component %: currency % does not match package % currency %',
            NEW.entity_id, NEW.currency, NEW.package_ref, package_currency
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION compensation_component_forbid_currency_mismatch() IS
    'DB-010 RED: a compensation_component''s currency must match its compensation_package''s currency.';

CREATE TRIGGER compensation_component_forbid_currency_mismatch_trigger BEFORE INSERT ON compensation_component
    FOR EACH ROW EXECUTE FUNCTION compensation_component_forbid_currency_mismatch();
CREATE TRIGGER compensation_component_forbid_inplace_update BEFORE UPDATE ON compensation_component
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE compensation_component ENABLE ROW LEVEL SECURITY;
ALTER TABLE compensation_component FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compensation_component
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON compensation_component TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- CompensationBand
-- ---------------------------------------------------------------------------
CREATE TABLE compensation_band (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    job_code         semantic_key     NOT NULL,
    grade            semantic_key     NOT NULL,
    pay_zone         semantic_key     NOT NULL,
    currency         text             NOT NULL,
    minimum          numeric(19, 4)   NOT NULL,
    midpoint         numeric(19, 4)   NOT NULL,
    maximum          numeric(19, 4)   NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT compensation_band_currency_iso CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT compensation_band_range_ordered CHECK (minimum <= midpoint AND midpoint <= maximum),
    CONSTRAINT compensation_band_range_non_negative CHECK (minimum >= 0),
    CONSTRAINT compensation_band_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT compensation_band_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX compensation_band_history ON compensation_band (tenant_id, entity_id, recorded_at);
CREATE INDEX compensation_band_scope ON compensation_band (tenant_id, job_code, grade, pay_zone) WHERE superseded_at IS NULL;
CREATE TRIGGER compensation_band_forbid_inplace_update BEFORE UPDATE ON compensation_band
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE compensation_band ENABLE ROW LEVEL SECURITY;
ALTER TABLE compensation_band FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compensation_band
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON compensation_band TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- WorkforceBudget. budget_type/unit vocabulary from
-- planning/specs/workforce-budget-authority.md.
-- ---------------------------------------------------------------------------
CREATE TABLE workforce_budget (
    row_id             uuid             NOT NULL,
    tenant_id          tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id          uuid             NOT NULL,
    canonical_id       semantic_key     NOT NULL,
    budget_type        text             NOT NULL,
    owner_system       text             NOT NULL,
    scope              semantic_key     NOT NULL,
    period             semantic_key     NOT NULL,
    currency           text,
    unit               text             NOT NULL,
    available_quantity numeric(19, 4)   NOT NULL,
    baseline_version   semantic_key,
    effective_from     timestamptz      NOT NULL,
    effective_to       timestamptz,
    recorded_at        timestamptz      NOT NULL DEFAULT now(),
    superseded_at      timestamptz,
    digest_algorithm   digest_algorithm NOT NULL,
    digest             content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT workforce_budget_type_allowed CHECK (
        budget_type IN ('HEADCOUNT_CAPACITY', 'COMPENSATION_POOL', 'FINANCE_COST_BUDGET')
    ),
    CONSTRAINT workforce_budget_unit_allowed CHECK (unit IN ('HEAD', 'FTE', 'MONEY', 'PERCENT')),
    CONSTRAINT workforce_budget_currency_iso CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    CONSTRAINT workforce_budget_currency_required_for_money CHECK (unit <> 'MONEY' OR currency IS NOT NULL),
    CONSTRAINT workforce_budget_available_non_negative CHECK (available_quantity >= 0),
    CONSTRAINT workforce_budget_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT workforce_budget_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX workforce_budget_history ON workforce_budget (tenant_id, entity_id, recorded_at);
CREATE INDEX workforce_budget_live ON workforce_budget (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE TRIGGER workforce_budget_forbid_inplace_update BEFORE UPDATE ON workforce_budget
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE workforce_budget ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_budget FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_budget
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON workforce_budget TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- BudgetReservation. budget_reservation_forbid_overcommit sums every other
-- live HELD/COMMITTED reservation against the same budget and refuses an
-- insert that would push the total past the budget's own live
-- available_quantity, and refuses a currency that does not match the
-- budget's (planning/specs/workforce-budget-authority.md's
-- "Reservation: REQUESTED -> HELD -> COMMITTED | RELEASED | EXPIRED").
-- ---------------------------------------------------------------------------
CREATE TABLE budget_reservation (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    budget_ref       uuid             NOT NULL,
    proposal_ref     uuid,
    amount           numeric(19, 4)   NOT NULL,
    currency         text,
    status           text             NOT NULL,
    expiry           timestamptz,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, budget_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    -- proposal_ref names a DB-011 proposal_revision row, owned by a different
    -- migration lane; not foreign-keyed here (out of this todo's tables).
    CONSTRAINT budget_reservation_status_allowed CHECK (
        status IN ('REQUESTED', 'HELD', 'COMMITTED', 'RELEASED', 'EXPIRED')
    ),
    CONSTRAINT budget_reservation_currency_iso CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    CONSTRAINT budget_reservation_amount_non_negative CHECK (amount >= 0),
    CONSTRAINT budget_reservation_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT budget_reservation_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX budget_reservation_history ON budget_reservation (tenant_id, entity_id, recorded_at);
CREATE INDEX budget_reservation_budget ON budget_reservation (tenant_id, budget_ref) WHERE superseded_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION budget_reservation_forbid_overcommit() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    budget_available numeric(19, 4);
    budget_currency  text;
    committed        numeric(19, 4);
BEGIN
    IF NEW.status NOT IN ('HELD', 'COMMITTED') THEN
        RETURN NEW;
    END IF;

    SELECT available_quantity, currency INTO budget_available, budget_currency
    FROM workforce_budget
    WHERE tenant_id = NEW.tenant_id AND entity_id = NEW.budget_ref AND superseded_at IS NULL;

    IF budget_available IS NULL THEN
        RAISE EXCEPTION
            'budget_reservation %: budget % has no live row',
            NEW.entity_id, NEW.budget_ref
            USING ERRCODE = '23514';
    END IF;

    IF budget_currency IS DISTINCT FROM NEW.currency THEN
        RAISE EXCEPTION
            'budget_reservation %: currency % does not match budget % currency %',
            NEW.entity_id, NEW.currency, NEW.budget_ref, budget_currency
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(amount), 0) INTO committed
    FROM budget_reservation
    WHERE tenant_id = NEW.tenant_id
      AND budget_ref = NEW.budget_ref
      AND superseded_at IS NULL
      AND status IN ('HELD', 'COMMITTED')
      AND entity_id <> NEW.entity_id;

    IF committed + NEW.amount > budget_available THEN
        RAISE EXCEPTION
            'budget_reservation %: amount % plus already-committed % would exceed budget % available %',
            NEW.entity_id, NEW.amount, committed, NEW.budget_ref, budget_available
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION budget_reservation_forbid_overcommit() IS
    'DB-010 RED: live HELD/COMMITTED budget_reservation amounts can never exceed the budget''s own live available_quantity, and currency must match.';

CREATE TRIGGER budget_reservation_forbid_overcommit_trigger BEFORE INSERT ON budget_reservation
    FOR EACH ROW EXECUTE FUNCTION budget_reservation_forbid_overcommit();
CREATE TRIGGER budget_reservation_forbid_inplace_update BEFORE UPDATE ON budget_reservation
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE budget_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON budget_reservation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON budget_reservation TO hcmnext_app;

-- +goose Down
DROP POLICY tenant_isolation ON budget_reservation;
REVOKE ALL ON budget_reservation FROM hcmnext_app;
DROP TABLE budget_reservation;
DROP FUNCTION budget_reservation_forbid_overcommit();

DROP POLICY tenant_isolation ON workforce_budget;
REVOKE ALL ON workforce_budget FROM hcmnext_app;
DROP TABLE workforce_budget;

DROP POLICY tenant_isolation ON compensation_band;
REVOKE ALL ON compensation_band FROM hcmnext_app;
DROP TABLE compensation_band;

DROP POLICY tenant_isolation ON compensation_component;
REVOKE ALL ON compensation_component FROM hcmnext_app;
DROP TABLE compensation_component;
DROP FUNCTION compensation_component_forbid_currency_mismatch();

DROP POLICY tenant_isolation ON compensation_package;
REVOKE ALL ON compensation_package FROM hcmnext_app;
DROP TABLE compensation_package;
