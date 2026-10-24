-- name: Record :one
-- The unique key is the dedupe: the same moment recorded twice is one row.
INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, kind, source_key) DO NOTHING
RETURNING id;

-- name: ListSharing :many
SELECT category, shared FROM achievement_sharing WHERE user_id = $1;

-- name: SetSharing :exec
INSERT INTO achievement_sharing (user_id, category, shared)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, category) DO UPDATE SET shared = EXCLUDED.shared;

-- name: Feed :many
-- Your own achievements, and those of people you follow (accepted) in the
-- categories they share, minus anybody blocked either way. Newest first,
-- before a cursor.
SELECT a.id, a.user_id, a.category, a.kind, a.title, a.detail, a.occurred_at, a.source_key, a.private,
       u.display_name, u.handle, mc.metric AS monthly_metric, mc.target AS monthly_target,
       (SELECT count(*) FROM kudos k WHERE k.achievement_id = a.id) AS kudos,
       EXISTS (SELECT 1 FROM kudos k WHERE k.achievement_id = a.id AND k.user_id = sqlc.arg('viewer')) AS kudoed
FROM achievements a
JOIN users u ON u.id = a.user_id
LEFT JOIN monthly_challenges mc ON a.kind = 'monthly_challenge_met' AND mc.user_id = a.user_id
  AND mc.month = CASE WHEN a.kind = 'monthly_challenge_met' THEN (a.source_key || '-01')::date END
WHERE a.occurred_at < sqlc.arg('before')
  AND (
    a.user_id = sqlc.arg('viewer')
    OR (
      EXISTS (SELECT 1 FROM follows f
              WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = a.user_id AND f.status = 'accepted')
      AND NOT a.private
      AND sharing_on(a.user_id, a.category)
      AND NOT EXISTS (SELECT 1 FROM blocks b
                      WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = a.user_id)
                         OR (b.blocker_id = a.user_id AND b.blocked_id = sqlc.arg('viewer')))
    )
  )
-- Ties break by person, then source key, so the badges of one moment sit side
-- by side and in a stable order for the card they collapse into. Shared rows
-- come before private ones, so the owner's card leads with the badge their
-- followers see (and cheer).
ORDER BY a.occurred_at DESC, a.user_id, a.kind, a.private, a.source_key
LIMIT sqlc.arg('lim');

-- name: Visible :one
-- Whether the viewer may see (and so give kudos to) one achievement, by the
-- same rule as the feed; returns its owner and title for the notification.
SELECT a.user_id, a.title FROM achievements a
WHERE a.id = sqlc.arg('id')
  AND (
    a.user_id = sqlc.arg('viewer')
    OR (
      EXISTS (SELECT 1 FROM follows f
              WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = a.user_id AND f.status = 'accepted')
      AND NOT a.private
      AND sharing_on(a.user_id, a.category)
      AND NOT EXISTS (SELECT 1 FROM blocks b
                      WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = a.user_id)
                         OR (b.blocker_id = a.user_id AND b.blocked_id = sqlc.arg('viewer')))
    )
  );

-- name: GiveKudos :execrows
INSERT INTO kudos (achievement_id, user_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: TakeKudos :exec
DELETE FROM kudos WHERE achievement_id = $1 AND user_id = $2;

-- name: OwnAchievements :many
SELECT id, category, kind, title, detail, occurred_at FROM achievements
WHERE user_id = $1 ORDER BY occurred_at DESC;

-- name: OwnAchievementsBetween :many
-- Recap highlights can be shared, so a private badge is never among them.
SELECT id, category, kind, title, detail, occurred_at, source_key FROM achievements
WHERE user_id = sqlc.arg('user_id') AND occurred_at >= sqlc.arg('since') AND occurred_at < sqlc.arg('until')
  AND NOT private
ORDER BY occurred_at DESC, kind, source_key;

-- name: Post :one
-- A moment the person chose to show. Posting the same one again refreshes
-- its words rather than adding a row, and keeps occurred_at, so a re-post
-- does not jump to the top of every friend's feed.
INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, kind, source_key) DO UPDATE SET title = EXCLUDED.title, detail = EXCLUDED.detail
RETURNING id, occurred_at;
