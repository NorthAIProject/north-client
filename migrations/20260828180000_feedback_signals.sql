-- +goose Up
-- +goose StatementBegin

-- Did this help?
--
-- Nothing in the product asks. There is no rating, no thumbs, no "was this
-- useful" anywhere in the codebase, which means the only churn signal available
-- today is somebody quietly stopping — and by then it is too late to ask why.
--
-- It is also the one column a learned user model cannot derive. Adherence,
-- streaks and follow-through are all observable from behaviour; whether the
-- coach was any good is not. A human saying "no, that reply missed" is the
-- labelled data every later inference gets checked against.
--
-- Three states, so nullable rather than a boolean default. NULL is "not asked or
-- not answered", which is the honest majority case and must stay distinguishable
-- from "answered no" — a default of false would silently convert every ignored
-- reply into a complaint.
ALTER TABLE messages ADD COLUMN helpful boolean;
ALTER TABLE reports  ADD COLUMN helpful boolean;

COMMENT ON COLUMN messages.helpful IS
    'Did the person find this reply useful? NULL means they did not say.';
COMMENT ON COLUMN reports.helpful IS
    'Did the person find this report useful? NULL means they did not say.';

-- Partial indexes: the question asked of this data is always "show me the
-- answered ones", and the answered ones are a small minority of rows. Indexing
-- the NULLs would be indexing almost the whole table to find almost none of it.
CREATE INDEX messages_helpful_idx ON messages (helpful) WHERE helpful IS NOT NULL;
CREATE INDEX reports_helpful_idx  ON reports  (helpful) WHERE helpful IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS reports_helpful_idx;
DROP INDEX IF EXISTS messages_helpful_idx;
ALTER TABLE reports  DROP COLUMN IF EXISTS helpful;
ALTER TABLE messages DROP COLUMN IF EXISTS helpful;
-- +goose StatementEnd
