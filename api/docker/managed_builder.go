package docker

import (
	"github.com/docker/docker/api/types/container"
	"slices"
	"strings"
)

// Managed Compose builders are infrastructure, not application workloads.
// Check both names and the marker so renaming does not remove protection.
func IsManagedComposeBuilder(c container.InspectResponse) bool {
	return (c.ContainerJSONBase != nil && strings.HasPrefix(strings.TrimPrefix(c.Name, "/"), "buildx_buildkit_portainer-compose-")) ||
		(c.Config != nil && slices.Contains(c.Config.Env, "PORTAINER_MANAGED_COMPOSE_BUILDER=1"))
}
