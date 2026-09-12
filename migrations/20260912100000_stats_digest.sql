-- +goose Up
-- How often this person wants their numbers pushed to them.
--
-- One column rather than three booleans. A person wants one cadence; three
-- flags admit eight states of which four are nonsense, and "daily and monthly
-- but not weekly" is not a preference anybody holds.
--
-- Default 'off', matching weekly_report_auto and daily_briefing_auto, though
-- for a different reason. Those are off because each run spends a generation.
-- This one is read straight out of Postgres and costs nothing per send — but
-- cost is not the only reason to ask first. Shipping it on would mean every
-- account with a linked chat receiving a message on deploy day that nobody
-- asked for, about a feature nobody has seen. The switch is on the settings
-- page; people can turn it on once they have looked at the page it describes.
ALTER TABLE user_notification_prefs
  ADD COLUMN stats_digest_cadence text NOT NULL DEFAULT 'off'
    CHECK (stats_digest_cadence IN ('off', 'daily', 'weekly', 'monthly'));

COMMENT ON COLUMN user_notification_prefs.stats_digest_cadence IS
  'How often the insights digest is sent: off, daily, weekly or monthly.';

-- Which digests have already gone out.
--
-- The sweeper runs hourly and its gate is deliberately loose — a weekly digest
-- is due on any of Monday through Wednesday, a monthly one on any of the first
-- three days — so that a worker which was down for a day still catches up.
-- That only works because sending is guarded here instead: the gate says "this
-- could be sent", and an insert that changes nothing says "it already was".
--
-- Keyed by the window rather than by the send time, so the same week can never
-- be delivered twice however many times the sweep fires.
CREATE TABLE stats_digest_sends (
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cadence      text        NOT NULL,
    period_start date        NOT NULL,
    sent_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, cadence, period_start)
);

-- +goose Down
DROP TABLE stats_digest_sends;

ALTER TABLE user_notification_prefs
  DROP COLUMN stats_digest_cadence;
