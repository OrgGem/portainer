package compose

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	_ "github.com/docker/buildx/driver/docker-container"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/errdefs"
	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"github.com/rs/zerolog/log"
)

const managedBuilderMarker = "PORTAINER_MANAGED_COMPOSE_BUILDER=1"
const managedBuildkitImage = "moby/buildkit:v0.25.1"

var errLegacyBuildSelected = errors.New("legacy Compose build selected")

type buildBudget struct{ Memory, Quota int64 }

func calculateBuildBudget(info system.Info, policy portainer.ComposeBuildPolicy) (buildBudget, error) {
	if err := policy.Validate(); err != nil {
		return buildBudget{}, err
	}
	if info.CgroupVersion != "2" {
		return buildBudget{}, fmt.Errorf("managed Compose builds require cgroup v2; host configuration is not changed automatically")
	}
	if info.ID == "" || info.OSType != "linux" || info.NCPU < 1 || info.MemTotal < 1 ||
		!info.MemoryLimit || !info.SwapLimit || !info.CPUCfsQuota || !info.CPUCfsPeriod {
		return buildBudget{}, fmt.Errorf("managed Compose builds require a Linux Docker host with a daemon ID, CPU quota, memory and swap limit support")
	}
	b := buildBudget{Memory: info.MemTotal / 100 * int64(policy.MemoryPercent), Quota: int64(info.NCPU) * 1000 * int64(policy.CPUPercent)}
	if b.Memory < 6*1024*1024 {
		return buildBudget{}, fmt.Errorf("host build memory budget is below Docker's minimum")
	}
	return b, nil
}

// One entry per daemon ID, not per endpoint URL (Agent proxy URLs are ephemeral).
// A single Portainer server owns this queue. The container is shared by all slots.
type hostBuildQueue struct {
	mu          sync.Mutex
	changed     chan struct{}
	waiters     []*int
	active      int
	policy      portainer.ComposeBuildPolicy
	budget      buildBudget
	initialized bool
	preparing   bool
}

var hostBuildQueues sync.Map

func (q *hostBuildQueue) signal() { close(q.changed); q.changed = make(chan struct{}) }

func (q *hostBuildQueue) acquire(ctx context.Context, readPolicy func() (portainer.ComposeBuildPolicy, error), info system.Info,
	prepare func(portainer.ComposeBuildPolicy, buildBudget, bool) error) (func(), portainer.ComposeBuildPolicy, error) {
	ticket := new(int)
	q.mu.Lock()
	q.waiters = append(q.waiters, ticket)
	q.mu.Unlock()
	remove := func() {
		for i, item := range q.waiters {
			if item == ticket {
				q.waiters = slices.Delete(q.waiters, i, i+1)
				break
			}
		}
		q.signal()
	}
	for {
		p, err := readPolicy()
		b, budgetErr := calculateBuildBudget(info, p)
		if err == nil {
			err = budgetErr
		}
		if err == nil {
			err = ctx.Err()
		}
		q.mu.Lock()
		if err != nil {
			remove()
			q.mu.Unlock()
			return nil, p, err
		}
		if !q.preparing && q.waiters[0] == ticket && (q.active == 0 || (q.policy == p && q.budget == b && q.active < p.MaxConcurrentBuilds)) {
			q.preparing = true
			first := !q.initialized
			q.mu.Unlock()
			err := prepare(p, b, first)
			q.mu.Lock()
			q.preparing = false
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				remove()
				q.mu.Unlock()
				return nil, p, err
			}
			q.initialized = true
			q.policy, q.budget = p, b
			q.active++
			remove()
			q.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { q.mu.Lock(); q.active--; q.signal(); q.mu.Unlock() }) }, p, nil
		}
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			q.mu.Lock()
			remove()
			q.mu.Unlock()
			return nil, p, ctx.Err()
		case <-changed:
		}
	}
}

func (c *ComposeDeployer) buildPolicy() (portainer.ComposeBuildPolicy, error) {
	if c.GetBuildPolicy != nil {
		return c.GetBuildPolicy()
	}
	return portainer.DefaultComposeBuildPolicy(), nil
}

func saveBuildNode(cli command.Cli, ng *store.NodeGroup) error {
	tx, release, err := storeutil.GetStore(cli)
	if err != nil {
		return err
	}
	defer release()
	return tx.Save(ng)
}

