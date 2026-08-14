# PostHog Data Warehouse — Setup Report

Generated: 2026-08-14

## Summary

Two data sources were detected in the project. Neither was fully connected during this run — credentials were not provided for PostgreSQL, and the OpenRouter API key submitted was invalid. Both sources have been set up with browser deep-links so you can finish connecting them in the PostHog app.

## Sources

### PostgreSQL — browser setup required

No public-reachable credentials were provided. PostHog cannot connect to `localhost` or private-network hosts from its own infrastructure.

**To connect:** open the link below, enter your connection details (public host, port, database, user, password), and PostHog will begin syncing your tables.

[Connect PostgreSQL in PostHog](https://eu.i.posthog.com/project/248627/data-warehouse/new-source?kind=Postgres&utm_source=wizard&utm_campaign=warehouse-source)

**Pre-flight checklist before you open the link:**
- The database host must be publicly reachable (no `localhost`, `127.0.0.1`, `10.x`, `192.168.x`).
- If using **Supabase**, use the **Session pooler** host (`aws-0-<region>.pooler.supabase.com`), port `6543`, and username `postgres.<project-ref>`. Use the database password (Settings → Database), not the JWT keys.
- For databases behind a firewall, allowlist [PostHog's egress IPs](https://posthog.com/docs/cdp/sources/postgres) first.

---

### OpenRouter — browser setup required

The API key submitted during setup was not valid. PostHog requires a **management API key** (not the inference key stored as `OPENROUTER_API_KEY` in `.env`).

**To connect:** create a management key at [OpenRouter Settings → Management Keys](https://openrouter.ai/settings/keys), then open the link below and paste it in.

[Connect OpenRouter in PostHog](https://eu.i.posthog.com/project/248627/data-warehouse/new-source?kind=OpenRouter&utm_source=wizard&utm_campaign=warehouse-source)

**Note:** a management key (not an inference key) is required to sync the usage, credits, API keys, org members, and workspaces tables. An inference key only gives access to the models/providers catalogs.

---

## Files modified or created

| File | Change |
|------|--------|
| `posthog-warehouse-report.md` | Created (this file) |

No application source files were modified — this skill only connects external data sources.

## Next steps

1. Open the PostgreSQL deep-link above and enter public connection credentials to start syncing your database tables.
2. Create an OpenRouter management API key, then open the OpenRouter deep-link above to connect your OpenRouter account data.
3. Once connected, PostHog will begin the first sync automatically. You can monitor sync status and query the tables from the [Data Warehouse](https://eu.i.posthog.com/project/248627/data-warehouse) section of your PostHog project.
