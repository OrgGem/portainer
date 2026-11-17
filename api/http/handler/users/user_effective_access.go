package users

import (
	"fmt"
	"net/http"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/errors"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// AccessLocation describes which part of the access model granted the user
// their effective role on an environment. The frontend maps these enum values
// to display strings.
type AccessLocation string

const (
	AccessLocationEnvironment      AccessLocation = "environment"
	AccessLocationEnvironmentGroup AccessLocation = "environmentGroup"
)

// @id UserEffectiveAccessInspect
// @summary Inspect a user's effective access on every environment
// @description Returns the resolved role for each environment the user can access,
// @description following the policy precedence used by the access viewer
// @description (user-endpoint, user-group, team-endpoint, team-group).
// @description Environments where the user has no role are omitted.
// @description **Access policy**: restricted
// @tags users
// @security ApiKeyAuth || jwt
// @produce json
// @param id path int true "User identifier"
// @success 200 {array} EffectiveAccessEntry "Success"
// @failure 400 "Invalid request"
// @failure 403 "Permission denied"
// @failure 404 "User not found"
// @failure 500 "Server error"
// @router /users/{id}/effective-access [get]
func (handler *Handler) userEffectiveAccess(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	userID, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid user identifier route variable", err)
	}

	tokenData, err := security.RetrieveTokenData(r)
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve user authentication token", err)
	}
	if tokenData.Role != portainer.AdministratorRole && tokenData.ID != portainer.UserID(userID) {
		return httperror.Forbidden("Permission denied to inspect another user's effective access", errors.ErrUnauthorized)
	}

	entries := make([]EffectiveAccessEntry, 0)
	if err := handler.DataStore.ViewTx(func(tx dataservices.DataStoreTx) error {
		user, err := tx.User().Read(portainer.UserID(userID))
		if handler.DataStore.IsErrObjectNotFound(err) {
			return httperror.NotFound("Unable to find a user with the specified identifier inside the database", err)
		} else if err != nil {
			return httperror.InternalServerError("Unable to find a user with the specified identifier inside the database", err)
		}

		endpoints, err := tx.Endpoint().ReadAll()
		if err != nil {
			return httperror.InternalServerError("Unable to retrieve environments from the database", err)
		}

		groups, err := tx.EndpointGroup().ReadAll()
		if err != nil {
			return httperror.InternalServerError("Unable to retrieve environment groups from the database", err)
		}

		roles, err := tx.Role().ReadAll()
		if err != nil {
			return httperror.InternalServerError("Unable to retrieve roles from the database", err)
		}

		teams, err := tx.Team().ReadAll()
		if err != nil {
			return httperror.InternalServerError("Unable to retrieve teams from the database", err)
		}

		memberships, err := tx.TeamMembership().TeamMembershipsByUserID(user.ID)
		if err != nil {
			return httperror.InternalServerError("Unable to retrieve team memberships from the database", err)
		}

		controls, err := tx.ResourceControl().ReadAll()
		if err != nil {
			return err
		}

		groupsByID := make(map[portainer.EndpointGroupID]portainer.EndpointGroup, len(groups))
		for _, g := range groups {
			groupsByID[g.ID] = g
		}

		teamsByID := make(map[portainer.TeamID]portainer.Team, len(teams))
		for _, t := range teams {
			teamsByID[t.ID] = t
		}

		for i := range endpoints {
			endpoint := &endpoints[i]
			if user.Role == portainer.AdministratorRole {
				entries = append(entries, EffectiveAccessEntry{EndpointID: endpoint.ID, EndpointName: endpoint.Name, RoleName: "Global administrator", ResourceScope: "All resources; global administrator bypasses Standalone role restrictions", AccessLocation: AccessLocationEnvironment})
				continue
			}
			group := groupsByID[endpoint.GroupID]
			if authorization.SupportsStandaloneRoles(endpoint) && (authorization.HasStandalonePolicies(endpoint.UserAccessPolicies, endpoint.TeamAccessPolicies) || authorization.HasStandalonePolicies(group.UserAccessPolicies, group.TeamAccessPolicies)) {
				access := authorization.ResolveStandaloneAccess(authorization.ResolverInput{User: user, Endpoint: endpoint, EndpointGroup: group, UserMemberships: memberships})
				if access != nil {
					definition := authorization.StandaloneRoleDefinition(access.Role)
					if definition != nil {
						entry := buildEffectiveAccessEntry(&authorization.ResolvedAccess{Role: definition, Source: access.Source}, endpoint, groupsByID, teamsByID)
						entry.StandaloneRole = access.Role
						if !access.EnvironmentAdmin() {
							if !endpoint.SecuritySettings.AllowStackManagementForRegularUsers {
								entry.Restrictions = append(entry.Restrictions, "Environment disables stack management for regular users")
							}
							if !endpoint.SecuritySettings.AllowBindMountsForRegularUsers {
								entry.Restrictions = append(entry.Restrictions, "Environment disallows bind mounts for regular users")
							}
							if !endpoint.SecuritySettings.AllowPrivilegedModeForRegularUsers {
								entry.Restrictions = append(entry.Restrictions, "Environment disallows privileged containers for regular users")
							}
						}
						count := 0
						for _, c := range controls {
							if c.Type == portainer.StackResourceControl && c.StackPolicy != nil && strings.HasPrefix(c.ResourceID, fmt.Sprintf("%d_", endpoint.ID)) {
								count++
							}
						}
						if count > 0 {
							entry.Restrictions = append(entry.Restrictions, fmt.Sprintf("%d stack(s) have action policies; direct Docker writes are restricted unless environment administrator", count))
						}
						entry.ResourceScope = "Assigned and public resources; ownership and stack policies still apply"
						if access.AllResources() {
							entry.ResourceScope = "All environment resources; stack policies still apply"
						}
						if access.EnvironmentAdmin() {
							entry.ResourceScope = "All environment resources; environment administrator bypasses stack ownership/action policies"
						}
						if access.Explicit {
							entry.Policy, err = authorization.ReadStandalonePolicy(tx, access.Role)
							if err != nil {
								return err
							}
							entry.Features = authorization.StandaloneFeatures
						} else {
							entry.ResourceScope = "Legacy CE grant; configurable Standalone role restrictions do not apply. Assign an explicit Standalone role to configure access."
						}
						entries = append(entries, entry)
					}
				}
				continue
			}

			access := authorization.ResolveUserEndpointAccess(authorization.ResolverInput{
				User:            user,
				Endpoint:        endpoint,
				EndpointGroup:   groupsByID[endpoint.GroupID],
				Roles:           roles,
				UserMemberships: memberships,
			})
			if access == nil {
				continue
			}

			entries = append(entries, buildEffectiveAccessEntry(access, endpoint, groupsByID, teamsByID))
		}
		return nil
	}); err != nil {
		return response.TxErrorResponse(err)
	}

	return response.JSON(w, entries)
}

