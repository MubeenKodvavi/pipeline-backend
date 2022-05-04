BEGIN;
CREATE TYPE valid_mode AS ENUM (
  'MODE_UNSPECIFIED',
  'MODE_SYNC',
  'MODE_ASYNC'
);
CREATE TYPE valid_state AS ENUM (
  'STATE_UNSPECIFIED',
  'STATE_INACTIVE',
  'STATE_ACTIVE',
  'STATE_ERROR'
);
CREATE TABLE IF NOT EXISTS public.pipeline (
  uid UUID NOT NULL,
  id VARCHAR(255) NOT NULL,
  owner VARCHAR(255) NOT NULL,
  description VARCHAR(1023) NULL,
  recipe JSONB NOT NULL,
  mode VALID_MODE DEFAULT 'MODE_UNSPECIFIED' NOT NULL,
  state VALID_STATE DEFAULT 'STATE_UNSPECIFIED' NOT NULL,
  create_time TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP NOT NULL,
  update_time TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP NOT NULL,
  delete_time TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP NULL,
  CONSTRAINT pipeline_pkey PRIMARY KEY (uid)
);
CREATE UNIQUE INDEX unique_owner_id_delete_time ON public.pipeline (owner, id)
WHERE delete_time IS NULL;
CREATE INDEX pipeline_id_create_time_pagination ON public.pipeline (uid, create_time);
COMMIT;
