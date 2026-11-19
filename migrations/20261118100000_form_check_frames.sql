-- +goose Up
-- +goose StatementBegin

-- A form check is now read from stills the client cut out of the clip, so it
-- goes to the same vision provider as every other photo. The clip itself is
-- kept for playback only.
ALTER TABLE form_analyses
    -- Length of the clip as the client measured it. Zero on checks made before
    -- stills existed; the player then has no timeline to put markers on.
    ADD COLUMN duration_seconds double precision NOT NULL DEFAULT 0
        CHECK (duration_seconds >= 0),
    -- The catalogue exercise the person said they filmed, when they said.
    ADD COLUMN exercise_slug text NOT NULL DEFAULT '';

CREATE TABLE form_analysis_frames (
    analysis_id uuid NOT NULL REFERENCES form_analyses (id) ON DELETE CASCADE,
    -- Position in the clip, from 1. The model names an issue by this number.
    idx         integer NOT NULL CHECK (idx >= 1),
    media_id    uuid NOT NULL REFERENCES media (id) ON DELETE CASCADE,
    at_seconds  double precision NOT NULL CHECK (at_seconds >= 0),

    PRIMARY KEY (analysis_id, idx)
);

CREATE INDEX form_analysis_frames_media_idx ON form_analysis_frames (media_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE form_analysis_frames;

ALTER TABLE form_analyses
    DROP COLUMN exercise_slug,
    DROP COLUMN duration_seconds;

-- +goose StatementEnd
