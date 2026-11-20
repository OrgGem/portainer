package exec

import (
	"context"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

type vaultEnvironmentCaptureDeployer struct {
	libstack.Deployer
	options   libstack.DeployOptions
	filePaths []string
}

func (deployer *vaultEnvironmentCaptureDeployer) Deploy(_ context.Context, paths []string, options libstack.DeployOptions) error {
	deployer.filePaths = paths
	deployer.options = options
	return nil
}

func TestComposeStackManagerPassesVaultValuesOnlyToDeploymentOptions(t *testing.T) {
	t.Parallel()

	values := map[string]map[string]string{
		"backend": {"DB_PASSWORD": "synthetic-password"},
	}
	deployer := &vaultEnvironmentCaptureDeployer{}
	manager := NewComposeStackManager(deployer, nil)
	stack := &portainer.Stack{
		ID: 1, Name: "vault-env-test", Type: portainer.DockerComposeStack,
		ProjectPath: t.TempDir(), EntryPoint: "compose.yaml",
	}
	endpoint := &portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}

	err := manager.Up(t.Context(), stack, endpoint, portainer.ComposeUpOptions{VaultEnv: values, ForceRecreate: true})

	require.NoError(t, err)
	require.Equal(t, []string{stack.ProjectPath + "/compose.yaml"}, deployer.filePaths)
	require.Equal(t, values, deployer.options.VaultEnv)
	require.True(t, deployer.options.ForceRecreate)
	require.NotContains(t, deployer.options.EnvFilePath, "synthetic-password")
}
