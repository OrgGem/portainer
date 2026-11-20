# Validation receipt — 2026-11-17

Base: official CE tag `2.45.3`, commit
`0fa764dc81c76bb7914d65d8e62b026d7c746899`.
Implementation branch: `codex/standalone-stack-controls` (local working tree).

## Checks completed

| Check | Result |
| --- | --- |
| Go tests: authorization, revision files, stack handlers, Docker proxy | Passed |
| Go security package, including endpoint boundary tests | Passed on isolated rerun; see note below |
| Go deployment and public webhook packages | Passed |
| Resource ownership / policy concurrency regression | Passed for both regular-user rejection and administrator preservation |
| Compose configuration-only deployment test | Passed: selected service, pinned image, no build/pull/dependency recreation/prune |
| TypeScript typecheck and ESLint on changed frontend files | Passed |
| Configuration tab component tests | 3 passed |
| Production frontend webpack build | Passed; 2 bundle-size warnings |
| Linux amd64 server compilation | Passed with Go 1.26.10, CGO disabled |
| Full multi-stage source Docker build | Passed with Node 22.22.1 and Go 1.26.10; tagged `portainer-custom:2.45.3` |
| Prebuilt runtime image | Built and retained as `portainer-custom:2.45.3-prebuilt` |
| Standalone and Vault Compose manifests; Docker Desktop override | Compose configuration validation passed |
| Standalone HTTP/Docker integration test | Passed on both the final prebuilt image and the full source image |
| Vault AppRole/Agent integration test | Passed |

The combined Go run initially hit the existing `TestJWTRevocation` test's
one-second token lifetime while other builds were running (`invalid JWT token`).
The full security package passed when rerun separately. The test was not changed
or skipped. Local frontend checks used Node 24.21.0 / PNPM 10.26.2 and emitted the
upstream Node 22 engine warning; the source Dockerfile pins Node 22.22.1.

## Real Docker scenarios

`tests/smoke.py` starts a disposable Portainer and a two-service nginx stack.
It exercises the real HTTP authentication, database, Docker socket and Compose
engine, including a team-based apply grant:

- Upload creates a draft without replacing containers.
- Upload-only and apply-only roles cannot cross their action grants; direct
  Docker restart/archive, ownership modification and arbitrary stack creation
  are rejected.
- Apply mounts the exact uploaded bytes read-only, preserves the image ID and
  leaves the other service's container ID unchanged.
- An invalid nginx configuration fails deployment, restores the previous
  configuration and never becomes a successfully applied revision.
- Rollback restores an earlier successful revision.

`../vault-example/smoke.py` starts a separate, isolated Vault dev server and
application using synthetic credentials. It verifies AppRole cannot read an
unrelated path, Agent injects `DB_PASSWORD` into its child without adding it to
Docker `Config.Env`, and secret rotation restarts the child with the new value.

Both scripts removed their own containers, networks and volumes. Existing Docker
workloads were not redeployed. Test images remain available locally.

## Evidence and limits

Local logs are retained in the ignored `.tmp/validation/` directory. The final
source-built image before adding group roles (now retained as
`portainer-custom:2.45.3-stack-controls`) has ID
`sha256:aba8f3ab49de7964d9832547fa796aa3043b669205fb31b3b590a7fb44e1d23b`.
The prebuilt Portainer image tested has ID
`sha256:2e74e7481ba5dc2405f58a5da5055fd08cd927c7ba93edfe37b90a84a4288349`.
The Vault application image has ID
`sha256:89d44369636b0e4435be6ba9645e5d103e39a2a8b3cb4902815bc2da236d2fbc`.

This is not a full repository test run, a browser E2E run, a production data
migration or a production Vault deployment. Remote Docker/Agent, Swarm,
Kubernetes and multi-file/Git Compose configuration management are outside this
implementation. Interrupted-deployment recovery is covered with handler mocks;
the real Docker test covers automatic recovery after a failed apply.

