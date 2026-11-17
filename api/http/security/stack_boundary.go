package security

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/pkg/authorization"
)

var ErrStackDockerAccessDenied = errors.New("direct Docker changes are restricted to administrators on environments with stack policies; use the authorized stack operations")

// EndpointHasStackPolicies intentionally protects the entire Docker daemon:
// arbitrary container creation, mounts, exec and builds can bypass stack RBAC.
func EndpointHasStackPolicies(store dataservices.DataStore, endpointID portainer.EndpointID) (bool, error) {
	controls, err := store.ResourceControl().ReadAll()
	if err != nil {
		return false, err
	}
	prefix := strconv.Itoa(int(endpointID)) + "_"
	for _, control := range controls {
		if control.Type == portainer.StackResourceControl && control.StackPolicy != nil && strings.HasPrefix(control.ResourceID, prefix) {
			return true, nil
		}
	}
	return false, nil
}

// Unauthenticated automation cannot bypass either stack actions or scoped roles.
func EndpointHasUnattendedRestrictions(store dataservices.DataStore, endpointID portainer.EndpointID) (bool, error) {
	protected, err := EndpointHasStackPolicies(store, endpointID)
	if err != nil || protected {
		return protected, err
	}
	endpoint, err := store.Endpoint().Endpoint(endpointID)
	if err != nil {
		if store.IsErrObjectNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if authorization.SupportsStandaloneRoles(endpoint) {
		if authorization.HasStandalonePolicies(endpoint.UserAccessPolicies, endpoint.TeamAccessPolicies) {
			return true, nil
		}
		if endpoint.GroupID != 0 {
			group, err := store.EndpointGroup().Read(endpoint.GroupID)
			if err != nil {
				return false, err
			}
			if authorization.HasStandalonePolicies(group.UserAccessPolicies, group.TeamAccessPolicies) {
				return true, nil
			}
		}
	}
	return false, nil
}

func IsStackBoundaryRead(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	// Some GET endpoints expose interactive sessions or arbitrary files.
	p := strings.ToLower(r.URL.Path)
	if regexp.MustCompile(`/images/(?:.+/)?get$`).MatchString(p) {
		return false
	}
	if r.Method == http.MethodGet && regexp.MustCompile(`/v2/dockerhub/[0-9]+$`).MatchString(p) {
		return r.Header.Get("Upgrade") == ""
	}
	for _, part := range []string{"websocket", "/exec", "/attach", "/archive", "/export", "/browse", "/v2/"} {
		if strings.Contains(p, part) {
			return false
		}
	}
	return r.Header.Get("Upgrade") == ""
}

// CheckStackDockerAccess applies to raw Docker APIs and non-proxied operations.
func CheckStackDockerAccess(r *http.Request, store dataservices.DataStore, endpointID portainer.EndpointID) error {
	if IsStackBoundaryRead(r) {
		return nil
	}
	token, err := RetrieveTokenData(r)
	if err != nil {
		return err
	}
	if token.Role == portainer.AdministratorRole || IsStackBoundaryRead(r) {
		return nil
	}
	if endpoint, err := store.Endpoint().Endpoint(endpointID); err == nil {
		access, err := EndpointStandaloneAccess(r, store, endpoint)
		if err != nil {
			return err
		}
		if access.EnvironmentAdmin() {
			return nil
		}
	}
	protected, err := EndpointHasStackPolicies(store, endpointID)
	if err != nil {
		return err
	}
	if protected {
		return ErrStackDockerAccessDenied
	}
	return nil
}

type authorizedStackEndpointKey struct{}

// WithAuthorizedStackOperation may only be used after checking the stack action.
// The endpoint-bound context prevents a stack URL from bypassing the Docker gate.
func WithAuthorizedStackOperation(r *http.Request, endpointID portainer.EndpointID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authorizedStackEndpointKey{}, endpointID))
}

func isControlledStackRoute(r *http.Request, endpointID portainer.EndpointID) bool {
	id, ok := r.Context().Value(authorizedStackEndpointKey{}).(portainer.EndpointID)
	return ok && id == endpointID
}
