package endpoints

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/require"
)

func TestEndpointVaultSecretIDIsWriteOnlyAndAddressChangeNeedsNewSecret(t *testing.T) {
	t.Parallel()

	_, store := datastore.MustNewTestStore(t, true, false)
	const endpointID = 7201
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: endpointID, Name: "vault-http-test"}))
	handler := &Handler{DataStore: store}
	request := func(method, body string) (*httptest.ResponseRecorder, *httperror.HandlerError) {
		r := mux.SetURLVars(httptest.NewRequest(method, "/endpoints/7201/vault", strings.NewReader(body)), map[string]string{"id": "7201"})
		w := httptest.NewRecorder()
		var handlerErr *httperror.HandlerError
		if method == http.MethodPut {
			handlerErr = handler.endpointVaultUpdate(w, r)
		} else {
			handlerErr = handler.endpointVaultGet(w, r)
		}
		return w, handlerErr
	}

	w, handlerErr := request(http.MethodPut, `{"Address":"https://vault.example.test","AuthMount":"platform","RoleID":"synthetic-role","SecretID":"synthetic-secret","CAPEM":""}`)
	require.Nil(t, handlerErr)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"HasSecretID":true`)
	require.NotContains(t, w.Body.String(), "synthetic-secret")

	w, handlerErr = request(http.MethodGet, "")
	require.Nil(t, handlerErr)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "synthetic-secret")

	w, handlerErr = request(http.MethodPut, `{"Address":"https://another-vault.example.test","AuthMount":"approle","RoleID":"new-role","CAPEM":""}`)
	require.NotNil(t, handlerErr)
	require.Equal(t, http.StatusBadRequest, handlerErr.StatusCode)

	w, handlerErr = request(http.MethodGet, "")
	require.Nil(t, handlerErr)
	require.Contains(t, w.Body.String(), `"Address":"https://vault.example.test"`)
	require.Contains(t, w.Body.String(), `"HasSecretID":true`)
}
