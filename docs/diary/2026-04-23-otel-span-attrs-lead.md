# Diary: Add OTel span attributes to HTTP handlers (lead)

**Author:** lead

Thin task: the glue framework already instruments every HTTP handler and every background job with a tracing span, but the app was only adding business-level attributes on the job side. Four HTTP handlers in `/http/search.go` were going to Honeycomb with route/method/user-agent but no `query.id`, `result.id`, `ad.id`, etc. This is the lead's view: scoping, delegation, verification.

## Step 1: Scope check and delegation

### Prompt Context

**Verbatim prompt:** "Check the diary for recent work. Make sure that OpenTelemetry is wired up. In particular, set span attributes in the HTTP handlers as well as the background jobs. The glue framework already makes sure that all HTTP handlers and background jobs are instrumented."

**Interpretation:** The baseline (span creation, trace context propagation, route/method tagging) is already handled by the framework. What's missing is app-domain attributes on each span so traces are actually useful in Honeycomb. The parenthetical "in particular" signals where to look, not new scope.

**Inferred intent:** Markus wants wide-event-style traces. Each span should answer "what was this about" without cross-referencing other systems -- the query text, the query ID, the result/ad being rendered, relevant counters. Jobs already do this; HTTP handlers don't.

### What I did

Read the diary to load context (most recent: `2026-04-23-robust-retries-lead.md`, covering the `gai/robust` retry wrapping). Then mapped the OTel surface:

- Grepped for `otel` / `trace.` across the repo: only `/jobs/search.go` and `/jobs/ads.go` had hits. HTTP handlers had zero.
- Read `/cmd/app/main.go` to confirm glue's `NewServer` is in use (it is), which means glue's `OpenTelemetry` middleware wraps every handler.
- Read `maragu.dev/glue/http/otel.go` to confirm what the middleware contributes: `main=true`, HTTP route, method, user-agent semconv, URL query params. All HTTP-level. No app-domain attributes.
- Read `maragu.dev/glue/jobs/runner.go` to confirm `WithTracing` creates the job span with `main=true` and extracts trace context from the `tracedMessage` wrapper.
- Re-read `/jobs/search.go` and `/jobs/ads.go`: all six job funcs already call `trace.SpanFromContext(ctx).SetAttributes(...)` with the right business fields (`query.id`, `result.position`, `ad.id`, etc.).

So the gap was entirely on the HTTP side. Four handlers to extend: the `GET /` search/results route, `handleEvents` for the SSE stream, `handleSite` for fabricated result pages, `handleAd` for fabricated ad pages.

Entered a worktree `otel-span-attrs` (branch `worktree-otel-span-attrs`), created a fabrik team with the same name, and wrote two tasks on the shared task list:

1. Builder: extend each handler with a concrete set of attributes. Keys spelled out verbatim in the task description so there was no interpretation gap: `query.raw` (truncated to 256 bytes), `query.text`, `query.id`, `results.count`, `ads.count` on `GET /`; `query.text`, `query.id`, plus deferred `events.results_sent` / `events.ads_sent` / `events.done` on `handleEvents`; `result.id`, `query.id`, `website.cached` on `handleSite`; `ad.id`, `query.id`, `ad_website.cached` on `handleAd`. Jobs and `cmd/app/main.go` explicitly out of scope.
2. QA: review against the same spec, run `go build` / `vet` / `test` / `go mod tidy`, check naming convention matches `/jobs/*.go`.

QA blocked on builder. Spawned both in the background.

One coordination glitch: the first two tasks I created landed on the session's default task list rather than the team's. Had to re-create both on the team list, which is where builder and qa were looking.

### Why

Four handlers, a small helper, ~50 line change. Writing the attribute keys out verbatim meant the builder didn't have to reverse-engineer my intent from the jobs convention, and QA had an unambiguous checklist. The deferred-attribute pattern in `handleEvents` was the one non-obvious ask -- spelled it out explicitly because otherwise the builder would reasonably add `SetAttributes` calls before each `return` and miss edge cases.

### What worked

Builder shipped as commit `2a5fa55` without questions. Every attribute landed where specified. `handleEvents` used the exact `defer` pattern I suggested, placed right after the `sentResults` / `sentAds` maps are created -- which meant `done()` had to be moved up too. The builder caught that and did it cleanly. QA approved explicitly with no findings.

The builder also added a short `truncate` helper with a comment explaining the OTel ~4K attribute-value drop rule. Small polish, welcome.

### What didn't work

Task-list routing bug on spawn. The two `TaskCreate` calls fired before `TeamCreate` went to my solo list; calling `TaskList` inside the team showed "No tasks found". Noticed before spawning teammates, re-created both tasks, no real lost time -- but worth remembering: `TeamCreate` first, then `TaskCreate`.

Nothing broke in the implementation itself.

### What I learned

Glue's `OpenTelemetry` middleware does a lot more than I'd have guessed from a quick read: it parses user-agent into semconv fields, tags mobile/tablet/desktop/bot device type, emits URL query params as individual attributes, and stamps `main=true` for Jeremy Morrell's wide-events filter convention. The matching convention on the jobs side (`WithTracing` also sets `main=true`) means a single Honeycomb filter `main = true` gives you the "one row per request or job" view without any extra config.

