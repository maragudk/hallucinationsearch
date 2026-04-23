# Diary: Robust retries for ChatCompleters (builder)

**Author:** builder (robust-retries task)

Wrapping the four `gai.ChatCompleter`s in `llm/llm.go` with `maragu.dev/gai/robust.NewChatCompleter` so transient Anthropic failures retry with jittered exponential backoff instead of bubbling up as a dead search result or empty ad slot.

## Prompt context

**Verbatim prompt (from team-lead):** "Wrap each of the four `gai.ChatCompleter`s built in `NewClient` (resultCC, websiteCC, adCC, adWebsiteCC) with `maragu.dev/gai/robust.NewChatCompleter`. Single-element `Completers` slice each. No fallback. Defaults for retry settings. Pass `opts.Log`."

**Interpretation:** Surgical change. No behavior change in `GenerateResult`/`GenerateWebsite`/`GenerateAd`/`GenerateAdWebsite`. No tests required because the robust package is already tested upstream. Scope is `llm/llm.go` plus whatever `go mod tidy` does to `go.mod`/`go.sum`.

## What I did

### The wrap

Added `"maragu.dev/gai/robust"` import. Introduced a local `wrap` closure in `NewClient` to keep the four constructor calls uncluttered:

```go
wrap := func(inner gai.ChatCompleter) gai.ChatCompleter {
    return robust.NewChatCompleter(robust.NewChatCompleterOptions{
        Completers: []gai.ChatCompleter{inner},
        Log:        opts.Log,
    })
}
```

Each field is still typed `gai.ChatCompleter`, so no call-site changes were needed in any of the `Generate*` methods or their jobs. `robust.ChatCompleter` satisfies the interface, confirmed by the `_ gai.ChatCompleter = (*ChatCompleter)(nil)` assertion in the upstream package.

Relying on the defaults: `MaxAttempts=3`, `BaseDelay=500ms`, `MaxDelay=30s`, and the built-in conservative error classifier. No fallback chain -- each `Completers` slice has exactly one element because we only have Anthropic configured.

### go.mod

`go mod tidy` moved `maragu.dev/gai` from `// indirect` to a direct dependency (expected -- we now import `maragu.dev/gai/robust` directly). It also:

- Promoted `maragu.dev/goqite` from indirect to direct. `goqite` was already imported in `http/search.go`, `jobs/*`, and `service/fat.go`; the prior `// indirect` was a stale artifact of whatever tidy state the go.mod started in. This is a correctness fix.
- Dropped a pile of now-unreachable indirect dependencies (`github.com/alexedwards/scs/sqlite3store`, the AWS SDK v2 tree, `github.com/aws/smithy-go`). These were transitive from something that was itself only indirectly reached and is no longer needed. Pure cleanup.

None of these are in scope for this task's intent, but `go mod tidy` is the instructed command and I'd rather ship a clean go.mod than carry dead indirect deps forward.

### Verification

```
go mod tidy   # silent
go build ./...  # silent
go vet ./...    # silent
```

All three pass. The app still compiles; no call sites changed.

## Tradeoffs and decisions

- **Helper closure vs. four inline calls.** Four inline `robust.NewChatCompleter(...)` wraps would have produced 20+ lines of near-identical code. The closure reads better and avoids the copy-paste risk of forgetting to pass `opts.Log` in one of them. Local, unexported, six lines -- feels in balance.
- **Default retry settings.** Task explicitly said "Defaults for retry settings" -- 3 attempts, 500ms base, 30s cap, default classifier. The classifier is conservative by default (retries transient network/5xx, fails fast on 4xx auth/validation), which is the sensible behavior for a parody search engine that just wants to paper over the occasional blip.
- **Single-element `Completers` slice vs. future fallback.** No second model wired up. If we later want a Sonnet fallback for Haiku outages, it slots in as a second element in the same slice -- no structural change needed.
- **No tests.** The robust package is already tested upstream; this is pure constructor plumbing with no logic. Adding a wrap-level unit test would be testing that the `robust` package works, which isn't our job.

## Review and validation

- `git diff llm/llm.go` -- three additions: the import, the `wrap` closure, and wrapping the four constructor calls. No logic touched.
- `git diff go.mod` -- gai promoted to direct, goqite promoted to direct, AWS SDK indirects removed. Nothing spooky.
- Running the app (`make watch`) and hitting a search should behave identically in the happy path. Under simulated Anthropic flakiness (e.g. inject a 503 upstream), the request should now retry up to 3 times with 0-500ms / 0-1s / 0-2s jittered backoff before giving up -- observable as longer latency on a partially-recovered run rather than an immediate failure.
