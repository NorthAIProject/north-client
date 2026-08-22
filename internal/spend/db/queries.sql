-- name: RecordGeneration :exec
-- Appends one model call to the ledger. Fire-and-forget: the caller has already
-- served the user by the time this runs, and a failed insert must never turn a
-- successful reply into an error.
INSERT INTO ai_generations (
    user_id, surface, provider, model,
    input_tokens, output_tokens, cost_micros, priced, byok
) VALUES (
    @user_id, @surface, @provider, @model,
    @input_tokens, @output_tokens, @cost_micros, @priced, @byok
);

-- name: SpendByUser :many
-- Total spend per account over a window, most expensive first. NULL user_id
-- rows group together as the unattributed bucket.
SELECT
    user_id,
    count(*)                    AS generations,
    sum(input_tokens)::bigint   AS input_tokens,
    sum(output_tokens)::bigint  AS output_tokens,
    sum(cost_micros)::bigint    AS cost_micros
FROM ai_generations
WHERE created_at >= @from_time
  AND created_at < @to_time
  AND (NOT @billable_only::boolean OR byok = false)
GROUP BY user_id
ORDER BY sum(cost_micros) DESC;

-- name: SpendByModel :many
-- What the money went on. Answers whether a price is wrong before it answers
-- anything about a customer.
SELECT
    provider,
    model,
    count(*)                    AS generations,
    sum(input_tokens)::bigint   AS input_tokens,
    sum(output_tokens)::bigint  AS output_tokens,
    sum(cost_micros)::bigint    AS cost_micros
FROM ai_generations
WHERE created_at >= @from_time
  AND created_at < @to_time
  AND (NOT @billable_only::boolean OR byok = false)
GROUP BY provider, model
ORDER BY sum(cost_micros) DESC;

-- name: SpendBySurface :many
-- Which part of the product spends. This is the split that says whether the
-- answer is chat or the scheduled sweeps.
SELECT
    surface,
    count(*)                    AS generations,
    sum(input_tokens)::bigint   AS input_tokens,
    sum(output_tokens)::bigint  AS output_tokens,
    sum(cost_micros)::bigint    AS cost_micros
FROM ai_generations
WHERE created_at >= @from_time
  AND created_at < @to_time
  AND (NOT @billable_only::boolean OR byok = false)
GROUP BY surface
ORDER BY sum(cost_micros) DESC;

-- name: CountUnpricedGenerations :one
-- Calls for which no rate was found. A non-zero answer means the pricing table
-- is missing a model and every total is an understatement.
--
-- Keyed on `priced`, not on a zero cost: the free floor is priced at zero on
-- purpose, and treating that as a gap would cry wolf on every report.
SELECT count(*) FROM ai_generations
WHERE created_at >= @from_time
  AND created_at < @to_time
  AND byok = false
  AND priced = false
  AND (input_tokens > 0 OR output_tokens > 0);