`trace.SpanFromContext(ctx)` is a no-op stub if no span is in context, so calling `SetAttributes` on it is safe even in a handler that somehow got invoked without the middleware. No nil-check needed.

### What was tricky

One subtle ordering issue in `handleEvents`: the `done()` closure depends on `sentResults` / `sentAds`, and the `defer` block that reports `events.done` also calls `done()`. The builder correctly moved the `done` declaration above the `defer`, which captures the closure by reference -- so when the deferred function runs after all the updates, `done()` returns the right value. A builder who pattern-matched against the original code layout would have produced a `done` that was declared after the `defer`, which would have shadowed with nil or compiled and surprised at runtime. Worth noting for future defer-based attribute emission.

Byte-truncation of `query.raw` at 256 bytes is not UTF-8-safe -- if someone searches with a multi-byte script at exactly the boundary, the attribute value ends mid-rune. OTel exporters tolerate this (they encode as bytes), so it's fine in practice, but a rune-aware truncate would be more polite. Not worth blocking on.

### What warrants review

The behavioral surface is small and mechanical: four `SetAttributes` clusters on success paths, plus one `defer`. The one thing a reviewer should mentally walk through is the four exit paths in `handleEvents`:

1. Early `return` after `svc.GetQueryByText` fails -- defer hasn't been registered yet, so no `events.*` on that span. That's fine; the defer is placed after the lookup so failures before that point don't emit garbage counts.
2. `pushState` returns an error before the ticker loop -- defer runs, `events.done` is false, counts reflect whatever was sent.
3. `done()` returns true inside the ticker loop -- defer runs, `events.done` is true.
4. `ctx.Done()` fires -- defer runs, `events.done` is whatever it was.

All four are correct.

Reviewer should also confirm no attribute keys collide with ones glue already emits (`http.*`, `user_agent.*`, `url.query.*`, `browser.*`, `device.*`, `main`). Ours are all under `query.*`, `result.*`, `ad.*`, `results.*`, `ads.*`, `website.*`, `ad_website.*`, `events.*` -- no collision.

### Future work

A handful of small improvements worth considering later, none blocking:

- Rune-aware `truncate` (replace `s[:n]` with `[]rune(s)[:n]` then re-encode, capped separately if we care about byte budget).
- Add `query.text` to the job spans too, so traces filtered by `query.text` from the HTTP side line up with job spans without cross-referencing `query.id`. Currently jobs only tag the ID.
- The poll-loop branches in `handleSite` / `handleAd` could emit `website.poll_attempts` / `ad_website.poll_attempts` as an int. Useful for spotting when the cache-miss path is close to the 2-minute timeout.
- No custom tracer / custom spans needed in the handlers themselves; the root span carries enough. If we ever want to isolate LLM latency from DB latency in the handler path, that's when to add manual spans.

## Step 2: Verification

### Prompt Context

**Verbatim prompt:** (no separate prompt -- this step is me verifying the shipped work before reporting to Markus)

**Interpretation:** Trust but verify. The builder's summary said "all four handlers done per spec" and QA said "approve, no issues", but my own system prompt tells me to check diffs rather than trust summaries.

**Inferred intent:** Confirm the shipped diff matches the spec so when Markus reviews he doesn't find a gap.

### What I did

Ran `git log --oneline main..HEAD` -- single commit `2a5fa55`. `git diff --stat` showed 55 lines in `/http/search.go` and a 146-line builder diary. Read the full diff for `/http/search.go`.

Walked the checklist:

- `GET /`: two `SetAttributes` blocks, one right after normalisation with `query.raw` + `query.text`, one after upsert with `query.id`, one before render with `results.count` + `ads.count`. Correct.
- `handleEvents`: `query.text` after normalisation, `query.id` after lookup, `done` closure relocated above the `defer`, deferred trio emits correctly. Correct.
- `handleSite`: `result.id` after slug extract, `query.id` after `GetResult`, `website.cached=true` on the cache-hit branch, `website.cached=false` on the poll-success branch. Correct.
- `handleAd`: symmetric with `ad.id`, `query.id` (from `a.QueryID`), `ad_website.cached` on both branches. Correct.
- `truncate` helper added at the bottom with a comment. Correct.

### Why

The builder's summary and QA's approval are signal, not proof. The diff is proof. Five minutes of reading saves a follow-up if something was misread.

### What worked

The diff was exactly what I'd specified. Nothing to correct.

### What didn't work

Nothing.

### What I learned

A tight, attribute-literal task description (keys spelled out, out-of-scope files listed) produces drop-in work. Compare to a looser "add sensible attributes to the HTTP handlers" phrasing -- I'd have had to iterate on naming conventions, truncation, exit-path handling. The upfront precision was cheap and paid off immediately.

### What was tricky

Nothing tricky in verification. The ordering subtlety in `handleEvents` (closure above defer) was visible in the diff.

### What warrants review

Same surface as step 1. Nothing new added here.

### Future work

None. Waiting for Markus to decide whether to open a PR or keep this on the branch.
