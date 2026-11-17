# Shared Compose build policy

The custom CE server enforces a shared build budget on each supported Linux Docker daemon.
Existing databases automatically receive the effective defaults; no migration or
per-stack Compose YAML change is required.

## Configuration

Open **Settings → Compose build policy** as a global administrator.

| Setting | Default | Allowed |
| --- | --- | --- |
| CPU percentage of total host logical CPUs | 50 | Integer 1–50 |
| Memory percentage of total host RAM | 50 | Integer 1–50 |
| Concurrent build jobs per host | 1 | Integer 1–8 |
| Automatically configure private cgroup for builder | Off | Boolean |

All concurrent jobs share the same CPU/RAM budget. They do not each receive a
separate budget. Memory swap is disabled. There is no per-stack override or
unlimited fallback. For a 16-CPU, 32-GiB Docker host, defaults give a shared budget
of eight CPU equivalents and at most 16 GiB RAM. CPU is a CFS time quota over a
100-ms period, not an instantaneous ceiling on a monitoring graph.

The administrator API is `GET /api/settings` and `PUT /api/settings`:

```json
{
  "ComposeBuildPolicy": {
    "CPUPercent": 50,
    "MemoryPercent": 50,
    "MaxConcurrentBuilds": 1,
    "AutoConfigureBuilderCgroup": false
  }
}
```

Partial updates to unrelated settings preserve this policy. Within the policy,
all three numeric fields are required. The optional boolean defaults to false
when omitted, including in older databases. Values above 50%, fractional values, zero values
and concurrency outside 1–8 are rejected. Changes apply to waiting/new work
after existing host builds drain; existing builds retain their previous budget,
which is itself capped at 50%. Configuring more slots increases contention
within the same budget and can cause individual builds to run out of memory.

## Execution and scope

### Docker Agent prerequisite

Local socket environments need only the custom server image. Agent environments
also need `portainer-agent-custom:2.45.3-build-policy`, built with:

```sh
docker build -f deploy/standalone/agent/Dockerfile -t portainer-agent-custom:2.45.3-build-policy .
```

