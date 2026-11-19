# Custom container releases

The custom release version is in [VERSION](VERSION). The upstream server/API
version remains 2.45.3 for compatibility. Git tags use `v2.45.3-custom.N`, where
`N` increases for each release. Do not move an already published tag.

Pushing a matching tag starts [Release custom containers](../../.github/workflows/container-release.yml).
Branch pushes also run the validation jobs; publishing is restricted to tags.
It checks that the tag matches VERSION, runs the focused backend regression and
frontend component suites plus TypeScript checking, then builds and publishes:

| Component | Image for this release | Platform |
| --- | --- | --- |
| Portainer server | `ghcr.io/orggem/portainer:2.45.3-custom.3` | `linux/amd64` |
| Companion Agent | `ghcr.io/orggem/portainer-agent:2.45.3-custom.3` | `linux/amd64` |

The Agent build also runs its bidirectional proxy upgrade tests. The workflow
does not run the optional Docker/Vault integration smoke scripts or deploy any
host. Build results and immutable image digests are recorded in the Actions job
summaries. The two images publish independently after the shared test gates;
confirm both publish jobs succeeded before deploying an Agent environment.

GHCR authentication uses the workflow's `GITHUB_TOKEN` with `packages: write`;
no personal token or Docker Hub credentials are needed in repository secrets.
The repository/organization must permit Actions and package creation. New GHCR
packages may require authentication to pull until their visibility/access is
configured by the repository administrator. This workflow does not change it.
Actions are pinned by commit SHA and build caches are separate per component.

## Create the next release

1. Update VERSION, this image table and the release notes below.
2. Commit all intended release changes on `codex/standalone-stack-controls`.
3. Create an annotated tag matching VERSION, then push the branch and tag:

   ```sh
   git tag -a v2.45.3-custom.4 -m "Portainer CE 2.45.3 custom release 4"
   git push --atomic origin codex/standalone-stack-controls v2.45.3-custom.4
   ```

4. Wait for all jobs in Actions to succeed. For transient failures, rerun the
   failed jobs on the same commit. Source changes require a new release tag.

For deployment with the existing Compose files, set `PORTAINER_IMAGE` to the
published server image and use `docker compose ... pull` followed by
`docker compose ... up -d --no-build`. Preserve the existing project name, data
volume, environment settings and configuration storage mounts. See [README](README.md)
for Linux and Docker Desktop prerequisites. Agent environments must use the
companion Agent image for the managed Compose builder's upgraded connection.

## 2.45.3-custom.3

- Container writable-layer and volume usage displays show bytes used.
- Daily and on-demand S3 configuration backups use IAM role credentials.
- Backups include referenced stack configuration revisions, with validation
  during backup and restore.

See [Backup and operations](README.md#backup-và-vận-hành) for configuration,
restore requirements and coverage.

## 2.45.3-custom.2

- Appearance settings for branding, colors, typography, layout and default theme.
- Authentication and user activity log views with filters and CSV export.
- Release validation for the appearance and log backend and frontend tests.

See [Appearance](../../docs/customization/appearance.md) and
[Authentication and activity logs](LOGS.md) for usage and limits.

## 2.45.3-custom.1

- Standalone user/team roles, per-role feature switches persisted in the DB,
  effective access viewer and stack operation policies.
- Application configuration upload/revisions, apply/rollback and recovery,
  including active-target cleanup when a Compose service is renamed or removed.
- Vault reference mapping and inventory filters without secret values.
- Container/environment CPU, memory and storage metrics; compact container
  storage text with ellipsis and full hover details.
- UTF-8 text editing in the volume browser with save conflict protection.
- Shared Compose build limits per Docker daemon (at most 50% CPU/RAM, one build
  by default), queue/cancellation protections and optional builder-only private
  cgroup configuration, disabled by default.
- Companion Agent patch for bidirectional Docker/BuildKit upgrade streams.

See the feature documents and [validation receipts](VALIDATION.md) for scope and
known limitations. This customization is not Portainer Business Edition.
