-- name: GetAlertSchedule :one
SELECT * FROM user_alert_schedules
WHERE user_id = $1 AND kind = $2;

-- name: ListAlertSchedules :many
SELECT * FROM user_alert_schedules
WHERE user_id = $1
ORDER BY kind;

-- name: UpsertAlertSchedule :one
INSERT INTO user_alert_schedules (user_id, kind, enabled, every_days, reminder_days)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, kind) DO UPDATE
SET enabled       = EXCLUDED.enabled,
    every_days    = EXCLUDED.every_days,
    reminder_days = EXCLUDED.reminder_days,
    updated_at    = now()
RETURNING *;

-- name: GetUserNotificationPrefs :one
SELECT * FROM user_notification_prefs WHERE user_id = $1;

-- name: UpsertUserNotificationPrefs :one
INSERT INTO user_notification_prefs (
    user_id, nudge_missed_checkin, nudge_goal_deadline,
    weekly_report_auto, daily_briefing_auto, quiet_hours_enabled, quiet_start, quiet_end,
    coach_activity, training_reminders, stats_digest_cadence
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (user_id) DO UPDATE
SET nudge_missed_checkin = EXCLUDED.nudge_missed_checkin,
    nudge_goal_deadline  = EXCLUDED.nudge_goal_deadline,
    weekly_report_auto   = EXCLUDED.weekly_report_auto,
    daily_briefing_auto  = EXCLUDED.daily_briefing_auto,
    quiet_hours_enabled  = EXCLUDED.quiet_hours_enabled,
    quiet_start          = EXCLUDED.quiet_start,
    quiet_end            = EXCLUDED.quiet_end,
    coach_activity       = EXCLUDED.coach_activity,
    training_reminders   = EXCLUDED.training_reminders,
    stats_digest_cadence = EXCLUDED.stats_digest_cadence,
    updated_at           = now()
RETURNING *;

-- name: ClaimStatsDigest :one
-- Records that a digest is going out, and reports whether this call is the one
-- that gets to send it.
--
-- The insert is the lock. Two sweeps racing on the same window both attempt
-- it, exactly one inserts a row, and the loser is told nothing was claimed —
-- which is cheaper and more honest than reading first and hoping.
INSERT INTO stats_digest_sends (user_id, cadence, period_start)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, cadence, period_start) DO NOTHING
RETURNING sent_at;
