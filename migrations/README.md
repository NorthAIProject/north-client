# Migrations

Goose migrations, embedded into the binary (`fs.go`) and applied automatically
on start by `internal/shared/database.Migrate`.

## Naming: use a timestamp

New migrations are named with a UTC timestamp, not the next number:

```
20260808143000_add_thing.sql
```

Create one with:

```sh
date -u +%Y%m%d%H%M%S
```

**Digits only — no `T` separator.** Goose takes everything before the first `_`
and runs it through `strconv.ParseInt` (`NumericComponent`, goose
`migration.go`). `20260808T1430_add_thing.sql` does not parse, and goose then
*skips* the file rather than refusing it: `Migrate` returns no error, the schema
quietly lacks the change, and the first symptom is a query failing on a column
that the migration in front of you plainly creates.

`TestEveryMigrationFilenameIsParseable` in `internal/shared/database` fails the
build rather than letting that recur.

**Why not sequential numbers.** They only work when one person is adding
migrations at a time. Two branches developed in parallel both reach for the
same next number, and the collision is invisible until someone tries to merge
them — or worse, until two developers' databases disagree about what version
`00023` means. This repository hit that twice in a single day: three branches
each picked their own `00020`-and-up, and the shared development database
ended up with 23, 24, 25, 26, 27 applied while 22 was missing entirely.

A timestamp cannot collide, because two branches are never created in the same
minute by the same person, and if they somehow are, the filenames differ by
their description.

## Existing files keep their numbers

`00001` through `00030` stay exactly as they are. **Do not renumber them.**

Every database — development, and anything deployed — records the version it
applied in `goose_db_version`. Renaming `00007_x.sql` to a timestamp does not
rename that row: goose would see a version it has never applied and run the
file again, against a schema that already has those tables. The rename is
silent locally (a fresh database looks fine) and destructive everywhere that
already has data.

So `00030` is the last sequential migration. Everything after it is a
timestamp. Timestamps sort far above `30`, so ordering stays natural.

## Out-of-order migrations are allowed

`Migrate` sets `goose.WithAllowOutofOrder(true)`. A branch that merges second
carries a version older than one already applied, and without this the
application refuses to boot until someone renumbers by hand.

The trade: migrations run exactly once each, but not necessarily in the order
they were written. That was already true the moment the project had branches —
the numbering implied an ordering guarantee it could not actually keep. Write
migrations so this does not matter:

- Do not depend on another migration having run first, beyond what a foreign
  key or a `NOT NULL` already enforces.
- Prefer additive changes. A migration that adds a table or a nullable column
  is order-independent by construction.
- If two changes genuinely must be ordered, put them in one migration.

## Style

- One concern per file; related tables can share one (see `00014`, which
  creates the three tables that make up food reference data).
- Always write the `-- +goose Down`. It is the thing you want at 2am.
- Prefer `text` with a `CHECK` constraint over a Postgres enum: adding a value
  to an enum is a migration, adding one to a `CHECK` is a migration, but
  changing a Go-owned vocabulary validated in the service layer is neither.
  See `DOMAIN.md` on life domains for where that line sits.
- Comment the *why*. The schema says what; the comment should say what it cost
  to learn.
