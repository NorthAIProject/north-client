-- +goose Up
-- +goose StatementBegin

-- Which request put this job in the queue.
--
-- Jobs run minutes or hours after the request that created them, in a different
-- process, so a failure in the worker has until now been unattributable: the
-- log said which job and which kind, but not what a person did to cause it.
-- With this, one request id joins the web log line to the worker log line.
--
-- Nullable because the worker enqueues its own periodic sweeps and nothing put
-- those there. A column that invented a value for them would read as though a
-- request existed, which is worse than admitting there was none.
--
-- A column rather than a field on each payload struct: there are eight payload
-- types and Enqueue is one function, so this covers every kind at once and a
-- new payload cannot silently forget to carry it.
ALTER TABLE jobs ADD COLUMN request_id text;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN request_id;
-- +goose StatementEnd