The stock 2.45.3 Agent's local Docker proxy does not relay protocol upgrades as a
bidirectional connection. Containerized BuildKit therefore cannot use its exec
transport through that Agent. The included overlay uses Go's reverse proxy for
upgrade requests and keeps ordinary requests unchanged. Its build runs a binary
round-trip regression test. Agent sources are pinned to the 2.45.3 commit; the
final image keeps the original Agent image's helpers/configuration and replaces
only the Agent executable. This is related to the upstream
[Agent BuildKit transport issue](https://github.com/portainer/portainer/issues/13255).
The server does not bypass limits when used with an incompatible Agent.

### Managed execution

- Stack creation, update, Git redeployment, start and one-off Compose execution
  use the common managed build path when their selected services need building.
  Configuration-only deployments and image-only projects do not create builders.
- The scheduler keys by Docker daemon ID. Local socket and Agent environments
  pointing to the same daemon share one queue and one BuildKit container. Different
  daemons can build independently. One Portainer server must own this policy on
  a host; this is not a distributed queue between multiple Portainer installations.
- The deployment layer serializes operations per stack rather than holding the
  old global Compose deployment mutex. Different stacks can reach the host
  scheduler concurrently, while deploy/undeploy of the same stack remain ordered.
- A managed Buildx `docker-container` builder uses pinned
  `moby/buildkit:v0.25.1`. Its name is
  `buildx_buildkit_portainer-compose-<first 20 hex characters of SHA256(daemon ID)>`.
  Its dedicated `_state` volume retains cache across jobs/server restarts.
  The image must be available locally or pullable on first use.
- Portainer checks Docker's CPU, memory and swap support before admitting a job,
  applies CPU period/quota and memory/memory-swap limits, and verifies the Docker
  container configuration before invoking Compose Build. Unsupported hosts fail
  the build instead of silently using an unrestricted builder.
- Builds additionally require cgroup v2 and a builder with a private cgroup namespace.
  With automatic configuration off (default), creating a builder on a daemon
  configured with `--default-cgroupns-mode=host`, or using an existing builder
  with a host namespace, is rejected. An existing private builder remains usable.
- With **Automatically configure private cgroup for builder** enabled, Portainer
  may replace only its recognized idle builder with an equivalent container using
  `CgroupnsMode=private`. It preserves CPU/RAM limits and the named cache volume.
  New builders on a host-namespace daemon are adjusted before any Compose build
  starts. Automatic replacement is forbidden while other jobs are active, and
  private isolation is verified again before admitting the build. If Docker
  cannot create a private builder, the build fails and cache data is retained.
  Turning the switch off does not undo a successfully configured private builder.
- The switch never changes Docker daemon settings, upgrades cgroup v1, restarts
  Docker, or reboots the host. Cgroup v1 remains unsupported. Image-only and
  configuration-only deployments do not require a builder.
- Non-global-admin users, including Operator and Environment Admin, cannot
  mutate, attach to, exec into or recreate the managed builder through the
  container APIs. Ordinary application container permissions remain unchanged.
- Builder selection is explicit per job. Temporary Buildx records use the job's
  own Docker endpoint/proxy; no process-global builder environment is changed.
- The output image must exist on the target Docker daemon before deployment.
  Compose Up/Create/Run consume the built image with implicit rebuilding and
  repulling disabled for those services. Existing Git source selection is retained.
- Each job builds with private temporary image tags, while also exporting the
  original Compose tags. Deployment uses the immutable image ID from the job's
  tag, preventing another stack from replacing its image through a shared tag.
  Temporary tags are removed after deployment where Docker permits it. If the
  tag is the last reference to an image used by a running container, it is retained
  rather than force-deleting the image. Original Compose files are not rewritten.
- Builder preparation reserves the queue but runs outside its mutex, allowing
  other waiters to cancel and running jobs to release their slots during setup.
- Queue wait and build execution together have a one-hour maximum timeout;
  shorter upstream deployment deadlines still apply. Cancellation
  and build failures release the slot. A memory-exhausted build can fail; it does
  not automatically retry without limits. On server restart, the first job stops
  a previously running managed builder to cancel orphaned work before reusing it.
- BuildKit uses a privileged infrastructure container. Compose build requests
  cannot request privileged mode or additional entitlements. The configured
  limits assume trusted host administrators; direct Docker administrator access
  can change/remove the infrastructure container outside Portainer's control.

The budget covers the BuildKit daemon and its build processes. It does not cover
Portainer's Git checkout, Docker daemon image import/pull overhead, running app
containers, disk I/O, disk/cache capacity or bandwidth. For Docker Desktop, host
capacity means the Linux VM allocation. Builds invoked outside this Portainer
server, raw Docker image-build API calls and autonomous Edge Agent builds are
outside this policy. This change does not implement bandwidth shaping.

## Verification

Unit tests cover capacity calculation, fractional CPU capacity, hard ceilings,
unsupported hosts, swap verification, queue cancellation, shared parallel slots,
policy changes while builds are active, setup failures, and unsafe build requests.
Settings/UI tests cover effective defaults, administrator API validation and UI
save/persisted values.

Run `python deploy/standalone/tests/build_policy_smoke.py` with a disposable Linux
Docker test host and the `portainer-custom:2.45.3-build-policy` image (override via
`PORTAINER_TEST_IMAGE`). The test refuses to run if a managed builder already
exists. It creates disposable Portainer and Agent servers, verifies actual Docker
and cgroup limits, checks CPU usage during real builds, checks serialization and
parallel shared budgets across local/Agent aliases, induces a bounded OOM, and
checks recovery and restart persistence. Only test-owned resources are removed.
The test uses the custom Agent image by default; override with
`PORTAINER_TEST_AGENT_IMAGE` to test another Agent version.

The follow-up regressions run with the fixed server image:

```powershell
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go build -o .tmp/review-delay-proxy deploy/standalone/tests/fixtures/build_delay_proxy.go
python deploy/standalone/tests/build_policy_regression.py
```

This optional suite defaults to `portainer-custom:2.45.3-build-policy-fixes`
(`PORTAINER_TEST_IMAGE` overrides it). It uses a disposable delay proxy to
reproduce an image-tag race, checks Operator denial during an active build,
and creates a bounded disposable `docker:28.5.2-dind` daemon to verify rejection
with the switch off, private builder setup and measured CPU limits with it on,
and safe reuse after turning it off again. Docker daemon defaults remain unchanged.
The daemon image must be available or pullable.
