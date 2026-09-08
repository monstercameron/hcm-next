-- Owner: skill data lane. Adds durable append-only correction lineage to the
-- worker-skill evidence ledger created by 00116_skill.sql.
-- +goose Up

ALTER TABLE worker_skill_evidence
    ADD COLUMN IF NOT EXISTS supersedes_evidence_id text;

ALTER TABLE worker_skill_evidence
    ADD CONSTRAINT worker_skill_evidence_identity_unique
    UNIQUE (tenant_id, evidence_id);

ALTER TABLE worker_skill_evidence
    ADD CONSTRAINT worker_skill_evidence_worker_skill_identity_unique
    UNIQUE (tenant_id, evidence_id, worker_ref, skill_ref);

ALTER TABLE worker_skill_evidence
    ADD CONSTRAINT worker_skill_evidence_not_self_superseding
    CHECK (supersedes_evidence_id IS NULL OR supersedes_evidence_id <> evidence_id);

ALTER TABLE worker_skill_evidence
    ADD CONSTRAINT worker_skill_evidence_supersedes_same_worker_skill
    FOREIGN KEY (tenant_id, supersedes_evidence_id, worker_ref, skill_ref)
    REFERENCES worker_skill_evidence (tenant_id, evidence_id, worker_ref, skill_ref);

CREATE UNIQUE INDEX IF NOT EXISTS worker_skill_evidence_one_successor
    ON worker_skill_evidence (tenant_id, supersedes_evidence_id)
    WHERE supersedes_evidence_id IS NOT NULL;

-- Event sequence is the append order. Requiring a successor to point only
-- backward makes correction cycles impossible while preserving legacy rows
-- (whose nullable lineage remains NULL).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_worker_skill_evidence_supersession()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    predecessor_sequence bigint;
    predecessor_effective_from timestamptz;
BEGIN
    IF NEW.supersedes_evidence_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT event_sequence, effective_from
      INTO predecessor_sequence, predecessor_effective_from
      FROM worker_skill_evidence
     WHERE tenant_id = NEW.tenant_id
       AND evidence_id = NEW.supersedes_evidence_id
       AND worker_ref = NEW.worker_ref
       AND skill_ref = NEW.skill_ref;
    IF predecessor_sequence IS NULL THEN
        RAISE EXCEPTION 'superseded skill evidence predecessor is missing';
    END IF;
    IF predecessor_sequence >= NEW.event_sequence THEN
        RAISE EXCEPTION 'skill evidence successor must have a later event sequence';
    END IF;
    IF NEW.effective_from < predecessor_effective_from THEN
        RAISE EXCEPTION 'skill evidence successor cannot predate predecessor';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER worker_skill_evidence_supersession_fence
    BEFORE INSERT ON worker_skill_evidence
    FOR EACH ROW EXECUTE FUNCTION validate_worker_skill_evidence_supersession();

-- +goose Down
DROP TRIGGER IF EXISTS worker_skill_evidence_supersession_fence ON worker_skill_evidence;
DROP FUNCTION IF EXISTS validate_worker_skill_evidence_supersession();
DROP INDEX IF EXISTS worker_skill_evidence_one_successor;
ALTER TABLE worker_skill_evidence DROP CONSTRAINT IF EXISTS worker_skill_evidence_supersedes_same_worker_skill;
ALTER TABLE worker_skill_evidence DROP CONSTRAINT IF EXISTS worker_skill_evidence_not_self_superseding;
ALTER TABLE worker_skill_evidence DROP CONSTRAINT IF EXISTS worker_skill_evidence_worker_skill_identity_unique;
ALTER TABLE worker_skill_evidence DROP CONSTRAINT IF EXISTS worker_skill_evidence_identity_unique;
ALTER TABLE worker_skill_evidence DROP COLUMN IF EXISTS supersedes_evidence_id;