## Group-role extension — 2026-11-17

The group-role image before adding Vault UI is retained as
`portainer-custom:2.45.3-roles`:

`sha256:c3880589ba9726044a8ec1a21ee734bceedd15252b2eb1c776dc78e805a81264`.

| Check | Result |
| --- | --- |
| Go packages: authorization, security, Docker proxy, stacks, resourcecontrols, users, endpoints, endpointgroups, webhooks, deployments | All passed |
| Additional security test: inherited roles, role changes, Docker Agent classification, unsupported environment rejection, unattended restrictions, membership removal | Passed |
| Team-role assignment UI component tests | 4 passed: legacy grant preservation, default Read-only, Environment Admin assignment, retry after failed save |
| Configuration UI regression tests | 3 passed |
| TypeScript typecheck and ESLint for all changed frontend files | Passed; Node engine / Browserslist warnings remain |
| Full source image build | Passed; Node 22.22.1, Go 1.26.10, 2 webpack bundle-size warnings |
| `tests/roles_smoke.py` against source image | All 6 scenario groups passed |
| Existing `tests/smoke.py` against new image | All 5 configuration scenario groups passed |
| `git diff --check` | Passed |

Role integration tests used real HTTP requests, JWTs, persisted Teams and
memberships, environment/group policies, and disposable Docker Compose stacks.
They verified resource visibility for all five roles, write/console/file-access
denials for read roles, Operator versus Standard behavior, Environment Admin
resource creation/deletion without global settings or ownership access, immediate
role changes and direct-user precedence using the same JWT, stack-policy
intersection, and membership revocation. Both scripts cleaned up their test
containers, networks and volumes; running user workloads were not changed.

The first role smoke invocation reached the final permission assertions but the
test incorrectly expected a map instead of the API's array. Corrected the test
to assert the exact action set, then reran all scenarios successfully.

This does not establish full Business Edition parity, browser E2E acceptance,
Agent transport end-to-end behavior, production migration, or production
deployment. Docker Agent is covered by the role request classifier test; the
real integration tests use a local socket. File configuration management retains
its earlier local-socket restriction. Vault code was unchanged by this extension;
its earlier test evidence above was not rerun. No commit, push or running
production Portainer replacement was performed.

## Vault mapping UI and inventory — 2026-11-17

Final source-built image, tagged `portainer-custom:2.45.3-vault-ui`,
`portainer-custom:2.45.3`, and `portainer-custom:2.45.3-source`:

`sha256:d73d00f914c9ab3cf6fd1105f0223103f4f8dc60a4cbfc18ec66b988021e2f82`.

| Check | Result |
| --- | --- |
| Go stack handlers and Vault reference package | Passed, including version conflicts, forbidden fields, authorized inventory/counts/options, combined filters, pagination and repair of removed-service references |
| Go authorization, security and Docker proxy regression packages | Passed |
| Vault UI component tests | 6 passed: read-only display, reference-only edit payload, conflict draft preservation, removal, service export, combined inventory filters and empty inventory |
| Existing role/configuration UI component tests | 7 passed (13 total with Vault UI) |
| TypeScript typecheck and changed-file ESLint | Passed; existing Node engine/Browserslist warnings remain |
| Production source image build | Passed with 2 webpack bundle-size warnings |
| `tests/vault_mappings_smoke.py` on final image | All 5 scenario groups passed |
| Exported template with real Vault 1.21 AppRole/Agent | AppRole scope, runtime injection without Docker Config.Env, and rotation passed |
| `tests/roles_smoke.py` on final image | All 6 scenario groups passed |
| `tests/smoke.py` on final image | All 5 configuration scenario groups passed |

The Vault HTTP test used actual Teams, user JWTs, stack policies and a persisted
Portainer DB. It verified admin versus read-only writes, rejected value/token/
secret fields, excluded unrelated Compose environment values and private stack
references from inventory, tested server-side filters/counting/pagination,
verified metadata after restart and revoked membership without replacing the
current JWT. Its exported `env_template` was then merged into the test
application's agent configuration and used against an isolated Vault dev server
with synthetic secrets.

