package compose

import (
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	portainer "github.com/portainer/portainer/api"
)

type ComposeDeployer struct {
	createComposeServiceFn func(command.Cli, ...compose.Option) api.Compose
	// Read on admission, including after waiting, so queued builds use current settings.
	GetBuildPolicy func() (portainer.ComposeBuildPolicy, error)
}

// NewComposeDeployer creates a new compose deployer
func NewComposeDeployer() *ComposeDeployer {
	return &ComposeDeployer{
		createComposeServiceFn: compose.NewComposeService,
	}
}
