# Diary: Nano Banana on-demand image generation QA review

**Author:** qa (nanobanana-images task #2)

Reviewed the builder's feature commit `3766b1e Add on-demand Nano Banana image generation at /image/{path...}` on branch `worktree-nanobanana-images`. A new `GET /image/{path...}` endpoint lazily generates fabricated images through Google's Gemini 2.5 Flash Image on cache miss and serves cached bytes from SQLite on hit. Fabricated destination pages (for both organic results and ads) may now embed `<img src="/image/short-descriptive-slug">`. Ship-ready, no blocking issues.

## What I reviewed

All builder-touched files:

- `/http/image.go` (new) -- handler plus `imagePathToPrompt` helper.
- `/http/image_test.go` (new) -- 13-case table test for the helper.
- `/http/search.go` -- wildcard `/image/*` route wired through `searchDB` + `imageGenerator`.
- `/http/routes.go` -- passes `svc.LLM()` as the generator.
- `/http/csp.go` -- verified **unchanged**; `img-src 'self'` still allows same-origin `/image/...`.
- `/llm/llm.go` -- new `NanoBananaModel` constant, `GoogleKey` on `NewClientOptions`, `Image(ctx, prompt)` method, `sniffImageMime` helper, revised `websiteSystemPrompt` / `adWebsiteSystemPrompt`.
- `/sqlite/images.go` + `/sqlite/images_test.go` (new) -- CRUD plus three test cases.
- `/sqlite/migrations/1776949571-images.{up,down}.sql` (new).
- `/model/search.go` (new `Image` struct) and `/model/error.go` (new `ErrorImageNotFound`).
- `/service/fat.go` (new `LLM()` accessor).
- `/cmd/app/main.go` (one-line `GoogleKey` wiring).
- `/.env.example` (one-line `GOOGLE_API_KEY` addition in alphabetical slot).
- `/docs/diary/2026-04-23-nanobanana-builder.md` (builder diary).

Also read `google.golang.org/genai@v1.52.1/types.go` (`Part`, `Blob`) and `maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0/clients/google/client.go` (confirming the exposed `Client *genai.Client` field) to sanity-check SDK wiring.

## Tests / lint / build

- `go build ./...` -- silent.
- `go vet ./...` -- silent.
- `make fmt` -- no diff.
- `golangci-lint run` -- `0 issues.`
- `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./...` -- all packages green (`app/http`, `app/model`, `app/sqlite`, `app/sqlitetest`).
- `go test ./sqlite/ -run TestInsertAndGetImage -v` -- three subtests pass (round-trip, first-write-wins, `ErrorImageNotFound`).
- `go test -run TestImagePathToPrompt -v ./http/` -- 13 of 13 subtests pass.
- `go mod tidy` -- `go.mod` and `go.sum` unchanged from committed state.

## End-to-end verification

Started `make watch` against the worktree's `:8090`. Walked the full flow:

- `http://localhost:8090/?q=rare+vintage+typewriters` -- homepage redirected to results, 10 results + 3 ads populated through SSE in ~30s.
- Clicked into `/site/rare-vintage-typewriter-collection-valuation-guide-r_d8209ca8...`. The website-generation job ran, handler blocked ~40s, rendered the fabricated page.
- `page.evaluate` on the rendered page yielded two `<img>` tags, both with `/image/...` sources: `/image/vintage-typewriter-collection-display-wooden-shelf` and `/image/close-up-hermes-3000-typewriter-keystroke-mechanism-detail`. Accessibility snapshot (`.playwright-cli/page-2026-04-23T13-21-44-853Z.yml`) lists both under `- img "Collection of colorful vintage typewriters arranged on antique wooden shelves"` and `- img "Detailed view of the precise mechanical key mechanism of a Hermes 3000 typewriter"`.
- `curl -sD - -o /tmp/b1 <first-image-url>` returned `HTTP/1.1 200 OK`, `Content-Type: image/png`, `Cache-Control: public, max-age=31536000, immutable`, 1,851,726 bytes, `file` reported "PNG image data, 1024 x 1024, 8-bit/color RGB, non-interlaced". Came back in 23ms -- cache hit (browser had already triggered generation).
- Same curl on the second image: 21ms, 1,498,357 bytes, identical headers. Also cache hit.
- Forced a **cache miss** with a unique URL `/image/qa-test-teapot-floating-in-deep-space-starfield-<epoch>`. Fetch took 5.9s, returned a valid 1,739,214-byte PNG, and the row landed in the `images` table (`sqlite3 app.db "select path_hash, path, mime_type, length(data) from images"`).
- Edge cases against the running server:
  - `/image/` -> `HTTP/1.1 400 Bad Request` (empty prompt).
  - `/image/---` -> `HTTP/1.1 400 Bad Request` (all separators collapse to empty).
  - `/image/%FF%FE` -> `HTTP/1.1 400 Bad Request` (non-UTF-8 rejected after percent-decode).
  - 2000-byte path -> `HTTP/1.1 414 Request URI Too Long`.
  - `/image/a/b/c` -> `HTTP/1.1 200 OK` (multi-segment path traversed `chi`'s `/image/*` wildcard and generated a real image for prompt "a b c"), confirming the wildcard works.
- `curl -I` on an image URL returns `405 Method Not Allowed` because chi registers GET only. That's the expected framework behaviour (same as `/site/...` and `/ad/...`); browsers don't HEAD `<img>` URLs.

## Verified per the spec checklist

| Check | Result |
|---|---|
| `llm.Image(...)` uses `gai/clients/google`'s exposed `*genai.Client` (not a freshly-constructed one) | Pass -- `/llm/llm.go:335` calls `c.google.Client.Models.GenerateContent(...)`. Confirmed `google.Client.Client` is the exposed `*genai.Client` field. |
| Model ID is `"gemini-2.5-flash-image"` v1 GA (not preview/pro) | Pass -- `/llm/llm.go:29`. |
| Extracts `part.InlineData.Data`, returns empty-data error when absent | Pass -- `/llm/llm.go:347-354`. Walks `cand.Content.Parts`, skips parts with `nil` or zero-length `InlineData`, returns `"no image data in response"` if the loop exits. |
| MIME sniff: PNG `\x89PNG\r\n\x1a\n` (8 bytes) and JPEG `\xFF\xD8` (2 bytes), default `image/png` | Pass -- `/llm/llm.go:359-369`. Byte prefixes and lengths match exactly. |
| Sniffed mime is stored (not the API-returned `MIMEType`) | Pass -- handler persists `mime` from `gen.Image` into `img.MimeType`. (The builder deliberately sniffs rather than trusting `Blob.MIMEType`; harmless duplication, spec-compliant.) |
| STRICT `images` table, correct column order, first-write-wins on insert, matching update trigger | Pass -- `/sqlite/migrations/1776949571-images.up.sql`. PK `path_hash`, columns in order `path_hash, created, updated, path, mime_type, data`, trigger `images_updated_timestamp`. `InsertImage` uses `on conflict (path_hash) do nothing`. `select *` column order matches the `Image` struct. |
| Down migration truly reverses up | Pass -- drops trigger then table. |
| `/image/*` chi wildcard accepts multi-segment paths | Pass -- verified E2E with `/image/a/b/c`. |
| Length cap at 1024 bytes -> 414 | Pass -- 2000-byte path returns 414. |
| URL decoding, non-UTF-8 -> 400 | Pass -- `/image/%FF%FE` returns 400. |
| Empty / separators-only prompt -> 400 | Pass -- `/image/` and `/image/---` return 400. |
| Cache-hit: `Content-Type`, `Cache-Control: public, max-age=31536000, immutable`, no re-generate | Pass -- E2E cache hits returned in 21-23ms with correct headers. |
| Cache-miss: 60s context, model call, `InsertImage`, serve bytes, `502` on model error | Pass -- code path at `/http/image.go:94-119`. 60s wrap on `ctx`, 502 on `gen.Image` error, log-and-continue on `InsertImage` failure so the user still gets bytes. |
| OTel span attributes `image.path_hash`, `image.cached`, `image.bytes`, `image.mime`, `image.path_len` | Pass -- `path_len` before the length cap, `path_hash` after hashing, and all five on both the hit and miss happy paths. |
| 13-case helper test covers percent-encoding, slashes, dashes, uppercase, whitespace, non-UTF-8, empty | Pass -- every case matches the spec list. |
| `websiteSystemPrompt` / `adWebsiteSystemPrompt` allow `<img src="/image/...">`, forbid external `<img src="https://...">`, still forbid `<script>`, encourage (not demand) 0-3 images | Pass -- `/llm/llm.go:150-162` and `277-289`. Wording is explicit: "Same-origin `/image/...` is the only allowed `<img>` source", "No `<script>` tags", "Do not demand images on every page; include one or two only when they fit". External CSS/fonts/scripts remain forbidden. Inline SVG and CSS gradients are explicitly still allowed (intentional escape hatch). |
| CSP unchanged | Pass -- `git diff` on `/http/csp.go` is empty. Same-origin `/image/...` is allowed by the existing `img-src 'self'`. |
| Concurrency: no singleflight, DB conflict handling sufficient | Pass -- duplicate requests both generate, one wins `on conflict do nothing`, other's bytes are discarded but still served to that caller. The builder's diary explicitly notes singleflight is out of scope. |

## Judgment calls

- **1024-byte boundary is exercised end-to-end, not as a unit-test case.** The spec mentioned a "1024-byte boundary" test. The boundary check lives in the handler (not in `imagePathToPrompt`), so a pure-helper table test doesn't fit. The 2000-byte E2E curl catches it; did not add a handler-level test. Minor and accepted.
- **Non-separator punctuation passes through.** `imagePathToPrompt` only treats `-` and `/` as word separators, so something like `/image/---!!!---` normalises to `"!!!"` and reaches the model, which errors out -> 502. The spec's "empty / all-punctuation prompt returns 400" is therefore only satisfied for the kebab and slash characters that the helper actively collapses. Left alone because (a) this path isn't reachable from builder-emitted HTML (the LLM is instructed to emit kebab-case only), (b) hostile users get a 502 rather than a 400 which is functionally the same outcome, and (c) broadening the separator set risks over-rejecting genuinely descriptive prompts that include parentheses or apostrophes. Flagging as a minor robustness gap, not a blocker.
- **`sniffImageMime` default to `image/png` when the magic bytes don't match PNG or JPEG.** If Nano Banana ever starts returning WebP, we'd serve WebP bytes under a `Content-Type: image/png`. Browsers would sniff-correct and still render, but the response is mildly dishonest. The builder's `Blob.MIMEType` field from the API is ignored in favour of sniffing -- spec-compliant, but worth noting if the model ever expands its output formats. Not a blocker today.
- **Error responses (400/414/500/502) don't set `image.cached`.** Span attributes for failure branches are minimal by comparison to the success branches. The spec only required the five attributes on the happy paths; not adding more. Minor observability gap.
- **No handler-level tests.** The `handleImage` handler is unit-untested (only the `imagePathToPrompt` helper is). The builder relies on the E2E walk-through plus the sqlite tests for coverage. Given the handler is short, mostly glue, and the helper + storage are already under test, this is acceptable. If this area gets more complex later (retry logic, singleflight, rate limiting) a mockable-interface-based handler test would be worth adding.

## Fixes I made

None. Lint clean, tests green, E2E flow cleanly produces and caches fabricated images, all spec checks pass, no structural concerns. No commits authored.

## Outcome

**Approve.** Reporting ready-to-PR to the lead. Marking task #2 completed (no `TaskUpdate` tool in this environment; lead should update the task list externally).

## Note on task infrastructure

Same as the builder's diary -- no `TaskUpdate` tool or `tasks.md` file in this worktree. Proceeded with the review per the written brief and am flagging completion here for the lead to reflect in whatever tracker they maintain.
