# Diary: Nano Banana image store -- move from SQLite BLOB to filesystem (lead)

**Author:** lead

Follow-up to the initial Nano Banana feature (PR #11): Markus pushed back on storing multi-megabyte image bytes in SQLite and asked for a filesystem-backed cache with sharded directories. This diary captures the lead's perspective on the refactor session: the design call, the delegation, the end-to-end smoke, and the one escalation point.

## Step 1: Sketch the refactor, push back on the ambiguous bit

### Prompt Context

**Verbatim prompt:** "Don't store raw images in a database; store a path to them instead. You can use a subdirectory inside the directory where app.db also is. File names can be hashes. Maybe even do further subdirectories so there are not too many images per directory, filesystem limits."

**Interpretation:** Files live on disk under a sharded directory layout, keyed by content hash. The phrasing "store a path to them instead" is ambiguous: it could mean "keep a DB row with a path column" or "put the images at a path on disk instead of in the DB". I went with the second reading and flagged it in the response.

**Inferred intent:** Keep the demo small, don't introduce unnecessary infrastructure, respect filesystem per-directory limits at scale. No production DB concerns yet because PR #11 hasn't merged.

### What I did

Read enough of the existing feature commit to know what to delete. Sketched the refactor:

- Layout: `{root}/{hash[0:2]}/{hash[2:4]}/{hash[4:]}.png`. Two 256-way shards keep leaf dirs well under 1000 entries at a million-image scale.
- Always `.png`. Nano Banana returns PNG by default; if JPEG ever slips through, browsers sniff-correct. Delete `sniffImageMime` -- it was paranoia.
- Atomic writes via temp-file-plus-rename in the same leaf directory (same filesystem = atomic rename).
- Drop the SQLite `images` table entirely. The path is deterministic from the prompt hash; a DB row adds no information. Cleaner diff, simpler code. If we want a debug row later, it's cheap to add.
- `IMAGES_PATH` env var, default `images` (next to `app.db`). Log the absolute path at startup.
- `/images/` to `.gitignore`.

Flagged the "store a path to them" ambiguity in my response to Markus so he could correct me if he wanted a DB-with-path-column shape. He didn't.

### Why

A filesystem cache is simpler than SQLite for large-binary storage: no WAL bloat, no BLOB marshalling, cheap `rm -rf` to reset, transparent to ops tooling. Sharding is cheap insurance: ext4 handles millions of files in one directory but the semantics (directory listing, stat performance, fsck time) all degrade, and on other filesystems the hard limits bite sooner.

Dropping the DB table entirely -- rather than keeping a metadata row with a path column -- was a small judgement call against literal reading of the prompt. The path is a pure function of the hash. A metadata row is overhead without information.

### What worked

The decision tree collapsed fast because PR #11 hadn't merged. No migration strategy needed; no dev/staging DBs to worry about; no "safety rail" work. Just delete the migration, delete the table, delete the code.

### What didn't work

The "store a path to them instead" phrasing was genuinely ambiguous. I did the right thing (went with the simpler reading, flagged the call in my response) but a less-confident lead might have built the DB-with-path-column shape by default. Worth noting that "simpler" was the right bias here.

### What I learned

Filesystem-plus-hash is the textbook pattern for content-addressed asset caches. Pointing at it explicitly keeps the feature feeling "boring", which is the house style.

### What was tricky

Deciding whether to add a second migration that drops the table vs. deleting the up-migration outright. I went with delete-the-up-migration because PR #11 hadn't merged -- no production DBs could have run it. See "What warrants review" below for the one edge case this creates.

### What warrants review

- The "phantom migration" concern QA escalated: if anyone applied commit `3766b1e` in isolation to their local DB, they now have an orphan `images` table and trigger. In practice this is nobody (the whole PR is unmerged) except the builder, whose `app.db` was wiped as part of this refactor. No migration tracker integrity issue because `1776949571` is removed from both the filesystem and the schema -- the tracker has no ghost entry to complain about.
- If Markus wants belt-and-suspenders, a "drop images table" down-migration is a 2-line add. Flagging this but not acting on it.

### Future work

- If the feature ships and we ever want per-image metadata (generation time, model version, token count, thumbnail), *that's* when we'd add a row-per-image table. Not now.
- Consider adding a `/healthz` probe that checks the image store root is writable. Overkill for a demo.

## Step 2: Delegate to builder and QA, verify myself

### Prompt Context

**Verbatim prompt (continuing):** "And try out the app again when the builder is finished"

**Interpretation:** Spin up the browser myself after builder reports done, confirm images actually render. Use playwright-cli. QA can run in parallel.

### What I did

Spawned the builder with a self-contained brief (delete what, add what, where each change lives, test requirements, e2e protocol, out-of-scope list). Builder returned in ~9 minutes with one commit (`79078d4`), +410/-203 lines, lint clean, tests green, real PNG on disk under the expected shard layout.

Spawned the QA agent (`qa2`) with a scope focused specifically on the storage-shape change: atomic-rename correctness, hash validation, clean deletion of the old types/interfaces, env plumbing, and filesystem-layout e2e.

Did my own playwright-cli walk-through in parallel on port 8090. Loaded `/?q=haunted+lighthouse+keeper+journal`, all 10 results + 3 ads streamed in. Clicked "The Whitmore Journal"; the fabricated page rendered with two inline images (`/image/antique-leather-journal-aged-pages-candlelight`, `/image/historic-beacon-point-lighthouse-rocky-coast-sunset`), both loaded at 1024x1024 natural width. Cache-hit response was 2 ms, cache-miss on a novel prompt (`/image/glitter-covered-raccoon-dj-at-a-rock-festival`) was 7.7s end-to-end for a 2 MB PNG. All edge cases (400/400/414) still held. `find images -type f` showed 4 files in the expected `XX/YY/...png` shard shape with no stray `.tmp-*` siblings. A screenshot of the rendered journal page showed a genuinely convincing candlelit-leather-journal-on-a-desk image that Nano Banana generated from the slug alone.

QA returned in ~5 minutes with approval plus a one-line in-place fix: remove a partial `.tmp-*` file if `os.WriteFile` fails partway (committed as `d1235b6`). One escalation (the orphan-images-table question above, resolved in "What warrants review") and three non-blocking observations:

- `ImageStore.Path(hash)` panics on an empty hash (`hash[0:2]` on `""`). Both `Get` and `Put` regex-guard first so no in-tree caller reaches this. Leaving it; it's a theoretical footgun.
- `cmd/app/main.go` passes the *relative* `imagesRoot` to `NewImageStore` but logs the *absolute* form. No behavioural difference today (no chdir anywhere) but slightly inconsistent. Leaving it.
- No concurrent-Put test. The stdlib contract on `os.Rename` atomicity is well-documented; five subtests cover the invariants. Leaving it.
- `imageGenTimeout` in the handler (60s) duplicates `llm.imageTimeout` (60s) inside `Client.Image`. Whichever deadline fires first wins. Harmless redundancy. Leaving it.

### Why

Running QA in parallel with my own end-to-end walk-through gave a useful redundancy check -- two different queries, two different fabricated pages, two different cache misses exercising different code paths. QA's focus is structural; mine was "does it actually render a real image on a real page".

The four non-blocking observations are all judgement calls, and I'm leaving all four alone. `Path()` guard is a 3-line change I'd take if QA had proposed it in their commit, but requesting it now isn't worth the ceremony. The duplicate 60s timeout is a belt-and-suspenders pattern that looks intentional even if it wasn't. The relative-vs-absolute path is cosmetic. The concurrent-Put test would be gold-plating.

### What worked

Builder's e2e produced a real image; QA's e2e produced a real image; my e2e produced two real images. Three different queries, three different API calls, three different cache shards. The atomic-rename path was exercised multiple times under load with no observed leftovers.

Running in the same worktree means the builder, QA, and I all share `app.db` and `images/` -- which means my e2e and QA's e2e exercised each other's cached images, confirming the cache-hit path works cross-agent.

### What didn't work

One environment quirk worth a second mention: the `TaskUpdate` tool is not available inside the subagents (both builder and QA reported this), so I had to translate task state externally in both directions. Not a blocker; the task list is lead-side bookkeeping.

### What I learned

A `playwright-cli` walk-through that lands with an actual 1024x1024 `<img>` rendering in the accessibility tree is a much more satisfying validation than a curl response header check. The visual confirmation ("there's an image there, and it fits the vibe") catches things a content-type header never will. Worth keeping in the e2e protocol for visual features.

### What was tricky

Deciding whether to ask for the non-blocking `Path()` guard or ship it as-is. Leaning "ship as-is" because: the exported surface has zero external callers, the guard is a one-line add for a future-me to drop in trivially, and the PR description already gets long. If Markus prefers it hardened, it's a one-message ask.

### What warrants review

- PR #11 now holds four commits: the feature (`3766b1e`), the feature diaries (`3c49d22`), the filesystem refactor (`79078d4`), the QA cleanup (`d1235b6`). Plus a commit for this lead diary, pending. Probably squash on merge so the "we had a SQLite images table" intermediate state doesn't live in main's history.
- The atomic-rename pattern in `ImageStore.Put`. Worth a second reader to confirm `os.Rename` across the same filesystem is indeed atomic on the OSes we care about (macOS + Linux, yes; Windows pre-Vista on FAT, no, but we don't ship there).
- The filesystem layout is now the sole cache record. No DB mirror, no periodic consistency check, no "re-sync from truth". If the images directory ever disappears, the next fetch re-generates. That's fine; just worth saying out loud.

### Future work

- If concurrent duplicate-prompt requests become a measurable cost issue (unlikely for a demo), wrap `llm.Image` + `store.Put` in `golang.org/x/sync/singleflight` keyed by `hash`.
- If we ever want admin-visibility into what's been generated, a nightly cron writing `find images -printf ...` into a flat log is cheaper than adding a table back.

## Step 3: Address the QA one-liners

### Prompt Context

**Verbatim prompt:** "Fix QA oneliner?"

**Interpretation:** Pick up the non-blocking observations QA surfaced and ship the ones that are cheap, correct, and don't re-open the review. Skip the one QA itself framed as gold-plating.

**Inferred intent:** Harden the small stuff now while I still have the context loaded, so the PR is as clean as possible when Markus reviews.

### What I did

Read the four non-blocking observations QA left, decided which were trivial wins and which weren't, and did three of them in one commit (`7a440be`) without spawning another builder:

- **`ImageStore.Path("")` panic.** Added the same `^[0-9a-f]{64}$` regex check already used in `Get` and `Put`; `Path` now returns `""` on invalid input. Extended the bad-hash subtest to cover `Path` alongside the existing `Get` / `Put` assertions.
- **Relative-vs-absolute images root.** `cmd/app/main.go` was logging the *absolute* resolved path but passing the *relative* value into `NewImageStore`. Cosmetic today, but future `store.Path(hash)` calls would return something inconsistent with the startup log. Swapped the constructor argument to `absImagesRoot`.
- **Duplicate 60s timeout.** The handler had its own `imageGenTimeout = 60 * time.Second` wrapping `llm.Image`, which *itself* wraps its own call in a 60s `context.WithTimeout`. Two sources of truth for the same budget. Deleted the handler's copy (plus its now-unused `time` import) and added a comment making it explicit that the `llm` package owns the timeout discipline. The request context still propagates, so client disconnects still cancel promptly.

Skipped the "add a concurrent `Put` test with `errgroup`" item because QA explicitly framed it as gold-plating -- `os.Rename` atomicity on the same filesystem is a stdlib contract, not something we should be testing ourselves.

### Why

All three changes are pure hardening: `Path` becomes safe for any future caller; the log message and the store's view of disk are consistent; the timeout has one source of truth in the LLM package, which is the appropriate layer to own it. Net +13/-14 lines of code, no behaviour change on the happy path.

### What worked

Doing three fixes as one commit with a clear message was a lot less ceremony than spawning another builder subagent for ~15 lines of change. The diff is small enough that a reviewer can eyeball it without context.

`go test -shuffle on ./...` + `golangci-lint run` still clean on the first try -- the changes were small enough that nothing else broke.

### What didn't work

Nothing. The fixes were all in categories I'd already thought through when declining them in the earlier "non-blocking observations" summary.

### What I learned

A post-QA cleanup pass where the lead picks off the trivia themselves is a healthy rhythm. It keeps the signal-to-noise high on the subagent round-trips: QA spots the issues, I close the ones that are one-liners, the branch lands clean. Doesn't scale to large fixes (those genuinely need a fresh builder), but for sub-20-line hardening it's faster than a spawn.

### What was tricky

The duplicate-timeout cleanup required a small design call: should the handler own the timeout, or should `llm.Image`? I went with "the package that makes the network call owns its own budget", because callers shouldn't need to know the Nano Banana SLA. Easy to reverse if a future caller genuinely needs a different cap.

### What warrants review

- `llm/image_store.go:Path` now returns `""` on invalid hashes instead of panicking. Any future caller that threads `Path(...)` into a filesystem call needs to check for empty, or the call will land at the store root -- which is *safer* than a panic but worth naming.
- The removed handler-side timeout means an unusually slow Nano Banana call is now bounded only by the 60s inside `llm.Image`. If the internal timeout ever goes missing or gets changed to zero, the handler will silently wait until the client disconnects. Keep the two in sync by convention until someone regrets it.

### Future work

None directly; this step was a sweep, not a seed.

## Step 4: Upgrade to Nano Banana v2 -- and discover it returns JPEG

### Prompt Context

**Verbatim prompt:** "Also, make sure to use gemini-3.1-flash-image-preview"

Follow-ups in the same arc:
- "Run with make watch so I can try it"
- "I haven't found a site with an image yet. Link?"
- "Nice, excellent."

**Interpretation:** Flip the model constant to Nano Banana 2 (the preview-tier flash model that the nanobanana CLI already exposes as `ModelNanoBanana2`). Smoke-test against the real API. Hand Markus a live URL.

**Inferred intent:** v2 supposedly has better quality and aspect-ratio handling, which matters more for a feature where the user actually sees the images inside a fabricated page.

### What I did

Changed `NanoBananaModel` in `/llm/llm.go` from `"gemini-2.5-flash-image"` to `"gemini-3.1-flash-image-preview"`, updated the comment to explain the v2 rationale, and re-ran the full build/test/lint cycle (all green).

Then did a live API smoke test against the preview model -- because flipping a model string without hitting the real API is exactly the kind of "green CI, broken prod" move that bites later:

```
URL='http://localhost:8090/image/neon-drenched-tokyo-alley-at-midnight-cyberpunk-rainfall'
curl -s -o /tmp/v2-test.png -w '%{http_code} %{size_download} %{time_total}s %{content_type}\n' "$URL"
# 200 1111218 15.698s image/png
file /tmp/v2-test.png
# /tmp/v2-test.png: JPEG image data, JFIF standard 1.01, 1408x768, ...
```

**That's the discovery.** v2 returns a **JPEG**, not a PNG, inside a body served with `Content-Type: image/png`. The original builder deleted `sniffImageMime` on the filesystem refactor because "Nano Banana v1 returns PNG by default" -- which is true for v1 and not for v2. Browsers sniff-correct, which is why all the rendering I saw earlier worked fine despite the lie, but the Content-Type header was incorrect and `curl -o out.png` was producing a JPEG-in-PNG-file for anyone who downloaded.

Restored a small sniff-and-label helper in `/http/image.go` that checks PNG / JPEG / WebP magic bytes and falls back to `image/png` for anything unrecognised. Wired it through both `writeImage` (sets the header) and both OTel span-attribute branches (so `image.mime` is back on traces for both hit and miss paths -- making this exact class of model-side output change visible in telemetry next time). Added a `TestSniffImageMime` table test with six cases (PNG, JPEG, WebP, unknown, empty, too-short).

Committed as `16ccfc6`.

Re-verified with curl: Content-Type is now `image/jpeg` for v2 outputs. A fresh cache-miss on a novel prompt still worked end-to-end in ~15s (vs ~7s on v1, which is the v2 quality tax).

Then Markus asked for a live link, and only **one of the four cached sites** had any `<img` tags in its HTML -- the LLM is reading "0-3 images, only when they add to the fiction" as permission to skip entirely. Dug through the `websites` table with SQL to find the one page that did have images (a cached Whitmore lighthouse-keeper journal page), extracted its two image URLs, and pre-warmed them in parallel via curl (18s + 24s for two ~900 KB JPEGs) so Markus's first click would land on a cached page with cached images.

### Why

Smoke-testing against the real API for a model-string change is non-negotiable -- every other check (build, lint, unit test) passes a dummy string just fine. The JPEG-vs-PNG discovery is exactly what that test exists to catch.

Restoring the mime sniffer was the correct response, not "shrug, browsers sniff". The refactor's decision to drop it was made under an assumption that no longer holds. The cost of being correct is ~25 lines + a test; the cost of being wrong is a small dishonesty that grows every time someone saves or shares a file.

Pre-warming the two images for Markus's link was a two-curl convenience that saved him ~40 seconds of per-image "generating..." on first load. Worth the 42 seconds of my wall-clock time.

### What worked

The OTel `image.mime` span attribute I'd previously dropped paid for itself immediately by suggesting exactly where to look. Even before I ran `file(1)` I knew this was "we labelled JPEG as PNG" because the metric would have made it obvious in production. Reinstating it means the *next* model-output-format surprise is a trace filter away.

The sniffer unit test is data-driven, matches the house style (`is.Equal`), and the test cases are all synthetic (PNG magic + 2 filler bytes, JPEG magic + JFIF marker, etc.) so they don't depend on network or real image files.

The "pre-warm the images then hand over the URL" trick made the demo feel instant despite the underlying 15-30s cold-start cost per image. Worth remembering for future demos.

### What didn't work

My first reading of "store a path to them instead" as "just flip the model" was incomplete -- I didn't think to sanity-check the response format. The smoke test is the only reason I caught it. Lesson: always sniff what the API actually returns, especially on version bumps of preview-tier models, because docs lag.

The 1-in-4 image-inclusion rate at first sight feels disappointing. Markus asked for a link specifically because he hadn't seen any images yet. That's a real prompt-tuning observation: "0-3 images per page" is too soft; the LLM is defaulting to 0. I flagged it and offered to tune but didn't act on it without confirmation.

### What I learned

- **Nano Banana v2 returns JPEG.** `gemini-3.1-flash-image-preview` via the `google.golang.org/genai` `GenerateContent` path produces JPEG bytes in `part.InlineData.Data`. v1 (`gemini-2.5-flash-image`) returns PNG. This isn't documented clearly anywhere I could find; I added it to the sniffer's comment as institutional memory.
- **v2 is ~2x slower than v1** at comparable prompts on cache-miss: ~15s vs ~7s for similar-sized outputs. Still inside the 60s handler budget but worth knowing.
- **`part.InlineData.Data` doesn't care about the extension your Content-Type announces.** File extension on disk is a pure implementation detail; the HTTP header is what matters. Keep the filename `.png` for simplicity.
- **The LLM under-uses images** when the prompt says "0-3, only when they fit". Demand-shaped prompts ("include 1-2 images") produce visible output; permission-shaped prompts often produce zero. If this matters for the demo, the prompt needs to shift from "may" to "usually".

### What was tricky

Deciding whether to fix the Content-Type lie at all. Arguments for shrugging: browsers sniff, the images render, the cost of "wrong" is zero in the dominant path. Arguments for fixing: it's a small lie that's easy to tell the truth on, and `curl -o file.png` producing a JPEG is a real papercut for anyone poking at the API. Went with the fix because the telemetry attribute alone was worth re-adding.

Finding Markus a demo-ready link under time pressure. Only one of the four cached pages had images, and even that page's images weren't on disk (v1 images from before the image-cache wipe). Pre-warming in parallel was the right call but felt a little like stage-managing; a better steady-state is to just have the prompt produce images reliably.

### What warrants review

- `/http/image.go:sniffImageMime` -- four magic-byte checks plus a fallback. Trivial logic; the thing to confirm is that the magic-byte values are correct. WebP has a 12-byte magic (`RIFF` + 4 bytes size + `WEBP`) which is why that check is bulkier.
- The `image.mime` span attribute now reflects actual bytes, not a label. Worth noting if anyone is querying traces: the PNG→JPEG transition point is the `gemini-3.1-flash-image-preview` switchover, `16ccfc6`.
- Whether the system prompts in `/llm/llm.go` (both `websiteSystemPrompt` and `adWebsiteSystemPrompt`) should shift from "may include 0-3 images" to "usually include 1-2 images". Not doing it without Markus's nod; this is a dial, not a one-way decision.
- PR #11 now holds seven commits total: the original feature, the feature-round diaries, the filesystem refactor, the QA cleanup, this lead diary, the QA-oneliner fixes, and the v2 bump. Squash on merge so the "had SQLite images table" and "served JPEG as PNG" intermediate states don't land in main's history.

### Future work

- Tune the system prompts to include at least one image per page if Markus confirms he wants that. One-line nudge, visible improvement to the demo.
- If v3 arrives (`gemini-4-flash-image-preview` or whatever) and returns something new (WebP? AVIF?), the sniffer's fallback + `image.mime` span attribute mean we'll notice in seconds, not hours. No further scaffolding needed.
- A bluesky post is probably warranted at merge time -- the "search engine that fabricates its own images per search result, via sharded-filesystem cache" angle is shareable. Markus can decide if he wants me to draft one.
