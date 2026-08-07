-- +goose Up
-- +goose StatementBegin

-- A reference catalog of exercises, seeded in 00021 and not user-editable, so
-- it lives here rather than behind an admin feature — same reasoning as the
-- diets table in 00014.
--
-- Its point is the muscle columns. Before this table the AI plan generator
-- invented an exercise name AND assigned its own muscle keys, and the 3D
-- viewer highlighted whatever one model call guessed. A catalog row is a
-- fixed answer to "what does this movement train", so the viewer stops being
-- only as trustworthy as the last generation.
CREATE TABLE exercises (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Stable identifier the AI echoes back when it picks from the catalog
    -- (see plan.Exercise.CatalogSlug). Derived from the name, but its own
    -- column: a name can be corrected without silently repointing every plan
    -- that referenced it.
    slug              text        NOT NULL UNIQUE,

    name              text        NOT NULL,

    -- 'strength', 'cardio', 'stretching', 'plyometrics', 'powerlifting',
    -- 'olympic_weightlifting', 'strongman'. Left unconstrained: the seed's
    -- vocabulary is the source's, and a CHECK here would have to be migrated
    -- every time the catalog grows a category.
    category          text        NOT NULL DEFAULT 'strength',

    -- Normalised to the vocabulary internal/workouts/plan.availableEquipment
    -- already understands, so the plan validator and the catalog filter agree
    -- about what "the person has dumbbells" excludes.
    equipment         text        NOT NULL DEFAULT 'none',

    difficulty        text        NOT NULL DEFAULT 'beginner',

    instructions      text        NOT NULL DEFAULT '',
    video_url         text        NOT NULL DEFAULT '',

    -- Muscle keys from internal/workouts/plan.MuscleGroups. text[] rather
    -- than a join table: nothing needs to query an exercise *from* a muscle
    -- row, and the GIN index below serves the one direction that is queried.
    primary_muscles   text[]      NOT NULL DEFAULT '{}',

    -- Empty for most rows. The source carries one muscle per exercise, so
    -- secondaries are curated by hand for the compound lifts and left empty
    -- elsewhere — an empty array is honest, a guessed one is not.
    secondary_muscles text[]      NOT NULL DEFAULT '{}',

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Case-insensitive substring search via ILIKE, matching how ingredients are
-- searched in 00014. Good enough at 300 rows; revisit only if it stops being.
CREATE INDEX exercises_name_idx ON exercises (lower(name));

-- "What trains the lats" is the query the browse page and the plan generator's
-- equipment filter both run.
CREATE INDEX exercises_primary_muscles_idx ON exercises USING gin (primary_muscles);
CREATE INDEX exercises_equipment_idx ON exercises (equipment);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE exercises;
-- +goose StatementEnd
