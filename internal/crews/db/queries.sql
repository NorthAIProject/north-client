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
SELECT u.id, u.display_name, u.handle, u.timezone, u.tier, m.joined_at
FROM crew_members m JOIN users u ON u.id = m.user_id
WHERE m.crew_id = $1
ORDER BY m.joined_at;

-- name: OldestOtherMember :one
SELECT user_id FROM crew_members
WHERE crew_id = $1 AND user_id <> $2
ORDER BY joined_at LIMIT 1;

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
SELECT DISTINCT u.id, u.display_name
FROM crew_members mine
JOIN crew_members other ON other.crew_id = mine.crew_id AND other.user_id <> mine.user_id
JOIN users u ON u.id = other.user_id
WHERE mine.user_id = $1;
