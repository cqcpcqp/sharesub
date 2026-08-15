---
name: review-sub2api-openai-changes
description: Compare sub2api and share2api OpenAI forwarding implementations end to end, identify verified behavioral and architectural differences, and recommend which sub2api capabilities share2api should adopt or adapt. Use for full current-state comparisons, gap analyses, alignment reviews, or reviews of newly pulled sub2api OpenAI proxy, account protection, abuse prevention, rate-control, and reliability changes. Perform analysis only unless the user separately requests implementation.
---

# Review sub2api OpenAI Changes

Compare the two implementations from entrypoint to upstream and produce an evidence-backed alignment review. Recommend only changes that fit `share2api`; do not equate difference with deficiency.

## Fixed constraints

- Treat `/Users/cqcpcqp/Projects/sub2api` as the reference repository and `/Users/cqcpcqp/Projects/share2api` as the target repository.
- Read and obey `AGENTS.md` files that apply in both repositories before running repository commands or inspecting code.
- Never open a browser or use browser-based testing.
- Never guess an endpoint, request shape, response field, field type, business meaning, or architectural correspondence. Trace the actual code and tests.
- Treat backend response structures and field types as fixed. Never propose or implement response fallbacks.
- Keep the review read-only unless the user explicitly requests repository synchronization. Do not edit application code, commit, push, tag, deploy, or change production.
- Separate verified facts, engineering inference, and questions requiring human confirmation.

## 1. Select the comparison mode

Choose exactly one mode from the user's wording:

- **Full current-state comparison (default):** Use when asked to compare functionality, logic, gaps, or alignment without limiting the request to a recent update. Compare the complete OpenAI forwarding paths at each repository's current local `HEAD`.
- **Pull-delta comparison:** Use only when the user explicitly asks about the latest pull, newly synchronized changes, or changes introduced by an update. Analyze only the exact range introduced by that synchronization.

State the selected mode before presenting results. Do not silently substitute a date range, arbitrary commit count, tag, or pull request.

### Full current-state boundary

1. Record each repository's branch, `HEAD`, upstream, and worktree status.
2. Do not fetch or pull merely because a full comparison was requested. If the user asks for the latest remote state, follow the safe synchronization procedure below first.
3. Use the two recorded `HEAD` commits as the immutable primary comparison boundary. Derive committed facts from those revisions with Git-aware commands rather than silently treating dirty filesystem content as committed behavior.
4. If relevant tracked or untracked application files differ from `HEAD`, inspect them only as a separate **worktree overlay**. Label every overlay fact as uncommitted and keep it out of the committed capability counts and recommendations unless the user explicitly asks to compare working-tree implementations.
5. Continue read-only analysis when dirty state is unrelated to the OpenAI path. Disclose all dirty state. Never modify, stash, discard, reset, or clean user work.

### Safe synchronization and pull-delta boundary

1. Inspect the current sub2api branch, upstream, `HEAD`, and worktree status.
2. If tracked or untracked changes exist, stop before updating and report them. Never stash, discard, reset, clean, or overwrite user work.
3. If detached or missing an upstream, ask which remote branch is authoritative.
4. Record `PRE_PULL_HEAD` immediately before updating.
5. Fetch remotes and update only with `git pull --ff-only` on the existing tracking branch. Never merge, rebase, or switch branches implicitly.
6. Record `POST_PULL_HEAD`, branch, upstream, and sync status. Verify the final worktree is clean.
7. Define the pull delta exclusively as `PRE_PULL_HEAD..POST_PULL_HEAD`.

If both commits are equal in pull-delta mode, report that no new commits were introduced and stop. If synchronization fails, report the exact failure and stop; do not widen the range.

## 2. Build an implementation inventory

Use routes and protocol entrypoints as the primary index. Trace each path through handlers, services, account selection, stores/models, upstream clients, response handling, accounting, and tests. Search symbols and call sites, not filenames or commit messages alone.

Define scope by actual forwarding behavior:

- Include every route or background flow that can select an OpenAI/ChatGPT account or forward to an OpenAI/ChatGPT upstream, including asynchronous image or batch orchestration when applicable.
- Include shared OpenAI-compatible infrastructure only where it changes an included OpenAI path.
- Exclude routes that merely use an OpenAI-shaped API but exclusively select Grok or another non-OpenAI platform. List shared or visually similar exclusions so they are not mistaken for missed coverage.
- If platform selection is dynamic or unclear after tracing code and configuration, classify the path as `scope unresolved` and ask the user instead of guessing.

Inventory verified behavior in these areas:

1. **Ingress and protocol surface:** registered routes, HTTP methods, Responses/Chat/Images/Models endpoints, compact or auxiliary endpoints, WebSocket paths, and streaming modes.
2. **Admission and identity:** API authentication, client/version checks, request identity, model eligibility, account health, quotas, concurrency, rate limits, cooldowns, and abuse controls.
3. **Request preparation:** body parsing and transformation, allowed/removed fields, instructions, model mapping, tool handling, continuation state, headers, authentication, cache keys, and protocol fingerprints.
4. **Routing and upstream selection:** account scheduling, sticky or session routing, proxy selection, endpoint selection, fallback between eligible upstreams when explicitly implemented, retries, and cancellation.
5. **Response lifecycle:** status/error propagation, SSE or WebSocket framing, incomplete streams, upstream error classification, retry decisions, account-state transitions, and client disconnects.
6. **Accounting and operations:** token/usage extraction, billing, reconciliation, metrics, logs, tracing, operator visibility, and recovery behavior.
7. **Supporting lifecycle:** OAuth/token refresh, account tests, model discovery, configuration, persistence, migrations, and background maintenance that materially affect forwarding.

