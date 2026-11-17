package stacks

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	"github.com/portainer/portainer/api/stacks/stackutils"
	"github.com/portainer/portainer/api/stacks/vaultrefs"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type vaultStackResponse struct {
	Configuration portainer.StackVaultMappings `json:"Configuration"`
	Services      []string                     `json:"Services"`
	CanEdit       bool                         `json:"CanEdit"`
	Unavailable   string                       `json:"Unavailable,omitempty"`
}

func vaultConfiguration(stack *portainer.Stack) portainer.StackVaultMappings {
	if stack.VaultMappings != nil {
		return *stack.VaultMappings
	}
	return portainer.StackVaultMappings{Mappings: []portainer.VaultMapping{}}
}

func (h *Handler) stackVaultGet(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, _, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	result := vaultStackResponse{Configuration: vaultConfiguration(stack), Services: []string{}, CanEdit: security.ScopedEnvironmentAdmin(ctx, endpoint.ID)}
	if !ctx.IsAdmin && ctx.StandaloneRole != "" {
		policy, err := authorization.ReadStandalonePolicy(h.DataStore, ctx.StandaloneRole)
		if err != nil {
			return httperror.InternalServerError("Unable to read role policy", err)
		}
		result.CanEdit = result.CanEdit && authorization.RoleFeatureAllowed(policy, ctx.StandaloneRole, "vault.edit")
	}
	services, err := stackconfig.Services(stack)
	if err != nil || len(stack.AdditionalFiles) != 0 {
		result.Unavailable = "Mapping changes require a readable, single-file Compose definition."
		result.CanEdit = false
	} else {
		slices.Sort(services)
		result.Services = services
	}
	return response.JSON(w, result)
}

var errVaultVersion = errors.New("Vault mappings changed; reload before saving")

func (h *Handler) stackVaultUpdate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, _, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if !security.ScopedEnvironmentAdmin(ctx, endpoint.ID) {
		return httperror.Forbidden("Only global or environment administrators may change Vault mappings", nil)
	}
	if stack.Status == portainer.StackStatusDeploying || (stack.Configuration != nil && stack.Configuration.PendingRevision != "") {
		return httperror.Conflict("Wait for the stack deployment or configuration recovery to finish", nil)
	}
	var payload struct {
		Version  int
		Mappings []portainer.VaultMapping
	}
	if err := decodeControlPayload(w, r, &payload, 128*1024); err != nil {
		return httperror.BadRequest("Expected a version and reference-only mappings; values, tokens and credentials are not accepted", nil)
	}
	services, err := stackconfig.Services(stack)
	if err != nil || len(stack.AdditionalFiles) != 0 {
		return httperror.BadRequest("Mapping changes require a readable, single-file Compose definition", nil)
	}
	if payload.Mappings == nil {
		payload.Mappings = []portainer.VaultMapping{}
	}
	for i := range payload.Mappings {
		if payload.Mappings[i].ID == "" {
			payload.Mappings[i].ID = uuid.NewString()
		}
	}
	// Preserve untouched references to removed services so administrators can
	// repair or remove stale rows one at a time. New/changed rows must use a
	// current service; an old service never becomes a new allowed target.
	validationServices := slices.Clone(services)
	for _, mapping := range payload.Mappings {
		if !slices.Contains(services, mapping.Service) {
			if !slices.Contains(vaultConfiguration(stack).Mappings, mapping) {
				return httperror.BadRequest("New or changed mappings must select a service in the current Compose file", nil)
			}
			validationServices = append(validationServices, mapping.Service)
		}
	}
	if err := vaultrefs.Validate(payload.Mappings, validationServices); err != nil {
		return httperror.BadRequest("Invalid Vault mapping", err)
	}
	config := portainer.StackVaultMappings{Version: payload.Version + 1, Mappings: payload.Mappings, UpdatedAt: time.Now().Unix(), UpdatedBy: ctx.UserID}
	err = h.DataStore.UpdateTx(func(tx dataservices.DataStoreTx) error {
		current, err := tx.Stack().Read(stack.ID)
		if err != nil {
			return err
		}
		if vaultConfiguration(current).Version != payload.Version {
			return errVaultVersion
		}
		current.VaultMappings = &config
		return tx.Stack().Update(current.ID, current)
	})
	if errors.Is(err, errVaultVersion) {
		return httperror.Conflict(errVaultVersion.Error(), nil)
	}
	if err != nil {
		return httperror.InternalServerError("Unable to save Vault mappings", err)
	}
	return response.JSON(w, config)
}

