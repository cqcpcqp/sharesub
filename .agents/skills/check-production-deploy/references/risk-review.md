# Production release risk review

Use this checklist for the complete diff between the last successful production workflow commit and `origin/main`.

## Risk surfaces

### Startup and server crash

- Trace API startup through configuration parsing, database connection, automatic migrations, dependency construction, background jobs, listener binding, and health registration.
- Check new required environment variables, invalid defaults, panics/fatal exits, nil assumptions, incompatible serialized state, and architecture/runtime dependency changes.
- Confirm Docker image build inputs and Compose commands still produce the processes and ports expected by health checks.

### Availability

- Inspect health endpoints, Nginx routes, ports, service dependencies, timeouts, streaming/WebSocket behavior, graceful shutdown, connection pools, and resource limits.
- Identify deploy-time locks, long migrations, cache warmup, blocking external calls, unbounded memory/goroutines, and backward-incompatible API behavior.
- Check whether the deployment's rollback behavior applies. Migration releases intentionally do not auto-rollback binaries after a failed health check.

### Database and persistent data

- Treat existing migration filenames as immutable because `schema_migrations.name` marks them applied; editing their SQL does not rerun it on production.
- Verify each new migration succeeds against all valid existing rows and runs atomically under the repository migration runner.
- Flag `DROP`, truncation, lossy type conversion, broad `UPDATE`/`DELETE`, constraint validation, unique index creation, large-table rewrite, long exclusive locks, sequence changes, and changed cascade behavior.
- Check both compatibility directions: new application with old schema during startup and old application with new schema if rollback is needed.
- Verify code and SQL agree on nullability, enum/check values, precision, identifiers, ownership, and transaction boundaries.

### Release machinery and dependencies

- Review changes to workflows, deploy scripts, Compose, Dockerfiles, Nginx, version files/tags, dependency locks, Go/Node versions, image names, volumes, and secrets referenced by name.
- Confirm `TARGET` has a completed successful push run of `CI and images`; a PR-only run does not prove commit-tagged images were published.
- Verify the annotated `v$(VERSION)` tag points exactly to `TARGET` because the deploy workflow enforces it.

## Severity and verdict

- **Critical**: credible data loss/corruption, destructive or irreversible migration error, secret exposure, or certain broad outage. Always `NO-GO`.
- **High**: likely startup failure, missing image/config/secret, unsafe schema compatibility, failed tests/CI/health, or inability to identify baseline/target. Always `NO-GO`.
- **Medium**: plausible degradation with bounded impact or incomplete non-critical verification. Use `NO-GO` until resolved when it affects the deployment path.
- **Low**: limited operational concern that does not invalidate release gates. Report as residual risk.

## Evidence rules

- Cite repository paths and line numbers for code findings.
- Record command names and concise results; do not paste secrets or excessive logs.
- Mark a check `unverified` rather than assuming success.
- A healthy current release does not prove the candidate release is safe.
- Passing tests reduces risk but does not override a concrete incompatibility found in review.

## Required report shape

```text
Verdict: GO | NO-GO

Release identity
- Last successful deploy: SHA, workflow run URL, completed time
- Workflow target: origin/main SHA, annotated version tag
- Current checkout: branch, HEAD, clean/dirty, relationship to target

Findings
- [Critical|High|Medium|Low] evidence -> operational impact

Risk conclusions
- Server crash: low/credible/blocking + reason
- Service unavailable: low/credible/blocking + reason
- Database/data: low/credible/blocking + reason
- Basic services now: available/unavailable/unverified + exact checks

Validation
- Local tests
- GitHub CI/images and production-environment prerequisites
- Public health

Migration input
- Changed migration paths, or none
- Allow database migrations in this release: 选择（true）|不选择（false）

Recommendation
- Manually run the workflow | Do not run it yet
- Residual risks
```
