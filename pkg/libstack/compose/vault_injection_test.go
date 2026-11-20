package compose

import (
	"context"
	"errors"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/api"
	dockercompose "github.com/docker/compose/v2/pkg/compose"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

type vaultInjectionCompose struct {
	api.Compose
	project *types.Project
	err     error
}

func (c *vaultInjectionCompose) Up(_ context.Context, project *types.Project, _ api.UpOptions) error {
	c.project = project
	return c.err
}

func TestDeployInjectsVaultValuesPerServiceAndLabelsOnlyNames(t *testing.T) {
	t.Parallel()

	composeService := &vaultInjectionCompose{}
	deployer := NewComposeDeployer()
	deployer.createComposeServiceFn = func(command.Cli, ...dockercompose.Option) api.Compose { return composeService }

	file := createFile(t, t.TempDir(), "compose.yaml", `services:
  backend:
    image: nginx:alpine
    environment:
      DB_PASSWORD: compose-default
  worker:
    image: nginx:alpine
`)
	secret := "value with $ dollar, = equals, and\nnewline"
	err := deployer.Deploy(t.Context(), []string{file}, libstack.DeployOptions{
		Options: libstack.Options{ProjectName: "vault-injection-unit"},
		VaultEnv: map[string]map[string]string{
			"backend": {"DB_PASSWORD": secret},
			"worker":  {"DB_PASSWORD": "worker-secret"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, composeService.project)
	backend := composeService.project.Services["backend"]
	worker := composeService.project.Services["worker"]
	require.NotNil(t, backend.Environment["DB_PASSWORD"])
	require.Equal(t, secret, *backend.Environment["DB_PASSWORD"])
	require.Equal(t, "worker-secret", *worker.Environment["DB_PASSWORD"])
	require.Equal(t, "DB_PASSWORD", backend.CustomLabels["io.portainer.vault.env"])
	require.Equal(t, "DB_PASSWORD", worker.CustomLabels["io.portainer.vault.env"])
	require.NotContains(t, backend.CustomLabels["io.portainer.vault.env"], secret)
	require.NotContains(t, composeService.project.Environment, "DB_PASSWORD")
}

func TestDeployVaultInjectionRejectsUnknownServicesAndSanitizesComposeErrors(t *testing.T) {
	t.Parallel()

	file := createFile(t, t.TempDir(), "compose.yaml", `services:
  backend:
    image: nginx:alpine
`)

	t.Run("unknown service fails before compose up", func(t *testing.T) {
		composeService := &vaultInjectionCompose{}
		deployer := NewComposeDeployer()
		deployer.createComposeServiceFn = func(command.Cli, ...dockercompose.Option) api.Compose { return composeService }

		err := deployer.Deploy(t.Context(), []string{file}, libstack.DeployOptions{
			Options:  libstack.Options{ProjectName: "vault-injection-unknown-service"},
			VaultEnv: map[string]map[string]string{"missing": {"PASSWORD": "must-not-leak"}},
		})

		require.ErrorContains(t, err, "Vault mapping targets unknown service missing")
		require.NotContains(t, err.Error(), "must-not-leak")
		require.Nil(t, composeService.project, "the compose operation must not start for an invalid target")
	})

	t.Run("compose error does not leak the resolved value", func(t *testing.T) {
		const secret = "sensitive-compose-error-value"
		composeService := &vaultInjectionCompose{err: errors.New("compose failed while echoing " + secret)}
		deployer := NewComposeDeployer()
		deployer.createComposeServiceFn = func(command.Cli, ...dockercompose.Option) api.Compose { return composeService }

		err := deployer.Deploy(t.Context(), []string{file}, libstack.DeployOptions{
			Options:  libstack.Options{ProjectName: "vault-injection-sanitized-error"},
			VaultEnv: map[string]map[string]string{"backend": {"PASSWORD": secret}},
		})

		require.ErrorContains(t, err, "compose up operation failed for a Vault-enabled stack")
		require.NotContains(t, err.Error(), secret)
	})
}
