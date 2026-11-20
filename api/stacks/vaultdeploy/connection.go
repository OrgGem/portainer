// Package vaultdeploy resolves stack references at deployment time.
package vaultdeploy

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/dataservices/endpoint"
)

var (
	ErrInvalidConnection = errors.New("invalid Vault connection")
	ErrNotConfigured     = errors.New("Vault is not configured for this environment")
	authMountPattern     = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_./-]{0,255}$`)
)

// Connection reads an environment's connection. Its caller must not expose
// SecretID in an API response, an error, or a log.
func Connection(store dataservices.DataStore, endpointID portainer.EndpointID) (*portainer.VaultConnection, error) {
	var connection portainer.VaultConnection
	key := store.Connection().ConvertToKey(int(endpointID))
	err := store.Connection().GetObject(portainer.VaultConnectionBucket, key, &connection)
	if store.IsErrObjectNotFound(err) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	return &connection, nil
}

// SaveConnection atomically preserves a previously saved SecretID when the
// address remains unchanged and verifies that the environment still exists.
func SaveConnection(store dataservices.DataStore, endpointID portainer.EndpointID, connection portainer.VaultConnection) error {
	if err := validateConnection(&connection); err != nil {
		return err
	}
	key := store.Connection().ConvertToKey(int(endpointID))
	return store.Connection().UpdateTx(func(tx portainer.Transaction) error {
		var environment portainer.Endpoint
		if err := tx.GetObject(endpoint.BucketName, key, &environment); err != nil {
			return err
		}

		var previous portainer.VaultConnection
		err := tx.GetObject(portainer.VaultConnectionBucket, key, &previous)
		if err != nil && !store.IsErrObjectNotFound(err) {
			return err
		}
		if connection.SecretID == "" && previous.Address == connection.Address {
			connection.SecretID = previous.SecretID
		}
		if connection.SecretID == "" {
			return fmt.Errorf("%w: SecretID is required for a new Vault address", ErrInvalidConnection)
		}
		return tx.UpdateObject(portainer.VaultConnectionBucket, key, &connection)
	})
}

func validateConnection(connection *portainer.VaultConnection) error {
	connection.Address = strings.TrimSpace(connection.Address)
	address, err := url.Parse(connection.Address)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return fmt.Errorf("%w: address must be an HTTPS URL without credentials, query, or fragment", ErrInvalidConnection)
	}
	// Preserve a reverse-proxy prefix but reject escaped or relative path parts.
	if address.RawPath != "" || (address.Path != "" && address.Path != "/" && (path.Clean(address.Path) != strings.TrimSuffix(address.Path, "/") || strings.Contains(address.Path, "//"))) {
		return fmt.Errorf("%w: invalid address path", ErrInvalidConnection)
	}
	address.Path = strings.TrimSuffix(address.Path, "/")
	connection.Address = address.String()
	connection.AuthMount = strings.TrimSpace(connection.AuthMount)
	if connection.AuthMount == "" {
		connection.AuthMount = "approle"
	}
	if !authMountPattern.MatchString(connection.AuthMount) || path.Clean(connection.AuthMount) != connection.AuthMount || strings.Contains(connection.AuthMount, "../") {
		return fmt.Errorf("%w: invalid AppRole auth mount", ErrInvalidConnection)
	}
	if strings.TrimSpace(connection.RoleID) == "" || (connection.SecretID != "" && strings.TrimSpace(connection.SecretID) == "") || len(connection.RoleID) > 4096 || len(connection.SecretID) > 4096 || strings.ContainsRune(connection.RoleID, 0) || strings.ContainsRune(connection.SecretID, 0) {
		return fmt.Errorf("%w: invalid RoleID or SecretID", ErrInvalidConnection)
	}
	if len(connection.CAPEM) > 64*1024 {
		return fmt.Errorf("%w: CA certificate is too large", ErrInvalidConnection)
	}
	if connection.CAPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(connection.CAPEM)) {
			return fmt.Errorf("%w: invalid CA certificate", ErrInvalidConnection)
		}
	}
	return nil
}
