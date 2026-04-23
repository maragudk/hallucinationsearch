# Diary: Nano Banana on-demand image generation (lead)

**Author:** lead

Wiring Google's Gemini 2.5 Flash Image ("Nano Banana") into Hallucination Search so the fabricated destination pages can embed inline images that are themselves fabricated. This is the lead's perspective on the session: design sketch, delegation to builder + QA, and the PR.

## Step 1: Read context, sketch the design, skip the brainstorm

### Prompt Context

**Verbatim prompt:** "Check the diary for recent work. I'd like to add the ability for websites to display images as well. For that, we need a new HTTP handler which, given the path only (perhaps a very long one), can generate an image and return it through the HTTP handler. I think we need something like Gemini flash image, also called NanoBanana, for this. Figure out how we could wire that up. Is it possible through maragu.dev/gai? Or do we need the official Google GenAI SDK used underneath GAI (exposed as a field in GAI client). Let's not brainstorm right now. I'm off work in a minute, so sketch something for me and feel free to start implementation through a builder sub-agent. You can run the app with 'make watch' and test it with your Playwright-cli skill. I've added an API key to .env for you to work with. You can also use your NanoBanana skill to generate images right here in the terminal if you need to for some reason. When you're done wiring this up, create a pull request. Again, work independently. There's no harm done in creating a pull request."

**Interpretation:** Markus is handing me an end-to-end autonomous feature and the precise SDK question that was his only uncertainty -- is this possible through `maragu.dev/gai`, or do we need to drop to the underlying `*genai.Client`? He's off work in a minute. No brainstorm; sketch, delegate, PR.

**Inferred intent:** Prove the escape-hatch from gai's `ChatCompleter` abstraction is usable when the abstraction doesn't yet cover a capability. Ship the smallest thing that actually works in a browser. Don't over-scope.

### What I did

Read the three most recent builder diaries (`ads-builder`, `otel-span-attrs-builder`, `robust-retries-builder`) and the search/ads plumbing (`/llm/llm.go`, `/http/search.go`, `/sqlite/search.go`, the two search migrations, `/cmd/app/main.go`, `/service/fat.go`) to understand what "shape" this feature should take if it's a good neighbour.

Answered the SDK question by reading three upstream files directly:
- `/Users/maragubot/Developer/go/pkg/mod/maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0/clients/google/README.md` -- confirms "Multi-modal output [ ]" is unticked. gai's `ChatCompleter` abstraction does not expose image generation.
- `/Users/maragubot/Developer/go/pkg/mod/maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0/clients/google/client.go` -- `type Client struct { Client *genai.Client; ... }`. The raw genai client is a public field, exactly for this case.
- `/Users/maragubot/Developer/go/pkg/mod/maragu.dev/nanobanana@.../internal/nanobanana/client.go` -- reference implementation. Wraps `gai-google`'s NewClient, pulls out `.Client`, calls `Models.GenerateContent(ctx, "gemini-2.5-flash-image", genai.Text(prompt), nil)`, and extracts `part.InlineData.Data`.

So the answer to Markus's question: **gai as the constructor, genai as the call.** Add gai's google client (for auth + API-key plumbing + shared logger), then punch through to the underlying `*genai.Client` for the image call. Don't introduce a fresh genai client independently.

Sketched scope tightly before handing off:
- New `GET /image/{path...}` with chi wildcard. Path is URL-decoded, dashes/slashes to spaces, trimmed, lowercase -- that's both the prompt and (via sha256) the cache key. 1024-byte hard cap (414 if over); 400 on empty/non-UTF-8.
- STRICT `images` table keyed on `path_hash`, with `mime_type` + `data BLOB` + raw path for debugging. `on conflict do nothing` for concurrent duplicate requests.
- **No job.** Inline generation with a 60s context timeout. Rationale: images are small and fast (3-5s via Flash Image), and browsers don't retry failed `<img>` loads gracefully -- a poll-a-job pattern is inappropriate for something a browser fetches as a subresource. DB conflict handling is sufficient for concurrency safety.
- **No singleflight, no per-path mutex.** The duplicate-work cost is one extra API call under burst for the same novel path; the DB absorbs it. Not worth the surface.
- Update the website / ad-website system prompts: allow `<img src="/image/descriptive-slug">` (same-origin only), still ban external URLs and scripts. Encourage 0-3 images per page. The prompt should also nudge toward 10-80-char kebab-case paths so the cache key is stable across regens.
- `gemini-2.5-flash-image` (v1 GA) -- not v2-preview, not pro. This is a demo and flash is the right tier.
- Existing CSP (`img-src 'self'`) already covers same-origin image loads -- no CSP changes.

