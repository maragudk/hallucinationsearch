# Diary: Nano Banana filesystem-backed image store (builder)

**Author:** builder (nanobanana-fs task)

Refactor on top of the merged Nano Banana feature. Markus pushed back on storing the raw image bytes in SQLite, so this change moves the cache to a sharded directory tree on the filesystem. The cache key stays the same (sha256 hex of the normalised prompt); the persistence mechanism changes from a BLOB column to `{root}/{hash[0:2]}/{hash[2:4]}/{hash[4:]}.png` with an atomic temp-file rename.

## Step 1: Refactor from SQLite BLOB to sharded filesystem cache

### Prompt Context

**Verbatim prompt:** "You're refactoring the on-disk storage for the on-demand image feature in 'Hallucination Search'. Markus pushed back on storing raw image bytes in SQLite: he wants them on the filesystem instead, in a sharded directory layout so filesystem per-directory limits don't bite as the cache grows. [...] Drop the `images` SQLite table entirely. [...] Add a filesystem-backed image store. [...] Path layout: `{root}/{hash[0:2]}/{hash[2:4]}/{hash[4:]}.png`. [...] `Put` is **atomic**: write to `{finalPath}.tmp-{random}` in the same directory [...] then `os.Rename` to the final path. [...] `Image` method signature becomes `func (c *Client) Image(ctx context.Context, prompt string) ([]byte, error)` -- drop the mime return. Delete `sniffImageMime` and its tests. [...] Wire `IMAGES_PATH` env var."

**Interpretation:** A very mechanical refactor. The cache key, the HTTP contract, and the generation path all stay the same. Everything that touched `model.Image` or the `images` table needs to go, replaced by a narrow interface backed by `os.ReadFile` / atomic `os.Rename`. The handler stays thin glue; the only structural change is swapping the `imageService` interface for an `imageStore` interface.

**Inferred intent:** SQLite BLOB storage for multi-MB PNGs is the wrong tool -- it bloats the WAL, slows backups, and fights the page cache. Flat filesystem layout would hit per-directory inode limits (ext4: ~10k comfortable, more technically possible but slow on lookup), so two 256-way shards (64k buckets) keeps any leaf directory well under a few hundred entries until the cache is in the tens of millions. Atomic rename is standard "write + publish" for a cache where duplicate writers are fine.

### What I did

**Red-green TDD on the new store.** Wrote `/llm/image_store_test.go` first (five table-driven subtests: missing-file Get, round-trip, overwrite, on-disk shard layout with no leftover tmp files, bad-hash rejection for both Get and Put), confirmed it failed (no type `llm.ImageStore`), then wrote `/llm/image_store.go` to make it pass.

**New store.** `/llm/image_store.go` exports `NewImageStore(root)`, `Get(hash) (bytes, bool, error)`, `Put(hash, data) error`, and `Path(hash) string`. Hash input is validated against `^[0-9a-f]{64}$` before the store touches the filesystem (belt-and-suspenders: the handler always computes the hash itself, but a malformed hash must never be able to walk out of the root). `Put` writes to `{final}.tmp-{8 bytes hex from crypto/rand}`, `os.MkdirAll` the shard dirs with `0o755`, then `os.Rename` which silently overwrites. Best-effort `os.Remove` on the tmp file if the rename fails so we don't leak partial writes.

**LLM client.** Trimmed `Client.Image(ctx, prompt)` from `([]byte, string, error)` to `([]byte, error)` and deleted the `sniffImageMime` helper. Nano Banana returns PNG; the handler always serves `image/png`; browsers sniff-correct the JPEG that theoretically might slip through. No test file existed for `sniffImageMime`, so nothing to delete there.

**HTTP handler.** `/http/image.go` now depends on `imageStore` (`Get`/`Put`) instead of `imageService` (`GetImage`/`InsertImage`). Hit path serves the bytes with the same `Cache-Control: public, max-age=31536000, immutable`, `Content-Type: image/png`. Miss path calls `gen.Image`, `store.Put`, then serves. Span attributes are `image.path_hash`, `image.path_len`, `image.cached`, `image.bytes` -- dropped `image.mime` since it's always `image/png`. Kept all four edge cases intact (414 over 1024 bytes, 400 on bad/empty prompt, 502 on gen error, 60s gen context).

**Deletions.**
- `/sqlite/images.go`, `/sqlite/images_test.go`
- `/sqlite/migrations/1776949571-images.up.sql`, `.down.sql`
- `model.Image` struct in `/model/search.go`
- `ErrorImageNotFound` in `/model/error.go`
- `GetImage` / `InsertImage` from `searchDB` interface in `/http/search.go`
- `imageService` interface (replaced by `imageStore`) in `/http/image.go`
- `sniffImageMime` helper in `/llm/llm.go`
- Local `app.db`, `app.db-shm`, `app.db-wal` (now stale; regenerated cleanly on first boot).

