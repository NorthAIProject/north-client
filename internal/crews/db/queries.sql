-- name: CreateCrew :one
INSERT INTO crews (name, owner_id, code) VALUES ($1, $2, $3) RETURNING *;

-- name: AddMember :execrows
INSERT INTO crew_members (crew_id, user_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveMember :execrows
DELETE FROM crew_members WHERE crew_id = $1 AND user_id = $2;

-- name: CrewByCode :one
SELECT * FROM crews WHERE code = $1;

-- name: GetCrew :one
SELECT * FROM crews WHERE id = $1;

-- name: CountMembers :one
SELECT count(*) FROM crew_members WHERE crew_id = $1;

-- name: IsMember :one
SELECT EXISTS (SELECT 1 FROM crew_members WHERE crew_id = $1 AND user_id = $2);

-- name: ListMine :many
SELECT c.*, (SELECT count(*) FROM crew_members m2 WHERE m2.crew_id = c.id) AS members
FROM crews c JOIN crew_members m ON m.crew_id = c.id
WHERE m.user_id = $1
ORDER BY c.created_at;

-- name: Members :many
SELECT u.id, u.display_name, u.handle, u.timezone, u.tier, m.joined_at, m.role
FROM crew_members m JOIN users u ON u.id = m.user_id
WHERE m.crew_id = $1
ORDER BY m.joined_at;

-- name: OldestOtherMember :one
-- Who takes over when the owner leaves: the longest-standing admin, else the
-- longest-standing member.
SELECT user_id FROM crew_members
WHERE crew_id = $1 AND user_id <> $2
ORDER BY (role = 'admin') DESC, joined_at LIMIT 1;

-- name: MemberRole :one
SELECT role FROM crew_members WHERE crew_id = $1 AND user_id = $2;

-- name: SetMemberRole :execrows
UPDATE crew_members SET role = $3 WHERE crew_id = $1 AND user_id = $2;

-- name: CreateDefaultChannels :exec
-- Every crew starts with #general and #wins; the crew-spaces migration gave
-- the same two to crews made before.
INSERT INTO crew_channels (crew_id, kind, name, is_default, position)
VALUES ($1, 'text', 'general', true, 0), ($1, 'wins', 'wins', false, 1);

-- name: ListChannels :many
SELECT * FROM crew_channels WHERE crew_id = $1 ORDER BY position, name;

-- name: SetOwner :exec
UPDATE crews SET owner_id = $2 WHERE id = $1;

-- name: DeleteCrew :exec
DELETE FROM crews WHERE id = $1;

-- name: Rename :exec
UPDATE crews SET name = $2 WHERE id = $1;

-- name: GetChallenge :one
SELECT * FROM crew_challenges WHERE crew_id = $1;

-- name: SetChallenge :exec
INSERT INTO crew_challenges (crew_id, kind, target) VALUES ($1, $2, $3)
ON CONFLICT (crew_id) DO UPDATE SET kind = EXCLUDED.kind, target = EXCLUDED.target, created_at = now();

-- name: ClearChallenge :exec
DELETE FROM crew_challenges WHERE crew_id = $1;

-- name: CrewmatesOf :many
-- Everybody who shares at least one crew with the user, once each.
SELECT DISTINCT u.id, u.display_name, u.timezone
FROM crew_members mine
JOIN crew_members other ON other.crew_id = mine.crew_id AND other.user_id <> mine.user_id
JOIN users u ON u.id = other.user_id
WHERE mine.user_id = $1;

-- name: ChallengesOf :many
-- The user's crews that have a challenge, with it and when it was last set.
SELECT c.id, c.name, ch.kind, ch.target, ch.created_at
FROM crew_members m
JOIN crews c ON c.id = m.crew_id
JOIN crew_challenges ch ON ch.crew_id = c.id
WHERE m.user_id = $1
ORDER BY c.created_at;

-- name: ChannelInCrew :one
SELECT * FROM crew_channels WHERE id = $1 AND crew_id = $2;

-- name: MemberIDsByHandle :many
SELECT u.id FROM crew_members m JOIN users u ON u.id = m.user_id
WHERE m.crew_id = @crew_id AND lower(u.handle::text) = ANY(@handles::text[]);

-- name: InsertMessage :one
-- A repeated client_id from the same author is the same message sent twice;
-- no row comes back and the caller looks the first one up.
INSERT INTO crew_messages (crew_id, channel_id, author_id, kind, body, reply_to_id, mentions, mention_all, client_id)
VALUES (@crew_id, @channel_id, @author_id, 'text', @body, sqlc.narg(reply_to_id), @mentions, @mention_all, sqlc.narg(client_id))
ON CONFLICT (author_id, client_id) DO NOTHING
RETURNING id, seq;

-- name: MessageByClientID :one
SELECT id FROM crew_messages WHERE author_id = $1 AND client_id = $2;

-- name: InsertSystemMessage :exec
INSERT INTO crew_messages (crew_id, channel_id, kind, body) VALUES ($1, $2, 'system', $3);

-- The three reads below share one shape: a message as @viewer sees it. A
-- message is visible when it is not hidden, its author and the viewer have not
-- blocked each other, and the viewer has not reported it. The quoted reply is
-- unavailable on the same terms, or when deleted.

-- name: MessagesAfter :many
SELECT m.id, m.seq, m.channel_id, m.author_id, m.kind, m.body, m.mentions, m.mention_all,
       m.pinned_at, m.edited_at, m.deleted_at, m.created_at,
       au.display_name AS author_name, au.handle AS author_handle,
       r.id AS reply_id, ru.display_name AS reply_author_name, r.body AS reply_body,
       (r.id IS NOT NULL AND (r.deleted_at IS NOT NULL OR r.hidden_at IS NOT NULL
         OR EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = r.author_id) OR (b.blocker_id = r.author_id AND b.blocked_id = @viewer))
         OR EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = r.id)))::boolean AS reply_unavailable