func (h *Handler) stackVaultTemplate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, _, _, _, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	service := r.URL.Query().Get("service")
	services, err := stackconfig.Services(stack)
	if err != nil || !slices.Contains(services, service) {
		return httperror.BadRequest("Select an existing Compose service", nil)
	}
	content, err := vaultrefs.AgentTemplate(vaultConfiguration(stack).Mappings, service)
	if err != nil {
		return httperror.BadRequest("Unable to export Agent template", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, struct {
		Content string `json:"Content"`
		Version int    `json:"Version"`
	}{content, vaultConfiguration(stack).Version})
}

type vaultInventoryRow struct {
	portainer.VaultMapping
	EnvironmentID   portainer.EndpointID `json:"EnvironmentID"`
	EnvironmentName string               `json:"EnvironmentName"`
	StackID         portainer.StackID    `json:"StackID"`
	StackName       string               `json:"StackName"`
	Version         int                  `json:"Version"`
	UpdatedAt       int64                `json:"UpdatedAt"`
	UpdatedBy       portainer.UserID     `json:"UpdatedBy"`
}
type vaultInventorySummary struct {
	Mappings     int `json:"Mappings"`
	Variables    int `json:"Variables"`
	Paths        int `json:"Paths"`
	Environments int `json:"Environments"`
	Stacks       int `json:"Stacks"`
	Services     int `json:"Services"`
}
type vaultInventoryOption struct {
	ID            int                  `json:"ID"`
	Name          string               `json:"Name"`
	EnvironmentID portainer.EndpointID `json:"EnvironmentID,omitempty"`
}
type vaultInventoryResponse struct {
	Items        []vaultInventoryRow    `json:"Items"`
	Summary      vaultInventorySummary  `json:"Summary"`
	Environments []vaultInventoryOption `json:"Environments"`
	Stacks       []vaultInventoryOption `json:"Stacks"`
}