The restart test harness needed to refresh Docker's random published port and
reauthenticate after CE invalidated pre-restart JWTs. Those harness assumptions
were corrected; the complete final run passed. Membership revocation is checked
with the same post-restart JWT before and after removal. The initial Vault
readiness probe may log a connection refusal while its dev server starts; the
retry loop and all subsequent Agent assertions passed.

All test containers, networks and volumes were cleaned up. Existing workloads
were not redeployed. Local logs are under ignored `.tmp/validation/.tmp-vault-*`.

Scope: UI and APIs store references only and do not contact Vault for values.
Saving references does not automatically deploy them or prove runtime state.
Exported Agent templates require the application's existing authentication,
connection/TLS and exec configuration. This is not a browser E2E run, production
Vault acceptance, or a production deployment. No commit or push was performed.

## Container and environment resource metrics — 2026-11-17

Final source-built image, tagged `portainer-custom:2.45.3-metrics`,
`portainer-custom:2.45.3` and `portainer-custom:2.45.3-source`:

`sha256:8fae97cf287b1ba759d18a0372089bbbbdf7947f14cdd055fdc426222de1887a`.

The previous `portainer-custom:2.45.3-vault-ui` image remains available.

| Check | Result |
| --- | --- |
| Go resource metrics, endpoint handlers, security and Docker proxy packages | Passed |
| Linux collector tests in source image build | Passed: confined paths/symlinks, filesystem filtering, Docker filesystem ordering and CPU counter calculation |
| Metrics and Home card UI tests | 13 passed; 1 existing Home-card TODO |
| TypeScript typecheck | Passed |
| Changed frontend ESLint | No errors; existing Home-card platform switch warning and Browserslist notice remain |
| Linux and Desktop Compose configuration | Both validated |
| Full source image build | Passed; 2 webpack bundle-size warnings |
| Final image `tests/metrics_smoke.py` | All 4 scenario groups passed |
| Final image `tests/roles_smoke.py` | All 6 role scenario groups passed |
| Chrome manual UI smoke | Home, Environments table and Containers table rendered live metrics; host and container mount details expanded correctly; final image rechecked after rebuild |
| Whitespace check | Passed |

The real Docker test ran a disposable Linux container with **0.5 CPU / 64 MiB**
limits, a 4 MiB writable-layer file and a 3 MiB named-volume file. It checked
nonzero live CPU/RAM, correct limits, separate volume data versus shared
filesystem capacity, no secret/Env fields, and zero CPU/RAM after stop.
Environment metrics reported 16 CPU and 31,394,971,648 bytes of Linux VM RAM,
with 25 host mounts in this Docker Desktop environment. The filesystem backing
Docker is displayed first. These figures describe the test host, not fixed
application limits.

Authorization assertions covered anonymous requests, private containers,
invalid IDs, resource-control revocation after a successful cached sample,
and team-membership revocation with the same JWT. Fresh authorization inspection
is never cached. Metrics fetches, including inspect, share a concurrency bound.

Browser inspection used an isolated Portainer database and synthetic account;
existing application containers were only listed/read, never redeployed.
Quota text, progress bars, accumulated sparklines and mount expansion were
observed. This is a focused manual browser smoke, not a full browser E2E suite
or a multi-browser/mobile acceptance run.

All disposable test containers/volumes and browser-login credentials were
removed. Production Portainer was not replaced; no commit or push was made.
Logs are under ignored `.tmp/validation/metrics-*`.

Remote Agent transport was not integration-tested. Agent 2.45.3 only supplies
Docker-partition storage capacity here, not host CPU/RAM usage or all mounts;
those values are explicitly unavailable without a local host collector.
Filesystem quota is not inferred from volume size. See [METRICS.md](METRICS.md)
for sources, formulas, refresh intervals and Compose configuration. Previous
Vault/configuration integration suites were not rerun for this metrics-only
extension; their earlier evidence is recorded above.

