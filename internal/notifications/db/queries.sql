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
    coach_activity, training_reminders
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
    updated_at           = now()
RETURNING *;
