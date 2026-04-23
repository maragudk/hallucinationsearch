# Diary: Nano Banana on-demand image generation (builder)

**Author:** builder (nanobanana-images task)

Adding on-demand fabricated images to Hallucination Search. The fabricated destination pages (and ad landing pages) can now embed `<img src="/image/tabby-cat-on-library-books">` and the server lazily calls Google's Gemini 2.5 Flash Image (nano-banana v1) to materialise the image, caching the bytes in SQLite keyed by sha256(normalised-path). Works inline in the handler -- no job queue -- and reuses the existing `maragu.dev/gai` Google client's `Client.Client` field to reach the raw `google.golang.org/genai` API since `gai`'s `ChatCompleter` abstraction does not yet support multi-modal output.

## Step 1: Full feature build

### Prompt Context

**Verbatim prompt:** "You're implementing a new feature in the 'Hallucination Search' parody search engine: on-demand image generation via Google's Gemini 2.5 Flash Image ('Nano Banana'), so the fabricated destination websites (and ad landing pages) can embed inline images that are themselves fabricated. [...] Task #1 in the shared task list is yours to own; claim it [...]. [...] A new HTTP handler `GET /image/{path...}` [...] URL-decodes + normalises the path into a prompt string. Looks up a cached image in SQLite keyed by sha256(normalised-path). On cache miss, calls Nano Banana inline (no goqite job), stores the bytes in SQLite, then returns them. On cache hit, returns the stored bytes immediately. [...] Use model constant `gemini-2.5-flash-image` [...]. [...] Update the `websiteSystemPrompt` and `adWebsiteSystemPrompt` in `llm/llm.go`: allow `<img>` tags whose `src` starts with `/image/`. [...]"

**Interpretation:** A self-contained new endpoint with its own storage table, plus a small extension to the LLM client to reach the `genai` SDK underneath `gai`. The prompts on the website/ad-website generators need a targeted rewrite so the HTML generator knows it may now emit `/image/...` URLs without external references. Concurrency story is trivial (first-write-wins via `on conflict do nothing`).

**Inferred intent:** Lean into the fabrication joke -- every piece of the destination page becomes a hallucination, including the images. The cache key is the prompt itself (after normalisation), so a fresh `<img src="/image/tabby-cat">` on page A and page B yields the same bytes. No background jobs because inline is simpler and avoids the browser cache poisoning a transient 502 to `<img>`.

### What I did

**Task-list coordination.** There is no `TaskUpdate` tool in my environment and no `tasks.md` file in the worktree either. I proceeded anyway, documenting this in my final hand-off so the lead can update task state themselves.

**Schema.** New migration pair at `/sqlite/migrations/1776949571-images.up.sql` / `.down.sql`. The `images` table uses `path_hash` as the primary key (sha256 hex of the normalised prompt), stores `path` (the raw prompt for debugging), `mime_type`, and `data blob`, and carries the standard `created`/`updated` columns with the trigger. Down migration drops the trigger and the table.

**Model.** Added `Image` struct (`PathHash`, `Created`, `Updated`, `Path`, `MimeType`, `Data`) in `/model/search.go`, and `ErrorImageNotFound` in `/model/error.go`. Followed the shape of the existing `Website` / `AdWebsite` structs where the PK is not a generated id but the cache key itself.

**Storage layer.** New file `/sqlite/images.go` with `GetImage(ctx, pathHash) (Image, error)` returning `ErrorImageNotFound` on no rows, and `InsertImage(ctx, img)` using `on conflict (path_hash) do nothing`. Companion test file `/sqlite/images_test.go` with three subtests: round-trip (verifies all fields survive including the raw bytes), first-write-wins on conflict, and `ErrorImageNotFound` on missing.

**LLM client.** Extended `/llm/llm.go` with:
- `NanoBananaModel = "gemini-2.5-flash-image"` constant alongside the existing `HaikuModel`.
- `GoogleKey` field on `NewClientOptions`.
- `google *google.Client` field on the `Client` struct.
- `Image(ctx, prompt) ([]byte, string, error)` method that opens a 60s context, calls `c.google.Client.Models.GenerateContent(ctx, NanoBananaModel, genai.Text(prompt), nil)`, walks `resp.Candidates[0].Content.Parts` for the first `InlineData` part, and returns its bytes. Errors are wrapped with `fmt.Errorf("...: %w", err)` throughout.
- `sniffImageMime(data)` helper that recognises PNG (`\x89PNG\r\n\x1a\n`) and JPEG (`\xFF\xD8`) magic, defaulting to `image/png`.

