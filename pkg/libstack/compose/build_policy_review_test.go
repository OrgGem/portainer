package compose

import (
	"context"
	"github.com/docker/docker/api/types/container"
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestReviewCancelledWaiterDuringBuilderSetup(t *testing.T) {
	q := &hostBuildQueue{changed: make(chan struct{})}
	p := portainer.DefaultComposeBuildPolicy()
	read := func() (portainer.ComposeBuildPolicy, error) { return p, nil }
	preparing, unblock, firstDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(firstDone)
		release, _, err := q.acquire(context.Background(), read, buildHost(), func(portainer.ComposeBuildPolicy, buildBudget, bool) error {
			close(preparing)
			<-unblock
			return nil
		})
		if err == nil {
			release()
		}
	}()
	<-preparing
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, _, err := q.acquire(ctx, read, buildHost(), func(portainer.ComposeBuildPolicy, buildBudget, bool) error { return nil })
		if err == nil {
			release()
		}
		done <- err
	}()
	timely := false
	select {
	case <-done:
		timely = true
	case <-time.After(200 * time.Millisecond):
	}
	close(unblock)
	<-firstDone
	if !timely {
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
	}
	require.True(t, timely, "cancelled waiter cannot return until another job's builder setup finishes")
}

func TestReviewBuilderRejectsHostCgroupNamespace(t *testing.T) {
	b, err := calculateBuildBudget(buildHost(), portainer.DefaultComposeBuildPolicy())
	require.NoError(t, err)
	h := &container.HostConfig{CgroupnsMode: container.CgroupnsModeHost, Resources: container.Resources{CPUPeriod: 100000, CPUQuota: b.Quota, Memory: b.Memory, MemorySwap: b.Memory}}
	require.False(t, builderResourcesMatch(h, b), "host cgroup namespace does not confine BuildKit child cgroups")
}
