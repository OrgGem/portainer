package portainer

// StandaloneRole is an explicit, environment-scoped role in this CE fork.
// Keeping it separate from legacy RoleId avoids promoting old CE access grants.
type StandaloneRole string

// StandaloneRolePolicy restricts the built-in role's maximum permissions.
// Nil on legacy records means the built-in defaults, not a disabled role.
type StandaloneRolePolicy struct {
	Enabled  bool            `json:"Enabled"`
	Features map[string]bool `json:"Features"`
	Version  int             `json:"Version"`
}

const (
	StandaloneEnvironmentAdmin StandaloneRole = "environment-admin"
	StandaloneOperator         StandaloneRole = "operator"
	StandaloneHelpdesk         StandaloneRole = "helpdesk"
	StandaloneStandard         StandaloneRole = "standard"
	StandaloneReadOnly         StandaloneRole = "read-only"
)
