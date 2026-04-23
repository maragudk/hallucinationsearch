# Diary: Ads pipeline (builder)

**Author:** builder (ads task)

Adding the three-ad "sponsored results" feature to Hallucination Search. Mirrors the existing results pipeline end-to-end: a dedicated `ads` / `ad_websites` schema, three new jobs, an `ads` branch on the SSE signals payload, and a `/ad/{slug}` endpoint that serves a fabricated landing page. Visually, ad cards look almost identical to result cards but carry a "dark-pattern" near-white-on-white "Ad" label, a sponsor name, and a small CTA button.

## Prompt context

**Verbatim prompt:** "Claim task #1 from the shared task list and implement it. [...] Add three fabricated ads at the top of the Hallucination Search results page. New `ads` and `ad_websites` STRICT tables [...]. Three new jobs mirroring the results pipeline [...] using the existing HaikuModel. Extend the existing SSE events payload with an `ads` branch alongside `results`. New `GET /ad/{slug}` endpoint mirroring `/site/{slug}`. Ad cards are visually almost identical to result cards -- the 'Ad' label is near-white on white (dark-pattern camouflage), plus a sponsor name and a small call-to-action button."

**Interpretation:** A parallel track alongside the existing results pipeline. Everything about results has a one-to-one ad counterpart. The visual twist is the only product-design element -- the dark-pattern "Ad" label is the joke.

**Inferred intent:** The brief was tight enough that I mostly had to follow patterns rather than make design calls. Three ads instead of ten. Same LLM (Haiku). Same priorities (2/1/0). Same skeleton toggle. Same timeouts. Same slug shape but with `a_` prefix instead of `r_`.

## What I did

### Schema

New `sqlite/migrations/1776940925-ads.up.sql` / `.down.sql`, parallel to the existing `results` / `websites` migration:

- `ads` table: same columns as `results` (id/created/updated/query_id/position/title/display_url/description) plus `sponsor` and `cta`, `id` prefixed `a_`, position constrained 0-2 instead of 0-9.
- `ad_websites` table: straight copy of `websites` but keyed on `ad_id` instead of `result_id`.
- Both STRICT, both with the updated-timestamp trigger, both with the on-delete-cascade FK to their parent table.

### Model + errors + job names

- `model/search.go`: `AdID`, `Ad`, `AdWebsite` types.
- `model/error.go`: `ErrorAdNotFound`, `ErrorAdWebsiteNotFound`.
- `model/jobs.go`: three new job names (`generate-ads`, `generate-ad`, `generate-ad-website`) and three new job-data structs.

### Storage layer

Appended `GetAds`, `GetAdPositions`, `InsertAd`, `GetAd`, `GetAdWebsite`, `InsertAdWebsite` to `sqlite/search.go`. All same shape as their result/website counterparts.

### LLM client

Extended `llm/llm.go` with:

- `adCC` and `adWebsiteCC` completers on the existing `Client` (still Haiku for all four).
- `Ad` response struct with jsonschema-tagged fields including `sponsor` and `cta`.
- `GenerateAd` / `GenerateAdWebsite` methods with their own system prompts. The ad system prompt encourages distinct sponsor brands across the three positions. The ad-website prompt is tuned for "landing page" aesthetics (hero, features, CTAs) vs. the regular website prompt's "any fake site" aesthetics.

### Jobs

New `jobs/ads.go` containing `GenerateAds`, `GenerateAd`, `GenerateAdWebsite`. All three are copy-adapted from `jobs/search.go`, with the same tracing, same timeout values (60s for ad, 3m for ad-website), same on-conflict-do-nothing write semantics. Registered in `jobs/register.go`.

### HTTP

- `http/search.go`: extended `searchDB` interface with `GetAds`, `GetAd`, `GetAdWebsite`. Root handler now enqueues both `generate-results` and `generate-ads`, reads ads alongside results, passes both to the HTML page. New `/ad/{slug}` route wired with a `handleAd` function that mirrors `handleSite`. Added `enqueueGenerateAds` / `enqueueGenerateAdWebsite` helpers.
- SSE: `handleEvents` now tracks `sentResults` and `sentAds` maps independently, emits signal patches whenever either side has fresh content, and exits when both sides are fully populated. `writeSignalsPatch` emits a payload with `results` and/or `ads` branches.
- `adSignalPayload` adds `n` (sponsor) and `c` (CTA) to the existing compact SSE shape.
- `siteSlug` changed from `(title string, id model.ResultID)` to `(title, id string)` so both results and ads can use the same helper. New `extractAdID` / `adIDRe` to parse `a_`+32-hex from the `/ad/{slug}` URL.

### HTML

- `html/results.go`: `ResultsPageProps` now includes an `Ads []model.Ad` field. The page renders three ad cards above the ten result cards.
- `adCard`: same skeleton / filled toggle as `resultCard`, but with a small badge row containing a near-white "Ad" label (`text-white dark:text-gray-800`), the sponsor name (grey), a middle-dot, and the display URL. Below the title/description a small fuchsia CTA button that reuses the primary-action styling. Links point at `/ad/{slug}`.
- `initialAdsSignals` mirrors `initialResultsSignals` but with the extra `n` and `c` fields, seeded over three positions.
- `siteSlug` in the html package also changed from `(string, model.ResultID)` to `(string, string)` -- same migration as the http package's helper.

### Tests

Extended `sqlite/search_test.go` with `TestInsertAdAndGetAds`, `TestInsertAndGetAdWebsite`, `TestGetAd`. All follow the same subtest structure as the existing result/website tests -- round-trip, first-write-wins on conflict, `GetAdPositions` returns the filled set, error sentinel on missing.