Treat the list as discovery guidance, not proof that a feature exists. Record explicit absence only after checking the expected entrypoints, symbols, configuration, and tests.

Maintain an audit ledger for completeness. Map every registered in-scope route or protocol to its handler, service, upstream target, account-selection path, response path, and nearest tests. Mark a missing layer or test explicitly; leave no discovered in-scope route unexplained. Use the completed ledger to derive capability counts and report scope.

For pull-delta mode, first identify relevant commits and changed paths inside the exact range, then trace every changed behavior through the same inventory areas. Capture commit SHA, date, subject, changed symbols, before/after behavior, tests, and architecture-specific dependencies.

## 3. Compare behavior, not names

Create a working matrix with one row per verified capability or decision point. For each row:

1. Cite the sub2api route, symbols, surrounding implementation, and tests.
2. Locate the corresponding share2api path independently from its routes and call graph; do not assume similarly named files are equivalent.
3. Classify the result as:
   - `equivalent`
   - `share2api partial`
   - `sub2api only`
   - `share2api only`
   - `conflicting semantics`
   - `not comparable`
4. Explain the observable behavioral difference and the evidence supporting it.
5. Identify dependencies on schema, configuration, account state, deployment topology, or operations. Never claim a migration is required without locating the persisted data change.
6. Mark uncertain business intent as a user question instead of inventing a recommendation.

Do not recommend replacing a share2api implementation merely because sub2api uses a different abstraction. Prefer share2api's existing architecture and reuse its routes, services, repositories, state model, and conventions.

## 4. Form recommendations

Recommend a candidate only when all of the following are established:

- sub2api implements the behavior and the evidence is cited;
- share2api lacks it, implements only part of it, or has a concrete reliability/security/compatibility gap;
- the benefit to share2api is specific and plausible;
- an adaptation path fits share2api's architecture;
- compatibility, configuration, schema, testing, and operational risks are described;
- no backend-response fallback is introduced.

Use one disposition:

- `adopt` — behavior transfers with little semantic change;
- `adapt` — retain the idea but implement it through share2api abstractions;
- `investigate` — evidence shows a possible benefit but business or production facts are missing;
- `do not adopt` — already covered, irrelevant, harmful, or inseparable from sub2api-specific assumptions.

Prioritize by expected impact and evidence confidence: `P0` for an immediate severe correctness/security risk, `P1` for high-value reliability or compatibility gaps, `P2` for useful improvements, and `P3` for optional refinement. Do not manufacture a P0 or P1 item.

## 5. Validate the analysis

- Verify every cited commit, path, line, and symbol with Git and source commands.
- Read relevant implementations and tests on both sides; never infer behavior from names alone.
- Prefer `git grep`, `rg`, `git show`, `git diff`, `git log`, and focused language tests.
- Use static or command-line checks only. Never open a browser.
- Run focused, non-mutating tests only when they materially confirm a claim and are proportionate to an analysis task.
- Do not claim a test passed unless it was run. List skipped tests and the reason.
- Before finalizing, check that each recommended item has evidence from both repositories and that rejected or already-covered features are not presented as gaps.
- Check the audit ledger against route registration a second time. For every in-scope protocol area, either run the nearest focused tests or state why execution was unnecessary or impractical for the claims made.

## 6. Report for user review

Lead with:

- selected mode;
- repository branch/commit boundaries and dirty-state disclosures;
- for pull-delta mode, the exact `PRE_PULL_HEAD..POST_PULL_HEAD` range and synchronization result;
- scope covered and validation performed.
- whether a separate uncommitted worktree overlay was inspected.

Present recommendations ordered by expected value:

| Priority | Candidate | sub2api evidence | share2api current state | Behavioral gap | Expected benefit | Adaptation and risks | Recommendation |
| --- | --- | --- | --- | --- | --- | --- | --- |

Use local clickable file citations with line numbers and cite relevant commits when history is part of the evidence. Keep claims at symbol level when line numbers may shift.

After the table, include:

1. **Capability coverage summary** — compact counts or grouped matrix of equivalent, partial, unique, conflicting, and not-comparable behavior.
2. **Already covered in share2api** — relevant sub2api capabilities requiring no action.
3. **Share2api-only strengths** — target behaviors that should be preserved during alignment.
4. **Not suitable for share2api** — rejected ideas with evidence-based reasons.
5. **Questions for the user** — business or operational decisions required before implementation.
6. **Suggested next batch** — a small dependency-aware group for user approval.
7. **Validation notes** — tests run, results, and material checks skipped.

Stop after the report. Do not implement candidates until the user selects them or explicitly requests implementation.
