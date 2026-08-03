-- +goose Up
-- +goose StatementBegin

CREATE TABLE media (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    kind         text        NOT NULL,
    mime_type    text        NOT NULL,
    size_bytes   bigint      NOT NULL,

    -- Key in North's own object storage. This is the durable copy: provider
    -- file URIs expire within days, so anything needed later is re-uploaded
    -- from here rather than kept at the provider.
    storage_key  text        NOT NULL UNIQUE,

    original_name text       NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX media_user_created_idx ON media (user_id, created_at DESC);

CREATE TABLE form_analyses (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    media_id   uuid        NOT NULL REFERENCES media (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- pending -> running -> done | failed. The upload returns immediately and
    -- the page polls this, because analysing a video takes far longer than a
    -- request should.
    status     text        NOT NULL DEFAULT 'pending'
                           CHECK (status IN ('pending', 'running', 'done', 'failed')),

    analysis   jsonb,
    error      text        NOT NULL DEFAULT '',

    model      text        NOT NULL DEFAULT '',
    provider   text        NOT NULL DEFAULT '',

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX form_analyses_user_created_idx ON form_analyses (user_id, created_at DESC);
CREATE UNIQUE INDEX form_analyses_media_idx ON form_analyses (media_id);

CREATE TABLE jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text        NOT NULL,
    payload     jsonb       NOT NULL DEFAULT '{}'::jsonb,

    status      text        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'running', 'done', 'failed')),

    attempts    smallint    NOT NULL DEFAULT 0,
    max_attempts smallint   NOT NULL DEFAULT 3,

    -- When this job becomes eligible. Backoff is a future timestamp rather than
    -- a sleeping goroutine, so a restart does not lose the retry schedule.
    run_after   timestamptz NOT NULL DEFAULT now(),

    last_error  text        NOT NULL DEFAULT '',

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- The claim query orders by run_after among pending rows, so this is the index
-- that keeps the worker's poll cheap as the table grows.
CREATE INDEX jobs_claimable_idx ON jobs (status, run_after) WHERE status = 'pending';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE jobs;
DROP TABLE form_analyses;
DROP TABLE media;
-- +goose StatementEnd
