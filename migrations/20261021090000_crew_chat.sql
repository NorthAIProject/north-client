-- +goose Up
-- +goose StatementBegin

-- Crew chat. seq is the cursor clients page and catch up by: it only grows,
-- so "everything after 1234" never skips a message, whoever sent it.
--
-- Every write to these tables also runs pg_notify('crew_events', ...) in the
-- same transaction, carrying ids only. The web process turns those into live
-- signals; clients then fetch the messages themselves, rendered for them, so
-- blocks, hides and language are always applied on the server.
--
-- A system message has no author and holds an i18n key in body, so each
-- member reads it in their own language.
CREATE TABLE crew_messages (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq          bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    crew_id      uuid NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    channel_id   uuid NOT NULL REFERENCES crew_channels(id) ON DELETE CASCADE,
    author_id    uuid REFERENCES users(id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('text', 'system')),
    body         text NOT NULL CHECK (char_length(body) <= 4000),
    reply_to_id  uuid REFERENCES crew_messages(id) ON DELETE SET NULL,
    mentions     uuid[] NOT NULL DEFAULT '{}',
    mention_all  boolean NOT NULL DEFAULT false,
    client_id    uuid,
    pinned_at    timestamptz,
    pinned_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    edited_at    timestamptz,
    deleted_at   timestamptz,
    deleted_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    hidden_at    timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (author_id, client_id),
    CHECK ((kind = 'system') = (author_id IS NULL)),
    -- A deleted message keeps its place in the conversation, not its words.
    CHECK (deleted_at IS NULL OR body = '')
);
CREATE INDEX crew_messages_channel_seq ON crew_messages (channel_id, seq);
CREATE INDEX crew_messages_pins ON crew_messages (channel_id, pinned_at) WHERE pinned_at IS NOT NULL;

CREATE TABLE crew_message_reactions (
    message_id  uuid NOT NULL REFERENCES crew_messages(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji       text NOT NULL CHECK (emoji IN ('fire', 'muscle', 'clap', 'heart', 'laugh', 'hundred', 'trophy', 'eyes')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id, emoji)
);

-- How far each member has read each channel, by seq.
CREATE TABLE crew_channel_reads (
    channel_id     uuid NOT NULL REFERENCES crew_channels(id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_seq  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, user_id)
);

-- Crews from before chat learn they have channels now.
INSERT INTO crew_messages (crew_id, channel_id, kind, body)
SELECT ch.crew_id, ch.id, 'system', 'crews.sys.spaces'
FROM crew_channels ch
WHERE ch.is_default
  AND NOT EXISTS (
      SELECT 1 FROM crew_messages m WHERE m.channel_id = ch.id AND m.body = 'crews.sys.spaces'
  );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE crew_channel_reads;
DROP TABLE crew_message_reactions;
DROP TABLE crew_messages;
-- +goose StatementEnd
