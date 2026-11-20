// Package vaultinject resolves stack references for a single Compose operation.
package vaultinject

import (
	"context"
	"errors"
	"slices"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	"github.com/portainer/portainer/api/stacks/vaultdeploy"
	"github.com/portainer/portainer/api/stacks/vaultrefs"
	"github.com/portainer/portainer/pkg/authorization"
)

type resolvedKey struct{}

type resolved struct {
	stackID    portainer.StackID
	endpointID portainer.EndpointID
	values     map[string]map[string]string
}

// SupportedEndpoint covers local Docker and the standard Docker Agent.
func SupportedEndpoint(endpoint *portainer.Endpoint) bool {
	if endpoint == nil || !authorization.SupportsStandaloneRoles(endpoint) {
		return false
	}
	return endpoint.Type == portainer.AgentOnDockerEnvironment || strings.HasPrefix(endpoint.URL, "unix://") || strings.HasPrefix(endpoint.URL, "npipe://")
}

// Prepare validates and resolves all references before any Docker mutation.
// selected is nil for a complete deployment, or names the services being recreated.
func Prepare(ctx context.Context, store dataservices.DataStore, stack *portainer.Stack, endpoint *portainer.Endpoint, selected []string) (context.Context, error) {
	if existing, ok := ctx.Value(resolvedKey{}).(resolved); ok && existing.stackID == stack.ID && endpoint != nil && existing.endpointID == endpoint.ID {
		return ctx, nil
	}
	if stack.VaultMappings == nil || !stack.VaultMappings.InjectOnDeploy || len(stack.VaultMappings.Mappings) == 0 {
		return ctx, nil
	}
	if !SupportedEndpoint(endpoint) || stack.EndpointID != endpoint.ID || stack.Type != portainer.DockerComposeStack || stack.WorkflowID != 0 || stack.GitConfig != nil || len(stack.AdditionalFiles) != 0 {
		return ctx, errors.New("Vault injection requires a file-based, single-file Docker Standalone stack in its configured environment")
	}
	services, err := stackconfig.Services(stack)
	if err != nil {
		return ctx, errors.New("unable to read Compose services for Vault mappings")
	}
	// Check even inactive rows during a full deploy, to avoid silently dropping a
	// mapping whose service was removed from the candidate Compose definition.
	mappings := make([]portainer.VaultMapping, 0, len(stack.VaultMappings.Mappings))
	for _, mapping := range stack.VaultMappings.Mappings {
		if selected == nil || slices.Contains(selected, mapping.Service) {
			mappings = append(mappings, mapping)
		}
	}
	if err := vaultrefs.Validate(mappings, services); err != nil {
		return ctx, err
	}
	values := map[string]map[string]string{}
	if len(mappings) != 0 {
		values, err = vaultdeploy.Resolve(ctx, store, endpoint.ID, mappings)
		if err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, resolvedKey{}, resolved{stackID: stack.ID, endpointID: endpoint.ID, values: values}), nil
}

func Environment(ctx context.Context) map[string]map[string]string {
	result, _ := ctx.Value(resolvedKey{}).(resolved)
	return result.values
}