func removeBuildNode(cli command.Cli, name string) {
	tx, release, err := storeutil.GetStore(cli)
	if err != nil {
		log.Warn().Err(err).Msg("Unable to open managed builder store for cleanup")
		return
	}
	defer release()
	if err := tx.Remove(name); err != nil {
		log.Warn().Err(err).Msg("Unable to remove temporary builder record")
	}
}

func builderResourcesMatch(h *container.HostConfig, b buildBudget) bool {
	return h != nil && h.CgroupnsMode.IsPrivate() && h.CPUPeriod == 100000 && h.CPUQuota == b.Quota && h.NanoCPUs == 0 && h.Memory == b.Memory && h.MemorySwap == b.Memory
}

func managedBuilderNetworkMatches(current container.InspectResponse, hostNetwork bool) bool {
	if current.Config == nil || current.HostConfig == nil {
		return false
	}
	if hostNetwork {
		return current.HostConfig.NetworkMode.IsHost() && slices.Equal(current.Config.Cmd, []string{
			"--allow-insecure-entitlement=network.host", "--oci-worker-net=bridge",
		})
	}
	mode := current.HostConfig.NetworkMode
	return (mode.IsDefault() || mode == "bridge" || mode == "") && len(current.Config.Cmd) == 0
}

func prepareManagedBuilder(ctx context.Context, cli *command.DockerCli, ng *store.NodeGroup, b buildBudget, first, idle, allowRepair, defaultPrivate, hostNetwork bool) error {
	name := driver.BuilderName(ng.Nodes[0].Name)
	current, err := cli.Client().ContainerInspect(ctx, name)
	if err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	if errdefs.IsNotFound(err) && !defaultPrivate && !allowRepair {
		return fmt.Errorf("managed Compose builds require a private default cgroup namespace or an existing private builder; automatic builder configuration is disabled")
	}
	if errdefs.IsNotFound(err) && hostNetwork {
		if !idle {
			return fmt.Errorf("managed Compose builder disappeared during active builds")
		}
		if err := checkHostBuildNetwork(ctx, cli); err != nil {
			return fmt.Errorf("cannot configure managed host-network build: %w; resolve the conflict or use legacy Compose build", err)
		}
	}
	if err == nil {
		if current.Config == nil || !slices.Contains(current.Config.Env, managedBuilderMarker) || current.Config.Image != managedBuildkitImage {
			return fmt.Errorf("container %s is not a managed Compose builder", name)
		}
		if current.HostConfig == nil {
			return fmt.Errorf("container %s has no host configuration", name)
		}
		needsPrivate := !current.HostConfig.CgroupnsMode.IsPrivate()
		if needsPrivate && !allowRepair {
			return fmt.Errorf("managed Compose builds require a private cgroup namespace; enable automatic builder configuration or repair the idle builder manually")
		}
		needsNetworkChange := !managedBuilderNetworkMatches(current, hostNetwork)
		// On server restart, cancel any orphaned builds before accepting new work.
		if first && current.State != nil && current.State.Running {
			if err := cli.Client().ContainerStop(ctx, name, container.StopOptions{}); err != nil {
				return err
			}
			current.State.Running = false
		}
		if hostNetwork && (needsNetworkChange || current.State == nil || !current.State.Running) {
			if !idle {
				return fmt.Errorf("managed Compose builder configuration changed during active builds")
			}
			if current.HostConfig.NetworkMode.IsHost() && current.State != nil && current.State.Running {
				if err := cli.Client().ContainerStop(ctx, name, container.StopOptions{}); err != nil {
					return err
				}
				current.State.Running = false
			}
			if err := checkHostBuildNetwork(ctx, cli); err != nil {
				return fmt.Errorf("cannot configure managed host-network build: %w; resolve the conflict or use legacy Compose build", err)
			}
		}
		if needsPrivate || needsNetworkChange {
			if !idle {
				return fmt.Errorf("managed Compose builder configuration changed during active builds")
			}
			if err := recreateManagedBuilder(ctx, cli, name, current, needsPrivate, hostNetwork); err != nil {
				return err
			}
			return prepareManagedBuilder(ctx, cli, ng, b, false, idle, false, defaultPrivate, hostNetwork)
		}
		if !builderResourcesMatch(current.HostConfig, b) {
			result, err := cli.Client().ContainerUpdate(ctx, name, container.UpdateConfig{Resources: container.Resources{
				CPUPeriod: 100000, CPUQuota: b.Quota, Memory: b.Memory, MemorySwap: b.Memory,
			}})
			if err != nil {
				return err
			}
			if len(result.Warnings) != 0 {
				return fmt.Errorf("Docker did not accept build limits: %s", strings.Join(result.Warnings, "; "))
			}
		}
	}
	if err := saveBuildNode(cli, ng); err != nil {
		return err
	}
	instance, err := builder.New(cli, builder.WithName(ng.Name))
	if err != nil {
		return err
	}
	nodes, err := instance.LoadNodes(ctx, builder.WithData())
	if err != nil {
		return err
	}
	booted := false
	defer func() {
		// These are only readiness-probe clients. Compose opens its own build
		// session; keeping probes alive would leak Agent exec tunnels per job.
		for _, node := range instance.Nodes() {
			if node.Driver != nil && (booted || (node.DriverInfo != nil && node.DriverInfo.Status == driver.Running)) {
				if client, err := node.Driver.Client(ctx); err == nil {
					_ = client.Close()
				}
			}
		}
	}()
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
		}
	}
	if _, err := instance.Boot(ctx); err != nil {
		return err
	}
	booted = true
	current, err = cli.Client().ContainerInspect(ctx, name)
	if err != nil {
		return err
	}
	if current.HostConfig != nil && !current.HostConfig.CgroupnsMode.IsPrivate() && allowRepair {
		if err := recreateManagedBuilder(ctx, cli, name, current, true, hostNetwork); err != nil {
			return err
		}
		return prepareManagedBuilder(ctx, cli, ng, b, false, idle, false, defaultPrivate, hostNetwork)
	}
	if !builderResourcesMatch(current.HostConfig, b) {
		return fmt.Errorf("Docker builder CPU/memory limits could not be verified")
	}
	if !managedBuilderNetworkMatches(current, hostNetwork) {
		return fmt.Errorf("Docker builder network configuration could not be verified")
	}
	return nil
}