**Wiring.**
- `/cmd/app/main.go`: reads `IMAGES_PATH` via `env.GetStringOrDefault("IMAGES_PATH", "images")`, `os.MkdirAll(root, 0o755)`, logs the resolved absolute path (`log.Info("Initialised image store", "path", abs)`), constructs `llm.NewImageStore(root)`, passes it into `service.NewFat` as a new `ImageStore` field.
- `/service/fat.go`: added `imageStore *llm.ImageStore` field, `ImageStore` field on `NewFatOptions`, and `ImageStore()` accessor matching the existing `DB()` / `LLM()` / `Queue()` style.
- `/http/routes.go`: `InjectHTTPRouter` passes `svc.ImageStore()` into `Search(...)`.
- `/http/search.go`: `Search(...)` takes the new `store imageStore` alongside the existing `gen imageGenerator`, threaded into `handleImage(log, store, gen)`.

**Env + git ignore.**
- `/.env.example`: added `IMAGES_PATH=images` between `GOOGLE_API_KEY` and `JOB_QUEUE_TIMEOUT` (alphabetical).
- `/.gitignore`: added `/images/` so the generated cache doesn't get committed.

### Why

The store lives in `/llm` rather than `/sqlite` because it's no longer SQLite-backed -- dropping it in `/sqlite` would misadvertise what it does, and it's conceptually tightly coupled to the `llm.Client.Image` output (both deal with Nano Banana-generated PNG bytes). The alternative of a new `/cache` or `/imagecache` package felt like over-structuring for a single 80-line type.

The `ImageStore()` accessor on `service.Fat` mirrors the existing `DB()` / `LLM()` / `Queue()` pattern -- it's boring, matches the style, and keeps `InjectHTTPRouter` symmetric. Resisting the temptation to "just pass it as a function argument" kept the wiring consistent; if the handler ever needs more state, the `Fat` accessor pattern scales.

The hash validation lives in the store rather than the handler because it's a filesystem-safety invariant, not a handler-concern: any caller of `Get`/`Put` that could produce a malformed hash would be a bug, and the store should refuse to walk the FS on bug input. The cost is a regex match per call, which is irrelevant next to an `os.ReadFile`.

### What worked

- `go build ./...` clean after the first pass.
- `golangci-lint run`: `0 issues.`
- `go test -shuffle on ./...`: all green (`app/http`, `app/llm`, `app/model`, `app/sqlite`, `app/sqlitetest`). The five new subtests in `/llm/image_store_test.go` all pass; the 13 `imagePathToPrompt` cases in `/http/image_test.go` still pass unchanged.
- `go mod tidy`: no-op (the refactor only removes code, so no dep churn).
- `make watch` came up cleanly on port 8090 with the new `Initialised image store` log line at `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/nanobanana-images/images`.
- `playwright-cli` walk: loaded `?q=haunted+antique+lamps`, clicked the first card (which happened to be an ad -- `/ad/haunted-victorian-lamps-rare-estate-finds-a_...`), waited ~32s for the ad-website job, `document.images[0].src` returned `/image/vintage-ornate-victorian-lamp-glowing-ethereal-light`.
- `curl -sS -D-` on that URL: 200 OK, `Content-Type: image/png`, `Cache-Control: public, max-age=31536000, immutable`, 1,339,945 bytes, `file(1)` identified as "PNG image data, 1024 x 1024, 8-bit/color RGB, non-interlaced". Second `curl` of the same URL came back in 16ms with byte-identical output (filesystem cache hit).
- `find images -type f` showed `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/nanobanana-images/images/36/f0/8c3352077ccdb28a96339206f10ea3b52a86e6571397adbc640e927fb5e9.png` -- exactly the expected `root/ab/cd/ef01...png` shape.
- Edge cases: `/image/` → 400, `/image/---` → 400, 2000-byte path → 414. All match spec.

### What didn't work

Nothing meaningful. `curl -sSI` returned 405 Method Not Allowed because chi routes the `/image/*` pattern as GET-only -- same quirk the previous diary called out. Switched to `curl -sS -D- -o /tmp/...` which issues a GET with headers dumped, and everything looked correct.

### What was tricky

Three small judgement calls:

1. **Where does the new type live?** Picked `/llm/image_store.go` because it's coupled to Nano Banana output (PNG bytes keyed by a prompt hash), not because it talks to an LLM. An alternative `/cache` package would have been more "generic", but nothing else needs a blob cache, and the type name `llm.ImageStore` reads well at the call sites. If another subsystem ever needed a similar cache, I'd promote it then; premature extraction is worse than a trivial move later.

2. **Does the hash validation belong in the store or the handler?** Both. The handler already computes the sha256, so it can't produce bad input in production. But: the store is the *only* code that touches the filesystem, so it's the right place for the "this name must be safe to path.Join onto `root`" invariant. Cheap belt-and-suspenders.

3. **What happens when two concurrent requests miss the same hash?** Both generate, both `Put`. With atomic `os.Rename`, both writes succeed and whichever renames last wins. Since the content is deterministic-ish (same prompt → same model → byte-different but visually similar image), the caller's response is unaffected either way. The previous `on conflict (path_hash) do nothing` was first-write-wins; the new behaviour is last-write-wins. Nobody observes the difference externally, so I didn't spend any time on locking.