### Why

The design is a close parallel to `/site` and `/ad`, which makes it a small conceptual load on the reader. The one deliberate deviation -- inline generation instead of poll-a-job -- is forced by browser semantics: an `<img>` tag can't poll. Nano Banana flash is fast enough that inline is realistic.

Skipping the brainstorm was the right call for this size of feature. The only product question ("can images exist inside fabricated websites?") had a yes-or-no answer implicit in the prompt.

### What worked

Reading the three upstream source files took under five minutes and fully answered Markus's SDK question, which meant the builder brief could be categorical instead of exploratory ("do it this way" vs. "figure out a way"). That's the difference between a one-shot builder run and a back-and-forth.

The existing `/site` / `/ad` cache-and-serve-from-SQLite pattern made the `images` schema almost write itself. Following the STRICT / `_updated_timestamp`-trigger / `on conflict do nothing` conventions kept the builder's diff small and reviewable.

### What didn't work

Nothing meaningful in the design step.

### What I learned

gai's Google client is a thin auth/transport wrapper around `*genai.Client`; the `Client` field is intentionally exposed as the escape hatch for any capability not yet covered by the `ChatCompleter` abstraction. This is a clean design -- you get the benefit of the abstraction where it fits, and direct SDK access where it doesn't -- but it's easy to miss if you only read the `ChatCompleter` surface.

Nano Banana v1 is model string `"gemini-2.5-flash-image"`. The nanobanana CLI's internal package has this as `ModelNanoBanana`, with `ModelNanoBanana2` and `ModelNanoBananaPro` alongside. v1 is the default and the right demo pick.

### What was tricky

Deciding between inline generation and poll-a-job. Consistency with `/site` / `/ad` argued for the latter; browser `<img>` semantics and per-response latency argued for the former. Inline wins because the consumer is a browser subresource, not a human-visible page.

### What warrants review

- `llm.Image` SDK wiring -- specifically that it uses the exposed `*genai.Client`, not a fresh one.
- The website/ad-website prompt updates -- they still need to forbid external URLs, inline scripts, and off-origin resources. Encouraging images shouldn't re-open those doors.
- CSP should remain untouched -- if the builder added anything to `http/csp.go`, that's a red flag.

### Future work

If Nano Banana v2-preview or pro becomes worth the cost, upgrading the model is a one-line change to the const in `llm/llm.go`. For now, flash is right.

## Step 2: Delegate to builder + QA, open the PR

### Prompt Context

