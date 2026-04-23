# Diary: QA review of chat-completion swap from Claude Haiku to Gemini 3 Flash Preview

**Author:** qa (gemini-3-flash task)

Review the builder's swap of the four `gai.ChatCompleter`s in `/llm/llm.go` from Anthropic Claude Haiku 4.5 to Google `gemini-3-flash-preview`, reusing the already-present `*google.Client` that powers Nano Banana. Verify correctness, doc/naming, hygiene, and Go style. Sign off or route issues back to the builder.

## Step 1: Read the builder diary and the change

### Prompt Context

**Verbatim prompt:** "You are QA for a feature that just switched all chat-completion LLMs from Anthropic Claude Haiku to Google Gemini 3 Flash Preview. The builder just finished. [... full spec listing what to verify under Correctness, Doc/naming, Hygiene, Style, and what NOT to do ...]"

**Interpretation:** Two phases -- static/structural review first (read the diff, confirm the six correctness points, check for stale docs/imports, confirm `.env` untouched), then automated checks (`go build`, `go vet`, `go test`, `go mod tidy` no-op, golangci-lint). Write a QA diary entry. Fix trivial stale-doc stuff myself; flag substantive issues for the lead.

**Inferred intent:** Last line of defence before the lead does the live `make watch` smoke test. The point is to catch typos in the model string, a missed import, a broken call site, or a stale doc line -- nothing that requires hitting the live Gemini API.

### What I did

Read `/docs/diary/2026-04-23-gemini-3-flash-builder.md` end-to-end first so I knew exactly what the builder intended to change and which choices were deliberate. Then read the three edited source files (`/llm/llm.go`, `/cmd/app/main.go`, `/README.md`) in full rather than just the diff hunks -- wanted to confirm the doc comments and surrounding code still read correctly, not just that the line-level edits looked sensible.

Cross-checked the `maragu.dev/gai/clients/google` package in the module cache at `/Users/maragubot/Developer/go/pkg/mod/maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0/clients/google/chat_complete.go` to confirm the API shape the builder relied on: `type ChatCompleteModel string` at line 22, `type NewChatCompleterOptions struct { Model ChatCompleteModel }` at line 38, and `func (c *Client) NewChatCompleter(opts NewChatCompleterOptions) *ChatCompleter` at line 42. All three match how the builder called them.

Ran a broad grep across the whole tree (Go files, Markdown, YAML, Dockerfile, Makefile) for `anthropic`, `Anthropic`, `Haiku`, `HaikuModel`, `ANTHROPIC_API_KEY`, and `sk-ant` excluding the diary directory and the gitignored `.env`. Zero hits in Go code; zero hits in doc/config files. Only remaining reference is the stub in `.env` itself (expected, spec says leave it) and in `.env.example` (see "What warrants review" below).

Ran the full hygiene sweep: `go build ./...`, `go vet ./...`, `go test ./...` (all silent / ok), `go mod tidy` was a no-op (diffed `go.mod`/`go.sum` before and after -- zero bytes changed), and `golangci-lint run ./llm/... ./cmd/...` reported `0 issues.`.

Finally re-ran `go test -v -count=1 ./llm/...` to make sure the LLM package tests pass fresh rather than from cache. `TestImageStore` and its five subtests all PASS. The package doesn't contain chat-completer tests (it never did -- the gai upstream owns those), so no new assertions to add on our side.

### Why

The builder diary is unusually thorough and calls out exactly where to look, so I used it as a map rather than re-deriving the blast radius. That let me spend most of the time on the parts most likely to contain bugs -- the model string literal, the completer constructions, and the `NewClientOptions` wire-up in `cmd/app/main.go` -- rather than exploring files that weren't touched.

### What worked

Per-correctness-point verification against the actual code:

- **All four completers routed through `gc.NewChatCompleter(...)` wrapped with `robust` via `wrap`.** Confirmed in `/llm/llm.go` lines 70-73: `resultCC`, `websiteCC`, `adCC`, `adWebsiteCC` all `wrap(gc.NewChatCompleter(google.NewChatCompleterOptions{Model: ChatModel}))`. The `wrap` closure at lines 61-66 still produces `robust.NewChatCompleter(robust.NewChatCompleterOptions{Completers: []gai.ChatCompleter{inner}, Log: opts.Log})`. Retry wrapping intact.
- **Model string is exactly `gemini-3-flash-preview`.** Line 24: `const ChatModel = google.ChatCompleteModel("gemini-3-flash-preview")`. Character-by-character match with the spec. Not `gemini-3.0-flash-preview`, not `gemini-3-flash`, not `gemini-3-flash-preview-001`.
- **Nano Banana is untouched.** Line 30 still reads `const NanoBananaModel = "gemini-3.1-flash-image-preview"`. The `Image` method at lines 329-352 is byte-identical to main (`git diff HEAD -- llm/llm.go` shows no changes in that range).
- **`anthropic` import is gone.** Line 14-18 imports block contains only `google.golang.org/genai`, `maragu.dev/gai`, `maragu.dev/gai/clients/google`, `maragu.dev/gai/robust`. Grepped all `*.go` files in the tree for `anthropic` -- zero hits.
- **`NewClientOptions.Key` removed; `cmd/app/main.go` doesn't pass it.** The struct at lines 47-52 now has just `GoogleKey` and `Log`. `/cmd/app/main.go` lines 54-57 constructs `llm.NewClient(llm.NewClientOptions{GoogleKey: ..., Log: ...})` with no `Key` field. `go build ./...` confirms no compile errors elsewhere.
- **`.env` is untouched and the stub is still there.** `git status --ignored` shows `!! .env` (unchanged, still gitignored). Reading the file confirms `ANTHROPIC_API_KEY=sk-ant-...` is still on line 13 as required.