**Website prompts.** Rewrote the `<img>` clause in both `websiteSystemPrompt` and `adWebsiteSystemPrompt` to allow `<img src="/image/...">` with a short kebab-case description, explicitly banning external URLs. Encourage 0-3 images per page "where they genuinely fit the vibe". Left inline SVG and CSS gradients allowed. Forbidding `<script>` and external anything remained unchanged.

**HTTP handler.** New file `/http/image.go` with `handleImage(log, svc, gen)`. Flow:
1. Strip `/image/` prefix from `r.URL.Path`.
2. If longer than 1024 bytes, 414 URI Too Long.
3. `imagePathToPrompt(raw)` URL-decodes, rejects non-UTF-8, replaces `-` and `/` with space, lowercases, collapses whitespace, trims. Empty → 400.
4. sha256 the prompt for the `path_hash`.
5. `GetImage`; on hit, emit `Content-Type`, `Cache-Control: public, max-age=31536000, immutable`, write bytes.
6. On miss, inline call to `gen.Image(ctx, prompt)` with a 60s context. Failure → 502. Success → `InsertImage` (log-but-continue if the insert errors), then emit the bytes.
7. OTel span attributes: `image.path_hash`, `image.path_len`, `image.cached` (bool), `image.bytes`, `image.mime`.

**Route.** Extended `Search(r, log, svc)` in `/http/search.go` to take an `imageGenerator` alongside `svc`, wiring in `r.Mux.Get("/image/*", handleImage(...))` (chi wildcard so multi-segment paths like `/image/a/b/c` are legal). Also extended the `searchDB` interface with `GetImage` / `InsertImage` so the same adapter path through `service.Fat` works.

**Service accessor.** Added `LLM()` method on `service.Fat` in `/service/fat.go` so the HTTP router can reach the LLM client directly (same pattern as the existing `DB()` and `Queue()` accessors).

**Wiring.** `/cmd/app/main.go` now passes `GoogleKey: env.GetStringOrDefault("GOOGLE_API_KEY", "")` into `llm.NewClient`. `/http/routes.go` now passes `svc.LLM()` into `Search(...)` as the image generator.

**Env example.** Added `GOOGLE_API_KEY=AIza...` to `/.env.example` in the correct alphabetical slot (between `DATABASE_PATH` and `JOB_QUEUE_TIMEOUT`).

**Tests.** Beyond the new sqlite tests, added `/http/image_test.go` with a table-driven test of `imagePathToPrompt` covering 13 cases: basic kebab, uppercase, percent-encoded spaces, percent-encoded uppercase, collapsing dashes, leading/trailing dashes, slashes, mixed separators, empty, all-separators, internal whitespace collapse, invalid percent-encoding, non-UTF-8 percent-encoded bytes.

### Why

I followed the spec's shape very literally -- it was precise enough that most decisions were "follow the ads-builder pattern". The one place I added structure on my own was the `imageGenerator` interface inside `/http/image.go`. I could have passed `*llm.Client` directly, but the handler only needs one method (`Image`), and defining the interface locally lets the http tests stay light if we want to mock it later. Same pattern as the existing `imageService` narrow interface for the sqlite side.

The `LLM()` accessor on `service.Fat` is the minimum change needed to reach the new method -- nothing else routes LLM calls through HTTP today (the chat completers all run in jobs).

### What worked

End-to-end verification went cleanly:

