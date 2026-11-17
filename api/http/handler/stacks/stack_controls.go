package stacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	"github.com/portainer/portainer/api/stacks/stackutils"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

var stackActions = []portainer.StackAction{portainer.StackView, portainer.StackConfigUpload, portainer.StackConfigApply, portainer.StackDeploy, portainer.StackEdit}

// stackAccess wraps every stack-ID route, including routes that predate policies.
// The lock serializes configuration and ordinary stack mutations. Async existing
// deployments persist Deploying before releasing it; later mutations reject it.
func (h *Handler) stackAccess(action portainer.StackAction, next httperror.LoggerHandler) httperror.LoggerHandler {
	return func(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
		// External Swarm deletion identifies a stack by name. Its existing handler
		// checks environment authorization and cannot use the managed-stack exemption.
		if external, _ := request.RetrieveBooleanQueryParameter(r, "external", true); external && r.Method == http.MethodDelete {
			return next(w, r)
		}
		id, err := request.RetrieveNumericRouteVariableValue(r, "id")
		if err != nil {
			return httperror.BadRequest("Invalid stack ID", err)
		}
		if r.Method != http.MethodGet {
			unlock := h.lockStackMutation(portainer.StackID(id))
			defer unlock()
			if r.Context().Err() != nil {
				return httperror.BadRequest("Request cancelled", r.Context().Err())
			}
		}
		stack, err := h.DataStore.Stack().Read(portainer.StackID(id))
		if h.DataStore.IsErrObjectNotFound(err) {
			return httperror.NotFound("Stack not found", err)
		}
		if err != nil {
			return httperror.InternalServerError("Unable to read stack", err)
		}
		ctx, err := security.RetrieveRestrictedRequestContext(r)
		if err != nil {
			return httperror.Forbidden("Authentication required", err)
		}
		if !ctx.IsAdmin {
			endpoint, err := h.DataStore.Endpoint().Endpoint(stack.EndpointID)
			if err != nil {
				return httperror.NotFound("Environment not found", err)
			}
			if err := security.CheckStandaloneEndpointRole(r, h.DataStore, endpoint, false); err != nil {
				return httperror.Forbidden("Environment role does not permit this stack operation", err)
			}
			if ctx.StandaloneRole != "" {
				if stack.Type != portainer.DockerComposeStack {
					return httperror.Forbidden("Environment roles only support Docker Compose stacks", nil)
				}
				if raw := r.URL.Query().Get("endpointId"); raw != "" && raw != fmt.Sprint(stack.EndpointID) {
					return httperror.BadRequest("Environment ID must match the stack", nil)
				}
			}
		}
		environmentAdmin := security.ScopedEnvironmentAdmin(ctx, stack.EndpointID)
		control, err := h.DataStore.ResourceControl().ResourceControlByResourceIDAndType(stackutils.ResourceControlID(stack.EndpointID, stack.Name), portainer.StackResourceControl)
		if err != nil {
			return httperror.InternalServerError("Unable to read stack ownership", err)
		}
		managed := control != nil && control.StackPolicy != nil
		if managed {
			if raw := r.URL.Query().Get("endpointId"); raw != "" && raw != fmt.Sprint(stack.EndpointID) {
				return httperror.BadRequest("Environment ID must match the stack's recorded environment", nil)
			}
			if strings.HasSuffix(r.URL.Path, "/migrate") || strings.Contains(r.URL.Path, "/git") {
				return httperror.Conflict("Managed stacks cannot migrate or switch to Git deployment", nil)
			}
			if r.Method != http.MethodGet && !strings.HasSuffix(r.URL.Path, "/configurations/recover") {
				if stack.Status == portainer.StackStatusDeploying || (stack.Configuration != nil && stack.Configuration.PendingRevision != "") {
					return httperror.Conflict("A deployment is in progress or configuration recovery is required", nil)
				}
			}
			if !environmentAdmin {
				teams := authorization.TeamIDs(ctx.UserMemberships)
				allowed := authorization.UserCanPerformStackAction(ctx.UserID, teams, control, action)
				if action == portainer.StackEdit {
					allowed = allowed && authorization.UserCanPerformStackAction(ctx.UserID, teams, control, portainer.StackDeploy)
				}
				if !allowed {
					return httperror.Forbidden("Stack operation is not permitted", nil)
				}
			}
			r = security.WithAuthorizedStackOperation(r, stack.EndpointID)
		} else if !environmentAdmin && r.Method != http.MethodGet {
			protected, err := security.EndpointHasStackPolicies(h.DataStore, stack.EndpointID)
			if err != nil {
				return httperror.InternalServerError("Unable to verify stack policies", err)
			}
			if protected {
				return httperror.Forbidden("An administrator must assign an operation policy before this stack can be changed", nil)
			}
		}
		return next(w, r)
	}
}