// Docker cannot change these container settings in place. Replace only our idle
// builder, preserving its named cache volume and resource limits.
func recreateManagedBuilder(ctx context.Context, cli *command.DockerCli, name string, current container.InspectResponse, privateCgroup, hostNetwork bool) error {
	if current.Config == nil || current.HostConfig == nil || current.Config.Image != managedBuildkitImage || !slices.Contains(current.Config.Env, managedBuilderMarker) {
		return fmt.Errorf("refusing to reconfigure an unrecognized Compose builder")
	}
	host := *current.HostConfig
	if privateCgroup {
		host.CgroupnsMode = container.CgroupnsModePrivate
	}
	config := *current.Config
	config.Cmd = nil
	host.NetworkMode = container.NetworkMode("default")
	if hostNetwork {
		host.NetworkMode = container.NetworkMode("host")
		config.Cmd = []string{"--allow-insecure-entitlement=network.host", "--oci-worker-net=bridge"}
	}
	if current.State != nil && current.State.Running {
		if err := cli.Client().ContainerStop(ctx, name, container.StopOptions{}); err != nil {
			return err
		}
	}
	if err := cli.Client().ContainerRemove(ctx, name, container.RemoveOptions{}); err != nil {
		return err
	}
	if _, err := cli.Client().ContainerCreate(ctx, &config, &host, &network.NetworkingConfig{}, nil, name); err != nil {
		return fmt.Errorf("unable to recreate managed builder (cache retained): %w", err)
	}
	return nil
}

// build selects the Compose build path without changing process-wide builder settings.
func (c *ComposeDeployer) build(ctx context.Context, cli *command.DockerCli, service api.Compose, project *types.Project) (func(), error) {
	if !hasComposeBuild(project) {
		return func() {}, nil
	}
	policy, err := c.buildPolicy()
	if err != nil {
		return nil, err
	}
	if !policy.UseLegacyBuild {
		cleanup, err := c.managedBuild(ctx, cli, service, project)
		if !errors.Is(err, errLegacyBuildSelected) {
			return cleanup, err
		}
	}
	return buildAndPinImages(ctx, cli, service, project, api.BuildOptions{})
}