**Verbatim prompt:** (Markus's original brief -- no follow-ups; he was off work.)

**Interpretation:** Spawn builder with a self-contained brief. Chain QA behind it via a task-list dependency. Open the PR after QA approves. No further interaction expected.

**Inferred intent:** Ship this as a demonstrated, reviewed PR and let Markus look in the morning.

### What I did

Created a worktree at `.claude/worktrees/nanobanana-images` on branch `worktree-nanobanana-images`, allocated port `:8090` in its `.env` (main worktree is running on `:8081`), and confirmed `GOOGLE_API_KEY` was already in the copied `.env`.

Created two tasks in the shared task list: #1 (builder -- implement) and #2 (QA -- review, blocked on #1). Spawned `fabrik:builder` in the background with a self-contained brief: design summary, exact SDK wiring, exact model const, schema sketch, handler contract, prompt-update guidance, tests-to-write list, out-of-scope list, e2e validation protocol, diary requirement, coordination protocol.

Builder came back in ~13 minutes with one commit (`3766b1e`), 17 files touched, +678/-27, lint clean, tests green, and a real end-to-end walk-through that produced a 1.4 MB PNG at `/image/tabby-cat-wearing-reflective-sunglasses-secret-agent-pose` and served a byte-identical 13 ms cache hit on the second fetch. Two flags from the builder:

- Task tool and `tasks.md` file were not present in its subagent environment, so it couldn't drive task state. I updated task #1 externally.
- `go mod tidy` landed `google.golang.org/genai v1.52.1` (transitive via gai), not `v1.54.0`. All required symbols were present. A first `go get` briefly bumped gai itself; the builder reverted that and repinned. `go.mod` shows `maragu.dev/gai v0.0.0-20260417120024-f687d62fdde0` unchanged.

Spawned `fabrik:qa` with a review brief that spelled out exactly which invariants to re-verify (SDK wiring, mime sniff, schema, handler semantics, prompt surgery, CSP untouched, lint, tests, e2e, edge cases). QA came back in ~9 minutes with an approve and zero fixes. Three non-blocking observations worth noting but not worth acting on:

- `imagePathToPrompt` only treats `-` and `/` as separators, so a hostile path of pure punctuation like `/image/---!!!---` normalises to `!!!` and returns 502 from the model instead of 400 from the handler. Functionally equivalent for a caller; unreachable from builder-emitted HTML.
- API-returned `Blob.MIMEType` is ignored in favour of sniffing. Spec-compliant; would mildly misbehave if Nano Banana ever returns WebP (we'd label it `image/png`), but browser sniff-correction handles that.
- No handler-level unit test; the handler itself is covered only by the e2e walkthrough. Acceptable for thin glue.

### Why

Using the task list as the hand-off mechanism means the build / review steps are legible and serialisable. Running both subagents in the background kept the session interactive for Markus if he wanted to check in.

### What worked

One builder, one QA, sequential. Builder's e2e was real (actual model call, actual PNG, actual cache hit); QA's e2e was a second-opinion verification on a different query. The diff is reviewable at a glance (17 files, mostly new, touching 5 existing files with ~30 LoC of changes).

The SDK escape hatch worked exactly as sketched. gai's google client gave the builder the API-key plumbing for free; the raw `*genai.Client` gave the direct `Models.GenerateContent` call. No new top-level dependency.

### What didn't work

Nothing blocking. The subagent task-tool absence is an environment quirk -- the task list is part of the lead's environment, not the subagents' -- so the lead is responsible for transitioning task state regardless of whether the subagents would otherwise have done it. Unsurprising once you notice.

### What I learned

Subagents that don't have the task tool can still report task-related work through their summary; the lead just has to remember to translate it. Shouldn't reach for task state as a synchronisation primitive across agents; use it as lead-visible bookkeeping.

### What was tricky

Nothing, given the design decisions were made upfront. The biggest judgement call was picking inline-vs-job, and that was made before the builder was spawned.

### What warrants review

- The PR itself -- single commit, clear diff, diary entries from all three of us for the narrative.
- The builder's choice to sniff mime rather than trust the API response is a minor deviation from what a literal reading of the SDK would do. It's defensible; worth a reviewer's nod.
- The `imagePathToPrompt` edge cases (13-case table test in `/http/image_test.go`) cover the surface area; QA suggested one could add a "pure punctuation" case for the 502-vs-400 smell, but intentionally left it.

### Future work

- If the demo's aesthetic leans into "images everywhere", bump the prompt to "usually 1-2 images per page" and track whether image-fetch rate changes.
- If Nano Banana safety filter refuses on edgy prompts frequently, consider wrapping `llm.Image` in a single retry with a neutered prompt fallback. Not needed yet.
- If this feature goes beyond demo usage, add handler-level unit tests and a singleflight wrapper to dedupe concurrent generation for the same path.