- `go build ./...`: clean after first pass.
- `golangci-lint run`: `0 issues.` on first pass.
- `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./...`: all green, including the new sqlite round-trip and the 13-case path-to-prompt test.
- `make watch` started; `app.log` showed all six jobs registered and the new migration listed alongside `1776949571-images.down.sql` / `1776949571-images.up.sql`.
- `playwright-cli open 'http://localhost:8090/?q=cats+wearing+sunglasses'` populated all 10 results and 3 ads in ~30 seconds.
- Clicked the first result ("Operation Whisker Vision: The CIA's Classified Cat Surveillance Initiative"). The `/site/...` handler blocked for ~24 seconds while the website job ran, then rendered the fabricated page.
- `page.evaluate` on the rendered page returned one `<img>`: `/image/tabby-cat-wearing-reflective-sunglasses-secret-agent-pose`.
- `curl -sSD -` against that URL returned `HTTP/1.1 200 OK`, `Content-Type: image/png`, `Cache-Control: public, max-age=31536000, immutable`, body 1,426,731 bytes, recognised by `file` as "PNG image data, 1024 x 1024, 8-bit/color RGB, non-interlaced". The browser had already triggered the generation, so the curl was a cache hit; the second curl was byte-identical and came back in 13ms.
- `sqlite3 app.db "select path_hash, path, mime_type, length(data), created from images"` shows one row with path_hash `db117142970b7a23...`, path `tabby cat wearing reflective sunglasses secret agent pose`, mime `image/png`, 1,426,731 bytes.
- Edge cases: `/image/` → 400; `/image/---` → 400 (all separators); `/image/%FF%FE` → 400 (non-UTF-8); 2000-byte path → 414. All match spec.

The first fabricated page I hit included exactly one `<img>` on the first try -- no need for the "try a different card" fallback.

### What didn't work

First pass of `go mod tidy` upgraded `maragu.dev/gai` from `v0.0.0-20260417120024-f687d62fdde0` to `v0.0.0-20260423103759-9cce91048412` when I ran `go get google.golang.org/genai@v1.54.0 maragu.dev/gai/clients/google`. The spec was explicit that `gai` must stay pinned. I pinned it back with `go get maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0`, then ran `go mod tidy` which kept the pin and added the `google.golang.org/genai v1.52.1` transitive dependency that `gai@f687d62fdde0` pulls in. I did not re-request `v1.54.0` since v1.52.1 already has the symbols I need (`genai.Text`, `Models.GenerateContent`, `InlineData`, `Blob`).

The raw `genai.Client` does not panic on `nil` `config`: passing `nil` for the fourth arg to `GenerateContent` works and is what the nanobanana reference implementation does. No config needed for flash-image.

### What I learned

`genai.Text(prompt)` returns `[]*genai.Content` -- it's a convenience constructor for a single user-text message. That's the tidiest way to call `GenerateContent` without assembling the `Content`/`Part` structs by hand. I confirmed this by reading `/Users/maragubot/Developer/go/pkg/mod/google.golang.org/genai@v1.54.0/types.go` and the nanobanana reference implementation.

The response shape is `resp.Candidates[0].Content.Parts[]`, and text + inline data are siblings in that slice (the model can and does emit both a prose caption and a binary part). Walk the slice and keep the first part with a non-nil `InlineData` and non-empty `InlineData.Data`. Defaulting the mime to `image/png` is safe -- the server returns `MIMEType: "image/png"` in the blob anyway, but sniffing the bytes is more robust since the spec said to do so.

`chi`'s wildcard syntax is `/image/*`, which exposes the rest of the path via `r.URL.Path` (i.e. `strings.TrimPrefix(r.URL.Path, "/image/")`). There's no need for `chi.URLParam(r, "*")` -- the `URL.Path` contains the original, unescaped, multi-segment string.

`curl -I` against the endpoint returns `405 Method Not Allowed` because chi registers GET-only routes. Using `curl -sSD -` (GET with headers dumped) is the right way to verify response headers.

### What was tricky

The one real judgement call was the `imagePathToPrompt` contract. The spec said "URL-decode", "replace `-` and `/` with space", "trim", "collapse whitespace", "lowercase". I implemented that as: URL-decode → reject invalid UTF-8 → replace `-` and `/` with space → lowercase → collapse whitespace → trim right → reject empty. Rejecting empty is implicit ("if normalised is empty, respond 400"). I chose to reject invalid percent-encoding (e.g. `%ZZ`) as 400 too, since `url.PathUnescape` errors on it, which is a reasonable read of "non-UTF-8" → 400.

