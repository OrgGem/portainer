package stacks

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"net/http"
)

// Mutation responses also carry stack metadata. Redact a response copy, never
// the object that deployment code may still persist asynchronously.
func (h *Handler) writeRoleStackResponse(w http.ResponseWriter, r *http.Request, stack *portainer.Stack, err error) *httperror.HandlerError {
	if err != nil {
		return response.TxResponse(w, nil, err)
	}
	if stack == nil {
		return response.JSON(w, stack)
	}
	copy := *stack
	ctx, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return httperror.InternalServerError("Unable to read access context", err)
	}
	if !ctx.IsAdmin && ctx.StandaloneRole != "" {
		p, err := authorization.ReadStandalonePolicy(h.DataStore, ctx.StandaloneRole)
		if err != nil {
			return httperror.InternalServerError("Unable to read role policy", err)
		}
		if !authorization.RoleFeatureAllowed(p, ctx.StandaloneRole, "vault.view") {
			copy.VaultMappings = nil
		}
	}
	return response.JSON(w, copy)
}