### What I learned

`os.Rename` semantics are exactly what the spec said: on the same filesystem, it's atomic (a single inode-link swap), and it silently overwrites the destination. No `O_EXCL`/`link(2)` dance needed. The temp-file name has to live in the same directory as the destination or we risk cross-device rename; `filepath.Dir(final)` gives us the shard directory either way, so `final + ".tmp-" + random` is safe.

`crypto/rand.Read(buf[:])` is the tidy way to get 8 random bytes with no seed ceremony. `hex.EncodeToString` on a `[8]byte` slice produces 16 hex characters, which is well above the birthday-collision threshold for any realistic number of concurrent writers of the same hash.

`regexp.MustCompile` at package scope (`hashRe = regexp.MustCompile(...)`) is the Go-idiomatic way to keep the validator compiled once, which matches the other `regexp` usage in the codebase (`resultIDRe`, `adIDRe`, `nonSlugRe`).

### What warrants review

- `/llm/image_store.go`: the full type. Particularly the atomic-rename path (tmp file naming, `MkdirAll` permission, best-effort tmp cleanup on rename failure) and the hash regex (`^[0-9a-f]{64}$` matches the sha256 hex the handler produces).
- `/llm/image_store_test.go`: five subtests covering missing-file, round-trip, overwrite, on-disk shard layout including "no `.tmp-` leftover", and bad-hash rejection covering empty, 63-char, 65-char, uppercase, non-hex, path-traversal. `t.TempDir()` throughout so nothing escapes the sandbox.
- `/http/image.go`: the handler rewrite. Span attrs are now `image.path_hash`, `image.path_len`, `image.cached`, `image.bytes` (dropped `image.mime`). Hit/miss branching uses `found` rather than `errors.Is(err, model.ErrorImageNotFound)` since `Get` has three-valued semantics: `(nil, false, nil)` for not-found, `(nil, false, err)` for real I/O, `(data, true, nil)` for hit.
- `/llm/llm.go`: `Image` signature is now `(ctx, prompt) ([]byte, error)`; `sniffImageMime` is gone.
- `/service/fat.go`: added `imageStore *llm.ImageStore`, `ImageStore` field on options, `ImageStore()` accessor. Matches the existing accessor style.
- `/cmd/app/main.go`: `IMAGES_PATH` env read, `os.MkdirAll`, absolute-path log, `service.NewFat` now receives `ImageStore`.
- `/.env.example`: `IMAGES_PATH=images` between `GOOGLE_API_KEY` and `JOB_QUEUE_TIMEOUT`. Alphabetical.
- `/.gitignore`: `/images/` slotted alphabetically before `/*.log`.
- The CSP is unchanged (`img-src 'self'` already permits `/image/...`).

### Future work

- No size cap / eviction. Out of scope per spec; `rm -rf images` remains the emergency lever.
- No `Content-Length` on the hit path; the stdlib HTTP server picks chunked when we don't set it. Could `w.Header().Set("Content-Length", strconv.Itoa(len(data)))` for nicer CDN behaviour, but it's cosmetic.
- Background re-generation: on a cache hit, we never check whether the stored bytes are "stale". The prompt-as-key model means "stale" doesn't really make sense here, but if the underlying model changed and we wanted to refresh, we'd need a sidecar timestamp file or a cache-version prefix on the hash. Not worth building today.
- Temp-file leftovers from a crashed process: would accumulate in shard directories after a kill-9. A tiny janitor that sweeps `*.tmp-*` older than an hour would be nice, but the volume is low enough that manual `find images -name '*.tmp-*' -delete` covers it.

### Files touched

- `/llm/image_store.go` (new)
- `/llm/image_store_test.go` (new)
- `/llm/llm.go` (simplified `Image`, removed `sniffImageMime`)
- `/http/image.go` (new interface, dropped model/sqlite refs, dropped mime attr)
- `/http/search.go` (dropped `GetImage`/`InsertImage` from `searchDB`, `Search` signature now takes `imageStore`)
- `/http/routes.go` (wires `svc.ImageStore()` into `Search`)
- `/service/fat.go` (new `imageStore` field and accessor)
- `/cmd/app/main.go` (new `IMAGES_PATH` env, `os.MkdirAll`, startup log, pass to `NewFat`)
- `/model/search.go` (removed `Image` struct)
- `/model/error.go` (removed `ErrorImageNotFound`)
- `/.env.example` (added `IMAGES_PATH=images`)
- `/.gitignore` (added `/images/`)

Deleted:
- `/sqlite/images.go`
- `/sqlite/images_test.go`
- `/sqlite/migrations/1776949571-images.up.sql`
- `/sqlite/migrations/1776949571-images.down.sql`
- Local `app.db*` artefacts

Lint: `0 issues.` Tests: all packages pass. E2E: results page rendered, ad-website rendered with `<img src="/image/vintage-ornate-victorian-lamp-glowing-ethereal-light">`, 1.3MB PNG served, second fetch cache-hit in 16ms with identical bytes, on-disk path at `images/36/f0/8c33...png` matches the sharded layout.
