-- +goose Up
-- +goose StatementBegin

-- Following becomes instant: anyone with a handle can follow. Requests that are
-- still waiting for approval were made under the old rules, so they are
-- accepted now rather than left stranded in a state the app no longer has.
UPDATE follows
   SET status = 'accepted', accepted_at = COALESCE(accepted_at, now())
 WHERE status = 'pending';

-- New people share training, streaks and goals by default; until now a missing
-- achievement_sharing row meant "off". Flipping that default would silently
-- publish what existing people never chose to share, so every current person
-- gets an explicit false row first. Rows they already have are left alone.
-- XP stays off by default and needs no pinning.
INSERT INTO achievement_sharing (user_id, category, shared)
SELECT u.id, c, false
  FROM users u
 CROSS JOIN unnest(ARRAY['training', 'streaks', 'goals']) AS c
ON CONFLICT (user_id, category) DO NOTHING;

-- The one place the default lives. Every sharing query asks this instead of
-- reading achievement_sharing directly, so a missing row means the default and
-- the rule cannot drift between queries.
CREATE FUNCTION sharing_on(p_user uuid, p_category text) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT s.shared FROM achievement_sharing s
          WHERE s.user_id = p_user AND s.category = p_category),
        p_category IN ('training', 'streaks', 'goals'))
$$;

-- Tell everyone once. Dedupe key v1 keeps a re-run from repeating it.
INSERT INTO user_nudges (user_id, kind, dedupe_key, title, body, href)
SELECT id, 'open_follow', 'v1',
       'Khepri now has followers',
       'Anyone with your handle can follow you now. Choose what followers see in Friends.',
       '/app/friends#sharing'
  FROM users
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The accepted follows and the pinned false rows are not reversed: there is no
-- record of which were pending or which were pinned by this migration.
DROP FUNCTION IF EXISTS sharing_on(uuid, text);
DELETE FROM user_nudges WHERE kind = 'open_follow';
-- +goose StatementEnd
