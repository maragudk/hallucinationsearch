# Diary: Nano Banana filesystem image store QA review

**Author:** qa (nanobanana-fs task #4)

Reviewed commit `79078d4 Move image cache from SQLite BLOB to sharded filesystem` on branch `worktree-nanobanana-images`. Prior QA review already approved the feature end-to-end; this pass is scoped to the storage-shape change (SQLite BLOB -> sharded filesystem).

## What I reviewed

All builder-touched files:

- `/llm/image_store.go` (new) -- the new `ImageStore` type.
- `/llm/image_store_test.go` (new) -- five table-driven subtests.
- `/llm/llm.go` -- simplified `Image(ctx, prompt) ([]byte, error)`; `sniffImageMime` deleted.
- `/http/image.go` -- handler switched from SQLite `imageService` to filesystem `imageStore`; `image.mime` span attr dropped.
- `/http/routes.go`, `/http/search.go` -- wiring; `searchDB` no longer has `GetImage` / `InsertImage`.
- `/service/fat.go` -- added `imageStore` field + `ImageStore()` accessor.
- `/cmd/app/main.go` -- `IMAGES_PATH` env, `os.MkdirAll`, absolute-path log, `service.NewFat` plumbing.
- `/model/search.go`, `/model/error.go` -- `Image` struct and `ErrorImageNotFound` removed.
- `/.env.example`, `/.gitignore` -- new `IMAGES_PATH=images` entry and rooted `/images/` ignore.
- Deleted: `/sqlite/images.go`, `/sqlite/images_test.go`, `/sqlite/migrations/1776949571-images.{up,down}.sql`.
- `/docs/diary/2026-04-23-nanobanana-fs-builder.md` (builder diary).

## Tests / lint / build

- `go build ./...` -- silent.
- `go vet ./...` -- silent.
- `gofmt -l .` -- no diff.
- `make fmt` -- no diff.
- `golangci-lint run` -- `0 issues.`
- `go test -shuffle on ./...` -- all green (`app/http`, `app/llm`, `app/model`, `app/sqlite`, `app/sqlitetest`).
- `go test -run TestImageStore -v ./llm/` -- five subtests pass (missing-file, round-trip, overwrite, shard-layout + no `.tmp-` leftover, bad-hash rejection).
- `go mod tidy` -- no-op (pure deletion + refactor, no new deps).

## End-to-end verification

App was already running on `:8090` from the builder (`lsof -iTCP:8090 -sTCP:LISTEN -P` returned PID 55041), so I didn't spin up a second instance.

- `curl -sS -D -` on the cached image URL from the builder's walk-through (`/image/vintage-ornate-victorian-lamp-glowing-ethereal-light`) returned `HTTP/1.1 200 OK`, `Content-Type: image/png`, `Cache-Control: public, max-age=31536000, immutable`, 1,339,945 bytes. `file` identified it as `PNG image data, 1024 x 1024, 8-bit/color RGB, non-interlaced`.
- `find images -type f`: `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/nanobanana-images/images/36/f0/8c3352077ccdb28a96339206f10ea3b52a86e6571397adbc640e927fb5e9.png`. Exactly the `root/XX/YY/<60hex>.png` shape with depth 3 and 60-char filename. No `.tmp-*` files left behind in the shard dir.
- Edge cases: `/image/` -> 400, `/image/---` -> 400, 2000-byte path -> 414.
- `curl -I` on an image URL returns `405 Method Not Allowed` because chi registers GET only. Expected, documented in the builder diary.

## Spec checklist

| Check | Result |
|---|---|
| `ImageStore` has `Get`, `Put`, `Path` + `NewImageStore(root)` | Pass -- `/llm/image_store.go`. |
| Hash validated against `^[0-9a-f]{64}$` *before* any FS access in both `Get` and `Put` | Pass -- lines 48 and 69 guard before `s.Path(hash)` is called. |
| Strict hex (lowercase, exactly 64 chars) -- no uppercase / 63 / 65 / non-hex / `../` | Pass -- `hashRe` + `image_store_test.go` "bad hash is rejected" subtest covers empty, uppercase-and-too-long, 63-char, 65-char, non-hex, path-traversal, all-uppercase. |
| `Get` returns `(nil, false, nil)` on `os.ErrNotExist` (not an error) | Pass -- lines 53-56. |
| `Put` creates parent directories with `os.MkdirAll` / `0o755` | Pass -- line 75. |
| Temp file created in the *same directory* as the final path so `os.Rename` is atomic | Pass -- `tmp = final + ".tmp-" + hex(rand)`. Same shard dir, so rename is a single-device inode swap. |
| Temp suffix uses `crypto/rand` (not `math/rand`) | Pass -- `crypto/rand` import on line 4, `rand.Read(suffix[:])` on line 82. 8 bytes = 16 hex chars, plenty for concurrent writers of the same hash. |
| Store is effectively immutable after construction -- no goroutine-unsafe state | Pass -- single `root string` field set at construction. `*regexp.Regexp` methods are goroutine-safe. All FS operations are stdlib-safe. |
| 5 subtests using `t.TempDir()` | Pass -- every subtest creates its own tempdir. Nothing leaks. |
| Handler: hit path returns cached bytes; miss calls `gen.Image` + `store.Put`, then serves | Pass -- `/http/image.go:80-115`. Store `Put` failure is logged but bytes are still served (the lead explicitly specified this behaviour). |
| 502 on model error, 500 on store read error, 400 on bad path, 414 on oversize | Pass -- all edge cases verified E2E. |
| `Cache-Control` / `Content-Type` unchanged | Pass -- `public, max-age=31536000, immutable` / `image/png`. |
| Span attrs `image.path_hash`, `image.path_len`, `image.cached`, `image.bytes` | Pass on both hit and miss branches. `image.mime` dropped (always PNG). |
| No leftover references to `model.Image`, `ErrorImageNotFound`, `sniffImageMime`, `GetImage`, `InsertImage` in Go source | Pass -- `grep -rn` returns nothing. |
| `IMAGES_PATH` read, `os.MkdirAll`, absolute-path log, passed to `NewImageStore` | Pass -- `/cmd/app/main.go:60-69`. |
| `.env.example` has `IMAGES_PATH=images` | Pass -- alphabetical slot between `GOOGLE_API_KEY` and `JOB_QUEUE_TIMEOUT`. |
| `.gitignore` has `/images/` (rooted, not `images/`) | Pass. |
| Path traversal via `../` unreachable | Pass -- (a) handler sha256's its own input, (b) store regex blocks anything non-hex, (c) the shard path has a `.png` suffix appended -- a traversal string can't even reach `filepath.Join` to misbehave. |

## Fixes I made

One small hardening in `/llm/image_store.go`:

- If `os.WriteFile(tmp, ...)` fails part-way (e.g. disk full), `tmp` may exist as a partial file. The original code already cleans up on `os.Rename` failure; I added a mirrored `_ = os.Remove(tmp)` on the `WriteFile`-failure branch so shard directories don't accumulate orphaned tmp files after transient write failures. One-line best-effort cleanup, no behavioural change on the happy path.

Committed as `Best-effort tmp cleanup on WriteFile failure in ImageStore.Put`. Lint clean, tests green.

## Judgment calls

- **`Path(hash)` is not guarded by the hash regex.** `Get` and `Put` both validate before calling `s.Path(hash)`, so the two in-tree callers are safe. `Path` is exported as a logging helper and is not called anywhere outside the store today. I did *not* add a guard or change the signature: it would be a breaking API change (return tuple or panic) for a theoretical caller, and the builder diary's rationale (`"intended for logging and span attributes -- the file itself may or may not exist"`) is coherent. Flagging as a minor footgun for the lead's awareness; if anyone later calls `store.Path(untrustedHash)` from outside the store, it will panic on `hash[0:2]` for inputs shorter than 4 bytes.
- **`NewImageStore(imagesRoot)` receives the relative path while the log shows the absolute path.** Works correctly today (the process never chdirs), but there's a cosmetic split between "what we logged as the store root" and "what the store actually holds". A one-line fix would be to pass `absImagesRoot` to `NewImageStore`. Not strictly a bug; left alone to keep the diff minimal. Flagging for the lead's call.
- **Orphan `images` SQLite table on previously-migrated DBs.** This change *deletes* the up-migration file rather than shipping a new down-migration to drop the table. On a dev or production DB that already ran `1776949571-images.up.sql`, the `images` table and its trigger are now orphaned -- they'll sit there unused, and the migrations tracker (`glue/sql`) may flag the missing migration file as a schema-drift concern depending on its semantics. The builder diary notes they deleted local `app.db*` artefacts, so their local env is clean. For a solo pre-1.0 project, this is probably fine; if any shared/staging DB exists, a follow-up "drop images table" migration would tidy it up. **Escalating** to the lead rather than fixing in place because it's a schema-management decision, not a code bug.
- **No concurrent `Put` test.** The spec suggested "consider adding an errgroup concurrent Put case, but don't gold-plate if the current set is solid". The current five subtests cover the correctness properties that matter (round-trip, overwrite, shard layout, no `.tmp-` leftovers, bad-hash rejection). `os.Rename` semantics on same-filesystem atomic overwrite are well-documented stdlib contract, and the builder diary explicitly acknowledges "last-writer-wins" as the desired behaviour. I did not add a concurrent test.
- **`Path` panic on short hash** already described above -- same judgement applies.

## Non-blocking observations

- `os.WriteFile` uses mode `0o644` for the tmp file; `umask` can narrow this further. Fine for a cache.
- No `Content-Length` header on the hit path. The stdlib server chunks when we don't set it. Cosmetic, called out in the builder diary's "future work".
- No size cap / eviction. Explicitly out of scope.
- `.tmp-*` files left over after a crashed process accumulate in shard dirs. Builder diary flagged a tiny janitor as future work. Agreed -- not a blocker today.
- `imageGenTimeout` in the handler (60s) duplicates the internal `llm.imageTimeout` (60s) wrapping `Client.Image`. Slightly redundant -- whichever context dies first wins -- but harmless.

## Outcome

**Approve with a one-line fix in place.**

- Fix: `Best-effort tmp cleanup on WriteFile failure in ImageStore.Put` (diff: +3 lines in `/llm/image_store.go`).
- For lead's decision: whether to ship a follow-up migration that drops the now-orphan `images` table on previously-migrated DBs. Probably fine to defer on a pre-1.0 solo project; surfaced because "delete the up-migration instead of add a down-migration" is a choice worth the lead's confirmation.
- Nothing else to flag. Storage-shape change is clean, atomic-rename is correctly scoped, hash validation is tight, span attrs survive the refactor, E2E serves real 1.3MB PNGs from `images/36/f0/....png`.

## Note on task infrastructure

No `TaskUpdate` tool available in this environment. Task #4 needs closing externally.
