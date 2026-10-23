-- +goose Up
-- +goose StatementBegin

-- One challenge per person per local month, fixed when first looked at: the
-- metric and target are picked from the person's last three months and never
-- recomputed, so a good week cannot raise the bar mid-month. A met challenge is
-- an achievements row of kind monthly_challenge_met (source_key 'YYYY-MM');
-- an unmet one is simply never shown again.
CREATE TABLE monthly_challenges (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- First day of the person's local month.
    month      date        NOT NULL,
    metric     text        NOT NULL CHECK (metric IN ('workouts', 'habit_days', 'checkin_days')),
    target     integer     NOT NULL CHECK (target >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, month)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM achievements WHERE kind = 'monthly_challenge_met';
DROP TABLE monthly_challenges;

-- +goose StatementEnd
