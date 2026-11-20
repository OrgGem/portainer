package security

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/authorization"
	httprequest "github.com/portainer/portainer/pkg/libhttp/request"
	"net/http"
	"strings"
)

// Classify both the public route and the stripped Docker-proxy route. All
// required features must pass; the original role allowlist still applies.
func StandaloneRequestFeatures(r *http.Request, dockerProxy bool) []string {
	p := strings.TrimPrefix(r.URL.Path, "/api")
	if prefix := standaloneDockerPrefix.FindStringIndex(p); prefix != nil {
		p = "/" + p[prefix[1]:]
		dockerProxy = true
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if dockerProxy {
		p = httprequest.TrimDockerVersion(p)
		if strings.HasPrefix(p, "/v2/browse/") {
			return []string{"volumes.view", "volumes.files"}
		}
		if strings.Contains(p, "/exec") || strings.Contains(p, "/attach") || strings.Contains(p, "/resize") {
			return []string{"containers.view", "containers.console"}
		}
		if strings.Contains(p, "/archive") || strings.Contains(p, "/export") {
			return []string{"containers.view", "containers.files"}
		}
		if strings.HasPrefix(p, "/containers") {
			if read {
				if strings.HasSuffix(p, "/stats") {
					return []string{"containers.view", "metrics.view"}
				}
				return []string{"containers.view"}
			}
			if r.Method == http.MethodDelete || p == "/containers/create" || p == "/containers/prune" {
				return []string{"containers.view", "containers.manage"}
			}
			return []string{"containers.view", "containers.operate"}
		}
		for _, kind := range []string{"images", "networks", "volumes"} {
			if strings.HasPrefix(p, "/"+kind) {
				if read && !strings.HasSuffix(p, "/get") {
					return []string{kind + ".view"}
				}
				return []string{kind + ".view", kind + ".manage"}
			}
		}
		if strings.HasPrefix(p, "/build") || p == "/commit" || p == "/auth" {
			return []string{"images.view", "images.manage"}
		}
		if strings.HasPrefix(p, "/distribution/") || strings.HasPrefix(p, "/v2/dockerhub/") {
			return []string{"images.view"}
		}
		if p == "/system/df" || p == "/host/docker-storage" {
			return []string{"metrics.view", "volumes.view", "images.view", "containers.view"}
		}
		return []string{"environment.view"}
	}
	if strings.Contains(p, "/websocket/") {
		return []string{"containers.view", "containers.console"}
	}
	if strings.HasPrefix(p, "/stacks") {
		features := []string{"stacks.view"}
		switch {
		case strings.Contains(p, "/vault"):
			features = append(features, "vault.view")
			if !read {
				features = append(features, "vault.edit")
			}
		case strings.Contains(p, "/configurations") && !read:
			if strings.HasSuffix(p, "/apply") || strings.HasSuffix(p, "/rollback") {
				features = append(features, "config.apply")
			} else {
				features = append(features, "config.upload")
			}
		case !read:
			if strings.HasSuffix(p, "/start") || strings.HasSuffix(p, "/stop") || strings.HasSuffix(p, "/redeploy") {
				features = append(features, "stacks.deploy")
			} else {
				features = append(features, "stacks.edit", "stacks.deploy")
			}
		}
		return features
	}
	if strings.HasSuffix(p, "/metrics") {
		if strings.Contains(p, "/containers/") {
			return []string{"metrics.view", "containers.view"}
		}
		return []string{"metrics.view", "environment.view"}
	}
	if strings.HasPrefix(p, "/docker/") {
		if strings.HasSuffix(p, "/recreate") {
			return []string{"containers.view", "containers.manage"}
		}
		if strings.HasSuffix(p, "/gpus") {
			return []string{"containers.view"}
		}
		if strings.HasSuffix(p, "/images") {
			return []string{"images.view"}
		}
		if strings.HasSuffix(p, "/dashboard") {
			return []string{"environment.view", "containers.view", "images.view", "networks.view", "volumes.view", "stacks.view"}
		}
	}
	return []string{"environment.view"}
}

func StandalonePolicyAllowsRequest(policy *portainer.StandaloneRolePolicy, role portainer.StandaloneRole, r *http.Request, dockerProxy bool, endpointType portainer.EndpointType) bool {
	roleAllows := StandaloneRoleAllowsRequest(role, r, dockerProxy) ||
		(endpointType == portainer.AgentOnDockerEnvironment && isStandaloneAgentPing(r, dockerProxy))
	return roleAllows && authorization.RoleFeatureAllowed(policy, role, StandaloneRequestFeatures(r, dockerProxy)...)
}