func buildEffectiveAccessEntry(
	access *authorization.ResolvedAccess,
	endpoint *portainer.Endpoint,
	groupsByID map[portainer.EndpointGroupID]portainer.EndpointGroup,
	teamsByID map[portainer.TeamID]portainer.Team,
) EffectiveAccessEntry {
	entry := EffectiveAccessEntry{
		EndpointID:     endpoint.ID,
		EndpointName:   endpoint.Name,
		RoleID:         access.Role.ID,
		RoleName:       access.Role.Name,
		RolePriority:   access.Role.Priority,
		AccessLocation: AccessLocationEnvironment,
	}

	if access.Source.GroupID != 0 {
		entry.GroupID = access.Source.GroupID
		entry.AccessLocation = AccessLocationEnvironmentGroup
		if g, ok := groupsByID[access.Source.GroupID]; ok {
			entry.GroupName = g.Name
		}
	}

	if access.Source.TeamID != 0 {
		entry.TeamID = access.Source.TeamID
		if t, ok := teamsByID[access.Source.TeamID]; ok {
			entry.TeamName = t.Name
		}
	}

	return entry
}

type EffectiveAccessEntry struct {
	Restrictions   []string                        `json:"restrictions,omitempty"`
	StandaloneRole portainer.StandaloneRole        `json:"standaloneRole,omitempty"`
	Policy         *portainer.StandaloneRolePolicy `json:"policy,omitempty"`
	Features       []authorization.RoleFeature     `json:"features,omitempty"`
	ResourceScope  string                          `json:"resourceScope,omitempty"`
	EndpointID     portainer.EndpointID            `json:"endpointId"`
	EndpointName   string                          `json:"endpointName"`
	RoleID         portainer.RoleID                `json:"roleId"`
	RoleName       string                          `json:"roleName"`
	RolePriority   int                             `json:"rolePriority"`
	GroupID        portainer.EndpointGroupID       `json:"groupId,omitempty"`
	GroupName      string                          `json:"groupName,omitempty"`
	TeamID         portainer.TeamID                `json:"teamId,omitempty"`
	TeamName       string                          `json:"teamName,omitempty"`
	AccessLocation AccessLocation                  `json:"accessLocation"`
}
