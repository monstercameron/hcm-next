-- Owner: experience and data planes. Prototype demo support.
--
-- A worker photo keeps the retained original separate from the small public
-- display derivative. The ingestion pipeline creates both before the worker
-- row is inserted; the all-or-nothing constraint prevents a dangling upload
-- or a UI reference without a retained source.

-- +goose Up
ALTER TABLE journey_worker
	ADD COLUMN job_title text,
	ADD COLUMN profile_photo_original_ref text,
    ADD COLUMN profile_photo_proxy_ref text,
    ADD CONSTRAINT journey_worker_profile_photo_pair CHECK (
        (profile_photo_original_ref IS NULL AND profile_photo_proxy_ref IS NULL)
        OR
        (length(btrim(profile_photo_original_ref)) > 0 AND length(btrim(profile_photo_proxy_ref)) > 0)
    );

-- +goose Down
ALTER TABLE journey_worker
    DROP CONSTRAINT journey_worker_profile_photo_pair,
	DROP COLUMN job_title,
    DROP COLUMN profile_photo_proxy_ref,
    DROP COLUMN profile_photo_original_ref;
