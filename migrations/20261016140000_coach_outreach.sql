-- +goose Up
-- +goose StatementBegin

-- One unprompted coach message per person per day, and what they did with it.
-- The coach notices something (a low-recovery morning, a missed session),
-- acts or offers, and says so once. action and acted_at are how we learn
-- whether anyone answers.
CREATE TABLE coach_outreach (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The person's local date: the once-a-day limit is theirs, not the server's.
    day        date        NOT NULL,
    trigger    text        NOT NULL CHECK (trigger IN ('low_recovery', 'missed_session')),
    -- What the coach acted on or offered, for the message, the card and the
    -- actions: the session, the reason, which moves were possible.
    facts      jsonb       NOT NULL DEFAULT '{}',
    -- The message as sent, for the Today card and the chat edit after an
    -- answer, and the copy posted into the coach thread.
    body       text        NOT NULL DEFAULT '',
    message_id uuid        REFERENCES messages (id) ON DELETE SET NULL,
    action     text        CHECK (action IN ('undo', 'lighter', 'keep', 'today', 'tomorrow', 'let_go')),
    acted_at   timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, day)
);

-- Who made a lighter day: the person answering the offer, or the coach on a
-- low-recovery morning (which the person can undo).
ALTER TABLE lighter_days
    ADD COLUMN source text NOT NULL DEFAULT 'person' CHECK (source IN ('person', 'coach'));

-- coach_check_ins: the coach may message first, at most once a day.
-- coach_adjusts_training: on a low-recovery morning the coach lightens the
-- session itself; off, it only offers.
ALTER TABLE user_notification_prefs
    ADD COLUMN coach_check_ins        boolean NOT NULL DEFAULT true,
    ADD COLUMN coach_adjusts_training boolean NOT NULL DEFAULT true;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_notification_prefs
    DROP COLUMN coach_adjusts_training,
    DROP COLUMN coach_check_ins;
ALTER TABLE lighter_days DROP COLUMN source;
DROP TABLE coach_outreach;
-- +goose StatementEnd