## Volume text editor — 2026-11-17

Final source-built image, tagged `portainer-custom:2.45.3-volume-editor`,
`portainer-custom:2.45.3` and `portainer-custom:2.45.3-source`:

`sha256:2376bd9004540bed09b21ccc65b3ab44ba8168caf1cbb5ba97938d937e20a3db`.

The previous `portainer-custom:2.45.3-metrics` image remains available.

| Check | Result |
| --- | --- |
| Go Docker proxy, security and users packages | Passed |
| VolumeTextEditor UI tests | 5 passed |
| TypeScript typecheck | Passed, including final environment-type guard |
| Changed frontend ESLint | Passed; Browserslist data notice remains |
| Full source image build | Passed; existing Babel deprecation and 2 webpack size warnings |
| Final image `tests/volume_editor_smoke.py` | All 5 scenario groups passed with official Agent 2.45.3 |
| `tests/roles_smoke.py` regression | All 6 groups passed; before final frontend-only visibility guard, with the same backend source |
| Chrome manual UI smoke | Edit/Save and immediate Ctrl+S, invalid JSON blocking, conflict draft retention, confirmed reload; final image Edit/Save rechecked |
| Whitespace check | Passed with repository Git settings |

The integration test created an isolated Portainer database, synthetic accounts,
network and volumes. Agent 2.45.3 served real signed proxy requests. Assertions
covered shorter and zero-byte writes, nested paths, preserved owner/group/mode,
UTF-8 BOM/CRLF, binary and oversized files, missing Content, invalid JSON,
anonymous access, traversal rejection, stale versions and concurrent editor
saves (one success and one conflict).

Environment Admin and a legacy CE volume owner could edit. Explicit Read-only,
Helpdesk, Standard and Operator roles could not read/write arbitrary volume
files. Host browse remained denied to Environment Admin; ownership and team
membership revocation took effect using the existing JWT.

The browser opened a real Agent volume and saved JSON using CodeMirror. An
external test-helper write caused a 409 without losing the browser draft.
Reload required discard confirmation and loaded the externally changed content.
The final image displayed **Saved and verified**, and a separate read directly
from the test volume matched the browser content. Local screenshot:
`../../.tmp/validation/volume-editor.jpg`.

This is a focused Chrome smoke, not a complete multi-browser/mobile acceptance
suite. The feature supports Docker Agent environments; it is not implemented
for host browsing, local socket-only environments, Edge or Kubernetes. JSON
syntax is validated; arbitrary application configuration semantics are not.
Agent writes are in-place, with a pre-save content-version check and post-save
verification, not atomic filesystem compare-and-swap. See
[VOLUME-TEXT-EDITOR.md](VOLUME-TEXT-EDITOR.md) for limits and concurrency behavior.

All disposable containers, volumes, networks and browser-login credentials were
removed. Existing workloads and production Portainer were not redeployed.
No commit or push was performed. Logs are under ignored
`.tmp/validation/volume-editor-*`. Earlier Vault, stack configuration and full
metrics suites were not repeated for this extension.

## Review fixes 1–10 — 2026-11-17

Final source-built application image: `portainer-custom:2.45.3-review-fixes`,
also tagged `portainer-custom:2.45.3` and `portainer-custom:2.45.3-source`:

`sha256:e1c702f07519f5b1a8906efc0452f998a641e4e8aabda54f1086d763436e56dd`.

The earlier `2.45.3-volume-editor`, `2.45.3-metrics` and `2.45.3-vault-ui`
tags are preserved. See [REVIEW-FIXES.md](REVIEW-FIXES.md) for the issue mapping.

