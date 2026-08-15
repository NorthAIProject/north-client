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
