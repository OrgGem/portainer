package compose

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
)

func TestBuildPolicyRejectsPrivilegedBuildAndSkipsImageOnlyStacks(t *testing.T) {
	c := NewComposeDeployer()
	for _, config := range []*types.BuildConfig{
		{Privileged: true}, {Entitlements: []string{"security.insecure"}},
	} {
		project := &types.Project{Services: types.Services{"app": {Name: "app", Build: config}}}
		// Nil clients prove rejection happens before any Docker or build operation.
		_, err := c.managedBuild(t.Context(), nil, nil, project)
		require.ErrorContains(t, err, "not permitted")
	}
	project := &types.Project{Services: types.Services{"app": {Image: "app:existing"}}}
	cleanup, err := c.managedBuild(t.Context(), nil, nil, project)
	require.NoError(t, err)
	cleanup()
}

func TestBuildPolicyNeverFallsBackToLegacyBuilder(t *testing.T) {
	t.Setenv("DOCKER_BUILDKIT", "0")
	cli, err := command.NewDockerCli()
	require.NoError(t, err)
	project := &types.Project{Services: types.Services{"app": {Name: "app", Build: &types.BuildConfig{Context: "."}}}}
	// No Compose client is supplied: a legacy Build call would panic.
	_, err = NewComposeDeployer().managedBuild(t.Context(), cli, nil, project)
	require.ErrorContains(t, err, "require BuildKit")
}

func buildHost() system.Info {
	return system.Info{CgroupVersion: "2", SecurityOptions: []string{"name=cgroupns"}, ID: "same-daemon", OSType: "linux", NCPU: 16, MemTotal: 32 * 1024 * 1024 * 1024,
		MemoryLimit: true, SwapLimit: true, CPUCfsQuota: true, CPUCfsPeriod: true}
}

func TestBuildBudgetUsesTotalHostCapacityAndHardCeiling(t *testing.T) {
	p := portainer.DefaultComposeBuildPolicy()
	b, err := calculateBuildBudget(buildHost(), p)
	require.NoError(t, err)
	require.Equal(t, int64(800000), b.Quota) // eight CPUs per 100ms period
	require.LessOrEqual(t, b.Memory, int64(16*1024*1024*1024))
	require.Less(t, int64(16*1024*1024*1024)-b.Memory, int64(100))
	h := buildHost()
	h.NCPU = 1
	b, err = calculateBuildBudget(h, p)
	require.NoError(t, err)
	require.Equal(t, int64(50000), b.Quota) // fractional CPU, not rounded to one core
	for _, change := range []func(*system.Info){
		func(h *system.Info) { h.ID = "" }, func(h *system.Info) { h.OSType = "windows" },
		func(h *system.Info) { h.CgroupVersion = "1" },
		func(h *system.Info) { h.MemoryLimit = false }, func(h *system.Info) { h.SwapLimit = false },
		func(h *system.Info) { h.CPUCfsQuota = false }, func(h *system.Info) { h.CPUCfsPeriod = false },
		func(h *system.Info) { h.NCPU = 0 }, func(h *system.Info) { h.MemTotal = 1 },
	} {
		h := buildHost()
		change(&h)
		_, err := calculateBuildBudget(h, p)
		require.Error(t, err)
	}
	p.CPUPercent = 51
	_, err = calculateBuildBudget(buildHost(), p)
	require.Error(t, err)
}

func TestBuildImageLeasePreservesComposeTagsAndSeparatesJobs(t *testing.T) {
	original := &types.BuildConfig{Tags: []string{"app:extra"}}
	project := &types.Project{Services: types.Services{"app": {Name: "app", Image: "app:shared", Build: original}}}
	first := leaseBuildImages(project, "first")
	require.Equal(t, first["app"], project.Services["app"].Image)
	require.Equal(t, types.StringList{"app:extra"}, original.Tags)
	require.Contains(t, project.Services["app"].Build.Tags, "app:shared")
	second := leaseBuildImages(project, "second")
	require.NotEqual(t, first["app"], second["app"])
}

func TestBuildCgroupAutomaticConfigurationIsOptInAndNeverAllowsV1(t *testing.T) {
	p := portainer.DefaultComposeBuildPolicy()
	require.False(t, p.AutoConfigureBuilderCgroup)
	h := buildHost()
	h.SecurityOptions = nil
	_, err := calculateBuildBudget(h, p)
	require.NoError(t, err) // Namespace is checked on the actual builder, not just daemon defaults.
	p.AutoConfigureBuilderCgroup = true
	_, err = calculateBuildBudget(h, p)
	require.NoError(t, err)
	h.CgroupVersion = "1"
	_, err = calculateBuildBudget(h, p)
	require.Error(t, err)
}

