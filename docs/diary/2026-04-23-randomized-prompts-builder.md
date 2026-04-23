# Diary: Randomized prompts (builder)

**Author:** builder (randomized-prompts task)

Replacing the "avoid already-used titles/sponsors" dedup in `llm/llm.go` with per-job randomization along curated dimensions. Both `GenerateResult` and `GenerateAd` get three rolls per call; the "existing list" argument and the DB-gather code in `jobs/search.go` / `jobs/ads.go` go away. House style ("deadpan dry humor") gets locked into both system prompts instead of varying per roll.

## Step 1: Orient and start red/green

### Prompt Context

**Verbatim prompt:**
"You are the builder for a feature replacing the "avoid already-used titles/sponsors" dedup in `llm/llm.go` with per-job randomization along curated dimensions.

Working directory: `/Users/maragubot/Developer/hallucinationsearch/.claude/worktrees/randomized-prompts` (branch `worktree-randomized-prompts`). All your work happens here.

Your task is **#3** in the shared task list -- call `TaskGet` with id `3` to read the full spec (which includes all four curated lists verbatim), then `TaskUpdate` to set `status: in_progress` and claim ownership before starting. If those tools aren't available in your env, the full spec is self-contained in the task description below (but try TaskGet first).

## Design summary (the detailed spec is in task #3)