func hasComposeBuild(project *types.Project) bool {
	for _, service := range project.Services {
		if service.Build != nil {
			return true
		}
	}
	return false
}

func validateManagedBuild(project *types.Project, policy portainer.ComposeBuildPolicy) (bool, error) {
	selected := selectedBuildServices(project)
	hasBuild := false
	for _, services := range []types.Services{project.Services, project.DisabledServices} {
		for name, service := range services {
			if service.Build == nil || !selected[name] {
				continue
			}
			hasBuild = true
			if service.Build.Privileged {
				return false, fmt.Errorf("service %s: privileged builds are not permitted by the managed build policy", name)
			}
			hostRequested := service.Build.Network == "host"
			for _, entitlement := range service.Build.Entitlements {
				if entitlement != "network.host" {
					return false, fmt.Errorf("service %s: entitlement %s is not permitted by the managed build policy", name, entitlement)
				}
				hostRequested = true
			}
			if hostRequested && !policy.AllowHostNetwork {
				return false, fmt.Errorf("service %s: host networking is not permitted by the managed build policy", name)
			}
		}
	}
	return hasBuild, nil
}

func requestsHostBuildNetwork(project *types.Project) bool {
	selected := selectedBuildServices(project)
	for _, services := range []types.Services{project.Services, project.DisabledServices} {
		for name, service := range services {
			if selected[name] && service.Build != nil && (service.Build.Network == "host" || slices.Contains(service.Build.Entitlements, "network.host")) {
				return true
			}
		}
	}
	return false
}

// managedBuild deliberately does not use process-wide BUILDX_BUILDER, which
// would race between environments.
func (c *ComposeDeployer) managedBuild(ctx context.Context, cli *command.DockerCli, service api.Compose, project *types.Project) (func(), error) {
	if !hasComposeBuild(project) {
		return func() {}, nil
	}
	policy, err := c.buildPolicy()
	if err != nil {
		return nil, err
	}
	if policy.UseLegacyBuild {
		return nil, errLegacyBuildSelected
	}
	hasBuild, err := validateManagedBuild(project, policy)
	if err != nil {
		return nil, err
	}
	if !hasBuild {
		return func() {}, nil
	}
	if enabled, err := cli.BuildKitEnabled(); err != nil || !enabled {
		return nil, fmt.Errorf("managed Compose builds require BuildKit (DOCKER_BUILDKIT must not be disabled): %v", err)
	}
	// Bound both queue wait and execution; preserve the caller's cancellation.
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	info, err := cli.Client().Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read Docker host build capacity: %w", err)
	}
	// The effective policy (including optional private-namespace repair) is
	// validated by queue admission, before creating or starting a builder.
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(info.ID)))[:20]
	ng := &store.NodeGroup{Name: "portainer-job-" + uuid.NewString(), Driver: "docker-container", Nodes: []store.Node{{
		Name: "portainer-compose-" + hash, Endpoint: cli.Client().DaemonHost(),
	}}}
	// Agent endpoints use an authenticated local forwarding proxy as the host.
	// Each job has its own record so concurrent proxy ports cannot overwrite it.
	defer removeBuildNode(cli, ng.Name)
	value, _ := hostBuildQueues.LoadOrStore(info.ID, &hostBuildQueue{changed: make(chan struct{})})
	q := value.(*hostBuildQueue)
	log.Info().Str("host", info.ID).Str("stack", project.Name).Msg("Waiting for managed Compose build slot")
	readPolicy := func() (portainer.ComposeBuildPolicy, error) {
		policy, err := c.buildPolicy()
		if err != nil {
			return policy, err
		}
		if policy.UseLegacyBuild {
			return policy, errLegacyBuildSelected
		}
		_, err = validateManagedBuild(project, policy)
		// Jobs with different builder network modes wait for each other so one
		// managed container continues to enforce the shared host budget.
		policy.AllowHostNetwork = policy.AllowHostNetwork && requestsHostBuildNetwork(project)
		return policy, err
	}
	release, p, err := q.acquire(ctx, readPolicy, info, func(policy portainer.ComposeBuildPolicy, b buildBudget, first bool) error {
		ng.Nodes[0].DriverOpts = map[string]string{
			"image": managedBuildkitImage, "cpu-period": "100000", "cpu-quota": strconv.FormatInt(b.Quota, 10),
			"memory": strconv.FormatInt(b.Memory, 10), "memory-swap": strconv.FormatInt(b.Memory, 10),
			"default-load": "true", "env.PORTAINER_MANAGED_COMPOSE_BUILDER": "1",
		}
		if policy.AllowHostNetwork {
			ng.Nodes[0].DriverOpts["network"] = "host"
			ng.Nodes[0].BuildkitdFlags = []string{"--allow-insecure-entitlement=network.host", "--oci-worker-net=bridge"}
		}
		q.mu.Lock()
		idle := q.active == 0
		q.mu.Unlock()
		return prepareManagedBuilder(ctx, cli, ng, b, first, idle, policy.AutoConfigureBuilderCgroup && idle, slices.Contains(info.SecurityOptions, "name=cgroupns"), policy.AllowHostNetwork)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to enforce Compose build policy: %w", err)
	}
	defer release()
	log.Info().Str("host", info.ID).Int("cpuPercent", p.CPUPercent).Int("memoryPercent", p.MemoryPercent).Msg("Building within shared host budget")
	return buildAndPinImages(ctx, cli, service, project, api.BuildOptions{Builder: ng.Name})
}

