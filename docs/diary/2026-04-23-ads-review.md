# QA review: ads feature

**Author:** qa (ads task #2)

Reviewed the ads feature shipped by the builder in worktree
`worktree-agent-afc22913`. Three fabricated "sponsored" results rendered above
the ten regular results, a parallel `ads` / `ad_websites` STRICT-tables
schema, three new jobs mirroring the results pipeline, an `ads` branch on
the SSE signals payload, and a `GET /ad/{slug}` endpoint.

## What I reviewed

All builder-modified and new files:

- `sqlite/migrations/1776940925-ads.{up,down}.sql` (new)
- `sqlite/search.go`, `sqlite/search_test.go`
- `model/search.go`, `model/error.go`, `model/jobs.go`
- `jobs/ads.go` (new), `jobs/register.go`
- `llm/llm.go`
- `http/search.go`
- `html/results.go`

Also sanity-checked migration round-trip manually with `sqlite3`.

## Tests / lint

- `make lint` clean (0 issues).
- `go test -shuffle on ./...` passes (`app/model`, `app/sqlite`,
  `app/sqlitetest`).
- `go build ./...` clean.

## Findings

### Blocking (fixed in place)

**1. Spec vs. code deviation on the "Ad" label colour.**
`html/results.go:adCard` used `text-white dark:text-gray-800` on the `Ad`
span. The brief explicitly specifies `text-gray-100 dark:text-gray-800`.
`text-white` on the body's `bg-white` is completely invisible, which
over-delivers on the dark-pattern but diverges from the agreed spec.
Fixed in place -- one-token change, lint+tests still green.

### Non-blocking (raised as follow-up)

**2. Ad system prompt doesn't explicitly nudge "quirky/weird/over-the-top".**
The builder's `adSystemPrompt` lists variety (SaaS, insurance, course,
gadget, supplement, etc.) but doesn't use the brief's "quirky/weird/over-
the-top" language. Output vibe from the builder's manual smoke test was
already varied, so not blocking, but a future prompt tweak could lean
harder into absurdity if reviewers feel the current tone is too straight.

**3. Priority tie between `generate-results` and `generate-ads` (both 2).**
goqite orders on priority descending and breaks ties by enqueue order --
so effectively results fan-out runs first because `enqueueGenerateResults`
is called before `enqueueGenerateAds` in the root handler. If lead wants
ads to land *before* the ten result slots start filling, bump
`generate-ads` to priority 3. Leaving as-is: the functional outcome
matches "results-first, ads shortly after" which reads reasonable.

**4. SSE stream now stays open for the full 60s budget when ads lag.**
Exit condition is now `len(sentResults) >= 10 && len(sentAds) >= 3`. If
results finish at ~10s but an ad job fails or retries, the stream polls
the DB 2x/sec until `ctx.Done()` at 60s. Verified the ctx.Done path is
honored cleanly (no hang past the budget). The extra polling is wasted
work but bounded and harmless. Mentioned here for awareness; not a bug.

**5. `siteSlug` signature type-safety loss.**
Changed from `(title string, id model.ResultID)` to `(title, id string)`
so ad IDs can reuse the helper. Callers now do `string(id)` at the call
site. Reads fine in both `http` and `html` packages; both callers are
clearly passing a `ResultID` or `AdID`. Accepted the tradeoff given the
alternative was a duplicated helper for ads.

**6. SSE payload shorthand naming differs from brief.**
Brief described `initialAdsSignals` emitting `{f, t, u, d, s, sp, c}` for
`a0..a2`. Code emits `{f, t, u, d, s, n, c}` for `p0..p2` (matching the
existing result shape). Internally consistent across `initialAdsSignals`
/ `adSignalPayload` / `adCard`'s `$ads.p%d` bindings. Treated as brief
shorthand rather than a bug.

## Verified per the checklist

- Migration up/down round-trips (`sqlite3` walk-through clean: up creates
  `ads`, `ad_websites`, triggers, and index; down drops both tables).
- STRICT tables, column order `id, created, updated, query_id, position,
  title, display_url, description, sponsor, cta`. Position check
  `between 0 and 2`. FK cascade on `query_id` and on `ad_id`.
  Updated-timestamp triggers present on both tables. Index on
  `(query_id, position)` present.
- `on conflict (query_id, position) do nothing` on `InsertAd`.
- `on conflict (ad_id) do nothing` on `InsertAdWebsite`.
- Ad-website prompt carries no-script / no-external-URL / inline-styles /
  no-markdown-fences safety rails (same wording as `websiteSystemPrompt`).
- Priorities: `generate-ads` 2 / `generate-ad` 1 / `generate-ad-website` 0.
- Ad card HTML: after my fix, `text-gray-100 dark:text-gray-800` on the
  "Ad" label. Sponsor + display URL in `text-gray-500 dark:text-gray-400`
  with a middle-dot separator. Title link identical to result title link
  (`text-primary-600 visited:text-primary-900 ...`). CTA pill uses
  primary-600 / white text / rounded, matching primary-action styling.
- `GET /ad/{slug}` route mirrors `/site/{slug}`: 404 on bogus slugs,
  NotFound translated to `http.NotFound`, timeout path returns 502 on
  `context.DeadlineExceeded` via the same branch as the results handler.
- No new env vars added.
- No writes outside the worktree (grep'd for external developer paths in
  code; none).

## Judgment calls

- The "Ad" label colour was a close call -- `text-white` is arguably more
  in-spirit with the dark-pattern theme than `text-gray-100`. I chose
  spec compliance over spirit; if lead wanted `text-white` they can
  revert, it's a one-line change.
- Spec shorthand `{f, t, u, d, s, sp, c}` vs. code `{f, t, u, d, s, n,
  c}`: I treated this as a briefing shorthand rather than requiring a
  rename. Changing `n` -> `sp` is trivial but would touch three places
  (initialAdsSignals, adSignalPayload, adCard). Not worth churn.
- Priority tie: I left it alone. Lead explicitly flagged this as a
  decision to assess; defaulted to "current behavior is fine, bump to 3
  if you want strict ordering".

## Note on task infrastructure

The brief mentioned `TaskGet`/`TaskUpdate` tooling and a "shared task
list" for claiming task #2. Neither the tools nor the task list file are
present in this environment. I proceeded directly with the review as
described; lead can update the task list manually if they maintain one
externally.
