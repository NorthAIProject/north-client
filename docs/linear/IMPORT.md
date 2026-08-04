# Linear import — North Feature-ready OS

Ready-to-import backlog for the **Personal Growth Agentic OS** milestone.

| File | Contents |
|------|----------|
| `north-feature-ready-os.csv` | **41 issues** (title, description, status, priority, labels, estimate, project) |

## Before you import

Linear’s CSV importer maps rows to **one team**. Projects and labels are matched **by name** (created if missing, depending on workspace settings).

### 1. Create the project

In Linear:

1. Open your team → **Projects** → **New project**
2. Name (must match CSV): **`North — Feature-ready OS`**
3. Optional description:

   > Ship a stranger-ready Personal Growth Agentic OS: check-ins, long-term memory, knowledge/RAG, weekly reports, tool-calling coach, and one external surface (MCP or Telegram)—without Phase 4 native.

### 2. Create labels (recommended)

Create these labels **before** import so colors/groups stay consistent:

| Label | Suggested color | Meaning |
|-------|-----------------|---------|
| `must-ship` | Red | Required for feature-ready OS cut |
| `should-ship` | Orange | Strong v1; ship if capacity allows |
| `nice-to-have` | Yellow | After strangers use the product |
| `later` | Gray | Explicitly out of v1 |
| `phase-1-gap` | Blue | Closes README Phase 1 holes |
| `phase-2` | Purple | Reports, docs, search, MCP |
| `phase-3` | Teal | Messaging / fitness providers |
| `phase-4` | Gray | Native iOS etc. |
| `growth-loop` | Green | Check-ins, goals, onboarding |
| `memory` | Green | Long-term memory |
| `knowledge` | Green | Documents / RAG |
| `reports` | Green | Weekly / daily reviews |
| `coach` | Green | CoachService / context |
| `agentic` | Green | Tools, nudges, reflection |
| `mcp` | Green | MCP client/server |
| `messaging` | Green | Telegram etc. |
| `fitness` | Green | Strava / providers |
| `ui` | Blue | Frontend / HTMX |
| `trust` | Red | Export, limits, evals |
| `activation` | Orange | Empty states, onboarding |
| `settings` | Blue | User prefs |
| `ops` | Gray | Observability |
| `eval` | Gray | AI eval harness |
| `goals` | Green | Goals domain |
| `search` | Green | Semantic search |
| `integrations` | Green | External connectors |

### 3. Optional: milestones / project milestones

After import, group issues into **five project milestones** (or Linear project milestones):

1. **Growth loop** — check-ins, memory, onboarding, dashboard  
2. **Knowledge + weekly review** — docs, embeddings, RAG, reports  
3. **Agentic coach** — tools, confirmations, reflection, nudges  
4. **First external surface** — MCP server **or** Telegram (pick one for v1)  
5. **Strangers-ready polish** — export, rate limits, empty states, settings, evals  

Filter by label `must-ship` for the critical path.

---

## Import steps

1. Linear → team menu → **Import issues** (or **Settings → Import / Export → Import issues**)  
2. Choose **CSV**  
3. Upload:

   ```text
   docs/linear/north-feature-ready-os.csv
   ```

4. Map columns (defaults usually work):

   | CSV column   | Linear field |
   |--------------|--------------|
   | Title        | Title        |
   | Description  | Description  |
   | Status       | Status       |
   | Priority     | Priority     |
   | Labels       | Labels       |
   | Estimate     | Estimate     |
   | Project      | Project      |

5. Confirm team + that **Project** maps to `North — Feature-ready OS`  
6. Run import  

### Status note

All rows use **`Backlog`**. If your workflow uses different state names (`Todo`, `Triage`, etc.), remap **Status** during import or bulk-update after.

### Estimate note

Estimates are **story points** (Fibonacci-ish: 2, 3, 5, 8). If your team uses Linear **T-shirt** or no estimates, ignore/unmapped that column.

---

## Must-ship filter (feature-ready cut)

After import, filter:

```text
project:North — Feature-ready OS label:must-ship
```

These are the **19** issues that define “feature-ready Personal Growth Agentic OS”:

| Priority | Title |
|----------|--------|
| Urgent | Daily check-ins: schema, service, and UI |
| Urgent | Check-in ContextSource for CoachService |
| Urgent | Long-term memory store (profile facts) |
| Urgent | Memory ContextSource and prompt policy |
| Urgent | Tool-calling loop in CoachService |
| High | Guided check-in flow (HTMX, mobile-first) |
| High | Link check-ins to related goals |
| High | Memory extraction job after conversations |
| High | Onboarding: first-run goals and coaching style |
| High | Dashboard as today’s OS home |
| High | Documents: upload, storage, and metadata |
| High | Document extraction pipeline (worker) |
| High | Embeddings and search package (semantic memory) |
| High | Knowledge ContextSource (RAG into coach) |
| High | Weekly review report generation |
| High | Reports UI: view, archive, regenerate |
| High | Confirm side-effect tools in UI |
| High | Empty states and “what should I do next?” CTAs |
| High | Settings: timezone, coaching tone, notification prefs |

**Suggested first wave (unblocks everything else):**

1. Daily check-ins (schema/UI)  
2. Check-in ContextSource  
3. Long-term memory store  
4. Memory ContextSource  
5. Settings (timezone/tone)  
6. Empty states + dashboard home  
7. Tool-calling loop  

---

## Should-ship (strong v1)

Filter `label:should-ship` — includes MCP server v0, Telegram path, export/delete, rate limits, evals, milestones on goals, conversation summarization, etc.

Pick **one** external surface for v1:

- **MCP server v0** if you want Claude Desktop / agent ecosystem first, **or**  
- **Messaging adapter + Telegram** if you want daily habit on the phone first  

---

## Explicitly later (do not pull into v1)

Issues titled `[Later] …` / label `later`:

- Discord, WhatsApp  
- Native iOS / Watch / Live Activities  
- Apple Health / Garmin / Fitbit (after Strava interface)

---

## Architecture reminders (in every description)

Issues already encode North constraints:

- Vertical slices under `internal/<feature>/`  
- Thin handlers; logic in services  
- Coach via `CoachService` + `ContextSource`  
- AI prompts in `internal/ai/prompts/`  
- HTMX + templUI; no new frontend framework  
- Worker jobs for long work (extract, reports, memory extraction)

---

## Re-import safety

Linear CSV import **creates new issues**; it does not upsert. If you re-import, you will get duplicates. Prefer:

1. Import once  
2. Edit in Linear  
3. Or delete the project issues and re-import only if still empty  

---

## Optional: create via API/CLI later

If you install [Linear CLI](https://linear.app/docs/cli) or use the GraphQL API, you can recreate from this CSV with a small script. The CSV remains the source of truth for bulk bootstrap.