func (h *Handler) vaultInventory(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	ctx, err := security.RetrieveRestrictedRequestContext(r)
	if err != nil {
		return httperror.Forbidden("Authentication required", nil)
	}
	query := r.URL.Query()
	numbers := map[string]int{"environmentId": 0, "stackId": 0, "offset": 0, "limit": 50}
	for key := range numbers {
		if raw := query.Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				return httperror.BadRequest("Invalid inventory filter or pagination", nil)
			}
			numbers[key] = n
		}
	}
	if numbers["limit"] < 1 || numbers["limit"] > 200 {
		return httperror.BadRequest("Page size must be between 1 and 200", nil)
	}
	for _, key := range []string{"variable", "path", "key", "service"} {
		if len(query.Get(key)) > 512 {
			return httperror.BadRequest("Filter is too long", nil)
		}
	}
	result := vaultInventoryResponse{Items: []vaultInventoryRow{}, Environments: []vaultInventoryOption{}, Stacks: []vaultInventoryOption{}}
	stacks, err := h.DataStore.Stack().ReadAll()
	if err != nil {
		return httperror.InternalServerError("Unable to list stack references", err)
	}
	endpoints, err := h.DataStore.Endpoint().Endpoints()
	if err != nil {
		return httperror.InternalServerError("Unable to list environments", err)
	}
	controls, err := h.DataStore.ResourceControl().ReadAll()
	if err != nil {
		return httperror.InternalServerError("Unable to read stack access", err)
	}
	memberships, err := h.DataStore.TeamMembership().TeamMembershipsByUserID(ctx.UserID)
	if err != nil {
		return httperror.InternalServerError("Unable to read group membership", err)
	}
	// Resolve per environment and authorize before creating rows, options or counts.
	allowed := map[portainer.EndpointID]*portainer.Endpoint{}
	accesses := map[portainer.EndpointID]*authorization.StandaloneAccess{}
	for i := range endpoints {
		endpoint := &endpoints[i]
		if !authorization.SupportsStandaloneRoles(endpoint) {
			continue
		}
		if err := h.requestBouncer.AuthorizedEndpointOperation(r, endpoint); err != nil {
			continue
		}
		access, err := authorization.StandaloneAccessForUser(h.DataStore, ctx.UserID, endpoint)
		if err != nil {
			return httperror.InternalServerError("Unable to resolve environment access", err)
		}
		if !ctx.IsAdmin && access == nil {
			continue
		}
		allowed[endpoint.ID], accesses[endpoint.ID] = endpoint, access
	}
	envOptions, stackOptions := map[int]vaultInventoryOption{}, map[int]vaultInventoryOption{}
	for _, stack := range stacks {
		endpoint := allowed[stack.EndpointID]
		if endpoint == nil || stack.Type != portainer.DockerComposeStack || stack.VaultMappings == nil {
			continue
		}
		control := authorization.GetResourceControlByResourceIDAndType(stackutils.ResourceControlID(stack.EndpointID, stack.Name), portainer.StackResourceControl, controls)
		if !ctx.IsAdmin && !authorization.StandaloneResourceAccess(accesses[endpoint.ID], ctx.UserID, authorization.TeamIDs(memberships), control) {
			continue
		}
		for _, mapping := range stack.VaultMappings.Mappings {
			envOptions[int(endpoint.ID)] = vaultInventoryOption{ID: int(endpoint.ID), Name: endpoint.Name}
			stackOptions[int(stack.ID)] = vaultInventoryOption{ID: int(stack.ID), Name: stack.Name, EnvironmentID: endpoint.ID}
			if numbers["environmentId"] != 0 && numbers["environmentId"] != int(endpoint.ID) {
				continue
			}
			if numbers["stackId"] != 0 && numbers["stackId"] != int(stack.ID) {
				continue
			}
			matches := true
			for key, value := range map[string]string{"variable": mapping.Variable, "path": mapping.Path, "key": mapping.Key, "service": mapping.Service} {
				if !strings.Contains(strings.ToLower(value), strings.ToLower(query.Get(key))) {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			result.Items = append(result.Items, vaultInventoryRow{VaultMapping: mapping, EnvironmentID: endpoint.ID, EnvironmentName: endpoint.Name, StackID: stack.ID, StackName: stack.Name, Version: stack.VaultMappings.Version, UpdatedAt: stack.VaultMappings.UpdatedAt, UpdatedBy: stack.VaultMappings.UpdatedBy})
		}
	}
	for _, option := range envOptions {
		result.Environments = append(result.Environments, option)
	}
	for _, option := range stackOptions {
		result.Stacks = append(result.Stacks, option)
	}
	sortOption := func(a, b vaultInventoryOption) int {
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return a.ID - b.ID
	}
	slices.SortFunc(result.Environments, sortOption)
	slices.SortFunc(result.Stacks, sortOption)
	slices.SortFunc(result.Items, func(a, b vaultInventoryRow) int {
		if a.EnvironmentID != b.EnvironmentID {
			return int(a.EnvironmentID - b.EnvironmentID)
		}
		if a.StackID != b.StackID {
			return int(a.StackID - b.StackID)
		}
		if n := strings.Compare(a.Service, b.Service); n != 0 {
			return n
		}
		return strings.Compare(a.Variable, b.Variable)
	})
	result.Summary = summarizeVaultMappings(result.Items)
	start := min(numbers["offset"], len(result.Items))
	end := start + min(numbers["limit"], len(result.Items)-start)
	result.Items = result.Items[start:end]
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, result)
}

func summarizeVaultMappings(rows []vaultInventoryRow) vaultInventorySummary {
	variables, paths, environments, stacks, services := map[string]bool{}, map[string]bool{}, map[portainer.EndpointID]bool{}, map[portainer.StackID]bool{}, map[string]bool{}
	for _, row := range rows {
		variables[row.Variable], paths[row.Path], environments[row.EnvironmentID], stacks[row.StackID] = true, true, true, true
		services[strconv.Itoa(int(row.StackID))+":"+row.Service] = true
	}
	return vaultInventorySummary{Mappings: len(rows), Variables: len(variables), Paths: len(paths), Environments: len(environments), Stacks: len(stacks), Services: len(services)}
}
