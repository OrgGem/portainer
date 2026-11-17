package authorization

import (
	"fmt"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"strings"
)

type RoleFeature struct {
	ID   string
	Name string
}

var StandaloneFeatures = []RoleFeature{
	{"environment.view", "Environment information and dashboard"},
	{"containers.view", "View containers, logs and inspection"},
	{"containers.operate", "Start, stop and update existing containers"},
	{"containers.manage", "Create, recreate and delete containers"},
	{"containers.console", "Exec and attach console"},
	{"containers.files", "Container archive and export"},
	{"images.view", "View images"}, {"images.manage", "Build, pull, push and delete images"},
	{"networks.view", "View networks"}, {"networks.manage", "Manage networks"},
	{"volumes.view", "View volumes"}, {"volumes.manage", "Create and delete volumes"},
	{"volumes.files", "Browse, upload, download and edit volume files"},
	{"metrics.view", "CPU, RAM and storage metrics"},
	{"stacks.view", "View stacks and Compose files"}, {"stacks.edit", "Create, edit and delete stacks"},
	{"stacks.deploy", "Start, stop and redeploy stacks"},
	{"config.upload", "Upload application configuration"}, {"config.apply", "Apply and rollback configuration"},
	{"vault.view", "View Vault references and inventory"}, {"vault.edit", "Edit Vault references"},
}

func DefaultStandalonePolicy(role portainer.StandaloneRole) *portainer.StandaloneRolePolicy {
	p := &portainer.StandaloneRolePolicy{Enabled: true, Version: 1, Features: map[string]bool{}}
	write := role == portainer.StandaloneEnvironmentAdmin || role == portainer.StandaloneStandard
	operate := write || role == portainer.StandaloneOperator
	for _, f := range StandaloneFeatures {
		allowed := strings.HasSuffix(f.ID, ".view")
		switch f.ID {
		case "containers.operate", "containers.console", "stacks.deploy", "config.upload", "config.apply":
			allowed = operate
		case "containers.manage", "containers.files", "images.manage", "networks.manage", "volumes.manage", "stacks.edit":
			allowed = write
		case "volumes.files", "vault.edit":
			allowed = role == portainer.StandaloneEnvironmentAdmin
		}
		p.Features[f.ID] = allowed && StandaloneRolePriority(role) != 0
	}
	return p
}

func ReadStandalonePolicy(store dataservices.DataStoreTx, role portainer.StandaloneRole) (*portainer.StandaloneRolePolicy, error) {
	def := StandaloneRoleDefinition(role)
	if def == nil {
		return nil, fmt.Errorf("unknown role %q", role)
	}
	roles, err := store.Role().ReadAll()
	if err != nil {
		return nil, err
	}
	for _, saved := range roles {
		if saved.ID == def.ID && saved.StandalonePolicy != nil {
			return saved.StandalonePolicy, nil
		}
	}
	return DefaultStandalonePolicy(role), nil
}

func RoleFeatureAllowed(policy *portainer.StandaloneRolePolicy, role portainer.StandaloneRole, features ...string) bool {
	if policy == nil || !policy.Enabled {
		return false
	}
	defaults := DefaultStandalonePolicy(role)
	for _, feature := range features {
		if !defaults.Features[feature] || !policy.Features[feature] {
			return false
		}
	}
	return true
}

func ValidateStandalonePolicy(role portainer.StandaloneRole, p *portainer.StandaloneRolePolicy) error {
	defaults := DefaultStandalonePolicy(role)
	if p == nil || len(p.Features) != len(defaults.Features) {
		return fmt.Errorf("a complete feature map is required")
	}
	for feature, enabled := range p.Features {
		supported, exists := defaults.Features[feature]
		if !exists || (enabled && !supported) {
			return fmt.Errorf("unsupported feature %q for role %q", feature, role)
		}
	}
	return nil
}

// Seed only missing policies; preserve existing authorization records and user edits.
func EnsureStandalonePolicies(tx dataservices.DataStoreTx) error {
	roles, err := tx.Role().ReadAll()
	if err != nil {
		return err
	}
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneStandard, portainer.StandaloneReadOnly} {
		def := StandaloneRoleDefinition(role)
		for _, existing := range roles {
			if existing.ID == def.ID {
				copy := existing
				def = &copy
				break
			}
		}
		if def.StandalonePolicy == nil {
			def.StandalonePolicy = DefaultStandalonePolicy(role)
			if err := tx.Role().Update(def.ID, def); err != nil {
				return err
			}
		}
	}
	return nil
}

func StackActionFeature(action portainer.StackAction) string {
	switch action {
	case portainer.StackView:
		return "stacks.view"
	case portainer.StackEdit:
		return "stacks.edit"
	case portainer.StackDeploy:
		return "stacks.deploy"
	case portainer.StackConfigUpload:
		return "config.upload"
	case portainer.StackConfigApply:
		return "config.apply"
	}
	return ""
}