Doc/naming:

- **Package doc comment no longer mentions Anthropic.** Line 1-2 reads "Package llm wraps [maragu.dev/gai]'s Google client for generating fabricated search results and fabricated destination websites." Accurate -- chat completions and Nano Banana are both now Google.
- **`Client` struct doc updated.** Lines 35-37: "wraps gai chat-completers for results, ads, and both their destination websites (all Gemini 3 Flash Preview), plus the raw Google genai client used for image generation via Nano Banana." Reads naturally, factually accurate.
- **`ChatModel` has a sensible doc comment.** Lines 20-23 explain the role (chat-completion fabrication across all four call sites) and the rationale (fast/cheap enough for 10 parallel jobs + blocking /site under 2 min). Follows the Go style rule of "identifier first, complete the sentence without repeating itself".
- **README reads naturally.** Two paragraphs now reference "Gemini 3 Flash Preview" and `GOOGLE_API_KEY=AIza...`. Flow is unchanged from pre-edit.

Hygiene:

```
$ go build ./...
(silent)

$ go vet ./...
(silent)

$ go test ./...
?       app/cmd/app     [no test files]
ok      app/html        (cached)
ok      app/http        (cached)
?       app/jobs        [no test files]
ok      app/llm         (cached)
ok      app/model       (cached)
?       app/service     [no test files]
?       app/servicetest [no test files]
ok      app/sqlite      (cached)
ok      app/sqlitetest  (cached)

$ go mod tidy          # diffed go.mod and go.sum before/after
(no-op)

$ golangci-lint run ./llm/... ./cmd/...
0 issues.

$ go test -v -count=1 ./llm/...
=== RUN   TestImageStore
... (6 subtests all PASS)
ok      app/llm 0.551s
```

All green.

Style (per `fabrik:go`):

- Doc comments on `ChatModel` and `Client` start with the identifier name and don't repeat themselves. Conforms.
- Variable naming unchanged -- `gc` for the google client, `wrap` for the retry closure. Consistent with the rest of the file.
- No new exported identifiers beyond `ChatModel` (which replaces `HaikuModel`, same role).
- No debug `fmt.Println`, no commented-out code, no `TODO`/`FIXME` left behind. Grepped `/llm/llm.go` and `/cmd/app/main.go` for all of them -- zero hits.

### What didn't work

Nothing broke. Build, vet, tests, tidy, and golangci-lint all pass on first run with no edits needed from me. The code is in the exact state the builder described.

### What I learned

The `gai/clients/google` package in the vendored module has named constants for Gemini 2.x models (`ChatCompleteModelGemini2_0Flash`, `ChatCompleteModelGemini2_5Flash`, `ChatCompleteModelGemini2_5FlashLite`, `ChatCompleteModelGemini2_5Pro`) but no `gemini-3`-anything constant yet. The builder correctly used `google.ChatCompleteModel("gemini-3-flash-preview")` -- the raw string conversion -- because the upstream package hasn't added a named constant for the v3 preview line. When upstream does add one (likely named `ChatCompleteModelGemini3FlashPreview` by analogy), the `ChatModel` constant here should probably migrate to it. Noted in "Future work".

The naming pattern for Gemini model IDs is inconsistent across versions: v2.x uses dots (`gemini-2.5-flash`), Nano Banana uses a dot (`gemini-3.1-flash-image-preview`), but the v3 chat preview uses a hyphen (`gemini-3-flash-preview`). The spec was explicit that the exact string is `gemini-3-flash-preview` (with a hyphen, not a dot), so this is not a typo -- it's Google's inconsistency, which we faithfully reproduce. Flagged only so future readers don't "fix" the hyphen to a dot.

### What was tricky

One real judgment call around `.env.example`. See "What warrants review" below -- I'm not fixing it myself because I'm not sure if the builder's deliberate scope choice is the lead's preference, and it's only borderline trivial.

Zero other tricky spots. The change is as surgical as the builder claimed and the diary gives an unusually complete map of every choice made.

### What warrants review

**One unresolved inconsistency for the lead to decide on:**

`/.env.example` (tracked in git, line 1) still reads `ANTHROPIC_API_KEY=sk-ant-...` and contains no `GOOGLE_API_KEY` placeholder. But `/README.md` (now updated) tells users: `cp .env.example .env && echo "GOOGLE_API_KEY=AIza..." >> .env`. After that flow, the user ends up with a local `.env` containing both the stale `ANTHROPIC_API_KEY=sk-ant-...` stub (copied from the example) and the newly-appended `GOOGLE_API_KEY=AIza...` they pasted. The Anthropic stub is harmless (nothing reads it anymore) but it's misleading documentation-as-config. The builder's diary explicitly calls this out as deliberate -- they read the spec's "don't touch `.env`" rule as also covering `.env.example`, and listed the cleanup under "Future work".

