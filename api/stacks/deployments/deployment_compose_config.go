package deployments

import (
	"context"
	"fmt"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/registryutils"
	"github.com/portainer/portainer/api/stacks/stackutils"
	"github.com/portainer/portainer/pkg/authorization"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type ComposeStackDeploymentConfig struct {
	stack          *portainer.Stack
	endpoint       *portainer.Endpoint
	registries     []portainer.Registry
	isAdmin        bool
	user           *portainer.User
	forcePullImage bool
	ForceCreate    bool
	FileService    portainer.FileService
	StackDeployer  StackDeployer
	prune          bool
}

func CreateComposeStackDeploymentConfigTx(tx dataservices.DataStoreTx, securityContext *security.RestrictedRequestContext, stack *portainer.Stack, endpoint *portainer.Endpoint, fileService portainer.FileService, deployer StackDeployer, prune, forcePullImage, forceCreate bool) (*ComposeStackDeploymentConfig, error) {
	user, err := tx.User().Read(securityContext.UserID)
	if err != nil {
		return nil, fmt.Errorf("unable to load user information from the database: %w", err)
	}

	access, err := authorization.StandaloneAccessForUser(tx, user.ID, endpoint)
	if err != nil {
		return nil, err
	}
	registries, err := tx.Registry().ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve registries from the database: %w", err)
	}

	filteredRegistries := security.FilterRegistries(registries, user, securityContext.UserMemberships, endpoint.ID)

	registryutils.RefreshAndPersistECRTokens(tx, filteredRegistries)

	config := &ComposeStackDeploymentConfig{
		stack:          stack,
		endpoint:       endpoint,
		registries:     filteredRegistries,
		prune:          prune,
		isAdmin:        securityContext.IsAdmin || (authorization.SupportsStandaloneRoles(endpoint) && access.EnvironmentAdmin()),
		user:           user,
		forcePullImage: forcePullImage,
		ForceCreate:    forceCreate,
		FileService:    fileService,
		StackDeployer:  deployer,
	}

	return config, nil
}

func (config *ComposeStackDeploymentConfig) Deploy(ctx context.Context) error {
	if config.FileService == nil || config.StackDeployer == nil {
		log.Debug().Msg("file service or stack deployer is not initialized")
		return errors.New("file service or stack deployer cannot be nil")
	}

	if config.endpoint != nil {
		if err := ValidateStackForUser(config.stack, config.endpoint, config.user, config.StackDeployer, config.FileService, config.isAdmin); err != nil {
			return err
		}
	}

	if err := stackutils.ValidateComposeURLs(ctx, config.stack, config.FileService); err != nil {
		return err
	}

	if stackutils.IsRelativePathStack(config.stack) {
		return config.StackDeployer.DeployRemoteComposeStack(ctx, config.user.ID, config.stack, config.endpoint, config.registries, config.prune, config.forcePullImage, config.ForceCreate)
	}

	return config.StackDeployer.DeployComposeStack(ctx, config.stack, config.endpoint, config.registries, config.prune, config.forcePullImage, config.ForceCreate)
}

func (config *ComposeStackDeploymentConfig) Undeploy(ctx context.Context) error {
	if config.StackDeployer == nil {
		log.Debug().Msg("stack deployer is not initialized")
		return errors.New("stack deployer cannot be nil")
	}

	if stackutils.IsRelativePathStack(config.stack) {
		return config.StackDeployer.UndeployRemoteComposeStack(ctx, config.user.ID, config.stack, config.endpoint)
	}
	return config.StackDeployer.UndeployComposeStack(ctx, config.stack, config.endpoint)
}

func (config *ComposeStackDeploymentConfig) GetResponse() string {
	return ""
}

// PrepareVault is called before a retry can remove existing containers.
func (config *ComposeStackDeploymentConfig) PrepareVault(ctx context.Context) (context.Context, error) {
	if config.stack.VaultMappings == nil || !config.stack.VaultMappings.InjectOnDeploy || len(config.stack.VaultMappings.Mappings) == 0 {
		return ctx, nil
	}
	preparer, ok := config.StackDeployer.(interface {
		PrepareVault(context.Context, *portainer.Stack, *portainer.Endpoint) (context.Context, error)
	})
	if !ok {
		return ctx, errors.New("Vault deployment is unavailable")
	}
	return preparer.PrepareVault(ctx, config.stack, config.endpoint)
}