func TestBuildImageLeaseIncludesBuildDependenciesButLeavesUnusedProfilesAlone(t *testing.T) {
	p := &types.Project{Services: types.Services{"app": {Name: "app", Build: &types.BuildConfig{AdditionalContexts: types.Mapping{"base": "service:base"}}}}, DisabledServices: types.Services{
		"base":   {Name: "base", Image: "base:shared", Build: &types.BuildConfig{}},
		"unused": {Name: "unused", Image: "unused:existing", Build: &types.BuildConfig{}},
	}}
	tags := leaseBuildImages(p, "job")
	require.Contains(t, tags, "base")
	require.Equal(t, tags["base"], p.DisabledServices["base"].Image)
	require.NotContains(t, tags, "unused")
	require.Equal(t, "unused:existing", p.DisabledServices["unused"].Image)
}

func TestBuildLimitsRequireNoSwapAndExactQuota(t *testing.T) {
	b, err := calculateBuildBudget(buildHost(), portainer.DefaultComposeBuildPolicy())
	require.NoError(t, err)
	h := &container.HostConfig{CgroupnsMode: container.CgroupnsModePrivate, Resources: container.Resources{CPUPeriod: 100000, CPUQuota: b.Quota, Memory: b.Memory, MemorySwap: b.Memory}}
	require.True(t, builderResourcesMatch(h, b))
	h.MemorySwap = -1
	require.False(t, builderResourcesMatch(h, b))
	h.MemorySwap = b.Memory
	h.CPUQuota++
	require.False(t, builderResourcesMatch(h, b))
	require.False(t, builderResourcesMatch(nil, b))
}

func TestBuildQueueSerializesAndCancellationReleasesWaiter(t *testing.T) {
	q := &hostBuildQueue{changed: make(chan struct{})}
	p := portainer.DefaultComposeBuildPolicy()
	read := func() (portainer.ComposeBuildPolicy, error) { return p, nil }
	prepare := func(portainer.ComposeBuildPolicy, buildBudget, bool) error { return nil }
	release, _, err := q.acquire(t.Context(), read, buildHost(), prepare)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, _, err := q.acquire(ctx, read, buildHost(), func(portainer.ComposeBuildPolicy, buildBudget, bool) error {
			return errors.New("must not start a second build")
		})
		result <- err
	}()
	require.Eventually(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.waiters) == 1 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	release()
	release() // release is idempotent
	release, _, err = q.acquire(t.Context(), read, buildHost(), prepare)
	require.NoError(t, err)
	release()
}

func TestBuildQueueConcurrentSlotsShareBudgetAndPolicyChangeDrains(t *testing.T) {
	q := &hostBuildQueue{changed: make(chan struct{})}
	p := portainer.DefaultComposeBuildPolicy()
	p.MaxConcurrentBuilds = 2
	var policy atomic.Value
	policy.Store(p)
	read := func() (portainer.ComposeBuildPolicy, error) { return policy.Load().(portainer.ComposeBuildPolicy), nil }
	var budgets []buildBudget
	prepare := func(_ portainer.ComposeBuildPolicy, b buildBudget, _ bool) error {
		budgets = append(budgets, b)
		return nil
	}
	release1, _, err := q.acquire(t.Context(), read, buildHost(), prepare)
	require.NoError(t, err)
	release2, _, err := q.acquire(t.Context(), read, buildHost(), prepare)
	require.NoError(t, err)
	require.Equal(t, budgets[0], budgets[1])
	p.CPUPercent = 25
	p.MemoryPercent = 25
	p.MaxConcurrentBuilds = 1
	policy.Store(p)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		release, actual, err := q.acquire(t.Context(), read, buildHost(), func(actual portainer.ComposeBuildPolicy, b buildBudget, _ bool) error {
			if actual != p || b.Quota != 400000 {
				return errors.New("queued build used stale policy")
			}
			close(started)
			return nil
		})
		if err == nil {
			if actual != p {
				err = errors.New("incorrect policy")
			}
			release()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.waiters) == 1 }, time.Second, time.Millisecond)
	release1()
	select {
	case <-started:
		t.Fatal("policy changed while another build was running")
	default:
	}
	release2()
	require.NoError(t, <-done)
}

func TestBuildQueueSetupFailureDoesNotBlockLaterBuilds(t *testing.T) {
	q := &hostBuildQueue{changed: make(chan struct{})}
	read := func() (portainer.ComposeBuildPolicy, error) { return portainer.DefaultComposeBuildPolicy(), nil }
	_, _, err := q.acquire(t.Context(), read, buildHost(), func(portainer.ComposeBuildPolicy, buildBudget, bool) error { return errors.New("unsupported limits") })
	require.ErrorContains(t, err, "unsupported limits")
	release, _, err := q.acquire(t.Context(), read, buildHost(), func(_ portainer.ComposeBuildPolicy, _ buildBudget, first bool) error {
		require.True(t, first)
		return nil
	})
	require.NoError(t, err)
	release()
}
