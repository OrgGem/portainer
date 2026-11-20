package security

import (
	"errors"
	"net/http"
	"path"
	"regexp"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/pkg/authorization"
	httprequest "github.com/portainer/portainer/pkg/libhttp/request"
)

var ErrStandaloneRoleDenied = errors.New("operation is not permitted by the Docker Standalone environment role")

// The endpoint proxy strips /api/endpoints before its authorization check.
// Match both router stages without confusing /docker/{id} helper endpoints.
var standaloneDockerPrefix = regexp.MustCompile(`^/(?:endpoints/)?[0-9]+/(?:agent/)?docker(?:/|$)`)

// Resource-control IDs do not carry an environment ID. Scoped users therefore
// leave ownership administration to global administrators instead of applying
// an unscoped write using permissions from a different environment.
func UserHasStandaloneRole(store dataservices.DataStoreTx, userID portainer.UserID) (bool, error) {
	endpoints, err := store.Endpoint().Endpoints()
	if err != nil {
		return false, err
	}
	for i := range endpoints {
		access, err := authorization.StandaloneAccessForUser(store, userID, &endpoints[i])
		if err != nil {
			return false, err
		}
		if access != nil && access.Explicit {
			return true, nil
		}
	}
	return false, nil
}

func EndpointStandaloneAccess(r *http.Request, store dataservices.DataStoreTx, endpoint *portainer.Endpoint) (*authorization.StandaloneAccess, error) {
	token, err := RetrieveTokenData(r)
	if err != nil {
		return nil, err
	}
	if token.Role == portainer.AdministratorRole {
		return &authorization.StandaloneAccess{Role: portainer.StandaloneEnvironmentAdmin, Explicit: true}, nil
	}
	access, err := authorization.StandaloneAccessForUser(store, token.ID, endpoint)
	if err != nil {
		return nil, err
	}
	if access != nil && access.Explicit && !authorization.SupportsStandaloneRoles(endpoint) {
		return nil, ErrStandaloneRoleDenied
	}
	return access, nil
}

// CheckStandaloneEndpointRole is separate from resource ownership and stack
// action checks. Passing one layer never grants an exemption from the others.
func CheckStandaloneEndpointRole(r *http.Request, store dataservices.DataStoreTx, endpoint *portainer.Endpoint, dockerProxy bool) error {
	token, err := RetrieveTokenData(r)
	if err != nil {
		return err
	}
	if token.Role == portainer.AdministratorRole {
		return nil
	}
	access, err := EndpointStandaloneAccess(r, store, endpoint)
	if err != nil {
		return err
	}
	if access == nil || !access.Explicit {
		return nil
	}
	policy, err := authorization.ReadStandalonePolicy(store, access.Role)
	if err != nil {
		return err
	}
	if !StandalonePolicyAllowsRequest(policy, access.Role, r, dockerProxy, endpoint.Type) {
		return ErrStandaloneRoleDenied
	}
	if ctx, err := RetrieveRestrictedRequestContext(r); err == nil {
		ctx.RoleEndpointID, ctx.StandaloneRole = endpoint.ID, access.Role
	}
	return nil
}

// The UI reads the agent API version from GET /ping after the Docker checks.
// Match both the endpoint proxy route and the path stripped by the transport.
func isStandaloneAgentPing(r *http.Request, dockerProxy bool) bool {
	if r.Method != http.MethodGet || r.Header.Get("Upgrade") != "" || httprequest.ContainsEncodedSeparator(r.URL.EscapedPath()) {
		return false
	}
	if dockerProxy && r.URL.Path == "/ping" {
		return true
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/endpoints/") {
		p = strings.TrimPrefix(p, "/api")
	}
	prefix := standaloneDockerPrefix.FindStringIndex(p)
	return prefix != nil && p[prefix[1]:] == "ping"
}

func StandaloneRoleAllowsRequest(role portainer.StandaloneRole, r *http.Request, dockerProxy bool) bool {
	if authorization.StandaloneRolePriority(role) == 0 || httprequest.ContainsEncodedSeparator(r.URL.EscapedPath()) {
		return false
	}
	p := strings.TrimPrefix(r.URL.Path, "/api")
	if prefix := standaloneDockerPrefix.FindStringIndex(p); prefix != nil {
		p = "/" + p[prefix[1]:]
		dockerProxy = true
	}

	if !dockerProxy && strings.HasPrefix(p, "/docker/") {
		parts := strings.Split(strings.TrimPrefix(p, "/docker/"), "/")
		if len(parts) < 2 {
			return false
		}
		route := "/" + strings.Join(parts[1:], "/")
		if r.Method == http.MethodGet {
			return route == "/dashboard" || route == "/images" || regexp.MustCompile(`^/containers/[^/]+/gpus$`).MatchString(route)
		}
		return r.Method == http.MethodPost && (role == portainer.StandaloneEnvironmentAdmin || role == portainer.StandaloneStandard) && regexp.MustCompile(`^/containers/[^/]+/recreate$`).MatchString(route)
	}
	if dockerProxy {
		if strings.HasPrefix(httprequest.TrimDockerVersion(p), "/v2/browse/") {
			return standaloneVolumeBrowse(role, r, httprequest.TrimDockerVersion(p))
		}
		if r.Method == http.MethodGet && regexp.MustCompile(`^/v2/dockerhub/[0-9]+$`).MatchString(p) {
			return true
		}
		return standaloneDockerRequest(role, r.Method, httprequest.TrimDockerVersion(p), r.Header.Get("Upgrade") != "")
	}
	readOnly := role == portainer.StandaloneReadOnly || role == portainer.StandaloneHelpdesk
	if IsStackBoundaryRead(r) {
		return true
	}
	if readOnly {
		return false
	}
	if strings.Contains(p, "/websocket/") {
		return true
	}
	if strings.HasPrefix(p, "/stacks") {
		if strings.Contains(p, "/create/") && !strings.Contains(p, "/create/standalone/") {
			return false
		}
		if strings.HasSuffix(p, "/migrate") {
			return false
		}
		if role != portainer.StandaloneOperator {
			return true
		}
		if r.Method != http.MethodPost {
			return false
		}
		return strings.HasSuffix(p, "/start") || strings.HasSuffix(p, "/stop") || strings.HasSuffix(p, "/configurations") || strings.HasSuffix(p, "/apply") || strings.HasSuffix(p, "/rollback")
	}
	return false
}

