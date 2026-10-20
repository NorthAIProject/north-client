-- name: InsertReport :execrows
INSERT INTO content_reports (reporter_id, subject_kind, subject_id, subject_author_id, scope_id, reason, note)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (reporter_id, subject_kind, subject_id) DO NOTHING;

-- name: CountReporters :one
SELECT count(*) FROM content_reports WHERE subject_kind = $1 AND subject_id = $2;

-- name: ReportedBy :many
SELECT subject_id FROM content_reports
WHERE reporter_id = $1 AND subject_kind = $2 AND subject_id = ANY(@ids::uuid[]);

-- name: InsertAction :exec
INSERT INTO moderation_actions (actor_id, subject_kind, subject_id, action, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: TermsAccepted :one
SELECT EXISTS (SELECT 1 FROM ugc_terms_acceptances WHERE user_id = $1 AND version = $2);

-- name: AcceptTerms :exec
INSERT INTO ugc_terms_acceptances (user_id, version) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: Suspend :exec
INSERT INTO ugc_suspensions (user_id, until, reason) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET until = EXCLUDED.until, reason = EXCLUDED.reason, created_at = now();

-- name: Unsuspend :execrows
DELETE FROM ugc_suspensions WHERE user_id = $1;

-- name: Suspended :one
SELECT EXISTS (SELECT 1 FROM ugc_suspensions WHERE user_id = $1 AND (until IS NULL OR until > @now::timestamptz));