## Why

The brief called for parallel structure, so I leaned into it -- any shape that was already there I copied rather than generalised. The only places I *didn't* copy were:

- `siteSlug` signature: both http and html package helpers had to accept ad IDs too. I changed the parameter from `model.ResultID` to `string` rather than adding a second helper. Both callers pass `string(id)` explicitly now.
- `writeSignalsPatch`: rather than building a separate path for ads, I extended the existing function to take two pairs of (all, sent) maps. Cleaner than a parallel `writeAdsSignalsPatch` with its own wire format.
- SSE "done" condition: previously `len(sent) >= resultsPerQuery` was the exit. Now it's `len(sentResults) >= resultsPerQuery && len(sentAds) >= adsPerQuery`. This keeps the stream open until all 13 slots are filled instead of bailing early when the 10 results finish.

## What worked

End-to-end verification was very fast. After the first clean build and test run, I started the app, hit `/?q=test+ads`, waited ~30 seconds, and saw:

- All six jobs registered (`generate-ad`, `generate-ad-website`, `generate-ads`, plus the originals).
- Migration applied cleanly (DB goes from 0 ads to 3 after ~5s each).
- SSE stream emits a `{"ads": {...}, "results": {...}}` payload as expected.
- Ad cards in the browser snapshot look like the brief described: camouflaged "Ad" label, sponsor name, description, CTA button.
- `/ad/{slug}` returns 200 with a 22KB fabricated "AdValidate" landing page in ~35 seconds (first hit; cached from then on).
- Invalid slugs at both `/ad/<bogus>` and `/ad/a_0000...` return 404 as expected.

Playwright-cli visual verification matched the brief: the "Ad" glyph occupies a small slot in the badge row but is effectively invisible (near-white on white). Sponsor name reads first, display URL follows; the user reads past the label without noticing.

## What didn't work

Initially I'd left the `writeSignalsPatch` emitting under `results` even when only `ads` had fresh data, because my first refactor bailed with `if len(sent) == 0 { return nil }`. After separating into two maps I had to update the guard to `if len(sentResults) == 0 && len(sentAds) == 0` -- otherwise ad-only updates would have emitted an empty-results payload. Caught by reading the diff carefully before running.

## What I learned

The `on conflict (query_id, position) do nothing` idiom combined with the `a_`+randomblob default makes the race story trivial. Two concurrent `generate-ad` jobs for the same position will both succeed the LLM call, one of them wins the insert, the other's row is silently ignored. Same as results.

The JSON-schema-generation path (`gai.GenerateSchema[Ad]()`) picks up the jsonschema struct tags automatically, so the model already knew it had to return `sponsor` and `cta` without me adding any extra prose. That's a big win over manually maintaining a schema description.

## What was tricky

The SSE "done" condition needed care. The previous code exited on `len(sent) >= resultsPerQuery`; a lazy change would have been `len(sentResults) >= resultsPerQuery` which exits while ads are still pending. I made a helper `done()` so the two checks stay visible together.

The `siteSlug` signature migration touched three places: `http/search.go` (the canonical helper), `html/results.go` (the duplicated helper), and every caller. Both helpers now take `(title, id string)`. Kept in sync by hand per the comment; could be dedup'd later but not part of this task.

## What warrants review

- `sqlite/migrations/1776940925-ads.up.sql`: schema mirrors `results`/`websites`. Check `a_` prefix, `position between 0 and 2`, FK cascade, triggers, index.
- `html/results.go:adCard`: the dark-pattern is the product feature here. Verify the "Ad" label is actually invisible on white (`text-white dark:text-gray-800`). Verify sponsor reads first, display URL reads naturally.
- `http/search.go:handleEvents`: the two-map state and combined exit condition. Verify the edge where only ads are slow (or only results) still drains both.
- `llm/llm.go`: two new system prompts. Quality spot-check a few more ad landing pages to confirm the tonal direction (SaaS pitch vs. infomercial vs. MLM) actually varies.
- `sqlite/search_test.go`: three new test functions. Check they mirror the existing result/website tests without regression.

## Future work

- `siteSlug` is still duplicated across `http` and `html` packages. Could move into `model` or a small `url` subpackage. Not blocking.
- An `initialAdsSignals` table test would mirror the suggested future work in the lead's diary for `initialResultsSignals`.
- Could consider priority 3 for `generate-ads` vs. `generate-results` at priority 2, if we want ads to fan out *before* the regular-result fanout in the queue. Currently they're both priority 2 (tie); the single `generate-ads` job at priority 2 enqueues 3 `generate-ad` jobs at priority 1, competing for the same runner with 10 `generate-result` jobs at priority 1.
- Bigger cards vs. differentiated padding: the brief says "visually almost identical"; if reviewers want the ads to be *even less* distinguishable, the CTA button and the explicit "Ad" slot could be dropped. Left as-is because "small call-to-action button" was an explicit brief item.

## Files touched

- `sqlite/migrations/1776940925-ads.up.sql` (new)
- `sqlite/migrations/1776940925-ads.down.sql` (new)
- `sqlite/search.go`
- `sqlite/search_test.go`
- `model/search.go`
- `model/error.go`
- `model/jobs.go`
- `llm/llm.go`
- `jobs/ads.go` (new)
- `jobs/register.go`
- `http/search.go`
- `html/results.go`
- `docs/diary/2026-04-23-ads-builder.md` (this file)

Lint: `0 issues.` Tests: all pass (`ok app/model`, `ok app/sqlite`, `ok app/sqlitetest`). E2E: three ads rendered, one ad landing page generated successfully.
