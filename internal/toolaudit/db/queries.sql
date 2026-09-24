-- name: RecordToolExecution :one
INSERT INTO tool_executions (user_id, tool, arguments, surface, outcome, detail)
VALUES (@user_id, @tool, @arguments, @surface, @outcome, @detail)
RETURNING *;

-- name: ListToolExecutions :many
-- One person's executions, newest first. The only read this table has.
SELECT * FROM tool_executions
WHERE user_id = @user_id
ORDER BY created_at DESC
LIMIT @row_limit;

-- name: ToolArgumentsSince :many
-- The arguments of one tool's successful runs on one surface since a moment,
-- oldest first. How the coach learns what an agent read over MCP during a turn
-- it answered without calling tools itself.
SELECT arguments FROM tool_executions
WHERE user_id = @user_id
  AND tool = @tool
  AND surface = @surface
  AND outcome = 'executed'
  AND created_at >= @since
ORDER BY created_at;
