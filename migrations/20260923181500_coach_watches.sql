-- +goose Up
-- +goose StatementBegin

-- Standing tasks: "every morning at 8, look at my sleep and tell me if it
-- dropped under seven hours". Proposed by the coach through create_watch,
-- confirmed by the person on the approval card, then run by the worker's
-- sweep_watches every fifteen minutes.
CREATE TABLE coach_watches (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The thread the task was set up in, and where its results are posted.
    -- Null once that thread is deleted: the run then posts to the latest chat
    -- rather than losing the task along with the conversation.
    conversation_id uuid        REFERENCES conversations (id) ON DELETE SET NULL,

    -- What is watched ("your sleep") and when to speak up ("it drops under
    -- seven hours"). Kept apart because the confirmation reads them back in
    -- one sentence: "I'll watch X and ping you when Y".
    title           text        NOT NULL,
    condition       text        NOT NULL,

    -- The full instruction the coach runs each time. The only field the
    -- model reads on a run; the two above are for people.
    spec            text        NOT NULL,

    -- When, in the person's own timezone. Daily or weekly on one weekday
    -- (0 = Sunday, matching Go's time.Weekday), at a minute of the day.
    cadence         text        NOT NULL CHECK (cadence IN ('daily', 'weekly')),
    weekday         smallint    CHECK (weekday BETWEEN 0 AND 6),
    minute_of_day   integer     NOT NULL CHECK (minute_of_day BETWEEN 0 AND 1439),

    active          boolean     NOT NULL DEFAULT true,

    -- The next instant this is due. The sweep claims a run by moving this
    -- forward with a conditional update, so two workers that both see the
    -- row due cannot both run it.
    next_run_at     timestamptz NOT NULL,
    last_run_at     timestamptz,

    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT coach_watches_weekly_has_day CHECK (cadence <> 'weekly' OR weekday IS NOT NULL)
);

-- The sweep's only question: what is due now.
CREATE INDEX coach_watches_due_idx ON coach_watches (next_run_at) WHERE active;
CREATE INDEX coach_watches_user_idx ON coach_watches (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS coach_watches;
-- +goose StatementEnd
