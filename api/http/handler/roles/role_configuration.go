package roles

import (
	"encoding/json"
	"fmt"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"io"
	"net/http"
)

type configuredRole struct {
	Role     portainer.Role
	Key      portainer.StandaloneRole
	Policy   *portainer.StandaloneRolePolicy
	Defaults *portainer.StandaloneRolePolicy
}

var roleKeys = []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneStandard, portainer.StandaloneReadOnly}

func (h *Handler) roleConfiguration(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var result []configuredRole
	err := h.DataStore.ViewTx(func(tx dataservices.DataStoreTx) error {
		for _, key := range roleKeys {
			policy, err := authorization.ReadStandalonePolicy(tx, key)
			if err != nil {
				return err
			}
			result = append(result, configuredRole{Role: *authorization.StandaloneRoleDefinition(key), Key: key, Policy: policy, Defaults: authorization.DefaultStandalonePolicy(key)})
		}
		return nil
	})
	if err != nil {
		return httperror.InternalServerError("Unable to read roles", err)
	}
	return response.JSON(w, struct {
		Roles    []configuredRole
		Features []authorization.RoleFeature
	}{result, authorization.StandaloneFeatures})
}

func (h *Handler) rolePolicyUpdate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	id, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid role ID", err)
	}
	var key portainer.StandaloneRole
	for _, candidate := range roleKeys {
		if int(authorization.StandaloneRoleDefinition(candidate).ID) == id {
			key = candidate
		}
	}
	if key == "" {
		return httperror.NotFound("Unknown Standalone role", nil)
	}
	var payload portainer.StandaloneRolePolicy
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return httperror.BadRequest("Invalid role policy", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return httperror.BadRequest("Expected one JSON object", nil)
	}
	if err := authorization.ValidateStandalonePolicy(key, &payload); err != nil {
		return httperror.BadRequest("Invalid role features", err)
	}
	err = h.DataStore.UpdateTx(func(tx dataservices.DataStoreTx) error {
		current, err := authorization.ReadStandalonePolicy(tx, key)
		if err != nil {
			return err
		}
		if current.Version != payload.Version {
			return httperror.Conflict("Role changed; reload before saving", fmt.Errorf("stale version"))
		}
		def := authorization.StandaloneRoleDefinition(key)
		rows, err := tx.Role().ReadAll()
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.ID == def.ID {
				copy := row
				def = &copy
				break
			}
		}
		payload.Version++
		def.StandalonePolicy = &payload
		return tx.Role().Update(def.ID, def)
	})
	if err != nil {
		return response.TxErrorResponse(err)
	}
	return response.JSON(w, payload)
}
