-- +goose Up
-- +goose StatementBegin

-- The first week's morning message has one answer, Done, which records the
-- day's action. The constraint is the one coach_outreach was created with.
ALTER TABLE coach_outreach DROP CONSTRAINT coach_outreach_action_check;
ALTER TABLE coach_outreach ADD CONSTRAINT coach_outreach_action_check
    CHECK (action IN ('undo', 'lighter', 'keep', 'today', 'tomorrow', 'let_go', 'done'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE coach_outreach SET action = NULL, acted_at = NULL WHERE action = 'done';
ALTER TABLE coach_outreach DROP CONSTRAINT coach_outreach_action_check;
ALTER TABLE coach_outreach ADD CONSTRAINT coach_outreach_action_check
    CHECK (action IN ('undo', 'lighter', 'keep', 'today', 'tomorrow', 'let_go'));
-- +goose StatementEnd