| Check | Observed result |
| --- | --- |
| Go security, stacks, users, Docker handlers/helpers, proxy, UAC, deployments and authorization packages | Passed |
| Additional canonical container ID and policy-based Compose admin regressions | Passed |
| Final router-prefix security/proxy regression run | Passed |
| TypeScript and changed frontend ESLint | Passed |
| Group Access, ConfigurationTab and VolumeTextEditor UI tests | 13 passed across 3 files |
| Full source Linux image build | Passed; existing Babel deprecation and 2 webpack size warnings |
| Final image role smoke | 7 scenario groups passed |
| Final image configuration smoke | 5 groups passed: draft separation, permissions, selective apply, failed-apply recovery, rollback |
| Final image volume editor smoke | 5 groups passed with official Agent 2.45.3 |
| Final image Vault mapping smoke | 5 groups passed, including a nested real AppRole/Agent injection and rotation test |
| Git whitespace check | Passed |

The first role smoke exposed an omitted router stage: `/api/endpoints` is
stripped before endpoint-proxy authorization. The classifier and permanent
tests now cover both full and stripped Docker/Agent prefixes; the complete
role smoke then passed on the final image. Dashboard checks compare against
pre-existing host stack counts rather than assuming an empty Docker daemon.
Private GPU inspection and recreate attempts are denied with inherited stack
ownership. Go tests also cover the target migration authorization call,
explicit/legacy ties, image-export denial, stale-target retirement/ID reuse,
and independent-stack write concurrency.

This turn did not repeat manual browser acceptance or the full metrics smoke.
Go/UI tests and the disposable integration suites do not constitute a complete
security audit or testing of every Agent route. A temporary Vault readiness
probe logged connection refused before startup completed; injection and
rotation subsequently passed. Logs are in ignored `.tmp/review-fix-*` files.

Disposable test resources were removed. Production Portainer and existing
workloads were not redeployed. No commit or push was performed.

## Issue 8: active configuration targets — 2026-11-18

Image `portainer-custom:2.45.3-issue8-fix`, also tagged `2.45.3` and
`2.45.3-source`:

`sha256:e568c46af091c420e43e220dde82f947b0c98880deee5eda021450e08b0fc2d0`.

The preceding `2.45.3-review-fixes` image is preserved. Its issue-8 claim above
was incomplete for targets with ActiveRevision: an override resurrected the
removed service without an image, deployment failed, Compose rolled back,
and retirement was again forbidden. The expanded smoke reproduced that
failure on the old image before passing on this image.

The fix filters configuration overrides against the actual Compose services
without mutating target metadata. A renamed service never inherits the old
service's mount automatically. Active pointers/history therefore survive
Compose rollback, and missing-service targets become eligible for explicit
retirement after a successful update. Apply/rollback to missing services now
returns 409, including requests for the already-active revision. New targets
cannot borrow the validation exemption of a retained stale target. Async
deployment completes file rollback/cleanup before exposing terminal status,
keeping target mutations blocked during that interval.

| Check | Result |
| --- | --- |
| `go test ./api/exec ./api/stacks/deployments ./api/stacks/stackconfig ./api/http/handler/stacks` | Passed |
| Active override unit tests | Rename/delete skip missing services, preserve surviving mounts and history, restore mounts with restored Compose, reject unreadable Compose |
| Handler regressions | Missing-service apply/rollback rejected; stale target cannot authorize a new invalid target; target writes remain blocked through post-deploy cleanup |
| Expanded configuration smoke on old image | Failed as expected on valid service rename: old `web` service had neither image nor build context |
| Expanded configuration smoke on final image | All 10 scenario groups passed |
| Full source Linux image build | Passed; existing Babel deprecation and 2 webpack size warnings |
| Git whitespace check | Passed |

The real-Docker smoke calls the stack update API with an applied revision for
both service rename and deletion. It verifies successful deployment, orphan
removal with Prune=true, the unaffected service container, no mount transfer,
unchanged active pointers/history, 409 for stale apply/rollback, successful
retirement, readable historical content, denied ID reuse, and successful
upload/apply for a replacement target. Failure cases use invalid candidate
Compose before Docker mutations and verify restored Compose and configuration;
they do not claim automatic container restoration after every possible partial
ordinary deployment failure. Existing invalid-configuration recovery and
revision rollback tests also passed.

