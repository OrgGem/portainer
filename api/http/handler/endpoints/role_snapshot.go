package endpoints

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/pkg/authorization"
	"net/http"
)

// Snapshot aggregates bypass Docker proxy guards; suppress the whole aggregate
// if a role cannot read any of its constituent resource categories.
func (h *Handler) roleSnapshotVisible(r *http.Request, endpoint *portainer.Endpoint) (bool, error) {
	token, err := security.RetrieveTokenData(r)
	if err != nil {
		return false, err
	}
	if token.Role == portainer.AdministratorRole {
		return true, nil
	}
	access, err := authorization.StandaloneAccessForUser(h.DataStore, token.ID, endpoint)
	if err != nil {
		return false, err
	}
	if access == nil || !access.Explicit {
		return true, nil
	}
	p, err := authorization.ReadStandalonePolicy(h.DataStore, access.Role)
	if err != nil {
		return false, err
	}
	return authorization.RoleFeatureAllowed(p, access.Role, "environment.view", "containers.view", "images.view", "networks.view", "volumes.view", "stacks.view", "metrics.view"), nil
}
