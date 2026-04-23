# Diary: OTel span attributes QA review

**Author:** qa (otel-span-attrs task)

Reviewed the builder's wiring of business-level OTel attributes onto the four HTTP handlers in `http/search.go` on branch `worktree-otel-span-attrs` (commit `2a5fa55`). Ship-ready, no issues.

## What I reviewed

- `http/search.go` diff -- the two new imports (`go.opentelemetry.io/otel/attribute`, `go.opentelemetry.io/otel/trace`), the `SetAttributes` calls in each of the four handlers, and the new `truncate` helper at the bottom.
- Builder's diary at `docs/diary/2026-04-23-otel-span-attrs-builder.md`.
- `jobs/search.go` and `jobs/ads.go` to confirm naming convention parity (`query.id`, `result.id`, `ad.id`, `result.position`, etc.).

## What I checked

| Check | Result |
|---|---|
| `r.Get("/", ...)` sets `query.raw` (truncated 256) and `query.text` after normalising | Pass -- lines 79-82. |
| `r.Get("/", ...)` sets `query.id` after `UpsertQuery` | Pass -- line 90. |
| `r.Get("/", ...)` sets `results.count` and `ads.count` before rendering | Pass -- lines 110-113. |
| Empty-query homepage branch leaves attributes alone | Pass -- early return at line 71/76. |
| `handleEvents` sets `query.text` after normalising | Pass -- line 181. |
| `handleEvents` sets `query.id` after `GetQueryByText` | Pass -- line 190. |
| `handleEvents` sets `events.results_sent` / `events.ads_sent` / `events.done` on every exit path past the sent-maps declaration | Pass -- defer at lines 199-205, placed exactly where the spec asked. Captures `done()` closure correctly. |
| `handleSite` sets `result.id` after `extractResultID` | Pass -- line 364. |
| `handleSite` sets `query.id` after `GetResult` | Pass -- line 377. |
| `handleSite` sets `website.cached=true` on the cached-hit branch | Pass -- line 381. |
| `handleSite` sets `website.cached=false` on the poll-success branch | Pass -- line 409. |
| `handleAd` sets `ad.id` after `extractAdID` | Pass -- line 455. |
| `handleAd` sets `query.id` from `a.QueryID` after `GetAd` | Pass -- line 468. |
| `handleAd` sets `ad_website.cached=true` on cached hit and `false` on poll success | Pass -- lines 472 and 499. |
| `truncate(s, n)` helper added at bottom, byte-based, 256 used at the call site | Pass -- lines 580-587. |
| Attribute keys follow the dot-separated jobs/ convention | Pass -- mirrors `query.id` / `result.id` / `ad.id` from `jobs/search.go` and `jobs/ads.go`. The mixed `events.results_sent` / `ad_website.cached` keys use underscores within a segment, which matches the spec's literal naming. |
| `jobs/*.go` and `cmd/app/main.go` untouched | Pass -- `git diff main...HEAD --stat` shows only `http/search.go` and the builder's diary. |
| `go build ./...` | Pass -- silent. |
| `go vet ./...` | Pass -- silent. |
| `go test ./...` | Pass -- `model`, `sqlite`, `sqlitetest` green; other packages have no tests. |
| `go mod tidy` clean | Pass -- zero diff in `go.mod` / `go.sum`. |

## Judgment calls

- **`handleEvents` defer placement vs. "every exit path".** The spec literally says "every exit path" but parenthetically suggests "a defer placed right after `sentResults` / `sentAds` are declared". Three early returns happen *before* that point: missing `q` (400), no flusher (500), and `GetQueryByText` failure (silent). The builder's defer doesn't cover those, but for those branches `events.results_sent` and `events.ads_sent` would both be `0` and `events.done` would be `false` -- low-signal data, and the spec's parenthetical clearly endorses this placement. Calling it correct.
- **No smoke test.** The task lists smoke testing as optional and says to skip if `make watch` would clash. `make watch` is not running; rather than start a server just to verify trace plumbing the static checks already exercise, I skipped it. The handler-by-handler diff review is precise enough that a smoke test would only add noise.
- **No tests added.** The change is pure observability plumbing -- no behavior changed, no logic to assert. Asserting `SetAttributes` calls would mean mocking the global tracer, which is overkill for what amounts to four `SetAttributes` calls per handler.

## Outcome

Reporting "approve, no issues" to `team-lead`, marking task #2 completed. No feedback to builder.