The `gai` Google client panics inside `NewClient` if the API key is invalid *and* it tries to talk to the API at construction time -- but it actually just stores the key and lazy-creates the client. I didn't hit this, but it's worth flagging: if `GOOGLE_API_KEY` is the empty string, the `NewClient` call still succeeds, and it's the first `Image` call that fails. Since the `/image/...` handler responds 502 on generation failures, this degrades gracefully: no key → every cache miss is a 502, which a visual QA pass catches immediately.

### What warrants review

- `/sqlite/migrations/1776949571-images.up.sql` and `.down.sql`. STRICT table, primary key is `path_hash` (not a generated `i_`+hex id), `data` is blob. Triggers and defaults match the existing pattern. Down migration drops the trigger first, then the table.
- `/http/image.go`: the full handler. Specifically the OTel attribute set (`image.path_len`, `image.path_hash`, `image.cached`, `image.bytes`, `image.mime` -- all five present on both the hit and miss branches). The fallthrough semantics when `InsertImage` fails (log-and-continue so the user still gets bytes).
- `/http/image_test.go`: 13 table-driven cases for `imagePathToPrompt`. Particularly the edge cases: empty, all-separators, invalid percent-encoding, non-UTF-8 percent-encoded bytes.
- `/llm/llm.go`: the `Image` method plus `sniffImageMime`. Also the changes to `websiteSystemPrompt` / `adWebsiteSystemPrompt`. The prompts allow `/image/...` but forbid external URLs; confirm the wording will actually discourage the model from emitting external `<img>` tags.
- `/service/fat.go`: new `LLM()` accessor. Minimal change; worth a sanity-check that exposing the LLM client to the HTTP layer doesn't break any encapsulation assumptions elsewhere.
- `/cmd/app/main.go`: one-line addition of `GoogleKey: env.GetStringOrDefault("GOOGLE_API_KEY", "")`.
- `/.env.example`: one-line addition. Check alphabetical ordering was preserved.
- The CSP is unchanged because the existing `img-src 'self' https://cdn.usefathom.com` already permits same-origin `/image/...`.

### Future work

- No singleflight / per-prompt mutex. The spec explicitly called this out of scope; concurrent duplicate requests both generate, both pay the API cost, one wins the insert. If API cost is a concern we could add a local in-memory mutex keyed on `path_hash`, but it's a small delta against a low-traffic demo.
- The 60-second timeout is shared between the handler and the `llm.Client.Image` method (each sets its own with `context.WithTimeout`). If the handler's context is already near-expired, the inner `WithTimeout` will shorten naturally -- but if Nano Banana routinely takes 40-50s this could be flaky. Could promote the timeout to a constant somewhere and tune later.
- The image endpoint has no rate limiting, which is explicitly out of scope but would be the next lever if we noticed abuse.
- The prompts still allow inline SVG as an escape hatch. If we prefer every image to go through Nano Banana, we could tighten that. Left as-is for flexibility.
- If a particular website generator insists on emitting external URLs despite the prompt wording, the handler path doesn't punish it -- the browser just sees a broken external `<img>`. A server-side sanitiser in the website-generation job would catch this, but that's outside this task.

### Files touched

- `/sqlite/migrations/1776949571-images.up.sql` (new)
- `/sqlite/migrations/1776949571-images.down.sql` (new)
- `/sqlite/images.go` (new)
- `/sqlite/images_test.go` (new)
- `/model/search.go`
- `/model/error.go`
- `/llm/llm.go`
- `/http/image.go` (new)
- `/http/image_test.go` (new)
- `/http/search.go`
- `/http/routes.go`
- `/service/fat.go`
- `/cmd/app/main.go`
- `/.env.example`
- `/go.mod`, `/go.sum` (transitively pulled `google.golang.org/genai v1.52.1`)
- `/docs/diary/2026-04-23-nanobanana-builder.md` (this file)

Lint: `0 issues.` Tests: all packages pass (`ok app/http`, `ok app/model`, `ok app/sqlite`, `ok app/sqlitetest`). E2E: results page populated, destination page rendered, one `<img src="/image/tabby-cat-wearing-reflective-sunglasses-secret-agent-pose">` generated, 1.4MB PNG served, second fetch cache-hit in 13ms with identical bytes.