Logs: ignored `.tmp/issue8-{go,regression,build,smoke-before,smoke-after}.log`.
No frontend code changed; browser acceptance and unrelated Vault/metrics/editor
suites were not rerun. All disposable test resources were removed. Existing
workloads and production Portainer were not redeployed. No commit or push.

## Shared Compose build policy — 2026-11-18

Images built from this working tree:

- Server: `portainer-custom:2.45.3-build-policy`,
  `sha256:7516493323fda6ad3bcbc13f0cf862c3a573de5a39ec9931be7152ba0850a6f3`.
- Companion Agent: `portainer-agent-custom:2.45.3-build-policy`,
  `sha256:ad563fe3b0ca1f28527a796b79717ca05a9adf96a17580d43629002a7725c2d1`.

Settings exposes an administrator-only policy with defaults 50% CPU, 50% RAM
and one build per host. Percentages cannot exceed 50; concurrent builds share
one limited BuildKit container, including local/Agent aliases of the same daemon.
See [BUILD-POLICY.md](BUILD-POLICY.md) for deployment and enforcement scope.

| Check | Result |
| --- | --- |
| Compose build/config-only unit tests | Passed: budgets, unsupported hosts, unsafe requests, queue ordering/cancellation, shared slots and policy changes |
| Backend regression packages | Passed: api/exec, stacks/deployments, handler/stacks, handler/settings, proxy/factory and proxy/factory/agent |
| Settings UI tests | 2 files, 10 tests passed |
| TypeScript and changed-settings ESLint | Passed |
| Server/frontend source build | Passed; existing Babel deprecation and webpack size warnings |
| Agent source build and binary upgrade round-trip test | Passed |
| Real Docker smoke | All six scenario groups passed |
| Git whitespace check | Passed |

The real smoke used a Linux Docker daemon reporting 16 logical CPUs and
31,394,971,648 bytes of RAM. It checked the default 50% CPU/RAM values in Docker
HostConfig and cgroup v2, including disabled swap. With a reduced 5% CPU budget
(0.8 CPU equivalents), CPU-bound builds measured 0.777 and 0.777 CPU equivalents
when serialized, and 0.749 total when two builds overlapped. Both connection
types reused the same builder and shared RAM limit. These measurements cover
the test workload; kernel quota/memory settings enforce the aggregate budget.

The smoke also verified invalid settings/admin-only access, failure on memory
exhaustion without deploying the app, successful next build through Agent,
policy persistence after server restart and reuse of the same builder container.
The kernel may reject allocation directly or kill a process; both count as
memory-exhaustion enforcement. Disposable test containers, networks, volumes
and images were removed after completion.

Stock Agent 2.45.3 failed the BuildKit exec transport during integration testing.
Agent environments require the companion image, which relays bidirectional HTTP
upgrades. Socket environments need only the server image. No unrestricted build
fallback was added. The budget covers BuildKit and its build processes, excluding
Git checkout and Docker daemon overhead; one Portainer server owns the per-host
queue. This does not limit bandwidth or builds outside the managed Compose path.

Logs: ignored `.tmp/build-policy-{go,regression,ui,tsc,eslint,image,agent,server,smoke}.log`.
No manual browser acceptance or full unrelated Vault/metrics/editor suite was
performed in this change. Production Portainer and existing workloads were not
redeployed. Previous stable image tags were preserved. No commit or push.

## Build policy review fixes and private-cgroup switch — 2026-11-18

Final full-source image: `portainer-custom:2.45.3-build-policy-fixes`

`sha256:f2a0cd7a3214f35b89b72c4cbd863b425d26e475567552a67616f5d3e696fdd0`.

The preceding build-policy image is preserved. Its smoke results did not cover
the four issues subsequently reproduced during review. The final changes are:

