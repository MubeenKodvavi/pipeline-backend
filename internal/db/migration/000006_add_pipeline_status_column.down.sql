BEGIN;

ALTER TABLE pipeline ALTER COLUMN status DROP DEFAULT;

ALTER TABLE pipeline ALTER COLUMN status TYPE boolean
USING CASE WHEN status='STATUS_INACTIVE'::valid_status THEN FALSE ELSE TRUE END;

ALTER TABLE pipeline ALTER COLUMN status SET DEFAULT FALSE;

ALTER TABLE pipeline RENAME COLUMN status TO active;

DROP TYPE valid_status;

COMMIT;