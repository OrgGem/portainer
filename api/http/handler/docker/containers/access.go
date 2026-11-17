package containers

import (
	"net/http"

	containertypes "github.com/docker/docker/api/types/container"
	portainer "github.com/portainer/portainer/api"
	dockertypes "github.com/portainer/portainer/api/docker"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/uac"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
)

// Inspect canonical IDs and labels before resolving direct or inherited ownership.
func (h *Handler) authorizeContainer(r *http.Request, endpoint *portainer.Endpoint, id string) (string, *httperror.HandlerError) {
	ctx, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return "", httperror.Forbidden("Authentication required", err)
	}
	cli, err := h.dockerClientFactory.CreateClient(endpoint, r.Header.Get(portainer.PortainerAgentTargetHeader), nil)
	if err != nil {
		return "", httperror.InternalServerError("Unable to connect to Docker", err)
	}
	defer cli.Close()
	item, err := cli.ContainerInspect(r.Context(), id)
	if err != nil {
		return "", httperror.NotFound("Container not found", err)
	}
	if !ctx.IsAdmin && r.Method != http.MethodGet && dockertypes.IsManagedComposeBuilder(item) {
		return "", httperror.Forbidden("Managed Compose builders can only be modified by global administrators", nil)
	}
	labels := map[string]string{}
	if item.Config != nil {
		labels = item.Config.Labels
	}
	control, err := uac.ContainerResourceControlGetter(h.dataStore, endpoint.ID)(containertypes.Summary{ID: item.ID, Labels: labels})
	if err != nil {
		return "", httperror.InternalServerError("Unable to resolve container ownership", err)
	}
	access, err := security.EndpointStandaloneAccess(r, h.dataStore, endpoint)
	if err != nil {
		return "", httperror.Forbidden("Environment access denied", err)
	}
	if !ctx.IsAdmin && !authorization.StandaloneResourceAccess(access, ctx.UserID, authorization.TeamIDs(ctx.UserMemberships), control) {
		return "", httperror.Forbidden("Container access denied", nil)
	}
	return item.ID, nil
}
