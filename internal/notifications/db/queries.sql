-- name: GetUserNotificationPrefs :one
SELECT * FROM user_notification_prefs WHERE user_id = $1;

-- name: UpsertUserNotificationPrefs :one
INSERT INTO user_notification_prefs (
    user_id, nudge_missed_checkin, nudge_goal_deadline,
    weekly_report_auto, quiet_hours_enabled, quiet_start, quiet_end
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE
SET nudge_missed_checkin = EXCLUDED.nudge_missed_checkin,
    nudge_goal_deadline  = EXCLUDED.nudge_goal_deadline,
    weekly_report_auto   = EXCLUDED.weekly_report_auto,
    quiet_hours_enabled  = EXCLUDED.quiet_hours_enabled,
    quiet_start          = EXCLUDED.quiet_start,
    quiet_end            = EXCLUDED.quiet_end,
    updated_at           = now()
RETURNING *;
