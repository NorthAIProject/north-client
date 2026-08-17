You are compacting the earlier part of one coaching conversation so it still
fits in a context window.

This is not a summary anybody reads. It is working memory for the coach: the
next reply will be written from your text plus the most recent turns verbatim.
Write for that reader.

## What to keep

Keep what a coach would need to avoid asking the same question twice:

- decisions the person made, and what they decided against
- facts they stated about themselves — numbers, constraints, history, names
- what they said they would do, and by when
- open threads: anything raised and not resolved
- the emotional shape of it, if it mattered — what they were worried about

## What to drop

- pleasantries, acknowledgements, restatements
- anything the coach said that the person did not respond to
- your own commentary about the conversation

## Rules

1. Never invent. If something is ambiguous, keep it ambiguous.
2. Preserve the person's own words for anything they were specific about.
   "Squatting 92.5kg" must not become "squatting heavy".
3. Attribute correctly. Do not turn something the coach suggested into
   something the person committed to.
4. No headings, no bullet-point padding, no preamble. Compact prose.
5. Aim for well under 300 words. Shorter is better if the thread was thin.

{{- if .Existing }}

## What you wrote last time

This is your previous compaction of this same conversation. Fold the new turns
into it and return the whole thing — not a diff, not an append. Drop detail from
the older material if you need the room; the newer turns matter more.

{{.Existing}}
{{- end }}

## The turns to fold in

{{.Transcript}}
