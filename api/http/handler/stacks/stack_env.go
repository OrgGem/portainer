package stacks

import (
	"net/http"

	portainer "github.com/portainer/portainer/api"
)

const redactedEnvPlaceholder = "[REDACTED]"

// resolveStackEnv computes the updated environment variables for a stack update or redeploy.
//
// Rules:
//  1. When clearEnv is true, clear all environment variables explicitly.
//  2. When incoming is nil (omitted or null in JSON), preserve existing stack.Env.
//  3. When incoming is an empty slice ([]):
//     - Preserve existing stack.Env on redeploy-only endpoints (preserveOnEmptySlice)
//     or token-authenticated API/MCP requests (X-API-KEY / Authorization header),
//     where SDKs commonly serialize unset optional slice parameters as [].
//     - Allow clearing stack.Env on cookie-authenticated Portainer UI requests when the
//     user removes all environment variable rows in the form.
//  4. When incoming contains pairs with value "[REDACTED]" (from MCP secret redaction),
//     retain the existing stored value for matching variable names.
func resolveStackEnv(r *http.Request, existing, incoming []portainer.Pair, clearEnv, preserveOnEmptySlice bool) []portainer.Pair {
	if clearEnv {
		return []portainer.Pair{}
	}

	if incoming == nil {
		return existing
	}

	if len(incoming) == 0 {
		if preserveOnEmptySlice || isTokenAuthenticatedRequest(r) {
			return existing
		}
		return incoming
	}

	existingByName := make(map[string]string, len(existing))
	for _, pair := range existing {
		existingByName[pair.Name] = pair.Value
	}

	resolved := make([]portainer.Pair, len(incoming))
	for i, pair := range incoming {
		if pair.Value == redactedEnvPlaceholder {
			if prevVal, ok := existingByName[pair.Name]; ok {
				pair.Value = prevVal
			}
		}
		resolved[i] = pair
	}

	return resolved
}

func isTokenAuthenticatedRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	return r.Header.Get(portainer.APIKeyHeader) != "" || r.Header.Get("Authorization") != ""
}
