# Diary: Hallucination Search

Turn Markus's Go web-app template into a parody search engine: every result and every destination page is fabricated on demand by Claude (Sonnet 4.6 for results, Opus 4.6 for full HTML pages), cached in SQLite, and streamed to the UI via Datastar signals.

## Step 1: Read the template and plan

### Prompt Context

**Verbatim prompt:** Build "Hallucination Search" -- a demo web app that turns a fresh Go template repo into a working search engine where every result and every destination page is fabricated by an LLM (Anthropic Claude via `maragu.dev/gai`). Cached in SQLite. (... full brief in the lead teammate's original message.)

**Interpretation:** Implement a single-page search experience where Sonnet fabricates 10 result cards per query and Opus fabricates the destination HTML page per result. Caching, jobs queue, Datastar signal streaming, and a fuchsia re-theme.

**Inferred intent:** Ship a working demo that's faithful to the brief, uses the shapes Markus already has (glue router, gomponents, goqite jobs, sqlite helpers), and doesn't rip out more of the template than necessary.

### What I did

Read the glue stack top-to-bottom to understand how the pieces fit: `cmd/app/main.go`, `http/routes.go`, `http/home.go`, `html/common.go`, `service/fat.go`, `jobs/email.go`, `sqlite/*.go`, plus the relevant glue source (`/Users/maragubot/Developer/go/pkg/mod/maragu.dev/glue@.../sql/helper.go`, `.../http/server.go`, `.../jobs/runner.go`) and the gai anthropic client (`.../gai/clients/anthropic/chat_complete.go`, test file) so I knew exactly which knobs existed.

Sketched the data model in my head: three STRICT tables (`queries`, `results`, `websites`) with the template's `id` / `created` / `updated` convention and the existing `_updated_timestamp` trigger style.

### Why

Matching the template's conventions means less surprise for Markus on review and fewer gratuitous merge conflicts with the upstream template.

### What worked

The template is genuinely small; everything wired up from one place (`cmd/app/main.go`). The glue router surfaces `*chi.Router` via `r.Mux` so I could drop in a raw `http.HandlerFunc` for `/events` and `/site/...` without wrestling with the HTML page wrapper. `gluehttp.NewServerOptions.WriteTimeout` is already exposed, so no wrapping required.

### What didn't work

Nothing at this stage; it was reading.

### What I learned

Latest gai anthropic client exposes `ChatCompleteModelClaudeSonnet4_6Latest` / `...Opus4_6Latest` directly, so I could just reference those constants instead of hardcoding raw model strings.

### What was tricky

Deciding how to represent the SSE payload. Picked "signals map keyed by `p0..p9`" rather than HTML fragment patches so the page layout is static and Datastar reactively fills the text. That follows the skill's "signals over fragments" guidance and keeps both the server side and the client side simple.

### What warrants review

Just the plan -- no code yet.

### Future work

Implement in this order: model + migration, sqlite queries, LLM wrapper, jobs, HTTP handlers, HTML components, main.go wiring, tests.

## Step 2: Query normalisation (TDD) and the new data model

### Prompt Context

**Verbatim prompt:** (continuation of the same brief; the bit that asks for normalise-trim-lowercase-collapse-whitespace on the query string.)
**Interpretation:** Write a pure normalisation helper with a solid test, then add the `Query`, `Result`, `Website`, `QueryID`, `ResultID` types to the model package.
**Inferred intent:** Give the rest of the app a single, test-covered definition of "normalised query text" so the unique constraint on `queries.text` actually means something.

### What I did

Wrote `/model/search_test.go` first with a table of edge cases, then implemented `NormalizeQuery` in `/model/search.go`. Verified green with `go test ./model/ -run TestNormalizeQuery -v`.

Added the domain structs alongside it and wrote a new migration at `/sqlite/migrations/1776932606-search.up.sql` with STRICT tables and the template's `_updated_timestamp` trigger pattern. `queries.text` is `unique not null`, `results` has a compound unique on `(query_id, position)` with a `check` constraint on position 0..9, `websites.result_id` is both PK and FK.

### Why

Normalisation keeps the cache hit rate sensible across "cats", " Cats ", "CATS\n" etc. The STRICT tables + uniqueness constraints let us do idempotent upserts and first-write-wins inserts in SQL directly.

### What worked

All seven normalisation subtests passed on first try. The shape of `Result` matched the sqlx `db:"..."` tag convention the rest of the template uses, so `h.Get` / `h.Select` into `model.Result` just works.

### What didn't work

Nothing notable.

### What I learned

`unicode.IsSpace` handles `\t` and `\n` and other whitespace uniformly; simpler than reaching for `regexp`.

### What was tricky

Nothing; normalisation is bread-and-butter.

### What warrants review

The test cases in `/model/search_test.go` -- they're deliberately dry ("lowercases a shouty query", "preserves unicode in the query"). The CAFÉ test asserts we lowercase unicode properly, which is load-bearing for the DB uniqueness check.

### Future work

None; normalisation is done.

## Step 3: SQLite queries, LLM wrapper, and jobs

### Prompt Context

**Verbatim prompt:** (brief: three job types with specific priorities, ON CONFLICT DO NOTHING semantics, timeouts on LLM calls, use `maragu.dev/gai`.)
**Interpretation:** Flesh out the data access layer, the Anthropic wrapper, and the three jobs in a coherent group so I can wire them in one go.
**Inferred intent:** Make the fabrication pipeline robust to duplicate work (double-enqueue is harmless) and to partial failures (retry one slot at a time).

### What I did

Added `/sqlite/search.go` with `UpsertQuery`, `GetQueryByText`, `GetQuery`, `GetResults`, `GetResultPositions`, `InsertResult`, `GetResult`, `GetWebsite`, `InsertWebsite`, using `on conflict (...) do nothing` for both the query-text upsert and the position/website inserts. Tests at `/sqlite/search_test.go` cover the happy path, the first-write-wins semantics, and the not-found error paths.

Wrote `/llm/llm.go`, wrapping gai's anthropic client into a pair of `ChatCompleter`s (Sonnet for results, Opus for websites). Used `gai.GenerateSchema[Result]()` for Sonnet structured output, with a small `stripJSONFences` fallback in case the model decides to wrap its output in a code fence anyway. For Opus I allowed freeform text output and stripped optional ```html fences before sanity-checking that the payload contains `<!doctype html` or `<html`. System prompts explicitly tell the model the content is fiction and not to refuse, and the Opus prompt forbids external network references (no `<img src>`, no `<script>`, inline styles only) so the fabricated page renders offline.

Added `/jobs/search.go` with the three handlers. `GenerateResults` is the fanout job: it reads already-filled positions and only enqueues `generate-result` jobs for the gaps. `GenerateResult` calls Sonnet with a 60s context timeout and does a plain `InsertResult` (which itself swallows conflicts). `GenerateWebsite` calls Opus with a 3-minute context and stores the HTML. The register function in `/jobs/register.go` now takes `Database`, `LLM`, `Log`, `Queue` instead of the old email sender plumbing.

### Why

The brief spelled out the semantics (idempotent enqueue, first-write-wins, don't refuse), so I mirrored those exactly.

### What worked

Once it all compiled, the fanout-then-fan-in pattern worked on the very first real query (see Step 5 for the validation). Claude Sonnet produced delightfully varied results for "cats" -- a fake Wikipedia article, a crypto token site, a conspiracy blog, a fake academic paper, etc. Exactly the vibe the brief asked for.

### What didn't work

Goimports tried to merge my two separate `maragu.dev/glue/jobs` imports (one aliased `gluejobs`, one plain) and staticcheck (`ST1019`) rightly complained. I removed the alias and used the plain `jobs.Create(...)` call.

```text
jobs/search.go:11:2: ST1019: package "maragu.dev/glue/jobs" is being imported more than once (staticcheck)
```

### What I learned

`gai.ChatCompleteRequest.MaxCompletionTokens` is an `*int`, and `gai.Ptr(16_384)` works because the inferred type follows the pointer return type. I also re-confirmed that the Anthropic client uses the structured-output API when `ResponseSchema` is set -- the model returns JSON directly, no schema instruction prompt needed.

### What was tricky

Balancing the `existing titles` hint into the Sonnet prompt. I opted to pass the titles already generated for a query as a bulleted list in the user message rather than the system prompt, so each positional job gets a fresh snapshot of what's already been produced. This is vulnerable to a race (two jobs running in parallel may both see no prior titles), but the ON CONFLICT on `(query_id, position)` means duplicates just get discarded by the DB, which is acceptable. Markus said robustness > optimisation.

### What warrants review

The Sonnet and Opus prompts in `/llm/llm.go`. They're doing the real work. In particular the Opus prompt explicitly forbids `<script>` and external URLs so the fabricated page is safe to serve with our default CSP (`unsafe-inline` for style, no external anything).

### Future work

An eval loop to test prompt quality would be nice, but not worth building mocks for now.

## Step 4: HTTP handlers, HTML components, Datastar wiring

### Prompt Context

**Verbatim prompt:** (brief: `/`, `/events`, `/site/{slug-and-result-id}`, skeleton cards, Datastar signal streaming, fuchsia theme.)
**Interpretation:** Three GET endpoints, one gomponents page for the home view, one for the results view, and the tailwind theme swap.
**Inferred intent:** A minimal, Google-inspired UI that degrades gracefully -- the results page should render whatever's cached immediately and only show skeletons for positions that are genuinely still cooking.

### What I did

Wrote `/http/search.go` with the three handlers:

- `GET /` upserts the query row, fires a `generate-results` job (as a safety net, harmless if duplicate), loads whatever results are already cached, and renders `ResultsPage`.
- `GET /events?q=...` opens an SSE stream, polls the DB every 500ms up to 60 seconds, and pushes a single `datastar-patch-signals` event each tick containing the `p0..p9` positions we know about. Uses plain `fmt.Fprintf(w, "event: datastar-patch-signals\ndata: signals %s\n\n", ...)` because we don't need the full Datastar SDK for this.
- `GET /site/{slug}` extracts the trailing `r_xxxxxxxx` result ID from the cosmetic slug, looks up the result, returns cached HTML if available, otherwise enqueues a `generate-website` job and polls every 500ms up to 2 minutes. On timeout, returns `502 Bad Gateway` as the brief specified.

Wrote `/html/home.go` (big centred title + search form) and `/html/results.go` (pinned search bar + ten stacked cards). Each card has a hidden skeleton div (`data-show="$results.pN == null"`) and a filled div (`data-show="$results.pN != null"`) whose text is bound with `data.Text("$results.pN.t")` etc. Server-rendered Text() nodes act as the no-JS fallback; Datastar rebinds the same text when the signal lands.

Swapped `tailwind.css`'s `--color-indigo-*` custom properties to `--color-fuchsia-*`. The `primary-XXX` utilities now render fuchsia throughout because the theme is `@theme inline` referencing those vars.

Stripped `html/common.go`'s old counter footer for a simple "Hallucination Search · by maragu" line, and removed the `data "maragu.dev/gomponents-datastar"` import now that the footer doesn't use `data.Init`/`data.OnInterval`.

### Why

Following the skill's guidance: signals over HTML fragments. The server's `/events` endpoint doesn't have to know about HTML layout; it just ships JSON under `results.pN`. Pretty close to the sweet spot for this app.

### What worked

End-to-end test with query "cats" fabricated all 10 Sonnet results in ~7 seconds total, the SSE handler streamed them in five `datastar-patch-signals` events, and clicking a result fabricated a styled-up fake Tulsa newspaper article in ~2 minutes from Opus. Second hit on the same site URL was instant (cache).

The fuchsia theme swap took two characters of work because the template already parameterises via `--color-primary-N`.

### What didn't work

Two Datastar gotchas worth noting:

1. My first iteration used JavaScript optional chaining (`$results.pN?.t ?? "fallback"`) so that the *filled* card wouldn't crash if the signal was still `null`. But that whole div is already gated on `data-show="$results.pN != null"`, so the `?.` is both unnecessary and visually noisy. I dropped it.

2. The Opus website job once took 2 minutes 1 second while the HTTP handler polled for only 2 minutes, so the initial request returned `502 Bad Gateway` even though the job completed a moment later. Refresh served the cached page instantly. That's actually the behaviour specified in the brief ("On timeout, 502 Bad Gateway") -- but it's worth knowing that Opus + 21KB of HTML can run right up to the poll budget. The server `WriteTimeout` is set to 3 minutes so the handler has plenty of headroom; only the poll budget causes the 502.

### What I learned

`gluehttp.NewServerOptions.WriteTimeout` is a plain `time.Duration` and defaults to 10s. The brief's concern about wrapping the server in a custom `http.Server` was unfounded -- just pass `WriteTimeout: 3 * time.Minute`.

### What was tricky

The cosmetic slug in `/site/:slug`. The brief said "extract trailing `r_xxxxxxxx`". I used a compiled `regexp.MustCompile(\`r_[0-9a-f]{32}$\`)` on the path segment; anything before that is cosmetic. I also generate the slug server-side inside the signal payload (`"s": "tulsa-cat-r_290061..."`), so Datastar's `data-attr href` just reads `'/site/' + $results.pN.s` and the pretty URL travels through SSE.

### What warrants review

- `/http/search.go` `writeSiteHTML` sets `Content-Type: text/html; charset=utf-8` and `Cache-Control: public, max-age=86400`. The CSP middleware still runs before this handler, so the fabricated HTML inherits our `script-src 'self' 'unsafe-inline'` / `style-src 'self' 'unsafe-inline'`. Inline styles are needed for this to work; the `.env.example` sets `CSP_ALLOW_UNSAFE_INLINE=true` to match.
- The Datastar expressions in `/html/results.go` -- confirm the skeleton/filled toggle renders correctly and that there's no flicker on first paint when results are already cached.

### Future work

- Could add `retry: 1000` to SSE events so clients reconnect automatically on drop, but it's not needed for the demo.
- Could pre-render the initial signal payload straight into the `data-signals` attribute (already done) but also write an initial `datastar-patch-signals` frame with the remaining nulls -- not useful here because the initial HTML is the source of truth.

## Step 5: Cleanup, test, validate

### Prompt Context

**Verbatim prompt:** (brief: strip unused S3/email wiring, keep auth tables because "auth lives in the proxy", update README, run `make lint` and `make test`, hit the app in a browser.)
**Interpretation:** Remove everything the search engine doesn't need; keep everything auth-related even though nothing in-app uses it; verify with curl.
**Inferred intent:** Minimal diff, clean lint, passing tests, and actual manual confirmation that the whole loop works.

### What I did

Rewired `/service/fat.go` to hold `db`, `llm`, and `queue` (no more bucket / sender) and kept `GetUser` so the auth middleware keeps working untouched. Deleted `/jobs/email.go`, kept the auth tables (`users`, `accounts`, `tokens`, `roles`) and the sessions table -- harmless and preserves upstream merge compatibility. Removed `aws`/`s3`/`postmark` imports from `/cmd/app/main.go`. Removed `docker-compose.yml` and `.env.docker.example` (versitygw is no longer used) and simplified the Makefile to drop `test-up`/`test-down`/`up`/`down`/`clean-all` references to the S3 mock. Replaced the old example in `/.env.example` with an `ANTHROPIC_API_KEY` line and a concise block.

Rewrote `/README.md` to describe the new app and what `make watch` needs.

Ran `make fmt`, `golangci-lint run`, `go test -shuffle on ./...`. Lint clean, all tests pass (93% coverage on the model package thanks to `NormalizeQuery` coverage; 50% on sqlite from the new query tests).

Built the binary, started `make watch`, allocated port 9090 via `fabrik:worktrees`, and walked the UI end-to-end with curl:

```sh
curl -s 'http://localhost:9090/?q=cats' > /tmp/results.html
curl -s -N 'http://localhost:9090/events?q=cats' --max-time 25  # streamed 5 signal patches
curl -s 'http://localhost:9090/site/cheese-r_290061ba178371ff223e94a4b5829f0e' --max-time 180
```

All three endpoints behaved as specified. Dumped the DB with `sqlite3 app.db 'select position, title, display_url from results'` -- ten wildly different fabricated results for "cats": Felinepedia, a Wikipedia clone, CatZone Pro™, a Tulsa Observer article about a cheese-retrieving cat, a peer-reviewed paper on human overcrowding, a psychic bingo cat, sub-auditory feline vibrations, a government-drone conspiracy site, the "Cats (2019 Film) Legacy & The Butthole Cut Explained" fandom page, and a CATS coin token site.

### Why

Validation matters; shipping a PR that builds green but doesn't actually work is a waste of review time.

### What worked

Everything. `make watch` rebuilt cleanly across file saves. The Datastar signal stream carried JSON identical to what I'd expect. Cache hits on the second request were 3ms.

### What didn't work

The very first `make watch` attempt failed with `listen tcp :8080: bind: address already in use` because another worktree was already running on 8080. Fixed by allocating port 9090 via the worktrees skill and rewriting `.env`'s `SERVER_ADDRESS` / `BASE_URL`.

### What I learned

The watch tool (`maragu.dev/redo`) catches `.env` changes and automatically restarts the app, which is handy when iterating on prompts or port configs.

### What was tricky

Nothing notable beyond the port conflict.

### What warrants review

- `/cmd/app/main.go` -- I removed `postmark` / `s3` / `aws` wiring. If Markus wants to keep the auth email flows available for future login-by-email, the sender would need to come back. He said the proxy handles it, so the sender is gone.
- `/.env.example` shrank significantly. Check that nothing external (e.g. a deployment script) expects the old `S3_*` / `POSTMARK_*` vars.
- `docker-compose.yml` and `.env.docker.example` deleted because versitygw was the only service and it's no longer needed. If there was value keeping the versitygw mock around for something else, revert those deletions.

### Future work

- Add `ANTHROPIC_API_KEY` validation at startup so the app fails fast rather than on the first job.
- Consider a short result-display stub (e.g. "loading 10 fabrications...") above the cards when we just created a brand new query, so the user sees *something* besides the ten skeletons.
- Could add a `retry: 3` directive to SSE frames and client-side backoff on disconnect, but the 60s budget + initial-signals-in-HTML combo makes reconnect rarely necessary.
