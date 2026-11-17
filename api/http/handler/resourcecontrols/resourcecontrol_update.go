package resourcecontrols

import (
	"errors"
	"net/http"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	httperrors "github.com/portainer/portainer/api/http/errors"
	"github.com/portainer/portainer/api/http/security"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type resourceControlUpdatePayload struct {
	// Permit access to the associated resource to any user
	Public bool `example:"true"`
	// List of user identifiers with access to the associated resource
	Users []int `example:"4"`
	// List of team identifiers with access to the associated resource
	Teams []int `example:"7"`
	// Permit access to resource only to admins
	AdministratorsOnly bool `example:"true"`
}

func (payload *resourceControlUpdatePayload) Validate(r *http.Request) error {
	if len(payload.Users) == 0 && len(payload.Teams) == 0 && !payload.Public && !payload.AdministratorsOnly {
		return errors.New("invalid payload: must specify Users, Teams, Public or AdministratorsOnly")
	}

	if payload.Public && payload.AdministratorsOnly {
		return errors.New("invalid payload: cannot set public and administrators only")
	}

	return nil
}

// @id ResourceControlUpdate
// @summary Update a resource control
// @description Update a resource control
// @description **Access policy**: authenticated
// @tags resource_controls
// @security ApiKeyAuth
// @security jwt
// @accept json
// @produce json
// @param id path int true "Resource control identifier"
// @param body body resourceControlUpdatePayload true "Resource control details"
// @success 200 {object} portainer.ResourceControl "Success"
// @failure 400 "Invalid request"
// @failure 403 "Unauthorized"
// @failure 404 "Resource control not found"
// @failure 500 "Server error"
// @router /resource_controls/{id} [put]
func (handler *Handler) resourceControlUpdate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	resourceControlID, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid resource control identifier route variable", err)
	}

	var payload resourceControlUpdatePayload
	if err := request.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	resourceControl, err := handler.DataStore.ResourceControl().Read(portainer.ResourceControlID(resourceControlID))
	if handler.DataStore.IsErrObjectNotFound(err) {
		return httperror.NotFound("Unable to find a resource control with the specified identifier inside the database", err)
	} else if err != nil {
		return httperror.InternalServerError("Unable to find a resource control with the specified identifier inside the database", err)
	}

	securityContext, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve info from request context", err)
	}

	if !security.AuthorizedResourceControlAccess(resourceControl, securityContext) {
		return httperror.Forbidden("Permission denied to access the resource control", httperrors.ErrResourceAccessDenied)
	}
	if !securityContext.IsAdmin {
		scoped, err := security.UserHasStandaloneRole(handler.DataStore, securityContext.UserID)
		if err != nil {
			return httperror.InternalServerError("Unable to verify scoped roles", err)
		}
		if scoped {
			return httperror.Forbidden("Scoped environment roles cannot change resource ownership", security.ErrStandaloneRoleDenied)
		}
	}
	if resourceControl.StackPolicy != nil && !securityContext.IsAdmin {
		return httperror.Forbidden("Only administrators may change ownership of a stack with an operation policy", httperrors.ErrResourceAccessDenied)
	}

	resourceControl.Public = payload.Public
	resourceControl.AdministratorsOnly = payload.AdministratorsOnly

	var userAccesses = make([]portainer.UserResourceAccess, 0)
	for _, v := range payload.Users {
		userAccess := portainer.UserResourceAccess{
			UserID:      portainer.UserID(v),
			AccessLevel: portainer.ReadWriteAccessLevel,
		}
		userAccesses = append(userAccesses, userAccess)
	}
	resourceControl.UserAccesses = userAccesses

	var teamAccesses = make([]portainer.TeamResourceAccess, 0)
	for _, v := range payload.Teams {
		teamAccess := portainer.TeamResourceAccess{
			TeamID:      portainer.TeamID(v),
			AccessLevel: portainer.ReadWriteAccessLevel,
		}
		teamAccesses = append(teamAccesses, teamAccess)
	}
	resourceControl.TeamAccesses = teamAccesses

	if !security.AuthorizedResourceControlUpdate(resourceControl, securityContext) {
		return httperror.Forbidden("Permission denied to update the resource control", httperrors.ErrResourceAccessDenied)
	}

	err = handler.DataStore.UpdateTx(func(tx dataservices.DataStoreTx) error {
		if !securityContext.IsAdmin {
			scoped, err := security.UserHasStandaloneRole(tx, securityContext.UserID)
			if err != nil {
				return err
			}
			if scoped {
				return httperrors.ErrResourceAccessDenied
			}
		}
		current, err := tx.ResourceControl().Read(resourceControl.ID)
		if err != nil {
			return err
		}
		// A policy may have been enabled after the initial ownership read. Check
		// and preserve the latest policy in the same transaction as this write.
		if !security.AuthorizedResourceControlAccess(current, securityContext) ||
			(current.StackPolicy != nil && !securityContext.IsAdmin) {
			return httperrors.ErrResourceAccessDenied
		}
		resourceControl.StackPolicy = current.StackPolicy
		return tx.ResourceControl().Update(resourceControl.ID, resourceControl)
	})
	if errors.Is(err, httperrors.ErrResourceAccessDenied) {
		return httperror.Forbidden("Permission denied to update the current resource control", err)
	}
	if err != nil {
		return httperror.InternalServerError("Unable to persist resource control changes inside the database", err)
	}

	return response.JSON(w, resourceControl)
}