func (h *Handler) loadControlledStack(r *http.Request) (*portainer.Stack, *portainer.Endpoint, *portainer.ResourceControl, *security.RestrictedRequestContext, *httperror.HandlerError) {
	id, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return nil, nil, nil, nil, httperror.BadRequest("Invalid stack ID", err)
	}
	stack, err := h.DataStore.Stack().Read(portainer.StackID(id))
	if err != nil {
		return nil, nil, nil, nil, httperror.NotFound("Stack not found", err)
	}
	ctx, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return nil, nil, nil, nil, httperror.Forbidden("Authentication required", err)
	}
	endpoint, err := h.DataStore.Endpoint().Endpoint(stack.EndpointID)
	if err != nil {
		return nil, nil, nil, nil, httperror.NotFound("Environment not found", err)
	}
	if err := h.requestBouncer.AuthorizedEndpointOperation(r, endpoint); err != nil {
		return nil, nil, nil, nil, httperror.Forbidden("Environment access denied", err)
	}
	canManage, err := h.userCanManageStacks(ctx, endpoint)
	if err != nil {
		return nil, nil, nil, nil, httperror.InternalServerError("Unable to check stack management", err)
	}
	if !canManage {
		return nil, nil, nil, nil, httperror.Forbidden("Stack management is disabled for regular users", nil)
	}
	control, err := h.DataStore.ResourceControl().ResourceControlByResourceIDAndType(stackutils.ResourceControlID(stack.EndpointID, stack.Name), portainer.StackResourceControl)
	if err != nil {
		return nil, nil, nil, nil, httperror.InternalServerError("Unable to read stack ownership", err)
	}
	access := &authorization.StandaloneAccess{Role: ctx.StandaloneRole, Explicit: ctx.StandaloneRole != ""}
	if !ctx.IsAdmin && !authorization.StandaloneResourceAccess(access, ctx.UserID, authorization.TeamIDs(ctx.UserMemberships), control) {
		return nil, nil, nil, nil, httperror.Forbidden("Stack access denied", nil)
	}
	if stack.Type != portainer.DockerComposeStack || stack.WorkflowID != 0 || stack.GitConfig != nil {
		return nil, nil, nil, nil, httperror.BadRequest("Operation controls require a file-based Docker Compose stack", nil)
	}
	return stack, endpoint, control, ctx, nil
}

func decodeControlPayload(w http.ResponseWriter, r *http.Request, payload any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("multiple JSON values are not allowed")
	} else if err != io.EOF {
		return err
	}
	return nil
}

type stackControlsResponse struct {
	Policy                   *portainer.StackAccessPolicy  `json:"Policy"`
	Permissions              []portainer.StackAction       `json:"Permissions"`
	Configuration            *portainer.StackConfiguration `json:"Configuration"`
	Services                 []string                      `json:"Services"`
	ConfigurationUnavailable string                        `json:"ConfigurationUnavailable,omitempty"`
}

