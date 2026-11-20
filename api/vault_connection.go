package portainer

// VaultConnectionBucket stores Vault connections separately from endpoint
// responses, keyed by environment ID.
const VaultConnectionBucket = "vault_connections"

// VaultConnection is persisted using the datastore's configured encryption.
// API handlers must use a separate response type that excludes SecretID.
type VaultConnection struct {
	Address   string `json:"Address"`
	AuthMount string `json:"AuthMount"`
	RoleID    string `json:"RoleID"`
	SecretID  string `json:"SecretID"`
	CAPEM     string `json:"CAPEM"`
}