// Environment admins can manage volume files, but this does not grant the
// agent's host browser. Resource controls and the stack boundary still run.
func standaloneVolumeBrowse(role portainer.StandaloneRole, r *http.Request, p string) bool {
	volumes := r.URL.Query()["volumeID"]
	if role != portainer.StandaloneEnvironmentAdmin || len(volumes) != 1 || volumes[0] == "" || strings.ContainsAny(volumes[0], "/\\\x00") || r.Header.Get("Upgrade") != "" {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		return p == "/v2/browse/ls" || p == "/v2/browse/get" || p == "/v2/browse/text"
	case http.MethodPost:
		return p == "/v2/browse/put"
	case http.MethodPut:
		return p == "/v2/browse/rename" || p == "/v2/browse/text"
	case http.MethodDelete:
		return p == "/v2/browse/delete"
	}
	return false
}

// Explicit allowlists keep new Docker endpoints from silently becoming writable.
func standaloneDockerRequest(role portainer.StandaloneRole, method, p string, upgrade bool) bool {
	readOnly := role == portainer.StandaloneReadOnly || role == portainer.StandaloneHelpdesk
	operator := role == portainer.StandaloneOperator
	match := func(pattern string) bool {
		if strings.HasPrefix(pattern, "/images/*") || strings.HasPrefix(pattern, "/distribution/*") {
			prefix, suffix, _ := strings.Cut(pattern, "*")
			name := strings.TrimSuffix(strings.TrimPrefix(p, prefix), suffix)
			return strings.HasPrefix(p, prefix) && strings.HasSuffix(p, suffix) && name != "" && !strings.Contains(name, "//") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
		}
		ok, _ := path.Match(pattern, p)
		return ok
	}
	if method == http.MethodGet || method == http.MethodHead {
		if upgrade || strings.Contains(p, "/attach") {
			return !readOnly && match("/containers/*/attach/ws")
		}
		for _, exact := range []string{"/_ping", "/version", "/info", "/events", "/system/df", "/host/docker-storage", "/containers/json", "/images/json", "/images/search", "/networks", "/volumes"} {
			if p == exact {
				return true
			}
		}
		for _, pattern := range []string{"/containers/*/json", "/containers/*/logs", "/containers/*/stats", "/containers/*/top", "/containers/*/changes", "/images/*/json", "/images/*/history", "/networks/*", "/volumes/*", "/distribution/*/json"} {
			if match(pattern) {
				return true
			}
		}
		if match("/exec/*/json") {
			return !readOnly
		}
		if match("/containers/*/archive") || match("/containers/*/export") || match("/images/*/get") || p == "/images/get" {
			return !readOnly && !operator
		}
		return false
	}
	if readOnly {
		return false
	}
	if method == http.MethodPost {
		for _, action := range []string{"start", "stop", "restart", "kill", "pause", "unpause", "update", "rename", "exec", "attach", "resize", "wait"} {
			if match("/containers/*/" + action) {
				return true
			}
		}
		if match("/exec/*/start") || match("/exec/*/resize") {
			return true
		}
		if operator {
			return false
		}
		for _, exact := range []string{"/containers/create", "/images/create", "/images/load", "/build", "/auth", "/networks/create", "/volumes/create"} {
			if p == exact {
				return true
			}
		}
		for _, pattern := range []string{"/images/*/push", "/images/*/tag", "/networks/*/connect", "/networks/*/disconnect"} {
			if match(pattern) {
				return true
			}
		}
		if role == portainer.StandaloneEnvironmentAdmin {
			for _, exact := range []string{"/commit", "/containers/prune", "/images/prune", "/networks/prune", "/volumes/prune", "/build/prune"} {
				if p == exact {
					return true
				}
			}
		}
	}
	if operator {
		return false
	}
	if method == http.MethodDelete {
		for _, pattern := range []string{"/containers/*", "/images/*", "/networks/*", "/volumes/*"} {
			if match(pattern) {
				return true
			}
		}
	}
	return method == http.MethodPut && match("/containers/*/archive")
}

func ScopedEnvironmentAdmin(ctx *RestrictedRequestContext, endpointID portainer.EndpointID) bool {
	return ctx.IsAdmin || (ctx.RoleEndpointID == endpointID && ctx.StandaloneRole == portainer.StandaloneEnvironmentAdmin)
}

func ScopedAllResources(ctx *RestrictedRequestContext, endpointID portainer.EndpointID) bool {
	return ctx.IsAdmin || (ctx.RoleEndpointID == endpointID && (&authorization.StandaloneAccess{Role: ctx.StandaloneRole, Explicit: ctx.StandaloneRole != ""}).AllResources())
}