func (h *Handler) stackControlsGet(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	result := stackControlsResponse{Configuration: stack.Configuration, Permissions: []portainer.StackAction{}}
	if control != nil {
		result.Policy = control.StackPolicy
	}
	var rolePolicy *portainer.StandaloneRolePolicy
	var err error
	if !ctx.IsAdmin && ctx.StandaloneRole != "" {
		rolePolicy, err = authorization.ReadStandalonePolicy(h.DataStore, ctx.StandaloneRole)
		if err != nil {
			return httperror.InternalServerError("Unable to read role policy", err)
		}
	}
	for _, action := range stackActions {
		if rolePolicy != nil && !authorization.RoleFeatureAllowed(rolePolicy, ctx.StandaloneRole, "stacks.view", authorization.StackActionFeature(action)) {
			continue
		}
		if !ctx.IsAdmin && !authorization.StandaloneStackActionAllowed(ctx.StandaloneRole, action) {
			continue
		}
		if security.ScopedEnvironmentAdmin(ctx, stack.EndpointID) || result.Policy == nil || authorization.UserCanPerformStackAction(ctx.UserID, authorization.TeamIDs(ctx.UserMemberships), control, action) {
			result.Permissions = append(result.Permissions, action)
		}
	}
	services, err := stackconfig.Services(stack)
	if err != nil {
		return httperror.InternalServerError("Unable to read Compose services", err)
	}
	slices.Sort(services)
	result.Services = services
	if err := stackconfig.ValidateEndpoint(stack, endpoint); err != nil {
		result.ConfigurationUnavailable = err.Error()
	} else if _, err := stackconfig.Root(); err != nil {
		result.ConfigurationUnavailable = err.Error()
	}
	return response.JSON(w, result)
}

func (h *Handler) stackPolicyUpdate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, _, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if !ctx.IsAdmin {
		return httperror.Forbidden("Only administrators may assign stack permissions", nil)
	}
	if stack.Status == portainer.StackStatusDeploying {
		return httperror.Conflict("Stack deployment is in progress", nil)
	}
	if control == nil {
		return httperror.Conflict("Set stack ownership before assigning operation permissions", nil)
	}
	var payload portainer.StackAccessPolicy
	if err := decodeControlPayload(w, r, &payload, 64*1024); err != nil {
		return httperror.BadRequest("Invalid policy", err)
	}
	version := 0
	if control.StackPolicy != nil {
		version = control.StackPolicy.Version
	}
	if payload.Version != version {
		return httperror.Conflict("Policy changed; refresh before saving", nil)
	}
	if len(payload.Grants) > 100 {
		return httperror.BadRequest("Too many grants", nil)
	}
	for _, grant := range payload.Grants {
		if (grant.UserID > 0) == (grant.TeamID > 0) || grant.UserID < 0 || grant.TeamID < 0 || !slices.Contains(grant.Actions, portainer.StackView) {
			return httperror.BadRequest("Each grant must select one user or team and include view", nil)
		}
		for _, action := range grant.Actions {
			if !slices.Contains(stackActions, action) {
				return httperror.BadRequest("Unknown stack action", fmt.Errorf("%s", action))
			}
		}
		if grant.UserID > 0 {
			if _, err := h.DataStore.User().Read(grant.UserID); err != nil {
				return httperror.BadRequest("Unknown user", err)
			}
		}
		if grant.TeamID > 0 {
			if _, err := h.DataStore.Team().Read(grant.TeamID); err != nil {
				return httperror.BadRequest("Unknown team", err)
			}
		}
	}
	payload.Version++
	err := h.DataStore.UpdateTx(func(tx dataservices.DataStoreTx) error {
		current, err := tx.ResourceControl().Read(control.ID)
		if err != nil {
			return err
		}
		current.StackPolicy = &payload
		return tx.ResourceControl().Update(current.ID, current)
	})
	if err != nil {
		return httperror.InternalServerError("Unable to save stack policy", err)
	}
	return response.JSON(w, payload)
}
