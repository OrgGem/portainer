package roles

import (
	"github.com/portainer/portainer/pkg/authorization"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// @id RoleList
// @summary List roles
// @description List all roles available for use
// @description **Access policy**: administrator
// @tags roles
// @security ApiKeyAuth
// @security jwt
// @produce json
// @success 200 {array} portainer.Role "Success"
// @failure 500 "Server error"
// @router /roles [get]
func (handler *Handler) roleList(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	roles := authorization.StandaloneRoleDefinitions()
	for i, key := range roleKeys {
		policy, err := authorization.ReadStandalonePolicy(handler.DataStore, key)
		if err != nil {
			return httperror.InternalServerError("Unable to read role policies", err)
		}
		roles[i].StandalonePolicy = policy
	}
	return response.JSON(w, roles)
}
