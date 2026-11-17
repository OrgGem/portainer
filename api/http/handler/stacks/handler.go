package stacks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	dockerclient "github.com/portainer/portainer/api/docker/client"
	"github.com/portainer/portainer/api/docker/consts"
	"github.com/portainer/portainer/api/gitops/scheduling"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/authorization"
	"github.com/portainer/portainer/api/internal/endpointutils"
	"github.com/portainer/portainer/api/kubernetes/cli"
	"github.com/portainer/portainer/api/logs"
	"github.com/portainer/portainer/api/stacks/deployments"
	"github.com/portainer/portainer/api/stacks/stackutils"
	"github.com/portainer/portainer/api/stacks/teardown"
	roleauth "github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

// Handler is the HTTP handler used to handle stack operations.
type Handler struct {
	stackCreationMutex *sync.Mutex
	stackDeletionMutex *sync.Mutex
	stackControlsMutex sync.Mutex
	stackMutationLocks map[portainer.StackID]*stackMutationLock
	requestBouncer     security.BouncerService
	*mux.Router
	DataStore               dataservices.DataStore
	DockerClientFactory     *dockerclient.ClientFactory
	FileService             portainer.FileService
	GitService              portainer.GitService
	SwarmStackManager       portainer.SwarmStackManager
	ComposeStackManager     portainer.ComposeStackManager
	KubernetesDeployer      portainer.KubernetesDeployer
	KubernetesClientFactory *cli.ClientFactory
	SourceScheduler         *scheduling.SourceScheduler
	StackDeployer           deployments.StackDeployer
	teardownService         teardown.Service
}

func stackExistsError(name string) *httperror.HandlerError {
	msg := fmt.Sprintf("A stack with the normalized name '%s' already exists", name)
	err := errors.New(msg)
	return httperror.Conflict(msg, err)
}

// NewHandler creates a handler to manage stack operations.
func NewHandler(bouncer security.BouncerService, teardownService teardown.Service) *Handler {
	h := &Handler{
		Router:             mux.NewRouter(),
		stackCreationMutex: &sync.Mutex{},
		stackDeletionMutex: &sync.Mutex{},
		requestBouncer:     bouncer,
		teardownService:    teardownService,
	}

	h.Handle("/stacks/create/{type}/{method}",
		bouncer.AuthenticatedAccess(httperror.LoggerHandler(h.stackCreate))).Methods(http.MethodPost)
	h.Handle("/stacks",
		bouncer.AuthenticatedAccess(httperror.LoggerHandler(h.stackList))).Methods(http.MethodGet)
	h.Handle("/stacks/vault/mappings", bouncer.AuthenticatedAccess(httperror.LoggerHandler(h.vaultInventory))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}/vault", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackVaultGet))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}/vault", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackVaultUpdate))).Methods(http.MethodPut)
	h.Handle("/stacks/{id}/vault/template", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackVaultTemplate))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackInspect))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackEdit, h.stackDelete))).Methods(http.MethodDelete)
	h.Handle("/stacks/{id}/associate",
		bouncer.AdminAccess(h.stackAccess(portainer.StackEdit, h.stackAssociate))).Methods(http.MethodPut)
	h.Handle("/stacks/name/{name}",
		bouncer.AuthenticatedAccess(httperror.LoggerHandler(h.stackDeleteKubernetesByName))).Methods(http.MethodDelete)
	h.Handle("/stacks/{id}",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackEdit, h.stackUpdate))).Methods(http.MethodPut)
	h.Handle("/stacks/{id}/git",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackEdit, h.stackUpdateGit))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/git/redeploy",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackDeploy, h.stackGitRedeploy))).Methods(http.MethodPut)
	h.Handle("/stacks/{id}/file",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackFile))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}/migrate",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackEdit, h.stackMigrate))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/start",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackDeploy, h.stackStart))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/stop",
		bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackDeploy, h.stackStop))).Methods(http.MethodPost)
	h.Handle("/stacks/webhooks/{webhookID}",
		bouncer.PublicAccess(httperror.LoggerHandler(h.webhookInvoke))).Methods(http.MethodPost)

	h.Handle("/stacks/{id}/controls", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackControlsGet))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}/policy", bouncer.AdminAccess(h.stackAccess(portainer.StackView, h.stackPolicyUpdate))).Methods(http.MethodPut)
	h.Handle("/stacks/{id}/configurations/targets", bouncer.AdminAccess(h.stackAccess(portainer.StackView, h.stackConfigTargets))).Methods(http.MethodPut)
	h.Handle("/stacks/{id}/configurations", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackConfigUpload, h.stackConfigUpload))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/configurations/{revision}/content", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackView, h.stackConfigContent))).Methods(http.MethodGet)
	h.Handle("/stacks/{id}/configurations/{revision}/apply", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackConfigApply, h.stackConfigApply))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/configurations/{revision}/rollback", bouncer.AuthenticatedAccess(h.stackAccess(portainer.StackConfigApply, h.stackConfigApply))).Methods(http.MethodPost)
	h.Handle("/stacks/{id}/configurations/recover", bouncer.AdminAccess(h.stackAccess(portainer.StackView, h.stackConfigRecover))).Methods(http.MethodPost)

	return h
}

