-- +goose Up
-- Records whether a user's own provider can actually call Khepri's tools.
--
-- Why this is worth a column. A BYOK provider that accepts a tools array and
-- ignores it costs the coach every write capability — logging water, swapping
-- an exercise, recording a check-in — and the whole grounding mechanism with
-- it. There is no error: the model answers in prose and looks fine.
--
-- Found in production. A gateway serving an agent behind an OpenAI-compatible
-- facade returned the catalogue's own pull-up row, verbatim and correct, while
-- reporting no tool calls at all: it had resolved the lookup on its own side.
-- The coach then told its user "I don't have written pull-up cues in the
-- catalogue" about a row holding 368 characters of them. Nothing logged, and
-- it took reading the spend ledger to find which provider had answered.
--
-- NULL means not yet probed, which is every row that predates this and any
-- provider whose capability could not be established. Only an explicit false
-- changes what the coach does.
ALTER TABLE user_ai_credentials
  ADD COLUMN supports_tools boolean;

COMMENT ON COLUMN user_ai_credentials.supports_tools IS
  'NULL = unprobed, true = returns tool_calls, false = ignores the tools array';

-- +goose Down
ALTER TABLE user_ai_credentials
  DROP COLUMN supports_tools;
