package vaultdeploy

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/stretchr/testify/require"
)

func TestResolveAppRoleReadsKV1AndKV2OncePerPathWithCustomCA(t *testing.T) {
	t.Parallel()

	var logins, v2Reads, v1Reads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/platform/login":
			logins.Add(1)
			var payload map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, "synthetic-role-id", payload["role_id"])
			require.Equal(t, "synthetic-secret-id", payload["secret_id"])
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
		case "/v1/kv/data/apps/backend":
			v2Reads.Add(1)
			require.Equal(t, "synthetic-token", r.Header.Get("X-Vault-Token"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"data":{"password":"p@ss=$word","empty":""},"metadata":{"version":4}}}`))
		case "/v1/legacy/apps/worker":
			v1Reads.Add(1)
			require.Equal(t, "synthetic-token", r.Header.Get("X-Vault-Token"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"TOKEN":"legacy-token"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	_, store := datastore.MustNewTestStore(t, true, true)
	const endpointID = 7531
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: endpointID, Name: "vault-resolve-test"}))
	parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
	address := server.URL
	require.NoError(t, SaveConnection(store, endpointID, portainer.VaultConnection{
		Address:   address,
		AuthMount: "platform",
		RoleID:    "synthetic-role-id",
		SecretID:  "synthetic-secret-id",
		CAPEM:     string(caPEM),
	}))

	resolved, err := Resolve(t.Context(), store, endpointID, []portainer.VaultMapping{
		{ID: "backend-password", Service: "backend", Variable: "DB_PASSWORD", Path: "kv/data/apps/backend", Key: "password", Engine: "kv-v2"},
		{ID: "backend-empty", Service: "backend", Variable: "OPTIONAL_VALUE", Path: "kv/data/apps/backend", Key: "empty", Engine: "kv-v2"},
		{ID: "worker-token", Service: "worker", Variable: "API_TOKEN", Path: "legacy/apps/worker", Key: "TOKEN", Engine: "kv-v1"},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]string{
		"backend": {"DB_PASSWORD": "p@ss=$word", "OPTIONAL_VALUE": ""},
		"worker":  {"API_TOKEN": "legacy-token"},
	}, resolved)
	require.EqualValues(t, 1, logins.Load(), "AppRole login should happen once per deploy")
	require.EqualValues(t, 1, v2Reads.Load(), "a Vault path shared by mappings should be fetched once")
	require.EqualValues(t, 1, v1Reads.Load())

	databaseFile, ok := store.Connection().(interface{ GetDatabaseFilePath() string })
	require.True(t, ok)
	stored, err := os.ReadFile(databaseFile.GetDatabaseFilePath())
	require.NoError(t, err)
	require.False(t, bytes.Contains(stored, []byte("synthetic-secret-id")), "the encrypted datastore file must not contain the SecretID as plaintext")
}

func TestResolveDoesNotFollowVaultRedirects(t *testing.T) {
	t.Parallel()

	var redirected atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = w.Write([]byte(`{"data":{"password":"should-not-be-read"}}`))
	}))
	t.Cleanup(other.Close)

	var unauthenticatedRedirect atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
			return
		}
		unauthenticatedRedirect.Add(1)
		http.Redirect(w, r, other.URL+"/v1/secret", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	_, store := datastore.MustNewTestStore(t, true, false)
	const endpointID = 7532
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: endpointID, Name: "vault-redirect-test"}))
	parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
	address := server.URL
	require.NoError(t, SaveConnection(store, endpointID, portainer.VaultConnection{
		Address:  address,
		RoleID:   "synthetic-role-id",
		SecretID: "synthetic-secret-id",
		CAPEM:    string(caPEM),
	}))

	_, err = Resolve(t.Context(), store, endpointID, []portainer.VaultMapping{
		{ID: "password", Service: "backend", Variable: "DB_PASSWORD", Path: "secret", Key: "password", Engine: "kv-v1"},
	})
	require.ErrorContains(t, err, "Vault returned HTTP 302")
	require.NotContains(t, fmt.Sprint(err), "synthetic-secret-id")
	require.EqualValues(t, 1, unauthenticatedRedirect.Load())
	require.Zero(t, redirected.Load(), "the HTTP client must not follow a redirect and disclose its token")
}

func TestResolveRejectsMissingAndNonStringValuesWithoutReturningAnyValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "missing", response: `{"data":{"data":{}}}`, wantErr: "Vault key is missing"},
		{name: "non-string", response: `{"data":{"data":{"password":42}}}`, wantErr: "must contain a string"},
		{name: "null", response: `{"data":{"data":{"password":null}}}`, wantErr: "must contain a string"},
		{name: "embedded NUL", response: `{"data":{"data":{"password":"a\u0000b"}}}`, wantErr: "must contain a string"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/auth/approle/login" {
					_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
					return
				}
				_, _ = w.Write([]byte(test.response))
			}))
			t.Cleanup(server.Close)

			_, store := datastore.MustNewTestStore(t, true, false)
			const endpointID = 7533
			require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: endpointID, Name: "vault-value-validation-test"}))
			parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
			require.NoError(t, err)
			caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
			address := server.URL
			require.NoError(t, SaveConnection(store, endpointID, portainer.VaultConnection{
				Address:  address,
				RoleID:   "synthetic-role-id",
				SecretID: "synthetic-secret-id",
				CAPEM:    string(caPEM),
			}))

			resolved, err := Resolve(t.Context(), store, endpointID, []portainer.VaultMapping{
				{ID: "password", Service: "backend", Variable: "DB_PASSWORD", Path: "kv/data/apps/backend", Key: "password", Engine: "kv-v2"},
			})
			require.ErrorContains(t, err, test.wantErr)
			require.Nil(t, resolved, "a failed secret resolution must not return a partially populated env")
		})
	}
}

func TestResolveEmptyMappingsDoesNotRequireAVaultConnection(t *testing.T) {
	t.Parallel()

	_, store := datastore.MustNewTestStore(t, true, false)
	resolved, err := Resolve(context.Background(), store, 99999, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Empty(t, resolved)
}

func TestSaveConnectionRequiresNewSecretIDForChangedAddressAndPreservesSameAddress(t *testing.T) {
	t.Parallel()

	_, store := datastore.MustNewTestStore(t, true, false)
	const endpointID = 7534
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: endpointID, Name: "vault-save-test"}))
	original := portainer.VaultConnection{Address: "https://vault.example.test", RoleID: "role", SecretID: "initial-secret-id"}
	require.NoError(t, SaveConnection(store, endpointID, original))

	updated := original
	updated.SecretID = ""
	updated.CAPEM = ""
	require.NoError(t, SaveConnection(store, endpointID, updated))
	saved, err := Connection(store, endpointID)
	require.NoError(t, err)
	require.Equal(t, original.SecretID, saved.SecretID)

	updated.Address = "https://another-vault.example.test"
	require.ErrorIs(t, SaveConnection(store, endpointID, updated), ErrInvalidConnection)
}
