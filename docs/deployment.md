# ShareSub deployment workflow

## Why the deployment flow changed

The production host currently has 2 CPU cores, about 2 GiB of RAM, and no
swap. It also runs ShareSub, sub2api, PostgreSQL, Redis, WireGuard, and the
Docker daemon. The previous release process built both images on that host.
The frontend image runs `vue-tsc -b && vite build`; together with BuildKit and
the resident services, its memory peak can make this host unresponsive.

Image compilation now belongs to GitHub-hosted runners. Production only pulls
already-built images and switches containers.

## Previous workflow

`make deploy` used to perform this sequence from a developer machine:

1. Require a clean, pushed `main` branch and read `.deployed-commit` remotely.
2. Compare `backend/migrations` and require explicit migration approval.
3. Run the complete backend and frontend test/build suite locally.
4. Archive the commit and synchronize the source tree to production with
   `rsync`.
5. Validate `.env`, Docker Compose, and the installed Nginx configuration.
6. Tag the current mutable `latest` images as `previous`.
7. Run `docker compose build api` on production.
8. Run `docker compose build web` on production.
9. Back up PostgreSQL and validate the dump.
10. Recreate the containers, wait for health, write `.deployed-commit`, and
    verify public endpoints.

The service was not switched before both builds and the backup succeeded, but
the builds competed with production workloads for the same CPU and memory.
Mutable `latest`/`previous` tags also made the exact release identity less
obvious.

## Improved workflow

### Continuous integration and image publishing

Every pull request runs backend tests plus frontend tests, type checking, and a
production build. Every push to `main` runs the same checks, then Buildx builds
the API and Web images in parallel on GitHub-hosted runners.

Successful images are published as:

```text
ghcr.io/cqcpcqp/sharesub-api:<full-git-sha>
ghcr.io/cqcpcqp/sharesub-web:<full-git-sha>
```

The `main` tag is also published for convenient first-time installation, but
production releases always use the immutable full SHA tags.

### Production release

The `Deploy production` workflow is manually dispatched. It uses the GitHub
`production` Environment so repository owners can add required reviewers and
an approval gate. It checks out the current `main` with full history and runs:

1. The complete test suite again as a release gate.
2. The existing migration diff check against production's
   `.deployed-commit`.
3. Source/config synchronization and production configuration validation.
4. `docker compose pull api web` for the exact commit SHA images.
5. Local rollback tags for the image IDs of the currently running containers.
6. A verified PostgreSQL custom-format backup.
7. `docker compose up -d --no-build` with the exact SHA images.
8. Internal and public health checks, followed by writing the deployed commit.

The server never runs Go, Node.js, TypeScript, Vite, or Docker build steps.
Pull or backup failures leave running containers untouched. A health failure
without migrations recreates the old containers from the locally preserved
image IDs. A migration release is not automatically rolled back because the
old binary may not be compatible with the migrated database; its verified
backup path is reported instead.

## One-time GitHub configuration

### 1. Package access

The workflow publishes with its built-in `GITHUB_TOKEN` and `packages: write`.
If organization policy overrides workflow permissions, allow GitHub Actions to
write packages for this repository.

The simplest server configuration is to make both GHCR packages public. If
they remain private, create a GitHub token with only `read:packages` and log in
once on the production server using an account that can read the packages:

```bash
docker login ghcr.io -u cqcpcqp
```

Enter the token at the password prompt. Do not put the token in `.env`, shell
history, the repository, or an Actions log. Docker stores the credential for
future `compose pull` operations.

### 2. Production Environment and secrets

Create a GitHub Environment named `production`. Adding a required reviewer is
recommended so dispatching a workflow is not sufficient by itself to change
production.

Add these Environment secrets:

| Secret | Value |
| --- | --- |
| `PRODUCTION_SSH_HOST` | Public hostname or IP of the production server |
| `PRODUCTION_SSH_USER` | Restricted deployment SSH user |
| `PRODUCTION_SSH_PRIVATE_KEY` | Private key dedicated to Actions deployment |
| `PRODUCTION_SSH_KNOWN_HOSTS` | Verified `known_hosts` entry for that host |

Install the matching public key in the deployment user's
`~/.ssh/authorized_keys`. Generate `PRODUCTION_SSH_KNOWN_HOSTS` from a trusted
network and compare its fingerprint with the server before storing it; do not
blindly trust an unverified `ssh-keyscan` result inside the workflow.

The server must allow the deployment user to run Docker without interactive
sudo, and `/home/cqcpcqp/share2api` plus its `.env` must already exist. The
current script defaults to that directory; `SHARESUB_DEPLOY_DIR` can override
it when invoking the script outside Actions.

### 3. First release

1. Push `main` and wait for `CI and images` to finish successfully.
2. Confirm both SHA-tagged packages exist in GHCR.
3. Manually run `Deploy production`.
4. Enable `allow_migrations` only when the release intentionally changes
   `backend/migrations` and the backup/compatibility implications are accepted.
5. Confirm the workflow reports the commit, backup path, container state, and
   healthy public response.

Do not dispatch production while the image workflow for the same commit is
still running. An early dispatch fails during `compose pull` before backup or
container switching and can be retried safely after images are available.

## Resource sizing

### GitHub-hosted runner

No production-server resize is required for GitHub-hosted builds. CPU and RAM
belong to an ephemeral GitHub runner and disappear after the job. If the
standard runner later becomes too slow or runs out of memory, select a larger
GitHub-hosted runner in repository/organization Actions settings (availability
and billing depend on the GitHub plan) and change `runs-on` to its configured
label.

### Self-hosted runner

Do not install a self-hosted build runner on the production machine; that
would recreate the original resource contention and also expands the security
impact of workflow code. A separate runner should have at least 4 CPU cores,
8 GiB RAM, swap, and 30–50 GiB free Docker space. For more predictable parallel
API/Web builds, 4 cores and 16 GiB RAM is the comfortable target.

### Production server

After removing builds, 2 cores and 2 GiB can run a small workload, but this
host also carries several unrelated services and currently has no swap. The
recommended minimum is:

- 2 CPU cores and 4 GiB RAM;
- 2–4 GiB encrypted swap as an emergency buffer, not as normal working memory;
- existing disk size is adequate while usage remains low, with image and
  backup retention monitored.

For traffic growth or more predictable headroom, choose 4 cores and 8 GiB RAM.
CPU is secondary to memory for the incident that prompted this change.

The provider-neutral resize procedure is: verify off-host backups, take a
snapshot, stop the VM if the provider requires it, select the larger instance
shape, start it, and verify networking, Docker, all containers, disk mounts,
memory, and public health. Provider-specific controls and downtime behavior
must be confirmed in that provider's console before resizing.

Even after resizing, keep builds in GitHub Actions. More production capacity
should become safety margin for live traffic, not build capacity.
