package compose

import (
	"context"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/api"
	composeapi "github.com/docker/compose/v2/pkg/compose"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

type configOnlyCompose struct {
	api.Compose
	called bool
	t      *testing.T
}

func (m *configOnlyCompose) Up(_ context.Context, p *types.Project, opts api.UpOptions) error {
	m.called = true
	require.Len(m.t, p.Services, 1)
	service := p.Services["web"]
	require.Equal(m.t, "sha256:pinned", service.Image)
	require.Equal(m.t, types.PullPolicyNever, service.PullPolicy)
	require.Nil(m.t, service.Build)
	require.Empty(m.t, service.DependsOn)
	require.Equal(m.t, []string{"web"}, opts.Create.Services)
	require.Equal(m.t, []string{"web"}, opts.Start.Services)
	require.Equal(m.t, api.RecreateNever, opts.Create.RecreateDependencies)
	require.False(m.t, opts.Create.RemoveOrphans)
	require.True(m.t, opts.Create.IgnoreOrphans)
	require.True(m.t, opts.Start.Wait)
	return nil
}

func TestConfigOnlyDeploymentPreservesImagesAndOtherServices(t *testing.T) {
	file := createFile(t, t.TempDir(), "compose.yml", `services:
  web:
    image: app:mutable-tag
    build: .
    depends_on:
      - db
  db:
    image: postgres:17
`)
	mock := &configOnlyCompose{t: t}
	d := &ComposeDeployer{createComposeServiceFn: func(command.Cli, ...composeapi.Option) api.Compose { return mock }}
	err := d.Deploy(t.Context(), []string{file}, libstack.DeployOptions{Options: libstack.Options{ProjectName: "test", Env: []string{"COMPOSE_REMOVE_ORPHANS=true"}}, ConfigOnly: true, ServiceImages: map[string]string{"web": "sha256:pinned"}, ForceRecreate: true})
	require.NoError(t, err)
	require.True(t, mock.called)
	// Build is deliberately not implemented by the mock: invoking it would panic.
}
