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
