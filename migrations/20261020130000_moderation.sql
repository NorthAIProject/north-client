-- +goose Up
-- +goose StatementBegin

-- Moderation is shared by every place people write to each other (crews, the
-- daily post's comments). Subjects are named by kind and id rather than by
-- foreign key, so a new kind of content needs no change here. The weekly
-- review already owns the name "reports", hence content_reports.
CREATE TABLE content_reports (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject_kind       text NOT NULL,
    subject_id         uuid NOT NULL,
    subject_author_id  uuid REFERENCES users(id) ON DELETE CASCADE,
    -- scope_id is where the subject lives, a crew for example, so that
    -- place's admins can see its reports.
    scope_id           uuid,
    reason             text NOT NULL CHECK (reason IN ('spam', 'harassment', 'hate', 'sexual', 'violence', 'self_harm', 'other')),
    note               text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    status             text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'actioned', 'dismissed')),
    resolved_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    resolved_at        timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (reporter_id, subject_kind, subject_id)
);
CREATE INDEX content_reports_subject ON content_reports (subject_kind, subject_id);
CREATE INDEX content_reports_open ON content_reports (created_at) WHERE status = 'open';
CREATE INDEX content_reports_scope ON content_reports (scope_id) WHERE scope_id IS NOT NULL;

-- Every hide, delete and suspension, by whom; actor_id NULL is automatic.
CREATE TABLE moderation_actions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      uuid REFERENCES users(id) ON DELETE SET NULL,
    subject_kind  text NOT NULL,
    subject_id    uuid NOT NULL,
    action        text NOT NULL CHECK (action IN ('hide', 'unhide', 'delete', 'suspend', 'unsuspend')),
    reason        text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_actions_subject ON moderation_actions (subject_kind, subject_id);

-- The rules a person accepts before their first post anywhere; a new
-- version asks again.
CREATE TABLE ugc_terms_acceptances (
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    version      smallint NOT NULL,
    accepted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, version)
);

-- A suspended person reads but cannot post; until NULL is open-ended.
CREATE TABLE ugc_suspensions (
    user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    until       timestamptz,
    reason      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ugc_suspensions;
DROP TABLE ugc_terms_acceptances;
DROP TABLE moderation_actions;
DROP TABLE content_reports;
-- +goose StatementEnd