| Issue | Bounded change |
| --- | --- |
| Operator changes active builder limits | Protect managed builder writes, exec/attach and recreate from non-global-admin users; application permissions remain intact |
| Host cgroup namespace bypass | Require cgroup v2 and verify actual private builder namespace before build; reject unsupported configurations |
| Shared image tags race across stacks | Export job-specific tags, deploy immutable image IDs, retain original Compose tags; also isolate referenced build dependencies |
| Cancellation blocked by builder setup | Reserve setup state under the queue mutex, perform network work outside it, and allow waiters to cancel during preparation |

The user selected **builder-only automatic configuration**, with no Docker/host
restart. `AutoConfigureBuilderCgroup` is exposed in Settings and defaults to false,
including older saved settings. When true, Portainer may recreate only its own
idle builder with a private namespace on a cgroup v2 host, retaining its named
cache volume and resource limits. The daemon's defaults are not changed.
Turning the switch off leaves an already-private builder usable. Cgroup v1
remains unsupported. Original Compose files are not rewritten. Temporary job
tags may remain when removing the last reference would delete an image in use;
no forced image deletion was introduced.

Verification on the final image:

- Compose budget/queue/config-only tests passed, including the formerly failing
  cancelled-waiter and host-namespace checks, image lease/profile handling, and
  the cgroup-v1 rejection with automatic configuration enabled.
- Backend regression packages passed: api/exec, stacks/deployments,
  proxy/factory/docker, handler/docker/containers, handler/stacks and handler/settings.
  Builder protection tests include normal application access and global-admin
  maintenance; the recreate helper rejects Environment Admin for managed builders.
- Two settings UI files / ten tests passed, including the default-off switch,
  saving its enabled value, and loading it from persisted settings. TypeScript
  and changed-file ESLint passed.
- Full source server/frontend image build passed with two webpack size warnings.
- All six original real-Docker smoke groups passed, including local/Agent aliases,
  concurrent budgets, memory exhaustion/recovery and restart persistence. At a
  0.8-CPU test budget, serial workloads measured 0.786 and 0.768 CPU equivalents;
  the combined parallel workload measured 0.751.
- Four additional runtime regressions passed: stack A retains image A despite
  stack B replacing the shared tag; Operator receives 403 for update/exec during
  an active build; host namespaces are rejected with the switch off; enabling
  the switch configures a private builder and preserves measured CPU enforcement.
  In the last case, the observed usage was 0.159 CPU against a 0.16-CPU budget.
  The same builder was reused after switching off, and the daemon retained its
  original host-namespace default.
- Git whitespace check passed. Disposable test containers, volumes, networks
  and test image tags were removed.

Logs are under ignored `.tmp/build-fix-*`. The companion Agent image from the
build-policy feature remains required for Agent environments; it was exercised
again by the final smoke. No browser acceptance, race-detector run, or unrelated
Vault/metrics/editor smoke was performed. Production was not redeployed. No
commit or push was performed.

## Vault deploy-time injection — 2026-11-19

The local source-built image `portainer-custom:2.45.3-vault-deploy`,
image ID `aad668c5e0cb`, targets `linux/amd64`. Its isolated Docker acceptance
run completed five scenario groups:

1. TLS and separate environment AppRoles, with KV v2 on local Docker using
   Editor and KV v1 through the official Portainer Agent using multipart Upload.
2. Changing a Vault value leaves existing and restarted containers on their
   original environment until redeployment.
3. Service-only configuration Apply and Rollback read current Vault values and
   preserve the other service.
4. A denied Vault read, including the retry path that normally undeploys first,
   leaves the active service available.
5. Repairing the mapping allows deployment to recover after the failed retry.

The frontend run passed 31 tests, with two existing TODO tests. TypeScript and
focused ESLint checks passed. The Docker acceptance used disposable resources,
a Vault dev server and synthetic credentials. It does not establish behavior
against the externally managed production Vault. The user's running Portainer
instance was excluded from the test and was not replaced.
