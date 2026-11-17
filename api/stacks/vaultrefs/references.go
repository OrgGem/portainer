// Package vaultrefs validates and exports references without contacting Vault.
package vaultrefs

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	portainer "github.com/portainer/portainer/api"
)

var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,127}$`)
var safePath = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_./-]{0,511}$`)
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func Validate(mappings []portainer.VaultMapping, services []string) error {
	if len(mappings) > 128 {
		return errors.New("a stack can have at most 128 Vault mappings")
	}
	ids, variables := map[string]bool{}, map[string]bool{}
	for _, mapping := range mappings {
		if !safeID.MatchString(mapping.ID) || ids[mapping.ID] {
			return errors.New("mapping IDs must be unique identifiers")
		}
		if !slices.Contains(services, mapping.Service) {
			return errors.New("select a service declared in this stack's Compose file")
		}
		if !identifier.MatchString(mapping.Variable) || !identifier.MatchString(mapping.Key) {
			return errors.New("variable and key names must start with a letter or underscore and contain only letters, digits and underscores (maximum 128 characters)")
		}
		if !safePath.MatchString(mapping.Path) || path.Clean(mapping.Path) != mapping.Path || strings.Contains(mapping.Path, "../") || mapping.Path == "." || mapping.Path == ".." {
			return errors.New("Vault path must be a relative API path using letters, digits, underscores, dots, slashes and hyphens")
		}
		if mapping.Engine != "kv-v1" && mapping.Engine != "kv-v2" {
			return errors.New("select KV v1 or KV v2")
		}
		if mapping.Engine == "kv-v2" && !strings.Contains(mapping.Path, "/data/") {
			return errors.New("KV v2 requires the API path including /data/, for example kv/data/apps/backend")
		}
		key := mapping.Service + "\x00" + mapping.Variable
		if variables[key] {
			return errors.New("each service can map an environment variable only once")
		}
		ids[mapping.ID], variables[key] = true, true
	}
	return nil
}

// AgentTemplate produces only env_template stanzas. auto_auth, vault connection
// and exec configuration belong to the application's separately provisioned agent.
func AgentTemplate(mappings []portainer.VaultMapping, service string) (string, error) {
	selected := []portainer.VaultMapping{}
	for _, mapping := range mappings {
		if mapping.Service == service {
			selected = append(selected, mapping)
		}
	}
	if len(selected) == 0 {
		return "", errors.New("no mappings are configured for this service")
	}
	if err := Validate(selected, []string{service}); err != nil {
		return "", err
	}
	slices.SortFunc(selected, func(a, b portainer.VaultMapping) int { return strings.Compare(a.Variable, b.Variable) })
	var result strings.Builder
	result.WriteString("# Reference-only env_template stanzas. Merge into your application's agent.hcl.\n# Keep auto_auth, Vault address/TLS and exec configuration in that file.\n# This export does not deploy or verify the runtime configuration.\n\n")
	for _, mapping := range selected {
		data := ".Data"
		if mapping.Engine == "kv-v2" {
			data += ".data"
		}
		contents := fmt.Sprintf("{{ with secret %s }}{{ %s.%s }}{{ end }}", strconv.Quote(mapping.Path), data, mapping.Key)
		fmt.Fprintf(&result, "env_template %s {\n  contents = %s\n  error_on_missing_key = true\n}\n\n", strconv.Quote(mapping.Variable), strconv.Quote(contents))
	}
	return result.String(), nil
}
