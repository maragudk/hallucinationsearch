# Diary: Randomized prompts (QA)

**Author:** QA (task #4)
**Scope:** Verify the builder's work replacing the "avoid already-used titles/sponsors" dedup in `llm/llm.go` with per-job randomization along curated dimensions. Live smoke test runs in parallel and is the lead's responsibility.

## Verdict

All checks pass. No blocking issues found. Ready to ship.

## What I verified

### Curated lists (`llm/constraints.go`)

Cross-checked each of the four lists in `llm/constraints.go` against the verbatim spec pasted into the task:

- `resultArchetypes` (100): section counts 5+7+10+8+8+7+8+8+7+7+7+8+5+5 = 100. Every entry in spec order. No typos, no swaps. Section comments preserved and accurate.
- `reliabilitySignals` (6): verbatim match. Escaped inner double-quote on `"my uncle told me"` parses and passes the no-empty test.
- `adProductCategories` (40): verbatim match in spec order.
- `adPitchAngles` (15): verbatim match in spec order. Parenthetical examples added to 7 of the 15 entries (fear-based, aspirational, social-proof-heavy, hyper-niche insider, curiosity, contrarian, problem-awareness), which is what the spec asked for ("full parenthetical examples per the builder's paste"). The other 8 entries are bare, which matches the spec's single-line paste.

### Correctness

- `llm/llm.go:95` - `GenerateResult(ctx context.Context, query string, position int) (Result, error)` - three params, no `existingTitles`.
- `llm/llm.go:212` - `GenerateAd(ctx context.Context, query string, position int) (Ad, error)` - three params, no `existingSponsors`.
- `llm/constraints.go:5` imports `math/rand/v2` (not `math/rand`). Global `rand.IntN` is documented as safe for concurrent use, so nil-r production callers are fine under fan-out.
- Constraints block text (`llm/constraints.go:218-227` and `241-250`) matches the spec character-for-character, including the bullet order (archetype / weirdness / reliability for results; category / pitch / weirdness for ads). Ordering here is load-bearing because the system-prompt back-references those names in the same order.
- House-style paragraph in both system prompts matches the spec (`llm/llm.go:88` for results, `llm/llm.go:204` for ads). "Tone should vary wildly..." line is gone from both; confirmed by test assertions and by grep.

### Call sites

- `jobs/search.go`: `generateResultDB` interface no longer has `GetResults`; `resultGenerator.GenerateResult` signature matches the new LLM shape (`query, position`); call site at line 108 is clean; per-job title gather is removed.
- `jobs/ads.go`: symmetric. `generateAdsDB` / `adGenerator` interfaces trimmed; per-job sponsor gather removed.
- `sqlite/search.go:33` `GetResults` and `sqlite/search.go:107` `GetAds` still present and still used by `http/search.go` (lines 101, 106, 211, 215) for rendering. DB surface is intact - only the per-job gather calls went away, as intended.
- `grep -rn 'GenerateResult\|GenerateAd' .` shows no stale callers passing the old slice parameter.

### Hygiene

- `go build ./...` - clean.
- `go vet ./...` - clean.
- `go test ./...` - all packages pass.
- `go test -shuffle on -race ./llm/` - clean.
- `go test -count=3 ./llm/` - clean, no flakes.
- `go mod tidy` - no-op (no diff on go.mod/go.sum).
- `golangci-lint run ./...` - `0 issues.`
- `grep -rn 'existingTitle\|existingSponsor\|existing titles\|existing sponsors\|Tone should vary wildly' .` - the only remaining hits are in explanatory comments in `llm/llm.go:93`, `llm/llm.go:210`, `jobs/search.go:85`, `jobs/ads.go:85` (describing *why* the old mechanism was replaced) and in `llm/llm_test.go:180,192` (negative assertions that the old line is gone). No stale production references.

### Style (fabrik:go)

- All four package-level list vars have doc comments beginning with the identifier name, per `fabrik:go`.
- `resultRolls` / `adRolls` types, their `constraintsBlock()` methods, and the `roll*Constraints` / `pickString` / `pickWeirdness` helpers all have doc comments and idiomatic Go names.
- Everything beyond `GenerateResult` / `GenerateAd` stays unexported.
- `goimports` drift: none (builder verified in their diary; nothing for me to re-run).

### Tests (`llm/llm_test.go`)

- List sizes (100 / 6 / 40 / 15) pinned.
- No duplicates, no empties.
- Seeded-PCG determinism for both rollers.
- `nil` RNG falls back to global generator.
- `constraintsBlock()` on both roll types contains the expected values.
- System prompts mention "house style" + "deadpan" (or "dry") and no longer contain "Tone should vary wildly".

Coverage is good for the data + helpers surface. End-to-end test of `GenerateResult` / `GenerateAd` would require a fake gai completer - out of scope here, consistent with the rest of the codebase.

## Non-issues I considered and dismissed

- `pickString` panics on empty slice. Callers are package-private and the four curated lists have compile-time test assertions pinning non-empty. Acceptable.
- `pickWeirdness` uniform distribution 1-10 (no skew). Builder flagged this in their diary; lead said nothing about wanting a skew in the spec, so uniform is correct per the "Honor the spec" rule. Easy to change later.
- `pickString`'s nil-handling comment is slightly more detailed than `pickWeirdness`'s. Micro-style nit, not worth flagging.

## What I did not do

- Did not run `make watch` or hit the live API (lead is doing that in parallel).
- Did not commit or push.
- Did not edit any code (nothing trivial enough to warrant a fix; everything I checked was correct).

## Files reviewed

- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/llm/constraints.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/llm/llm.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/llm/llm_test.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/jobs/search.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/jobs/ads.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/jobs/register.go`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/docs/diary/2026-04-23-randomized-prompts-builder.md`
- `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts/go.mod`

## Recommendation

Approve. Lead can ship once live smoke test confirms the prompts read sensibly end-to-end.
