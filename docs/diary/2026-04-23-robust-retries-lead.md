# Diary: Wrap gai completers with robust retries (lead)

**Author:** lead

Thin task: wrap the four `gai.ChatCompleter` instances in `/llm/llm.go` with `maragu.dev/gai/robust.NewChatCompleter` so transient Anthropic errors get retried. Explicitly no fallback chain -- a single-element `Completers` slice each, retries only. This is the lead's view: scope confirmation, delegation, end-to-end verification in the browser.

## Step 1: Scope check and delegation

### Prompt Context

**Verbatim prompt:** "Are we using maragu.dev/gai/robust package? No fallback, just for retries."

**Interpretation:** First a yes/no question about current state, then an implicit offer to wire it in with tight scope -- retries only, no cascading fallback completer list. Don't over-build.

**Inferred intent:** Markus wants the app's LLM calls to survive transient Anthropic errors (rate limits, 5xx, network blips) without him having to babysit or rerun. He's explicitly not asking for multi-provider resiliency; one completer per wrapper, defaults for everything else.

### What I did

Grepped for any existing `robust` usage -- zero hits. Confirmed `maragu.dev/gai` was pinned indirect in `/go.mod`. Answered "no, not yet" and asked if he wanted it wired in.

On "yes", read `/llm/llm.go` end to end and the robust package at `/Users/maragubot/Developer/go/pkg/mod/maragu.dev/gai@v0.0.0-20260417120024-f687d62fdde0/robust/chat_completer.go` to confirm the API shape (single `Completers` slice, defaults for `MaxAttempts`/`BaseDelay`/`MaxDelay`, default `ErrorClassifier`). Entered a worktree `robust-retries`, created a fabrik team with the same name, wrote two tasks (builder wrap + QA review, QA blocked by builder), spawned `builder` (fabrik:builder) and `qa` (fabrik:qa) in the background.

Builder's requirements in the task description were explicit: single-element `Completers` slice each, no override of defaults, threaded logger, untouched `Generate*` methods, clean `go mod tidy` / `go build` / `go vet`, scope limited to `/llm/llm.go` + `/go.mod` / `/go.sum`.

### Why

A single file, ~20 line change. Writing out requirements upfront meant the builder didn't have to guess on scope and QA had a checklist to verify against. The explicit "no fallback" constraint was the one thing that could have been misread, so it was stated twice.

### What worked

Builder shipped commit `8bad2b2` without questions. QA approved without feedback. The wrap went in via a local `wrap` closure so each of the four completer constructor calls picked up retries identically. `go mod tidy` as a side effect also promoted `maragu.dev/goqite` to direct (was stale indirect despite direct imports in `/http/`, `/jobs/`, `/service/`) and dropped unused AWS SDK v2 + `scs/sqlite3store` indirect entries -- legitimate cleanup, QA grep-confirmed zero code references.

### What didn't work

Nothing broke. Two noisy delivery echoes from the mailbox -- builder and QA each got a duplicate task-assignment notification after completing their tasks. Both correctly flagged them rather than re-doing the work; I acknowledged the echoes so they'd stand down.

### What I learned

`robust.ChatCompleter` satisfies `gai.ChatCompleter`, so wrapping is drop-in -- no `Client` struct changes, no call-site changes in the four `Generate*` methods. The default classifier ships with a conservative built-in, which is the right choice for "I just want retries, don't think about it".

### What was tricky

One minor edge. `robust.ChatCompleter`'s `ChatComplete` pulls the first part of the response stream eagerly before committing (`commitOnFirstPart`) so it can classify errors that only surface mid-first-token. The wrapped response then yields that buffered first part followed by the rest. This means the existing `for part, err := range res.Parts()` loops in the `Generate*` methods keep working unchanged, but the caller MUST drain the iterator or the internal goroutine leaks -- we already do drain in all four methods, so no change needed. Worth knowing if we ever add early-return logic.

### What warrants review

None expected. The change is mechanical and the review surface is tiny: four constructor calls in `/llm/llm.go` plus the import. `go.mod` tidy churn is the only thing that could surprise a reviewer; they should confirm the dropped indirect deps really are unreferenced (grep for `aws-sdk-go-v2` and `sqlite3store` under `/`).

### Future work

Could later: (a) tune `MaxAttempts` / `MaxDelay` against observed Anthropic failure rates once we have traces, (b) wire in the OTel tracer if we ever add observability, (c) add a custom `ErrorClassifierFunc` if the conservative default retries too little or too much. None of those are needed now.

## Step 2: End-to-end verification in the browser

### Prompt Context

**Verbatim prompt:** "Start the app with make watch and check it yourself with the playwright-cli skill."

**Interpretation:** Static checks aren't enough -- run the real binary against the real Anthropic API and confirm the search + site generation paths still work through the wrapper.

**Inferred intent:** Catch any behavioral regression the wrapper might introduce that `go build` / `go vet` / `go test` wouldn't. Specifically the stream-drain contract and the first-part-commit peek -- if those were subtly broken, results wouldn't render.

### What I did

Started `make watch` in the background, watched `/app.log` for the "Starting server" line, then drove Chrome via `playwright-cli`:

1. Loaded `http://localhost:8081/` -- landing page rendered clean.
2. Searched `cat festivals in denmark` -- waited 8s, snapshot showed 3 ads + 10 fabricated results, exercising `adCC` and `resultCC` through the wrapper.
3. Clicked the first result (`Aarhus International Cat Festival`) -- site page rendered after ~30s, 4.7KB of `innerText`, exercising `websiteCC`.
4. Grepped `/app.log` for `error|panic|robust|failed` -- zero matches.

`adWebsiteCC` uses the same wrapper in the same way; not separately clicked but the code path is identical.

### Why

The stream-drain contract in `commitOnFirstPart` was the one thing a compile-time check couldn't catch. If the wrapper somehow broke streaming, the search page would show empty results or hang. It didn't.

### What worked

All four LLM code paths produced output with no log noise. Only console errors were pre-existing favicon 404s, unrelated to this change.

### What didn't work

One playwright click timed out because the fabricated-site page takes ~30s to generate (16K `MaxCompletionTokens`) and `playwright-cli click` has a 5s default navigation timeout. Re-snapshotting after a longer sleep worked. Not a robust-wrapper issue.

Verbatim:
```
TimeoutError: locator.click: Timeout 5000ms exceeded.
```

### What I learned

Two good smoke tests for any LLM-pipeline change: (1) search a query and confirm all 13 entries (3 ads + 10 results) appear, (2) click a result and confirm the fabricated site renders. Covers all four completer paths in under a minute. Worth remembering for future regressions.

### What was tricky

Nothing tricky. The playwright timeout above was expected given the `/site/` handler's 2-minute budget for a 16K-token response.

### What warrants review

Same review surface as Step 1 -- nothing new. This step only confirms nothing broke.

### Future work

None.
