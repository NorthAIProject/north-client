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

-- name: LockSubject :exec
-- Reports on one subject take turns, so concurrent reporters cannot both
-- count short of the threshold.
SELECT pg_advisory_xact_lock(hashtextextended(@subject_kind::text || ':' || @subject_id::text, 0));

-- name: HideDecided :one
-- Whether the subject was already hidden or unhidden; either way automatic
-- hiding has had its say.
SELECT EXISTS (
    SELECT 1 FROM moderation_actions
    WHERE subject_kind = $1 AND subject_id = $2 AND action IN ('hide', 'unhide')
);

-- name: OpenReports :many
-- Open reports, one row per subject, oldest first. hidden is whether the
-- latest hide/unhide on it was a hide.
SELECT r.subject_kind, r.subject_id,
       coalesce((array_agg(r.subject_author_id))[1], '00000000-0000-0000-0000-000000000000')::uuid AS author_id,
       array_agg(r.reason)::text[] AS reasons,
       count(*)::int AS n,
       min(r.created_at)::timestamptz AS first_at,
       coalesce((SELECT a.action = 'hide' FROM moderation_actions a
                 WHERE a.subject_kind = r.subject_kind AND a.subject_id = r.subject_id AND a.action IN ('hide', 'unhide')
                 ORDER BY a.created_at DESC LIMIT 1), false)::boolean AS hidden
FROM content_reports r
WHERE r.status = 'open'
GROUP BY r.subject_kind, r.subject_id
ORDER BY first_at;

-- name: CloseReports :exec
UPDATE content_reports SET status = @status WHERE subject_kind = @subject_kind AND subject_id = @subject_id AND status = 'open';