FROM crew_messages m
LEFT JOIN users au ON au.id = m.author_id
LEFT JOIN crew_messages r ON r.id = m.reply_to_id
LEFT JOIN users ru ON ru.id = r.author_id
WHERE m.channel_id = @channel_id AND m.seq > @after
  AND m.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = m.author_id) OR (b.blocker_id = m.author_id AND b.blocked_id = @viewer))
  AND NOT EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = m.id)
ORDER BY m.seq
LIMIT @lim;

-- name: MessagesBefore :many
SELECT m.id, m.seq, m.channel_id, m.author_id, m.kind, m.body, m.mentions, m.mention_all,
       m.pinned_at, m.edited_at, m.deleted_at, m.created_at,
       au.display_name AS author_name, au.handle AS author_handle,
       r.id AS reply_id, ru.display_name AS reply_author_name, r.body AS reply_body,
       (r.id IS NOT NULL AND (r.deleted_at IS NOT NULL OR r.hidden_at IS NOT NULL
         OR EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = r.author_id) OR (b.blocker_id = r.author_id AND b.blocked_id = @viewer))
         OR EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = r.id)))::boolean AS reply_unavailable
FROM crew_messages m
LEFT JOIN users au ON au.id = m.author_id
LEFT JOIN crew_messages r ON r.id = m.reply_to_id
LEFT JOIN users ru ON ru.id = r.author_id
WHERE m.channel_id = @channel_id AND m.seq < @before
  AND m.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = m.author_id) OR (b.blocker_id = m.author_id AND b.blocked_id = @viewer))
  AND NOT EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = m.id)
ORDER BY m.seq DESC
LIMIT @lim;

-- name: MessageForViewer :one
SELECT m.id, m.seq, m.channel_id, m.author_id, m.kind, m.body, m.mentions, m.mention_all,
       m.pinned_at, m.edited_at, m.deleted_at, m.created_at,
       au.display_name AS author_name, au.handle AS author_handle,
       r.id AS reply_id, ru.display_name AS reply_author_name, r.body AS reply_body,
       (r.id IS NOT NULL AND (r.deleted_at IS NOT NULL OR r.hidden_at IS NOT NULL
         OR EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = r.author_id) OR (b.blocker_id = r.author_id AND b.blocked_id = @viewer))
         OR EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = r.id)))::boolean AS reply_unavailable
FROM crew_messages m
LEFT JOIN users au ON au.id = m.author_id
LEFT JOIN crew_messages r ON r.id = m.reply_to_id
LEFT JOIN users ru ON ru.id = r.author_id
WHERE m.id = @id AND m.crew_id = @crew_id
  AND m.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = m.author_id) OR (b.blocker_id = m.author_id AND b.blocked_id = @viewer))
  AND NOT EXISTS (SELECT 1 FROM content_reports cr WHERE cr.reporter_id = @viewer AND cr.subject_kind = 'crew_message' AND cr.subject_id = m.id);

-- name: ReactionsFor :many
-- Reactions on ids as @viewer sees them: none from anyone blocked either way.
SELECT x.message_id, x.emoji, count(*)::int AS n, bool_or(x.user_id = @viewer)::boolean AS mine
FROM crew_message_reactions x
WHERE x.message_id = ANY(@ids::uuid[])
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @viewer AND b.blocked_id = x.user_id) OR (b.blocker_id = x.user_id AND b.blocked_id = @viewer))
GROUP BY x.message_id, x.emoji;