// Preserve each build's output until its deployment finishes, even if another
// stack concurrently moves the same user-facing image tag.
func buildAndPinImages(ctx context.Context, cli *command.DockerCli, service api.Compose, project *types.Project, options api.BuildOptions) (func(), error) {
	tags := leaseBuildImages(project, uuid.NewString())
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, tag := range tags {
			if _, err := cli.Client().ImageRemove(cleanupCtx, tag, image.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
				log.Warn().Err(err).Str("image", tag).Msg("Unable to remove temporary build tag")
			}
		}
	}
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	if err := service.Build(ctx, project, options); err != nil {
		return nil, err
	}
	// Up/Create/Run must only consume these images, never invoke an implicit
	// default builder or replace a just-built image by pulling the same tag.
	for name, s := range project.Services {
		if s.Build == nil {
			continue
		}
		built, err := cli.Client().ImageInspect(ctx, tags[name])
		if err != nil {
			return nil, fmt.Errorf("built image %s was not loaded into the target host: %w", tags[name], err)
		}
		if built.ID == "" {
			return nil, fmt.Errorf("built image %s has no immutable ID", tags[name])
		}
		s.Image = built.ID
		s.Build, s.PullPolicy = nil, types.PullPolicyNever
		project.Services[name] = s
	}
	success = true
	return cleanup, nil
}

// Include profile-disabled services referenced by additional build contexts.
func selectedBuildServices(project *types.Project) map[string]bool {
	selected := map[string]bool{}
	var include func(string)
	include = func(name string) {
		if selected[name] {
			return
		}
		selected[name] = true
		s, ok := project.Services[name]
		if !ok {
			s = project.DisabledServices[name]
		}
		if s.Build != nil {
			for _, target := range s.Build.AdditionalContexts {
				if dependency, ok := strings.CutPrefix(target, types.ServicePrefix); ok {
					include(dependency)
				}
			}
		}
	}
	for name := range project.Services {
		include(name)
	}
	return selected
}

// Keep user tags for compatibility, but retain each job's image until deployment
// finishes. An overlapping build may move a user tag before ContainerCreate.
func leaseBuildImages(project *types.Project, job string) map[string]string {
	tags := map[string]string{}
	selected := selectedBuildServices(project)
	// Compose can enable profile-disabled services referenced by additional_contexts.
	for _, services := range []types.Services{project.Services, project.DisabledServices} {
		for name, s := range services {
			if s.Build == nil || !selected[name] {
				continue
			}
			tag := "portainer-build-job:" + job + "-" + fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:12]
			build := *s.Build
			build.Tags = append(slices.Clone(build.Tags), api.GetImageNameOrDefault(s, project.Name))
			s.Build = &build
			s.Image = tag
			services[name] = s
			tags[name] = tag
		}
	}
	return tags
}
