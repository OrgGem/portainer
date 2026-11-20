package endpoints

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/stacks/vaultdeploy"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// vaultConnectionResponse deliberately excludes the saved SecretID.
type vaultConnectionResponse struct {
	Address     string `json:"Address"`
	AuthMount   string `json:"AuthMount"`
	RoleID      string `json:"RoleID"`
	CAPEM       string `json:"CAPEM"`
	HasSecretID bool   `json:"HasSecretID"`
}

func (handler *Handler) endpointVaultGet(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	endpointID, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid environment identifier", nil)
	}
	if _, err := handler.DataStore.Endpoint().Endpoint(portainer.EndpointID(endpointID)); err != nil {
		if handler.DataStore.IsErrObjectNotFound(err) {
			return httperror.NotFound("Environment not found", nil)
		}
		return httperror.InternalServerError("Unable to read environment", err)
	}

	connection, err := vaultdeploy.Connection(handler.DataStore, portainer.EndpointID(endpointID))
	if errors.Is(err, vaultdeploy.ErrNotConfigured) {
		w.Header().Set("Cache-Control", "no-store")
		return response.JSON(w, vaultConnectionResponse{AuthMount: "approle"})
	}
	if err != nil {
		return httperror.InternalServerError("Unable to read Vault connection", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, vaultConnectionResponse{
		Address:     connection.Address,
		AuthMount:   connection.AuthMount,
		RoleID:      connection.RoleID,
		CAPEM:       connection.CAPEM,
		HasSecretID: connection.SecretID != "",
	})
}

func (handler *Handler) endpointVaultUpdate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	endpointID, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid environment identifier", nil)
	}
	var connection portainer.VaultConnection
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&connection); err != nil {
		return httperror.BadRequest("Invalid Vault connection payload", nil)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return httperror.BadRequest("Invalid Vault connection payload", nil)
	}

	if err := vaultdeploy.SaveConnection(handler.DataStore, portainer.EndpointID(endpointID), connection); err != nil {
		if errors.Is(err, vaultdeploy.ErrInvalidConnection) {
			return httperror.BadRequest(err.Error(), nil)
		}
		if handler.DataStore.IsErrObjectNotFound(err) {
			return httperror.NotFound("Environment not found", nil)
		}
		return httperror.InternalServerError("Unable to save Vault connection", err)
	}
	return handler.endpointVaultGet(w, r)
}
