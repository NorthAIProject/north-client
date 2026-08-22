-- +goose Up
-- +goose StatementBegin

-- One row per model call: what it was for, what it cost, and who to attribute
-- it to.
--
-- A table of its own rather than more columns on messages, because most model
-- calls are not messages. Weekly reports, daily briefings, memory extraction,
-- conversation summarisation, form-video analysis, workout planning and every
-- embedding go through a provider and none of them produce a chat row — so a
-- cost figure derived from messages understates reality by however much the
-- scheduled sweeps consume, which is exactly the number pricing depends on.
--
-- messages.usage stays where it is. It is a chat artifact, written for the turn
-- it belongs to; this is an accounting record.
CREATE TABLE ai_generations (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Nullable: not every call belongs to an account. A model warm-up or a
    -- future system-level call has no user, and recording that as a real user's
    -- spend would be worse than recording it as nobody's.
    user_id       uuid        REFERENCES users (id) ON DELETE SET NULL,

    -- What the call was for: coach, telegram, weekly_review, embedding, and so
    -- on. Free text validated in Go, the same choice quota_counters.action and
    -- health_metrics.metric make — labelling a new call path should be a line of
    -- Go, not a migration, or it will not get labelled.
    surface       text        NOT NULL,

    provider      text        NOT NULL,

    -- The model the provider actually answered with, not the configured one.
    -- AI_MODEL ships empty so each provider picks its own, so the configured
    -- value is usually nothing at all. Nullable because a provider that reports
    -- no model leaves this unknown, and an unknown model must be visible as a
    -- gap rather than guessed into a price.
    model         text,

    input_tokens  integer     NOT NULL DEFAULT 0,
    output_tokens integer     NOT NULL DEFAULT 0,

    -- Micros of the accounting currency, not a float. Money in floating point
    -- is how rounding errors become invoices. Zero when the model has no price
    -- in the table, which is a gap to fix rather than a call that was free.
    cost_micros   bigint      NOT NULL DEFAULT 0,

    -- Whether a rate was found at all. Without this, a model priced at zero is
    -- indistinguishable from one nobody has priced: both store a cost of zero.
    -- The free floor really does cost nothing, so counting it as a pricing gap
    -- would cry wolf on every report and train the reader to ignore the
    -- warning that matters.
    priced        boolean     NOT NULL DEFAULT false,

    -- True when the user's own key paid for it. Their spend is not our cost, and
    -- counting it would overstate COGS for exactly the users who cost least.
    byok          boolean     NOT NULL DEFAULT false,

    created_at    timestamptz NOT NULL DEFAULT now()
);

-- The shape every question about this table has: one account over a date range.
CREATE INDEX ai_generations_user_created_idx
    ON ai_generations (user_id, created_at DESC);

-- And the aggregate that prices a model, which reads across all accounts.
CREATE INDEX ai_generations_created_idx
    ON ai_generations (created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ai_generations;
-- +goose StatementEnd
