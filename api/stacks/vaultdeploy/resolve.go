package vaultdeploy

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/stacks/vaultrefs"
)

const (
	resolutionTimeout = 30 * time.Second
	responseSizeLimit = 1024 * 1024
)

// Resolve retrieves every referenced value before any Docker resource changes.
// The returned map is keyed by service and environment variable name.
func Resolve(ctx context.Context, store dataservices.DataStore, endpointID portainer.EndpointID, mappings []portainer.VaultMapping) (map[string]map[string]string, error) {
	values := make(map[string]map[string]string)
	if len(mappings) == 0 {
		return values, nil
	}

	services := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		services = append(services, mapping.Service)
	}
	if err := vaultrefs.Validate(mappings, services); err != nil {
		return nil, fmt.Errorf("invalid Vault mappings: %w", err)
	}
	connection, err := Connection(store, endpointID)
	if err != nil {
		return nil, err
	}
	if err := validateConnection(connection); err != nil || connection.SecretID == "" {
		return nil, fmt.Errorf("%w: saved Vault connection is incomplete", ErrInvalidConnection)
	}
	client, err := newClient(connection)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(ctx, resolutionTimeout)
	defer cancel()
	token, err := client.login(ctx, connection)
	if err != nil {
		return nil, err
	}

	// The API path identifies the response, so keys at the same path are
	// fetched once even when different services refer to them.
	secrets := make(map[string]map[string]json.RawMessage)
	for _, mapping := range mappings {
		data, ok := secrets[mapping.Path]
		if !ok {
			data, err = client.read(ctx, connection.Address, mapping.Path, token)
			if err != nil {
				return nil, fmt.Errorf("Vault read failed for service %s variable %s: %w", mapping.Service, mapping.Variable, err)
			}
			secrets[mapping.Path] = data
		}

		secretData := data
		if mapping.Engine == "kv-v2" {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(data["data"], &nested); err != nil || nested == nil {
				return nil, fmt.Errorf("Vault returned invalid KV v2 data for service %s variable %s", mapping.Service, mapping.Variable)
			}
			secretData = nested
		}
		raw, exists := secretData[mapping.Key]
		if !exists {
			return nil, fmt.Errorf("Vault key is missing for service %s variable %s", mapping.Service, mapping.Variable)
		}
		var value string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("Vault key must contain a string without NUL for service %s variable %s", mapping.Service, mapping.Variable)
		}
		if values[mapping.Service] == nil {
			values[mapping.Service] = make(map[string]string)
		}
		values[mapping.Service][mapping.Variable] = value
	}
	return values, nil
}

type client struct {
	http *http.Client
}

func newClient(connection *portainer.VaultConnection) (*client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if connection.CAPEM != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(connection.CAPEM)) {
			return nil, fmt.Errorf("%w: invalid CA certificate", ErrInvalidConnection)
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &client{http: &http.Client{
		Transport: transport,
		Timeout:   resolutionTimeout,
		// Redirects could send credentials or Vault tokens to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

func (c *client) login(ctx context.Context, connection *portainer.VaultConnection) (string, error) {
	address, err := url.JoinPath(connection.Address, "v1", "auth", connection.AuthMount, "login")
	if err != nil {
		return "", errors.New("Vault login address is invalid")
	}
	body, err := json.Marshal(struct {
		RoleID   string `json:"role_id"`
		SecretID string `json:"secret_id"`
	}{connection.RoleID, connection.SecretID})
	if err != nil {
		return "", errors.New("unable to create Vault login request")
	}
	var response struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := c.request(ctx, http.MethodPost, address, "", body, &response); err != nil {
		return "", fmt.Errorf("Vault AppRole login failed: %w", err)
	}
	if response.Auth.ClientToken == "" {
		return "", errors.New("Vault login returned no client token")
	}
	return response.Auth.ClientToken, nil
}

func (c *client) read(ctx context.Context, address, path, token string) (map[string]json.RawMessage, error) {
	endpoint, err := url.JoinPath(address, "v1", path)
	if err != nil {
		return nil, errors.New("invalid Vault path")
	}
	var response struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, endpoint, token, nil, &response); err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("Vault response contains no secret data")
	}
	return response.Data, nil
}

func (c *client) request(ctx context.Context, method, address, token string, body []byte, output any) error {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return errors.New("unable to create Vault request")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("unable to contact Vault or verify its TLS certificate")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Vault returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, responseSizeLimit+1))
	if err != nil || len(data) > responseSizeLimit {
		return errors.New("unable to read Vault response or response exceeds size limit")
	}
	if json.Unmarshal(data, output) != nil {
		return errors.New("Vault returned invalid JSON")
	}
	return nil
}
