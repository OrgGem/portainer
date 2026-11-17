package users

import (
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/internal/authorization"
	roleauth "github.com/portainer/portainer/pkg/authorization"
)

func (h *Handler) decorateStandaloneRoles(user *portainer.User) error {
	user.StandaloneEndpointRoles = nil
	if user.Role == portainer.AdministratorRole {
		return nil
	}
	endpoints, err := h.DataStore.Endpoint().Endpoints()
	if err != nil {
		return err
	}
	user.StandaloneEndpointRoles = make(map[portainer.EndpointID]portainer.StandaloneRole)
	if user.EndpointAuthorizations == nil {
		user.EndpointAuthorizations = make(portainer.EndpointAuthorizations)
	}
	for i := range endpoints {
		endpoint := &endpoints[i]
		access, err := roleauth.StandaloneAccessForUser(h.DataStore, user.ID, endpoint)
		if err != nil {
			return err
		}
		if access == nil || !access.Explicit {
			continue
		}
		user.StandaloneEndpointRoles[endpoint.ID] = access.Role
		policy, err := roleauth.ReadStandalonePolicy(h.DataStore, access.Role)
		if err != nil {
			return err
		}
		user.EndpointAuthorizations[endpoint.ID] = standaloneUIAuthorizations(access.Role)
		for op := range user.EndpointAuthorizations[endpoint.ID] {
			if !roleauth.RoleFeatureAllowed(policy, access.Role, uiOperationFeatures(string(op))...) {
				delete(user.EndpointAuthorizations[endpoint.ID], op)
			}
		}
		if !roleauth.SupportsStandaloneRoles(endpoint) {
			user.EndpointAuthorizations[endpoint.ID] = portainer.Authorizations{}
		}
	}
	return nil
}

func standaloneUIAuthorizations(role portainer.StandaloneRole) portainer.Authorizations {
	var permissions portainer.Authorizations
	switch role {
	case portainer.StandaloneEnvironmentAdmin:
		permissions = authorization.DefaultEndpointAuthorizationsForEndpointAdministratorRole()
	case portainer.StandaloneStandard:
		permissions = authorization.DefaultEndpointAuthorizationsForStandardUserRole(false)
	case portainer.StandaloneHelpdesk, portainer.StandaloneOperator:
		permissions = authorization.DefaultEndpointAuthorizationsForHelpDeskRole(false)
	case portainer.StandaloneReadOnly:
		permissions = authorization.DefaultEndpointAuthorizationsForReadOnlyUserRole(false)
	default:
		return portainer.Authorizations{}
	}
	for operation := range permissions {
		name := string(operation)
		if strings.HasPrefix(name, "DockerPlugin") || strings.HasPrefix(name, "DockerNode") || strings.HasPrefix(name, "DockerSwarm") || strings.HasPrefix(name, "DockerAgent") {
			delete(permissions, operation)
		}
	}
	if role == portainer.StandaloneReadOnly || role == portainer.StandaloneHelpdesk {
		for _, op := range []portainer.Authorization{portainer.OperationDockerContainerArchive, portainer.OperationDockerContainerArchiveInfo, portainer.OperationDockerContainerExport, portainer.OperationDockerImageGet, portainer.OperationDockerImageGetAll} {
			delete(permissions, op)
		}
	}
	if role == portainer.StandaloneOperator {
		for _, op := range []portainer.Authorization{portainer.OperationDockerContainerStart, portainer.OperationDockerContainerStop, portainer.OperationDockerContainerRestart, portainer.OperationDockerContainerKill, portainer.OperationDockerContainerPause, portainer.OperationDockerContainerUnpause, portainer.OperationDockerContainerUpdate, portainer.OperationDockerContainerRename, portainer.OperationDockerContainerExec, portainer.OperationDockerContainerAttach, portainer.OperationDockerContainerAttachWebsocket, portainer.OperationDockerExecStart, portainer.OperationDockerExecInspect, portainer.OperationDockerExecResize, portainer.OperationPortainerStackUpdate} {
			permissions[op] = true
		}
	}
	if role == portainer.StandaloneEnvironmentAdmin {
		for _, op := range []portainer.Authorization{portainer.OperationDockerAgentBrowseList, portainer.OperationDockerAgentBrowseGet, portainer.OperationDockerAgentBrowsePut, portainer.OperationDockerAgentBrowseRename, portainer.OperationDockerAgentBrowseDelete} {
			permissions[op] = true
		}
	}
	return permissions
}

// UI permissions are hints only; live API guards remain authoritative.
func uiOperationFeatures(op string) []string {
	for prefix, kind := range map[string]string{"DockerContainer": "containers", "DockerImage": "images", "DockerNetwork": "networks", "DockerVolume": "volumes"} {
		if strings.HasPrefix(op, prefix) {
			action := strings.TrimPrefix(op, prefix)
			switch action {
			case "List", "Inspect", "Logs", "Top", "Changes", "History", "Search":
				return []string{kind + ".view"}
			case "Stats":
				return []string{kind + ".view", "metrics.view"}
			case "Exec", "Attach", "AttachWebsocket", "Resize":
				return []string{kind + ".view", "containers.console"}
			case "Archive", "ArchiveInfo", "Export":
				return []string{kind + ".view", "containers.files"}
			case "Start", "Stop", "Restart", "Kill", "Pause", "Unpause", "Update", "Rename", "Wait":
				if kind == "containers" {
					return []string{kind + ".view", "containers.operate"}
				}
			}
			return []string{kind + ".view", kind + ".manage"}
		}
	}
	if strings.HasPrefix(op, "DockerExec") {
		return []string{"containers.view", "containers.console"}
	}
	if strings.HasPrefix(op, "DockerAgentBrowse") {
		return []string{"volumes.view", "volumes.files"}
	}
	if strings.HasPrefix(op, "PortainerStack") {
		if strings.Contains(op, "List") || strings.Contains(op, "Inspect") || strings.Contains(op, "File") {
			return []string{"stacks.view"}
		}
		if strings.Contains(op, "Update") {
			return []string{"stacks.view", "stacks.deploy"}
		}
		return []string{"stacks.view", "stacks.edit"}
	}
	return []string{"environment.view"}
}
