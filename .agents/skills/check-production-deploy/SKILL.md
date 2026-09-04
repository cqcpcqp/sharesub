---
name: check-production-deploy
description: Perform the final read-only safety audit before manually running the cqcpcqp/sharesub Deploy production GitHub workflow. Use when deciding whether the current release can cause startup crashes, service unavailability, database corruption or abnormal data, whether required CI and public health services are available, and whether to select "Allow database migrations in this release". Compare the latest successful production workflow commit with the actual main deployment target and the current branch, inspect all intervening changes, run release-appropriate tests, and produce a GO or NO-GO report without triggering deployment or changing production.
---

# Check Production Deploy

Audit a ShareSub production release using repository and GitHub evidence. Treat missing evidence as a blocker; never guess.

## Safety boundaries

- Keep every action read-only with respect to GitHub Actions and production. Never dispatch, rerun, approve, cancel, or edit a workflow.
- Never SSH to production, run `scripts/deploy.sh`, change secrets or environments, operate containers, or read/write the production database.
- Never open a browser. Use `git`, GitHub's read-only REST API, `curl`, and local test commands.
- Do not deploy or select the workflow input for the user. Report the exact choice for the user to make manually.
- Read `AGENTS.md`, `.github/workflows/deploy-production.yml`, `scripts/deploy.sh`, `docs/deployment.md`, and relevant changed code before reaching a conclusion. Repository source is authoritative.

## 1. Collect authoritative commits

Run `.agents/skills/check-production-deploy/scripts/collect-deploy-context.sh` from the repository root. It fetches `origin/main` and tags, then reports:

- the latest successful `Deploy production` workflow run and its commit;
- the exact commit the workflow would deploy (`origin/main`);
- the checked-out branch, local HEAD, and worktree state;
- release tag validity, the target commit's successful push CI/image run, and migration-path changes.

If GitHub access, the successful deployment run, either commit, or the target CI/image run cannot be verified, stop with `NO-GO`. Do not substitute a tag, release, guessed SHA, local reflog, or latest normal CI run for the deployment baseline.

The workflow always checks out `main`; it does not deploy the selected local branch. If the current branch or local HEAD differs from `origin/main`, explain that discrepancy. Use `origin/main` as the deployment target, but issue `NO-GO` when the user appears to expect unmerged or unpushed current-branch work to be deployed.

If baseline and target are identical, report that the recorded release is already deployed and do not recommend another run.

## 2. Review the complete release delta

Set `BASELINE` to the latest successful deployment SHA and `TARGET` to `origin/main`. Inspect, at minimum:

```bash
git log --oneline --decorate "$BASELINE..$TARGET"
git diff --stat "$BASELINE...$TARGET"
git diff --name-status "$BASELINE...$TARGET"
git diff "$BASELINE...$TARGET"
```

Use two-dot diff instead if the baseline is not an ancestor, and flag the non-linear history as a release risk. Review every changed file rather than relying on commit messages or statistics. Consult [risk-review.md](references/risk-review.md) for the required risk surfaces and report rules.

Pay special attention to startup paths, environment/config validation, dependency initialization, health handlers, proxy and routing config, Docker/Compose files, workflow and deployment scripts, API compatibility, concurrency, resource bounds, and code that deletes or rewrites persistent data. Backend response structures and types are fixed; do not propose or reward response fallbacks.

## 3. Decide database migration safety and the workflow checkbox

Use the exact gate implemented by `scripts/deploy.sh`:

```bash
git diff --name-status "$BASELINE" "$TARGET" -- backend/migrations
```

- If this diff is empty, report **Allow database migrations in this release: 不选择（false）**.
- If this diff is non-empty, report **Allow database migrations in this release: 选择（true）** because the deploy script otherwise rejects the release.

The checkbox is permission, not proof of safety. For a non-empty migration diff, inspect every SQL change plus `backend/internal/postgres/store.go` and its tests. Verify ordering, immutability of already-applied files, transaction behavior, locks/table rewrites, constraints against existing rows, uniqueness and foreign-key failures, destructive statements, data transformations, startup duration, old/new binary compatibility, and backup/rollback consequences.

Treat modification, renaming, or deletion of an already-deployed migration as `NO-GO` unless repository evidence proves the migration was never applied and the release strategy explicitly handles it. Treat an unsafe or insufficiently verified new migration as `NO-GO` even though the checkbox answer remains `true`. Never recommend enabling the checkbox merely as a precaution when no migration path changed.

## 4. Verify release gates and basic service availability

Run checks proportional to the delta, including the full repository gate:

```bash
./scripts/verify-version.sh --release
make test
```

Do not skip failures. Add focused tests for changed high-risk packages and migration integration tests when relevant. Do not use a browser.

Verify the GitHub push CI/image run for `TARGET` is completed successfully. Verify the `production` environment exposes the secret names referenced by the workflow when permissions allow; never print secret values. If access is denied, a prior successful deployment is sufficient evidence only when the workflow's referenced secret names and production-environment wiring have not changed since `BASELINE`. Otherwise state the unverified prerequisite and use `NO-GO` unless the user supplies independent evidence.

Check only the public endpoints already defined by repository deployment code:

```bash
test "$(curl -fsS --connect-timeout 10 --max-time 30 https://share.underelay.com/health)" = '{"status":"ok"}'
curl -fsS --connect-timeout 10 --max-time 30 -o /dev/null https://www.underelay.com/health
```

These calls verify current public reachability, not internal PostgreSQL/container health or future release correctness. State that limitation. The production workflow performs its own internal health, configuration, image pull, backup, and post-switch checks.

## 5. Produce the final decision

Lead with exactly one verdict:

- `GO` only when the target is unambiguous, all required evidence and tests pass, current public health passes, no unresolved high-risk finding remains, and any migration is reviewed safe.
- `NO-GO` for any failed or missing critical check, ambiguous deployment intent, unavailable required evidence, unsafe migration, incompatible release, or credible crash/data-loss/outage risk.

Always include:

1. Baseline deployment SHA, run URL/time, target SHA/tag, current branch/HEAD, and whether the baseline is an ancestor.
2. Findings ordered by severity with file/line evidence and operational impact.
3. Separate explicit conclusions for server crash risk, service unavailability risk, database corruption/abnormal-data risk, and current basic-service availability.
4. Tests and remote checks run, with pass/fail and any unverified item.
5. The exact checkbox answer: `选择（true）` or `不选择（false）`, plus the migration files that determine it.
6. Residual risks and a concise manual workflow recommendation.

Do not claim zero risk. Distinguish observed evidence from inference. If the verdict is `NO-GO`, tell the user not to manually run the workflow yet.
