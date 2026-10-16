# Coach outreach: the coach speaks first

Khepri's coach messages a person unprompted, at most once a day, when it
notices something in their day, and offers a one-tap answer. This is the
first slice: two triggers. It is a test of whether people answer before more
triggers are built.

Code: `internal/outreach` (sweep, triggers, actions), `coach.RunOutreach` and
`internal/ai/prompts/coach_outreach.md` (the message), `web/outreach` (the
Today card).

## Triggers

| Trigger | When (local time) | What the coach does |
|---|---|---|
| `low_recovery` | 07:00–12:00. Recovery is low (`insights.RecoveryData.Low`, the same rule as the lighter-day offer), a session is due and not done, and no lighter-day answer yet. | It lightens today to about 60% of the sets (`lighter_days.source = 'coach'`) and says why, with **Undo**. If the person turned off "Let my coach lighten a session", it only offers **Take it lighter / Keep the plan**. |
| `missed_session` | 09:00–20:00. Yesterday, in the same week, had a planned session that was not trained. | It offers **Do it today** (only if today has no session), **Tomorrow** (only if tomorrow is free) and **Let it go**. A move pins every other day of the week to its current session, so nothing else shifts and the missed session lands once. |

The first trigger that fires wins. One row per person per local day in
`coach_outreach` (`UNIQUE (user_id, day)`) enforces "at most once a day" across
sweeps and channels.

## Limits

- **Settings:** `coach_check_ins` (off means no messages) and
  `coach_adjusts_training` (off means offer only). Both are on by default and
  shown on web and in the settings API.
- **Quiet hours:** respected, the same as nudges.
- **Cost:** one model call per message, made only when a trigger fires (spend
  surface `coach_outreach`). If the model fails, a templated sentence from the
  catalogue is posted instead, so the action always lands.
- **Free chain:** the message goes through the person's provider chain, as the
  coach does. The prompt holds derived facts (the recovery reason, the session
  name), not raw samples.

## Delivery and answers

- **Coach thread:** the message is posted there, captioned "Your coach checked
  in".
- **Bell and push:** nudge kinds `coach_lighter` / `coach_missed`, push only.
  The iOS categories `COACH_LIGHTER` / `COACH_MISSED` put the answers under the
  banner. A button posts to `/api/v1/outreach/today/actions`.
- **Telegram:** its own copy, with inline buttons
  `outreach:<id>:<action>`. `messaging.Service.Handle` routes these to the
  action and never sends them to the coach as a turn. The message is edited
  with the outcome once answered.
- **Today card:** on web, under the recap strip. On a low-recovery day the
  lighter-day card hides, so there is only one Undo.
- **First answer counts:** a second answer is a 409. Only today's message can
  be answered.

## Measuring it

`coach_outreach.action` and `acted_at` are the point of the slice. Answered
rate by trigger over the last 14 days:

```sql
SELECT trigger,
       count(*)                                        AS sent,
       count(action)                                   AS answered,
       round(100.0 * count(action) / count(*))         AS answered_pct,
       count(*) FILTER (WHERE action = 'undo')         AS undone,
       count(*) FILTER (WHERE action IN ('today','tomorrow')) AS moved,
       count(*) FILTER (WHERE action = 'let_go')       AS let_go
FROM coach_outreach
WHERE day > current_date - 14
GROUP BY trigger;
```

Decision rule after two weeks of real use:
- **If people answer:** add triggers (commitments, personal bests, check-in
  follow-ups).
- **If they don't:** change the timing or the wording first.