My read: the spec I got was more specific -- it only carved out `.env` (the gitignored local file) and explicitly said the Anthropic stub there is fine. `.env.example` is tracked-in-git template documentation, which the spec didn't exempt, and the builder did update the README (also tracked-in-git documentation). So there's a defensible case for either updating `.env.example` to swap the Anthropic stub for a `GOOGLE_API_KEY=AIza...` stub, or leaving it for a separate doc-cleanup commit. Either is a one-line change. **Not fixing this myself** because the builder explicitly chose to leave it, and second-guessing that choice without the lead's input is the kind of scope drift QA should flag, not silently resolve.

**Three structural spots the builder also flagged and I independently confirmed clean:**

1. `/llm/llm.go` constructor -- four completers built from `gc` via `gc.NewChatCompleter(...)`, all `wrap`'d, `google: gc` field intact. Confirmed.
2. `/cmd/app/main.go` llmClient construction -- the `Key:` line is gone; `GoogleKey` and `Log` remain. No Go code anywhere still reads `ANTHROPIC_API_KEY`. Confirmed via grep.
3. `go.mod` / `go.sum` diff -- `github.com/anthropics/anthropic-sdk-go v1.35.1` dropped from the indirect block, plus `github.com/tidwall/{gjson, match, pretty, sjson}` (the Anthropic SDK's transitive JSON deps). `go.sum` shrank by 18 lines (net; 2 are the auxiliary `github.com/dnaeon/go-vcr` and `github.com/tidwall/gjson v1.14.2` older-version pair the SDK pulled in). No direct requires added or changed. Confirmed clean.

No substantive QA issues to route back to the builder. Sign-off from my end, modulo the `.env.example` judgment call for the lead.

### Future work

- Swap `google.ChatCompleteModel("gemini-3-flash-preview")` for an upstream named constant once `maragu.dev/gai/clients/google` adds one (likely `ChatCompleteModelGemini3FlashPreview` by analogy with the existing v2.x naming). That's a trivial follow-up when the version bump happens.
- Decide what to do with `/.env.example` line 1 (see "What warrants review"). Separate commit either way.
- Consider refreshing the `Client.google` field's doc comment at `/llm/llm.go` line 44 to note that the Google client now also powers chat completions (not only Nano Banana image generation). The builder flagged this in their own "Future work" too; I agree it's janitor-tier.

## Verification

Full sequence run cleanly:

```
$ go build ./...
(silent)

$ go vet ./...
(silent)

$ go test ./...
?       app/cmd/app     [no test files]
ok      app/html        (cached)
ok      app/http        (cached)
?       app/jobs        [no test files]
ok      app/llm         (cached)
ok      app/model       (cached)
?       app/service     [no test files]
?       app/servicetest [no test files]
ok      app/sqlite      (cached)
ok      app/sqlitetest  (cached)

$ go mod tidy
(no-op; go.mod and go.sum unchanged)

$ golangci-lint run ./llm/... ./cmd/...
0 issues.
```

Grep sweeps for stale references:

```
$ grep -rn "anthropic\|Anthropic\|HaikuModel\|Haiku" --include="*.go" .
(zero hits)

$ grep -rn "anthropic\|Anthropic\|Haiku\|HaikuModel" \
      --include="*.md" --include="*.yml" --include="*.yaml" \
      --include="Dockerfile" --include="Makefile" . | grep -v docs/diary
(zero hits)

$ grep -rn "ANTHROPIC_API_KEY" --include="*.go" --include="*.md" \
      --include="Dockerfile" --include="Makefile" \
      --include="*.yml" --include="*.yaml" . | grep -v docs/diary
(zero hits)
```

Files touched by this QA pass:

- `/docs/diary/2026-04-23-gemini-3-flash-qa.md` -- this entry. No source or config changes made during QA.

## Task completion

No `TaskGet` / `TaskUpdate` tool available in this environment (same constraint the builder hit; `ToolSearch` for `+task` surfaced only the unrelated `TaskStop`). Recording the completion summary here:

> QA pass complete on the Gemini 3 Flash Preview chat-completion swap. All correctness, doc/naming, hygiene, and style checks pass. Build, vet, tests (including fresh `-count=1` run of `./llm/...`), `go mod tidy` no-op, and `golangci-lint` clean. One non-blocking judgment call for the lead: `.env.example` still has a stale `ANTHROPIC_API_KEY=sk-ant-...` on line 1 and no `GOOGLE_API_KEY` placeholder, while the README was updated to reference Google. Builder's diary explains they left it deliberately; either completing the swap or leaving it for a follow-up commit is fine -- flagging so the lead decides. No code changes made during this QA pass. Files reviewed: `/llm/llm.go`, `/cmd/app/main.go`, `/README.md`, `/go.mod`, `/go.sum`, `/.env`, `/.env.example`.