The brainstorm concluded: parallel fan-out defeats the `existingTitles` / `existingSponsors` dedup lists (by the time job #3 starts, jobs #1 and #2 haven't finished yet). We're replacing that mechanism entirely with per-job random rolls across curated dimensions. Dry humor is the target house style, locked in the system prompts as a floor rather than a dice roll.

**Results get three rolls per call:** archetype (100 items), weirdness 1-10, reliability signal (6 items).
**Ads get three rolls per call:** product category (40 items), pitch angle (15 items), weirdness 1-10.

Both `Generate*` signatures drop their "existing list" parameter. Two call sites (`jobs/search.go` and `jobs/ads.go`) stop gathering the history and stop passing it. `resultSystemPrompt` and `adSystemPrompt` get a new "House style: deadpan..." paragraph; the "Tone should vary wildly..." line is removed. A `Constraints:` block listing the three rolled values is added to the user prompt in each function.

## What's out of scope

- `websiteSystemPrompt` and `adWebsiteSystemPrompt` -- untouched.
- Nano Banana / image generation -- untouched.
- Schemas, DB, gai wrapper, retry wrapping -- untouched.
- Don't add new dimensions beyond the three per prompt.

## Finishing up

1. Run `go mod tidy && go build ./... && go vet ./... && go test ./...` and confirm clean output. `math/rand/v2` is stdlib so no new deps.
2. Write the diary entry at `docs/diary/2026-04-23-randomized-prompts-builder.md` per `fabrik:diary`. Include the verbatim prompt, what changed, any surprises (especially around the DB-query cleanup if removing the title/sponsor gather renders a method dead), and verification output.
3. Mark task #3 completed with a short summary comment listing files touched. This unblocks task #4 (QA) and the lead's live smoke test.
4. Do NOT commit or open a PR.
5. Do NOT run `make watch` -- the lead does live testing.

If you hit ambiguity you can't resolve, leave a comment on task #3 and stop. The lead will clarify."

**Interpretation:** The design summary tells me *what* each roll is and *how many* items each curated list has, but the actual item strings for all four lists live in the Task #3 description itself, not in the design summary pasted into this prompt. The prompt's parenthetical ("which includes all four curated lists verbatim") is about task #3, not about the prompt text. So the lead expects me to read task #3 to get the verbatim lists.

**Inferred intent:** Build the randomization plumbing (two helpers, two roll types, two constraint blocks) using the lead-curated lists as drop-in data. Ship the call-site cleanup and the system-prompt rewrites in the same change. Keep the surface area small: three dimensions per prompt, no more.

### What I did

1. Read `llm/llm.go`, `jobs/search.go`, `jobs/ads.go` to map the current shape of the code. Noted the two gather loops (one in each jobs file) that collect `titles` / `sponsors` from the DB and pass them down.
2. Checked whether removing those gathers leaves dead code. `db.GetResults` and `db.GetAds` are *also* used from `http/search.go` and from the sqlite tests, so the methods themselves stay alive after the cleanup. Only the per-job gather loops disappear. Nothing to delete in `sqlite/search.go` or in the DB layer.
3. Confirmed `math/rand/v2` is available (Go 1.26 in `go.mod`) and is not imported anywhere in the project yet.
4. Checked for a `TaskGet` / `TaskUpdate` tool in the harness -- neither is available. Also no task-list file in the worktree (scope boundary prevents me from looking outside it).
5. Wrote `llm/llm_test.go` as a red-phase test file. Internal test package (`package llm`) because the rolls, constraints blocks, and curated lists are all unexported implementation details that should stay that way. The file covers:
   - List sizes (100 / 6 / 40 / 15)
   - No empty / no duplicate entries
   - `rollResultConstraints` and `rollAdConstraints` return values drawn from the curated lists, with weirdness in 1-10
   - Determinism when seeded (`rand.NewPCG(a, b)` same seed -> same rolls)
   - `nil *rand.Rand` falls back to the global generator
   - `constraintsBlock()` on both roll types includes a `Constraints:` header plus each rolled value
   - `resultSystemPrompt` and `adSystemPrompt` mention "house style" and "deadpan" (or "dry") and no longer contain the "Tone should vary wildly" line
6. Ran `go test ./llm/` to confirm red:

   ```
   # app/llm [app/llm.test]
   llm/llm_test.go:14:24: undefined: resultArchetypes
   ... (identifiers for the four lists, two roll types, two roll funcs, two constraintsBlock methods)
   FAIL    app/llm [build failed]
   ```

   Expected red, good baseline.

### Why

Red/green TDD: the tests pin down the shape the implementation has to fit, so when I wire up the rolls I can trust "tests pass" as the success signal rather than eyeballing. The tests also document the contract -- a reviewer or future me can read the test file and know what `rollResultConstraints` is supposed to do without reading the prose.

### What worked

The codebase scan was quick -- `grep -rn "GetResults\\|GetAds\\b"` immediately showed me that the `db.GetResults` / `db.GetAds` methods survive the cleanup because they're also used in `http/search.go`. That closed the "removing the gather renders a method dead" question the lead flagged as a surprise-worthy area: the answer is *no*, those methods stay.

### What didn't work

I'm blocked on the four verbatim curated lists. The prompt design summary tells me the *counts* (100 archetypes, 6 reliability signals, 40 product categories, 15 pitch angles), but the actual string contents live in the task #3 description which I can't access:

- `TaskGet` / `TaskUpdate` tools are not available in this environment (the harness only exposes a deferred-tool registry and none of the task-list tools are in it).
- The worktree contains no task-list file; the scope boundary prevents me from looking outside the worktree for one.
- The prompt text I received contains the design summary (pasted from somewhere upstream) but not the verbatim lists.

I considered inventing the lists myself, but the lead explicitly wrote "curated dimensions" and "the full spec ... includes all four curated lists verbatim". Inventing 161 strings would replace the lead's curated work with mine, which is the opposite of what was asked. Per the lead's standing instruction -- "If you hit ambiguity you can't resolve, leave a comment on task #3 and stop. The lead will clarify." -- I'm stopping here, even though I can't leave a task comment either.

### What I learned

- `math/rand/v2`'s global functions (`rand.IntN`, `rand.N`) are safe for concurrent use without needing a source, per stdlib docs. That would have let me skip the `*rand.Rand` plumbing entirely if I hadn't wanted deterministic tests.
- The jobs layer's `existing`-gather pattern is a ~8-line block that has a near-identical twin in `jobs/search.go` and `jobs/ads.go`. Both disappear in one commit. Clean, symmetric change.
- `GetResults` and `GetAds` at the DB layer survive the cleanup because HTTP handlers read them for rendering. So there is no DB-surface shrinkage to review -- only the per-job gather calls go away.

### What was tricky

Just the missing lists, covered above. Everything else is mechanical and well-scoped.

### What warrants review

Nothing ready to review yet -- the implementation stops at the red test file. Once I have the curated lists, the next commits will be:

1. `llm/llm.go`: four curated-list `var` blocks, two roll types (`resultRolls`, `adRolls`), two roll funcs (`rollResultConstraints`, `rollAdConstraints`) that accept `*rand.Rand` (nil -> global `rand/v2`), two `constraintsBlock()` methods, updated `resultSystemPrompt` / `adSystemPrompt` (new "House style: deadpan..." paragraph, old "Tone should vary wildly..." line removed), and `GenerateResult` / `GenerateAd` signatures that drop the trailing `existing*` slice and call the roller + constraints block.
2. `jobs/search.go`: delete the `existing, err := db.GetResults(...)` / `titles := ...` block, remove `GetResults` from the `generateResultDB` interface, update the `resultGenerator` interface signature, drop the `titles` argument at the call site.
3. `jobs/ads.go`: symmetric to the above for sponsors.

Everything downstream of that (verification commands, commit hygiene, task status) is on the "finishing up" list.

### Future work

Once unblocked:
- Run `go mod tidy && go build ./... && go vet ./... && go test ./...` and paste verification output into this diary.
- Mark task #3 completed with the files-touched list.

### Files touched so far

- `/llm/llm_test.go` (new, red; will go green once the implementation lands)
- `/docs/diary/2026-04-23-randomized-prompts-builder.md` (this file)

No production code changed yet.

## Step 2: Unblocked with verbatim lists, green, verified

### Prompt Context

**Verbatim prompt:**
"Good catch — my mistake, the lists should have been in your original brief. Here they are verbatim. Resume and finish the job.

## Archetype list for results — 100 items, exactly this order

```go
var resultArchetypes = []string{
    // Reference / encyclopedic (5)
    [...100 items across 14 section comments...]
}
```

Total: 5+7+10+8+8+7+8+8+7+7+7+8+5+5 = 100.

## Reliability signal list for results — 6 items

```go
var reliabilitySignals = []string{
    "cites 47 fake academic references",
    "screenshots from a deleted thread as sole evidence",
    "anonymous insider email",
    "\"my uncle told me\"-tier sourcing",
    "zero sources, total confidence",
    "peer-reviewed claim (fake citation included)",
}
```

## Product category list for ads — 40 items

```go
var adProductCategories = []string{ [...40 items...] }
```

## Pitch angle list for ads — 15 items

```go
var adPitchAngles = []string{ [...15 items...] }
```

Mind the single quotes inside those strings — Go strings with double-quote delimiters tolerate them fine, but the ones with escaped inner double-quotes (`"my uncle told me"` in reliabilitySignals) need `\"` escaping. I wrote that one in the form ready to paste.

## House style text for `resultSystemPrompt`

REPLACE this line:
```
Tone should vary wildly across positions for the same query - a straight-faced fake wiki article, a paranoid forum rant, an overt parody, a product listing, a local news page, an academic paper, a recipe blog, a conspiracy site, a press release, a personal homepage, etc. Pick something that hasn't been used already for this query, unless the query is very narrow.
```

WITH:
```
House style: deadpan, understated, absurdity played straight. Never wink at the reader. No jokes-as-jokes. The Constraints block in each user prompt specifies the archetype, weirdness level, and reliability signal -- honor them strictly.
```

## House style text for `adSystemPrompt`

REPLACE:
```
Tone should vary wildly across positions for the same query - a generic product listing, a questionable insurance pitch, a hyper-niche SaaS, a local service, a book, a newsletter, a course, a gadget, a supplement, an app. Pick something that hasn't been used already for this query.
```

WITH:
```
House style: deadpan marketing copy, absurdity played straight. Never wink at the reader. No jokes-as-jokes. The Constraints block in each user prompt specifies the product category, pitch angle, and weirdness level -- honor them strictly.
```

## Constraints block format

Results — in `GenerateResult`, replace the `avoid`-gathering block and the `avoid` substitution in the user-prompt template with a Constraints block appended after the \"Position\" line:

```
Constraints:
- Archetype: %s
- Weirdness: %d/10 (1 = plausibly real, 10 = committed-to-the-bit absurd, played straight)
- Reliability signal: %s

Honor these constraints strictly. The archetype defines the page format. The weirdness number controls how absurd the content may get; keep delivery deadpan regardless.
```

Ads — in `GenerateAd`, same treatment:

```
Constraints:
- Product category: %s
- Pitch angle: %s
- Weirdness: %d/10 (1 = real-feeling DTC product, 10 = committed-to-the-bit absurd product concept, pitched with a straight face)

Honor these constraints strictly. The product category defines what's being sold. The pitch angle is the hook. The weirdness number controls how absurd the product may get; the ad copy stays deadpan and marketing-shaped regardless.
```

## Go on

Your existing red test file and diary entry stay — just extend them as you implement. `math/rand/v2` is stdlib, no new deps. Finish with `go mod tidy && go build ./... && go vet ./... && go test ./...`, then mark task #3 completed.

Good catch on `db.GetResults` / `db.GetAds` still being live on the HTTP path — that confirms the DB surface doesn't shrink, just the per-job gather does."

(Full list contents are in-file at `/llm/constraints.go`; I've elided the bulk here to keep the diary readable.)

**Interpretation:** Drop in the lists exactly as written, rewrite the two system prompts per the REPLACE/WITH specs, emit the Constraints blocks verbatim (same wording, same order of bullets), and finish with the build/vet/test triplet. Keep the red test file in place — it will go green.

**Inferred intent:** The lead's lists are the curated data; my job is the plumbing. The Constraints block text is load-bearing (the system prompt explicitly refers to "the archetype, weirdness level, and reliability signal" / "the product category, pitch angle, and weirdness level" in that order), so I need to honor the exact wording and ordering rather than paraphrase.

### What I did

1. Added `math/rand/v2` to `/llm/llm.go` imports.
2. Rewrote `resultSystemPrompt` and `adSystemPrompt` per the REPLACE/WITH blocks.
3. Dropped the `existingTitles` parameter from `Client.GenerateResult`, replaced the avoid-gather with `roll := rollResultConstraints(nil)`, and stitched `roll.constraintsBlock()` into the user prompt after the "Position" line.
4. Same treatment for `Client.GenerateAd` with `existingSponsors` -> `rollAdConstraints(nil)` -> `roll.constraintsBlock()`.
5. Created `/llm/constraints.go` for the four curated lists, the two roll types (`resultRolls` / `adRolls`), the two `constraintsBlock()` methods, and two small picker helpers (`pickString` / `pickWeirdness`). Lists go in `constraints.go` rather than `llm.go` to keep `llm.go` from ballooning past 400 lines for what is mostly data.
6. Removed the `math/rand/v2` import from `/llm/llm.go` once I realized the rollers all live in `constraints.go`. The compiler's unused-import error caught this on first build — cleanest possible feedback loop.
7. In `/jobs/search.go`: removed `GetResults` from `generateResultDB`, removed `existingTitles` from `resultGenerator.GenerateResult`, deleted the `existing, err := db.GetResults(...)` + `titles := ...` block, updated the call site to `gen.GenerateResult(llmCtx, q.Text, jd.Position)`, added a doc-comment line noting that per-call randomization replaces the old dedup.
8. Same symmetric treatment in `/jobs/ads.go` for `GetAds` / `existingSponsors` / sponsors.
9. Ran the verification triplet:

   ```
   $ go mod tidy
   (no output)
   $ go build ./...
   (no output)
   $ go vet ./...
   (no output)
   $ go test ./...
   ?   app/cmd/app [no test files]
   ok  app/html    0.433s
   ok  app/http    0.488s
   ?   app/jobs    [no test files]
   ok  app/llm     (cached)
   ok  app/model   0.777s
   ?   app/service [no test files]
   ?   app/servicetest [no test files]
   ok  app/sqlite  1.530s
   ok  app/sqlitetest  1.146s
   ```

10. Reran with `-shuffle on` as the Go skill prefers:

    ```
    $ go test -shuffle on ./...
    ?   app/cmd/app [no test files]
    ok  app/html    0.260s
    ok  app/http    0.924s
    ?   app/jobs    [no test files]
    ok  app/llm     0.594s
    ok  app/model   0.252s
    ?   app/service [no test files]
    ?   app/servicetest [no test files]
    ok  app/sqlite  1.471s
    ok  app/sqlitetest  1.183s
    ```

11. Linted the two touched packages:

    ```
    $ golangci-lint run ./llm/ ./jobs/
    0 issues.
    ```

12. goimports reports no formatting drift:

    ```
    $ goimports -l -local app ./llm/ ./jobs/
    (no output)
    ```

### Why

Splitting the curated data and the helper functions into `/llm/constraints.go` keeps `/llm/llm.go` focused on the HTTP/gai surface and makes the data surface easier to audit at a glance -- a reviewer looking at "are the lists right?" reads one file, someone looking at "is the prompt wired correctly?" reads another. Same package, no visibility gymnastics, no circular imports.

The `pickString` / `pickWeirdness` helpers are deliberately tiny and private. They exist only so the two `rollXConstraints` funcs don't repeat the `r == nil` fallback branch four times. I kept them separate from the rollers rather than inlining with a generic, because two concrete helpers read cleaner than `pickOne[T any](...)` when there are exactly two call sites and two types.

The roll structs take a `*rand.Rand` parameter (with nil -> global `rand/v2`) purely so the tests can seed determinism. Production callers always pass nil. This costs a two-line branch and buys fully deterministic test assertions including equality-under-same-seed, which I used to sanity-check that my PCG-seeded test wasn't accidentally testing the default global's state.

### What worked

- The red test file I wrote in Step 1 fit the implementation on the first try -- no tweaks to the test to make it pass. Every assertion (list sizes, no dups, deterministic rolls, constraints-block content, system-prompt house-style mentions) exercised a real code path as intended.
- `go build ./...` caught the leftover unused `math/rand/v2` import in `llm.go` immediately after I added the rollers to `constraints.go`. One command, one obvious fix.
- The jobs-file cleanup was near-mechanical. Two parallel 12-line gather blocks deleted, two interface methods dropped, two interface signatures shortened, two call sites simplified. No surprises.
- `golangci-lint run` came back clean on both `./llm/` and `./jobs/` the first time I ran it.

### What didn't work

Nothing broke in Step 2. One minor false alarm: after the first `go test ./llm/` I saw the unused-import error and briefly wondered if I'd added a spurious import somewhere weird; a quick re-read of `llm.go` confirmed it was the top-level import I'd added in Step 1's plan-ahead work, which the actual implementation (having moved to `constraints.go`) no longer needed.

### What I learned

- `math/rand/v2`'s global functions (`rand.IntN`) are documented as safe for concurrent use without locking -- perfect for this job, where each of up to 13 fan-out calls (10 results + 3 ads) will roll independently under the same parent request.
- The `pickString` / `pickWeirdness` pair turns out cleaner than a generic `pickOne[T any]` at this scale. Keeping it concrete surfaces the `int` vs `string` return types at the declaration site instead of at the call site.
- When the lead says "the Constraints block ... specifies the archetype, weirdness level, and reliability signal -- honor them strictly", the *system prompt itself* has to refer to those three names in that order; if I shuffle the bullets in `constraintsBlock()` I break the system prompt's back-reference. I kept the bullet order matching the prose exactly.

### What was tricky

The `"my uncle told me"`-tier entry in `reliabilitySignals` needed `\"` escaping because the outer string is double-quoted. The lead called this out in the prompt ("I wrote that one in the form ready to paste"), so I copied it verbatim rather than retyping. Worth testing that it compiles, which it does (the `TestReliabilitySignals` subtest for "no empty entries" indirectly confirms parse correctness).

The test for "the rolled weirdness is in [1, 10]" had to match the implementation's `+ 1` offset. I used `rand.IntN(10) + 1` rather than `rand.IntN(11)` so 0 is never produced, and the test asserts `>= 1 && <= 10`.

### What warrants review

- `/llm/constraints.go`: the four lists are the lead's own data, reproduced verbatim. Worth a quick re-scan against the original prompt for any copy-paste typos (especially the escape sequence in the `"my uncle told me"` entry and the single-quotes inside the pitch-angle parentheticals, which are fine inside Go double-quoted strings).
- `/llm/llm.go:95-100` and `/llm/llm.go:212-217`: the two `Generate*` functions' user-prompt format. Confirm the Constraints block lands on its own "line" separated by blank lines on both sides, and that "Position" / "Ad position" still reads first.
- `/llm/llm.go:88` and `/llm/llm.go:204`: the two new House-style lines, which are single-line replacements for multi-line "Tone should vary wildly" blocks. Check that the raw backtick-delimited string still looks right and nothing stray got left behind.
- `/jobs/search.go` and `/jobs/ads.go`: the interface surface shrank (`GetResults`, `GetAds`, the extra slice param on `Generate*`). Confirm no other file in the project implicitly depended on the old interface shape. The `go build ./...` pass is the authoritative check here.
- Weirdness distribution: I use `rand.IntN(10) + 1` which is uniform over 1-10. The spec didn't ask for a skew (e.g., more 1-3s and fewer 8-10s), so I kept it uniform. Flag if you want skew.

### Future work

- If weirdness should skew low (most results plausible-real, a few absurd), the fix is a one-line change in `pickWeirdness`. I'd use something like `int(math.Ceil(math.Sqrt(rand.Float64()) * 10))` for a √-skew, or a small lookup table. Not in scope for this change.
- A `TestGenerateResult` / `TestGenerateAd` end-to-end test that exercises the whole LLM path would be nice, but it would need a fake `gai.ChatCompleter` and is a bigger change than warranted here.
- The `rollXConstraints(r)` helpers' `nil`-means-global branch exists only for test determinism. If the Client ever needs its own RNG (e.g., per-client seeding for reproducibility in traces), it can grow a `rand *rand.Rand` field and pass it in; today the package-level global is fine.

### Files touched

- `/llm/llm.go`: dropped `existingTitles` / `existingSponsors` from `GenerateResult` / `GenerateAd`, rewrote the two system prompts per the REPLACE/WITH specs, swapped in roll + constraints-block.
- `/llm/constraints.go` (new): four curated lists (100 / 6 / 40 / 15), two roll types with a `constraintsBlock()` method each, two roll funcs (`rollResultConstraints` / `rollAdConstraints`), two small picker helpers.
- `/llm/llm_test.go` (new, from Step 1; now green): internal-package tests for list sizes / no-dups / no-empty, deterministic rollers, constraint-block content, system-prompt house-style.
- `/jobs/search.go`: dropped `GetResults` from `generateResultDB`, dropped `existingTitles` from `resultGenerator.GenerateResult`, deleted the per-job gather block.
- `/jobs/ads.go`: symmetric cleanup for `GetAds` / `existingSponsors`.
- `/docs/diary/2026-04-23-randomized-prompts-builder.md` (this file).

Verification: `go build` clean, `go vet` clean, `go test ./...` and `go test -shuffle on ./...` both pass across all seven packages with tests, `golangci-lint run ./llm/ ./jobs/` reports `0 issues.`, `goimports` reports no drift.

Task #3 ready for QA. No commit and no PR per instructions. The lead takes the live smoke test from here.
