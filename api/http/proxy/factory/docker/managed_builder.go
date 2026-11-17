package docker

import (
	portainer "github.com/portainer/portainer/api"
	dockertypes "github.com/portainer/portainer/api/docker"
	"github.com/portainer/portainer/api/http/proxy/factory/utils"
	"net/http"
	"strings"
)

// Called after normal resource authorization, for non-global-admin callers.
// Internal Compose clients use their own authenticated connection, not this proxy.
func (t *Transport) executeRestrictedContainerRequest(r *http.Request, id string, kind portainer.ResourceControlType) (*http.Response, error) {
	unsafe := (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Header.Get("Upgrade") != "" || strings.Contains(r.URL.Path, "/attach")
	if kind == portainer.ContainerResourceControl && unsafe {
		cli, err := t.dockerClientFactory.CreateClient(t.endpoint, r.Header.Get(portainer.PortainerAgentTargetHeader), nil)
		if err != nil {
			return nil, err
		}
		defer cli.Close()
		item, err := cli.ContainerInspect(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if dockertypes.IsManagedComposeBuilder(item) {
			return utils.WriteAccessDeniedResponse()
		}
	}
	return t.executeDockerRequest(r)
}
