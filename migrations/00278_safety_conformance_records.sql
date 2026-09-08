-- Owner: data plane. SAFETY-002 conformance records.
-- +goose Up

CREATE TABLE safety_filing_revision (
 tenant_id tenant_ref NOT NULL REFERENCES tenant(tenant_id), row_id uuid NOT NULL,
 case_ref uuid NOT NULL, compartment_ref uuid NOT NULL, filing_id uuid NOT NULL,
 revision cas_version NOT NULL, parent_revision cas_version, parent_digest content_digest,
 canonical_digest content_digest NOT NULL, incident_ref uuid NOT NULL, authority_ref uuid NOT NULL,
 provider_ref uuid NOT NULL, submission_ref text NOT NULL, signer_ref uuid NOT NULL,
 signature_ref text NOT NULL, status text NOT NULL, observation_ref text,
 observed_at timestamptz, observed_at_nanos smallint, obligations text[] NOT NULL DEFAULT '{}',
 PRIMARY KEY (tenant_id,row_id), UNIQUE (tenant_id,case_ref,filing_id,revision),
 CONSTRAINT safety_filing_lineage CHECK ((revision=1 AND parent_revision IS NULL AND parent_digest IS NULL) OR (revision>1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL AND parent_revision=revision-1))
);

CREATE TABLE safety_workers_comp_payment_revision (
 tenant_id tenant_ref NOT NULL REFERENCES tenant(tenant_id), row_id uuid NOT NULL,
 case_ref uuid NOT NULL, compartment_ref uuid NOT NULL, payment_id uuid NOT NULL,
 revision cas_version NOT NULL, parent_revision cas_version, parent_digest content_digest,
 canonical_digest content_digest NOT NULL, claim_ref text NOT NULL, worker_ref uuid NOT NULL,
 amount_minor bigint NOT NULL, currency text NOT NULL, status text NOT NULL,
 observation_ref text NOT NULL, reversal_ref text,
 PRIMARY KEY (tenant_id,row_id), UNIQUE (tenant_id,case_ref,payment_id,revision),
 UNIQUE (tenant_id,reversal_ref),
 CONSTRAINT safety_payment_lineage CHECK ((revision=1 AND parent_revision IS NULL AND parent_digest IS NULL) OR (revision>1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL AND parent_revision=revision-1))
);

CREATE TABLE safety_restriction_clearance_revision (
 tenant_id tenant_ref NOT NULL REFERENCES tenant(tenant_id), row_id uuid NOT NULL,
 case_ref uuid NOT NULL, compartment_ref uuid NOT NULL, clearance_id uuid NOT NULL,
 revision cas_version NOT NULL, parent_revision cas_version, parent_digest content_digest,
 canonical_digest content_digest NOT NULL, restriction_ref uuid NOT NULL, worker_ref uuid NOT NULL,
 evidence_ref text NOT NULL, observation_ref text NOT NULL, authority_ref text NOT NULL,
 evidence_digest text NOT NULL,
 PRIMARY KEY (tenant_id,row_id), UNIQUE (tenant_id,case_ref,clearance_id,revision),
 CONSTRAINT safety_clearance_lineage CHECK ((revision=1 AND parent_revision IS NULL AND parent_digest IS NULL) OR (revision>1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL AND parent_revision=revision-1))
);

CREATE TABLE safety_reconciliation_revision (
 tenant_id tenant_ref NOT NULL REFERENCES tenant(tenant_id), row_id uuid NOT NULL,
 case_ref uuid NOT NULL, compartment_ref uuid NOT NULL, reconciliation_id uuid NOT NULL,
 revision cas_version NOT NULL, parent_revision cas_version, parent_digest content_digest,
 canonical_digest content_digest NOT NULL, incident_ref uuid NOT NULL, source_revision_digest content_digest NOT NULL,
 observation_ref text NOT NULL, amended_filing_ref text, repair_ref text, obligations text[] NOT NULL DEFAULT '{}',
 observed_at timestamptz NOT NULL, observed_at_nanos smallint NOT NULL DEFAULT 0, prior_observed_at timestamptz, prior_observed_at_nanos smallint, status text NOT NULL,
 PRIMARY KEY (tenant_id,row_id), UNIQUE (tenant_id,case_ref,reconciliation_id,revision),
 CONSTRAINT safety_reconciliation_lineage CHECK ((revision=1 AND parent_revision IS NULL AND parent_digest IS NULL) OR (revision>1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL AND parent_revision=revision-1))
);