func (handler *Handler) userCanAccessStack(securityContext *security.RestrictedRequestContext, resourceControl *portainer.ResourceControl) (bool, error) {
	if securityContext.StandaloneRole != "" && (resourceControl == nil || strings.HasPrefix(resourceControl.ResourceID, fmt.Sprintf("%d_", securityContext.RoleEndpointID))) {
		access := &roleauth.StandaloneAccess{Role: securityContext.StandaloneRole, Explicit: true}
		return roleauth.StandaloneResourceAccess(access, securityContext.UserID, authorization.TeamIDs(securityContext.UserMemberships), resourceControl), nil
	}
	user, err := handler.DataStore.User().Read(securityContext.UserID)
	if err != nil {
		return false, err
	}

	userTeamIDs := authorization.TeamIDs(securityContext.UserMemberships)

	if resourceControl != nil && authorization.UserCanAccessResource(securityContext.UserID, userTeamIDs, resourceControl) {
		return true, nil
	}

	return stackutils.UserIsAdminOrEndpointAdmin(user), nil
}

func (handler *Handler) userIsAdmin(userID portainer.UserID) (bool, error) {
	user, err := handler.DataStore.User().Read(userID)
	if err != nil {
		return false, err
	}

	isAdmin := user.Role == portainer.AdministratorRole

	return isAdmin, nil
}

func (handler *Handler) userCanCreateStack(securityContext *security.RestrictedRequestContext) (bool, error) {
	user, err := handler.DataStore.User().Read(securityContext.UserID)
	if err != nil {
		return false, err
	}

	return stackutils.UserIsAdminOrEndpointAdmin(user), nil
}

// if stack management is disabled for non admins and the user isn't an admin, then return false. Otherwise return true
func (handler *Handler) userCanManageStacks(securityContext *security.RestrictedRequestContext, endpoint *portainer.Endpoint) (bool, error) {
	if endpoint != nil && securityContext.RoleEndpointID == endpoint.ID && securityContext.StandaloneRole == portainer.StandaloneEnvironmentAdmin {
		return true, nil
	}
	// When the endpoint is deleted, stacks that the deleted endpoint created will be tagged as an orphan stack
	// An orphan stack can be adopted by admins
	if endpoint == nil {
		return true, nil
	}

	if endpointutils.IsDockerEndpoint(endpoint) && !endpoint.SecuritySettings.AllowStackManagementForRegularUsers {
		canCreate, err := handler.userCanCreateStack(securityContext)

		if err != nil {
			return false, fmt.Errorf("failed to get user from the database: %w", err)
		}

		return canCreate, nil
	}
	return true, nil
}

func (handler *Handler) checkUniqueStackName(endpoint *portainer.Endpoint, name string, stackID portainer.StackID) (bool, error) {
	stacks, err := handler.DataStore.Stack().ReadAll()
	if err != nil {
		return false, err
	}

	for _, stack := range stacks {
		if strings.EqualFold(stack.Name, name) && (stackID == 0 || stackID != stack.ID) && stack.EndpointID == endpoint.ID {
			return false, nil
		}
	}

	return true, nil
}

func (handler *Handler) checkUniqueStackNameInDocker(endpoint *portainer.Endpoint, name string, stackID portainer.StackID, swarmMode bool) (bool, error) {
	isUniqueStackName, err := handler.checkUniqueStackName(endpoint, name, stackID)
	if err != nil {
		return false, err
	}

	if handler.DockerClientFactory == nil {
		return isUniqueStackName, nil
	}

	dockerClient, err := handler.DockerClientFactory.CreateClient(endpoint, "", nil)
	if err != nil {
		return false, err
	}
	defer logs.CloseAndLogErr(dockerClient)
	if swarmMode {
		services, err := dockerClient.ServiceList(context.Background(), types.ServiceListOptions{})
		if err != nil {
			return false, err
		}

		for _, service := range services {
			serviceNS, ok := service.Spec.Labels["com.docker.stack.namespace"]
			if ok && serviceNS == name {
				return false, nil
			}
		}
	}

	containers, err := dockerClient.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return false, err
	}

	for _, container := range containers {
		containerNS, ok := container.Labels[consts.ComposeStackNameLabel]

		if ok && containerNS == name {
			return false, nil
		}
	}

	return isUniqueStackName, nil
}

func (handler *Handler) checkUniqueWebhookID(tx dataservices.DataStoreTx, webhookID string) (bool, error) {
	_, err := tx.Stack().StackByWebhookID(webhookID)
	if tx.IsErrObjectNotFound(err) {
		return true, nil
	}
	return false, err
}
