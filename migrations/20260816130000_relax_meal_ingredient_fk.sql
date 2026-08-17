-- +goose Up
-- +goose StatementBegin

-- Lets an account be deleted.
--
-- meal_ingredients.ingredient_id was RESTRICT, for a good reason that 00015
-- states: an ingredient row that vanishes leaves a meal line with real macros
-- attached to nothing, so deleting an in-use ingredient should fail loudly.
--
-- That reason still holds. What it did not anticipate is that ingredients are
-- user-ownable — ingredients.user_id is a nullable CASCADE column, NULL for the
-- shared seeded list and set for one a person created themselves. So deleting a
-- user cascades their own ingredients away, and RESTRICT fires on any
-- meal_ingredients row still pointing at one. Those rows are being deleted by
-- the very same statement, through meals -> meal_plans -> users, but RESTRICT is
-- checked the instant the referenced row goes rather than at the end of the
-- statement, and the order the cascade takes is not ours to choose. Anyone who
-- had logged a meal using an ingredient they had entered themselves could not
-- be deleted at all.
--
-- NO ACTION DEFERRABLE INITIALLY DEFERRED is the same rule checked at commit
-- instead. It has to be deferred rather than merely NO ACTION: a cascade runs
-- as a tree of nested statements, and an immediate check fires at the end of
-- the inner one that removed the ingredient, which is still too early. Waiting
-- for the commit is the first moment the whole cascade has finished and the
-- referencing rows are provably gone.
--
-- What this costs: deleting an in-use ingredient on its own still fails, but
-- the error now arrives at COMMIT rather than at the DELETE. The rule 00015
-- wanted is intact; the line it is reported on moves.
ALTER TABLE meal_ingredients
    DROP CONSTRAINT meal_ingredients_ingredient_id_fkey;

ALTER TABLE meal_ingredients
    ADD CONSTRAINT meal_ingredients_ingredient_id_fkey
    FOREIGN KEY (ingredient_id) REFERENCES ingredients (id)
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE meal_ingredients
    DROP CONSTRAINT meal_ingredients_ingredient_id_fkey;

ALTER TABLE meal_ingredients
    ADD CONSTRAINT meal_ingredients_ingredient_id_fkey
    FOREIGN KEY (ingredient_id) REFERENCES ingredients (id) ON DELETE RESTRICT;
-- +goose StatementEnd
