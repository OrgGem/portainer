package portainer

// VaultMapping contains references only. Credentials and resolved values are
// kept outside the stack model.
type VaultMapping struct {
	ID       string `json:"ID"`
	Service  string `json:"Service"`
	Variable string `json:"Variable"`
	Path     string `json:"Path"`
	Key      string `json:"Key"`
	Engine   string `json:"Engine"`
}

type StackVaultMappings struct {
	Version        int            `json:"Version"`
	Mappings       []VaultMapping `json:"Mappings"`
	InjectOnDeploy bool           `json:"InjectOnDeploy"`
	UpdatedAt      int64          `json:"UpdatedAt"`
	UpdatedBy      UserID         `json:"UpdatedBy"`
}
