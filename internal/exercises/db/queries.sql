-- name: GetExercise :one
SELECT * FROM exercises WHERE id = $1;

-- name: GetExerciseBySlug :one
SELECT * FROM exercises WHERE slug = $1;

-- name: ListExercisesBySlugs :many
SELECT * FROM exercises WHERE slug = ANY(@slugs::text[]);

-- SearchExercises is the browse page's one query. Every filter is optional:
-- an empty query, muscle, category, or equipment list means "no constraint",
-- which keeps this to a single statement rather than a builder.
--
-- Paged by offset rather than a keyset cursor. The ORDER BY is name, which is
-- stable and unique enough here, and the catalog is 455 rows — an offset deep
-- into it costs nothing a keyset would save. Page links also need to jump to an
-- arbitrary page, which a cursor cannot do.
-- name: SearchExercises :many
SELECT * FROM exercises
WHERE (@query::text = '' OR name ILIKE '%' || @query::text || '%')
  AND (@muscle::text = '' OR @muscle::text = ANY(primary_muscles) OR @muscle::text = ANY(secondary_muscles))
  AND (@category::text = '' OR category = @category::text)
  AND (cardinality(@equipment::text[]) = 0 OR equipment = ANY(@equipment::text[]))
ORDER BY name
LIMIT @result_limit::int OFFSET @result_offset::int;

-- CountExercises mirrors SearchExercises' filters so the browse page can say
-- how many matched without paging through them.
-- name: CountExercises :one
SELECT count(*) FROM exercises
WHERE (@query::text = '' OR name ILIKE '%' || @query::text || '%')
  AND (@muscle::text = '' OR @muscle::text = ANY(primary_muscles) OR @muscle::text = ANY(secondary_muscles))
  AND (@category::text = '' OR category = @category::text)
  AND (cardinality(@equipment::text[]) = 0 OR equipment = ANY(@equipment::text[]));

-- ListExercisesForEquipment feeds the plan generator's candidate list: every
-- exercise someone with this equipment could actually perform, bodyweight
-- included. Ordered so the list handed to the model is stable between runs.
-- name: ListExercisesForEquipment :many
SELECT * FROM exercises
WHERE equipment = ANY(@equipment::text[])
ORDER BY category, name
LIMIT @result_limit::int;
