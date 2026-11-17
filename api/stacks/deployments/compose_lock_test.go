package deployments

import (
	"context"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
)

type blockingComposeManager struct {
	portainer.ComposeStackManager
	entered chan portainer.StackID
	proceed chan struct{}
}

func (m *blockingComposeManager) Up(ctx context.Context, stack *portainer.Stack, _ *portainer.Endpoint, _ portainer.ComposeUpOptions) error {
	m.entered <- stack.ID
	select {
	case <-m.proceed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *blockingComposeManager) Down(_ context.Context, stack *portainer.Stack, _ *portainer.Endpoint) error {
	m.entered <- stack.ID
	return nil
}

func TestComposeDeploymentAllowsDifferentStacksToReachHostScheduler(t *testing.T) {
	m := &blockingComposeManager{entered: make(chan portainer.StackID, 2), proceed: make(chan struct{})}
	d := NewStackDeployer(nil, m, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 2)
	for _, id := range []portainer.StackID{1, 2} {
		go func() {
			done <- d.DeployComposeStack(ctx, &portainer.Stack{ID: id}, &portainer.Endpoint{ID: 1}, nil, false, false, false)
		}()
	}
	for range 2 {
		select {
		case <-m.entered:
		case <-time.After(time.Second):
			t.Fatal("unrelated stack was blocked by a global mutex")
		}
	}
	close(m.proceed)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.Empty(t, d.composeLocks)
}

func TestComposeUndeployWaitsForSameStackDeployment(t *testing.T) {
	m := &blockingComposeManager{entered: make(chan portainer.StackID, 2), proceed: make(chan struct{})}
	d := NewStackDeployer(nil, m, nil, nil, nil)
	stack, endpoint := &portainer.Stack{ID: 1}, &portainer.Endpoint{ID: 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- d.DeployComposeStack(ctx, stack, endpoint, nil, false, false, false) }()
	<-m.entered
	go func() { done <- d.UndeployComposeStack(ctx, stack, endpoint) }()
	require.Eventually(t, func() bool {
		d.composeLockMutex.Lock()
		defer d.composeLockMutex.Unlock()
		return d.composeLocks[1].references == 2
	}, time.Second, time.Millisecond)
	select {
	case <-m.entered:
		t.Fatal("undeploy raced an active deployment")
	default:
	}
	close(m.proceed)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.Equal(t, portainer.StackID(1), <-m.entered)
	require.Empty(t, d.composeLocks)
}
