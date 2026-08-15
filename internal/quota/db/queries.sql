-- name: ConsumeQuota :one
-- Count a request against its window and report the running total.
--
-- One statement, because two concurrent requests reading a count and then
-- writing it back would both see the same number and both be allowed. The
-- primary key makes the second arrival an UPDATE, so the increment happens
-- under the row lock rather than in the application.
--
-- The window floor is computed here rather than passed in, so the boundary is
-- the database's clock. Handlers on different pods disagreeing about the time
-- would otherwise put the same request in different windows.
INSERT INTO quota_counters (user_id, action, window_start, used)
VALUES (
    @user_id,
    @action,
    to_timestamp(floor(extract(epoch FROM now()) / @window_seconds::bigint) * @window_seconds::bigint),
    1
)
ON CONFLICT (user_id, action, window_start) DO UPDATE
SET used = quota_counters.used + 1
RETURNING used, window_start;

-- name: SweepQuotaCounters :exec
-- Drop windows that have closed. Nothing reads them — a request always lands in
-- the current window — and without this the table grows one row per user per
-- action per window forever.
DELETE FROM quota_counters
WHERE window_start < @before;