CREATE TABLE safety_correction_revision (
 tenant_id tenant_ref NOT NULL REFERENCES tenant(tenant_id), row_id uuid NOT NULL,
 case_ref uuid NOT NULL, compartment_ref uuid NOT NULL, correction_id uuid NOT NULL,
 revision cas_version NOT NULL, parent_revision cas_version, parent_digest content_digest,
 canonical_digest content_digest NOT NULL, incident_ref uuid NOT NULL, source_revision_digest content_digest NOT NULL,
 reason text NOT NULL, evidence_ref text NOT NULL, amended_filing_ref text, repair_ref text,
 PRIMARY KEY (tenant_id,row_id), UNIQUE (tenant_id,case_ref,correction_id,revision),
 CONSTRAINT safety_correction_lineage CHECK ((revision=1 AND parent_revision IS NULL AND parent_digest IS NULL) OR (revision>1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL AND parent_revision=revision-1))
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION safety_payment_reversal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p safety_workers_comp_payment_revision;
BEGIN
 IF NEW.status = 'REVERSED' THEN
  IF NEW.reversal_ref IS NULL OR NEW.revision = 1 THEN RAISE EXCEPTION 'invalid safety payment reversal'; END IF;
  SELECT * INTO p FROM safety_workers_comp_payment_revision WHERE tenant_id=NEW.tenant_id AND case_ref=NEW.case_ref AND payment_id=NEW.payment_id AND revision=NEW.parent_revision;
  IF NOT FOUND OR p.status <> 'SETTLED' OR p.canonical_digest <> NEW.parent_digest OR p.amount_minor <> NEW.amount_minor OR p.currency <> NEW.currency OR p.claim_ref <> NEW.claim_ref OR p.worker_ref <> NEW.worker_ref THEN RAISE EXCEPTION 'safety payment reversal does not exactly offset settled parent'; END IF;
 END IF; RETURN NEW;
END $$;
CREATE TRIGGER safety_payment_reversal_guard BEFORE INSERT ON safety_workers_comp_payment_revision FOR EACH ROW EXECUTE FUNCTION safety_payment_reversal_guard();
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION safety_conformance_parent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent_digest_value text; parent_compartment uuid; parent_case uuid; record_id uuid; parent_count bigint;
BEGIN
 IF NEW.revision = 1 THEN RETURN NEW; END IF;
 record_id := (to_jsonb(NEW)->>TG_ARGV[0])::uuid;
 EXECUTE format('SELECT canonical_digest, compartment_ref, case_ref FROM %I WHERE tenant_id=$1 AND %I=$2 AND revision=$3', TG_TABLE_NAME, TG_ARGV[0])
  INTO parent_digest_value, parent_compartment, parent_case USING NEW.tenant_id, record_id, NEW.parent_revision;
 GET DIAGNOSTICS parent_count = ROW_COUNT;
 IF parent_count <> 1 OR parent_digest_value <> NEW.parent_digest OR parent_case <> NEW.case_ref OR parent_compartment <> NEW.compartment_ref THEN
  RAISE EXCEPTION 'safety conformance successor is not bound to its exact parent';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['safety_filing_revision','safety_workers_comp_payment_revision','safety_restriction_clearance_revision','safety_reconciliation_revision','safety_correction_revision'] LOOP
  EXECUTE format('CREATE OR REPLACE TRIGGER %I_append_only BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',t,t);
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t); EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid)',t);
  EXECUTE format('GRANT SELECT, INSERT ON %I TO hcmnext_app',t);
 END LOOP; END $$;
-- +goose StatementEnd

CREATE TRIGGER safety_filing_parent_guard BEFORE INSERT ON safety_filing_revision FOR EACH ROW EXECUTE FUNCTION safety_conformance_parent_guard('filing_id');
CREATE TRIGGER safety_payment_parent_guard BEFORE INSERT ON safety_workers_comp_payment_revision FOR EACH ROW EXECUTE FUNCTION safety_conformance_parent_guard('payment_id');
CREATE TRIGGER safety_clearance_parent_guard BEFORE INSERT ON safety_restriction_clearance_revision FOR EACH ROW EXECUTE FUNCTION safety_conformance_parent_guard('clearance_id');
CREATE TRIGGER safety_reconciliation_parent_guard BEFORE INSERT ON safety_reconciliation_revision FOR EACH ROW EXECUTE FUNCTION safety_conformance_parent_guard('reconciliation_id');
CREATE TRIGGER safety_correction_parent_guard BEFORE INSERT ON safety_correction_revision FOR EACH ROW EXECUTE FUNCTION safety_conformance_parent_guard('correction_id');

-- +goose Down
DROP TABLE safety_correction_revision, safety_reconciliation_revision, safety_restriction_clearance_revision, safety_workers_comp_payment_revision, safety_filing_revision;
DROP FUNCTION safety_conformance_parent_guard();
DROP FUNCTION safety_payment_reversal_guard();
