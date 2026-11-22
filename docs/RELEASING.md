# Releasing

**Every push to `master` ships a version.** The pipeline is
[`.github/workflows/release.yml`](../.github/workflows/release.yml).

## Versioning

1. Run the full verify gate (format, vet, `go test -race`, type-check, lint,
   Vitest). A push that breaks a check publishes nothing.
2. Compute the next semver version: a patch bump per push, `[minor]` or
   `[major]` in the head commit message bumps those components, and the first
   release is `v0.1.0`. Pushing a `v*` tag publishes exactly that version;
   `workflow_dispatch` accepts an explicit version.
3. Publish one immutable GitHub Release `vX.Y.Z` with all archives and
   `SHA256SUMS`. There is deliberately no moving `latest` Release: GitHub
   badges the newest semver release as "Latest" by itself.

Runs queue (`cancel-in-progress: false`), so rapid pushes each get their own
version.

## Native archives

Each archive contains one self-contained executable (embedded UI, pure-Go
SQLite) plus `README.md`, `LICENSE` and `AGENTS.md`:

| OS      | Architectures                            |
| ------- | ---------------------------------------- |
| Linux   | amd64, arm64, 386, armv7, ppc64le, s390x |
| macOS   | amd64, arm64                             |
| Windows | amd64, arm64                             |

`SHA256SUMS` is verified by the workflow before the Release is created.

## Container images

There are two image repositories because Linux and Windows containers use
different operating systems and hosts:

| Repository                       | Platform          | Built on      |
| -------------------------------- | ----------------- | ------------- |
| `ghcr.io/<owner>/splash`         | 5 Linux platforms | `ubuntu-latest` |
| `ghcr.io/<owner>/splash-windows` | `windows/amd64`   | `windows-2022`  |

With Docker Hub configured, the same pair appears under
`docker.io/<user>/splash` and `docker.io/<user>/splash-windows`.

- Exactly **two tags per repository**: the immutable exact version (`1.2.3`)
  and the moving `latest`. No major/minor aliases, no commit-sha tags.
- Linux architectures: `linux/amd64`, `linux/arm64`, `linux/arm/v7`,
  `linux/ppc64le`, `linux/s390x`. `linux/386` is a native archive only: the
  distroless runtime does not publish a 386 variant.
- Windows: `windows/amd64`, based on Windows Server 2022 Nano Server and built
  on a Windows runner. It runs as the default Windows container account
  (`ContainerAdministrator`) so the data volume is writable, and only on a
  matching Windows container host. Windows and macOS native binaries remain
  available in the Release.

The Linux build stages are pinned to `$BUILDPLATFORM` and cross-compile, so
multi-arch builds need no QEMU. CI builds every published architecture, plus
the Windows image and a Windows-container health check, without pushing on each
pull request; a base image or Dockerfile that breaks a platform fails there
rather than on release day.

### Existing tags on a registry

Changing the workflow stops creating old aliases (`X.Y`, `X`, `sha-*`), but it
cannot delete tags that already exist on the registries. Remove the old tags
once in the GHCR package settings and Docker Hub tag list — for both `splash`
and `splash-windows` if applicable. After that, each release adds only `latest`
and the exact version to each repository.

## Secrets

| Secret               | Required  | Purpose                                                                 |
| -------------------- | --------- | ----------------------------------------------------------------------- |
| `GITHUB_TOKEN`       | automatic | Tag push, Release upload, GHCR push (`contents: write`, `packages: write`) |
| `GHCR_TOKEN`         | optional  | PAT with `write:packages`; overrides `GITHUB_TOKEN` for the GHCR login  |
| `DOCKERHUB_USERNAME` | optional  | Docker Hub namespace; omit to publish to GHCR only                      |
| `DOCKERHUB_TOKEN`    | optional  | Docker Hub access token (not the account password)                      |
| `VPS_SSH_HOST`       | optional  | VPS hostname/IP; with `VPS_SSH_PASSWORD` enables the post-release deploy |
| `VPS_SSH_PORT`       | optional  | SSH port (default 22)                                                   |
| `VPS_SSH_USER`       | optional  | SSH user                                                                |
| `VPS_SSH_PASSWORD`   | optional  | Runs `bash /ga/script/deploy-splash.sh` on the VPS                      |

The Docker Hub half and the VPS deploy are skipped cleanly when their secrets
are absent.

## Troubleshooting: GHCR push denied

The workflow requests `packages: write`, so `permission_denied: write_package`
comes from repository or package settings:

1. **Repository workflow permissions**: Settings → Actions → General →
   *Workflow permissions* → **Read and write permissions** → Save.
2. **Package Actions access**: open `ghcr.io/<owner>/splash` → Package
   settings → *Manage Actions access* → make sure this repository has the
   **Write** role. Alternatively delete an empty package and let the next run
   recreate it.
3. **Or bypass both**: create a classic PAT with `write:packages` and
   `read:packages`, store it as `GHCR_TOKEN`, and re-run the failed jobs.

After fixing, use **Re-run failed jobs** on the failed run — no new commit or
version bump is needed.

## Doing it locally

```bash
SPLASH_VERSION=v1.2.3 node scripts/build.mjs release
ls dist/            # one archive per platform + SHA256SUMS
(cd dist && sha256sum --check --strict SHA256SUMS)
```

The local release script is the same one CI runs.

## Verification

CI runs six commands before anything is published:

```bash
gofmt -l .
go vet ./...
go test -race ./...
pnpm -C web run typecheck
pnpm -C web run lint
pnpm -C web test
```

The embedded-binary and container-image jobs additionally smoke-test the real
artifact.
