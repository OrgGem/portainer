package stacks

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

func configurationReady(stack *portainer.Stack, endpoint *portainer.Endpoint, control *portainer.ResourceControl) *httperror.HandlerError {
	if control == nil || control.StackPolicy == nil {
		return httperror.Conflict("Assign a stack operation policy before managing application configuration", nil)
	}
	if err := stackconfig.ValidateEndpoint(stack, endpoint); err != nil {
		return httperror.BadRequest("Configuration is unavailable for this stack", err)
	}
	if _, err := stackconfig.Root(); err != nil {
		return httperror.BadRequest("Configuration storage is not configured", err)
	}
	return nil
}

func (h *Handler) stackConfigTargets(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if !ctx.IsAdmin {
		return httperror.Forbidden("Only administrators may declare configuration targets", nil)
	}
	if herr := configurationReady(stack, endpoint, control); herr != nil {
		return herr
	}
	var payload struct {
		Version int
		Targets []portainer.StackConfigTarget
	}
	if err := decodeControlPayload(w, r, &payload, 64*1024); err != nil {
		return httperror.BadRequest("Invalid targets", err)
	}
	services, err := stackconfig.Services(stack)
	if err != nil {
		return httperror.InternalServerError("Unable to read Compose services", err)
	}
	validationServices := slices.Clone(services)
	for _, target := range payload.Targets {
		if slices.Contains(services, target.Service) {
			continue
		}
		unchanged := stack.Configuration != nil && slices.ContainsFunc(stack.Configuration.Targets, func(old portainer.StackConfigTarget) bool {
			return target.ID == old.ID && target.Service == old.Service && target.Target == old.Target
		})
		if !unchanged {
			return httperror.BadRequest("New configuration targets must reference an existing service", nil)
		}
		validationServices = append(validationServices, target.Service)
	}
	if err := stackconfig.ValidateTargets(payload.Targets, validationServices); err != nil {
		return httperror.BadRequest("Invalid configuration target", err)
	}
	if stack.Configuration == nil {
		stack.Configuration = &portainer.StackConfiguration{}
	}
	config := stack.Configuration
	if config.Version != payload.Version {
		return httperror.Conflict("Configuration changed; refresh before saving", nil)
	}
	for _, old := range config.Targets {
		idx := slices.IndexFunc(payload.Targets, func(t portainer.StackConfigTarget) bool { return t.ID == old.ID })
		used := slices.ContainsFunc(config.Revisions, func(rev portainer.StackConfigRevision) bool { return rev.TargetID == old.ID })
		if used && !(idx < 0 && !slices.Contains(services, old.Service)) && (idx < 0 || old.Service != payload.Targets[idx].Service || old.Target != payload.Targets[idx].Target) {
			return httperror.Conflict("Targets with revisions are immutable; declare a new target instead", nil)
		}
	}
	for i := range payload.Targets {
		if !slices.ContainsFunc(config.Targets, func(t portainer.StackConfigTarget) bool { return t.ID == payload.Targets[i].ID }) && slices.ContainsFunc(config.Revisions, func(rev portainer.StackConfigRevision) bool { return rev.TargetID == payload.Targets[i].ID }) {
			return httperror.Conflict("Retired target identifiers cannot be reused; declare a new identifier", nil)
		}
		payload.Targets[i].ActiveRevision = ""
		for _, old := range config.Targets {
			if old.ID == payload.Targets[i].ID {
				payload.Targets[i].ActiveRevision = old.ActiveRevision
			}
		}
	}
	config.Targets, config.Version = payload.Targets, config.Version+1
	if err := h.DataStore.Stack().Update(stack.ID, stack); err != nil {
		return httperror.InternalServerError("Unable to save targets", err)
	}
	return response.JSON(w, config)
}

func (h *Handler) stackConfigUpload(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if herr := configurationReady(stack, endpoint, control); herr != nil {
		return herr
	}
	var payload struct {
		TargetID string
		Filename string
		Content  string
		Version  int
	}
	if err := decodeControlPayload(w, r, &payload, 6*stackconfig.MaxFileSize+4096); err != nil {
		return httperror.BadRequest("Invalid upload", err)
	}
	if stack.Configuration == nil {
		return httperror.Conflict("Declare a configuration target first", nil)
	}
	config := stack.Configuration
	if config.Version != payload.Version {
		return httperror.Conflict("Configuration changed; refresh before uploading", nil)
	}
	if !slices.ContainsFunc(config.Targets, func(t portainer.StackConfigTarget) bool { return t.ID == payload.TargetID }) {
		return httperror.BadRequest("Unknown configuration target", nil)
	}
	if len(config.Revisions) >= stackconfig.MaxRevisions {
		return httperror.Conflict("Configuration history limit reached (100 revisions per stack)", nil)
	}
	content := []byte(payload.Content)
	if err := stackconfig.ValidateContent(payload.Filename, content); err != nil {
		return httperror.BadRequest("Invalid configuration file", err)
	}
	revision := portainer.StackConfigRevision{ID: uuid.NewString(), TargetID: payload.TargetID, Filename: payload.Filename, Size: len(content), CreatedAt: time.Now().Unix(), CreatedBy: ctx.UserID}
	sum, err := stackconfig.WriteRevision(stack.ID, revision.ID, content)
	if err != nil {
		return httperror.InternalServerError("Unable to store configuration revision", err)
	}
	revision.SHA256 = sum
	config.Revisions = append(config.Revisions, revision)
	config.Version++
	appendConfigEvent(config, revision.ID, "upload", ctx.UserID, true)
	if err := h.DataStore.Stack().Update(stack.ID, stack); err != nil {
		return httperror.InternalServerError("Unable to save revision metadata", err)
	}
	return response.JSON(w, revision)
}

func (h *Handler) stackConfigContent(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, _, _, _, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	id, err := request.RetrieveRouteVariableValue(r, "revision")
	if err != nil {
		return httperror.BadRequest("Invalid revision", err)
	}
	if stack.Configuration == nil {
		return httperror.NotFound("Revision not found", nil)
	}
	idx := slices.IndexFunc(stack.Configuration.Revisions, func(rev portainer.StackConfigRevision) bool { return rev.ID == id })
	if idx < 0 {
		return httperror.NotFound("Revision not found", nil)
	}
	content, err := stackconfig.ReadRevision(stack.ID, stack.Configuration.Revisions[idx])
	if err != nil {
		return httperror.InternalServerError("Unable to read revision", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, map[string]string{"Content": string(content)})
}

func (h *Handler) stackConfigApply(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if herr := configurationReady(stack, endpoint, control); herr != nil {
		return herr
	}
	if stack.Configuration == nil {
		return httperror.NotFound("Revision not found", nil)
	}
	config := stack.Configuration
	var payload struct{ Version int }
	if err := decodeControlPayload(w, r, &payload, 1024); err != nil {
		return httperror.BadRequest("Invalid apply request", err)
	}
	if payload.Version != config.Version {
		return httperror.Conflict("Configuration changed; review the current revision before applying", nil)
	}
	if stack.Status != portainer.StackStatusActive || config.PendingRevision != "" {
		return httperror.Conflict("Configuration can only be applied to an active stack without pending recovery", nil)
	}
	id, err := request.RetrieveRouteVariableValue(r, "revision")
	if err != nil {
		return httperror.BadRequest("Invalid revision", err)
	}
	idx := slices.IndexFunc(config.Revisions, func(rev portainer.StackConfigRevision) bool { return rev.ID == id })
	if idx < 0 {
		return httperror.NotFound("Revision not found", nil)
	}
	revision := config.Revisions[idx]
	action := "apply"
	if strings.HasSuffix(r.URL.Path, "/rollback") {
		action = "rollback"
		if revision.AppliedAt == 0 {
			return httperror.BadRequest("Rollback requires a previously successful revision", nil)
		}
	}
	targetIdx := slices.IndexFunc(config.Targets, func(t portainer.StackConfigTarget) bool { return t.ID == revision.TargetID })
	if targetIdx < 0 {
		return httperror.Conflict("Configuration target no longer exists", nil)
	}
	services, err := stackconfig.Services(stack)
	if err != nil {
		return httperror.InternalServerError("Unable to read Compose services", err)
	}
	if !slices.Contains(services, config.Targets[targetIdx].Service) {
		return httperror.Conflict("Configuration target service no longer exists; declare a new target or restore the service", nil)
	}
	if config.Targets[targetIdx].ActiveRevision == id {
		return response.JSON(w, config)
	}
	if _, err := stackconfig.ReadRevision(stack.ID, revision); err != nil {
		return httperror.InternalServerError("Revision integrity check failed", err)
	}
	images, err := h.runningServiceImages(r.Context(), stack, endpoint, config.Targets[targetIdx].Service)
	if err != nil {
		return httperror.Conflict("Unable to determine the running service image", err)
	}
	if herr := h.applyConfigurationRevision(stack, endpoint, ctx, idx, targetIdx, action, images); herr != nil {
		return herr
	}
	return response.JSON(w, stack.Configuration)
}

// applyConfigurationRevision records intent before Docker changes and commits the
// active revision only after a successful deployment. Call with the stack lock held.
func (h *Handler) applyConfigurationRevision(stack *portainer.Stack, endpoint *portainer.Endpoint, ctx *security.RestrictedRequestContext, idx, targetIdx int, action string, images map[string]string) *httperror.HandlerError {
	config := stack.Configuration
	id := config.Revisions[idx].ID
	config.PendingRevision = id
	config.PendingImages = images
	config.Version++
	stack.Status = portainer.StackStatusDeploying
	if err := h.DataStore.Stack().Update(stack.ID, stack); err != nil {
		return httperror.InternalServerError("Unable to persist pending deployment", err)
	}
	// Persisted active pointers stay at the last known good deployment until Up succeeds.
	candidate := *stack
	candidateConfig := *config
	candidateConfig.Targets = slices.Clone(config.Targets)
	candidate.Configuration = &candidateConfig
	candidateConfig.Targets[targetIdx].ActiveRevision = id
	deployErr := h.deployConfiguration(&candidate, endpoint, images)
	if deployErr != nil {
		appendConfigEvent(config, id, action, ctx.UserID, false)
		// Compose can fail after replacing a container. Restore its previous mounts
		// using the same pinned image before reporting the failed apply.
		recoverErr := h.deployConfiguration(stack, endpoint, images)
		if recoverErr == nil {
			config.PendingRevision = ""
			config.PendingImages = nil
			stack.Status = portainer.StackStatusActive
		} else {
			stack.Status = portainer.StackStatusError
		}
		config.Version++
		persistErr := h.DataStore.Stack().Update(stack.ID, stack)
		if recoverErr != nil || persistErr != nil {
			return httperror.InternalServerError("Apply failed; administrator recovery is required", errors.Join(deployErr, recoverErr, persistErr))
		}
		return httperror.InternalServerError("Apply failed; the previous configuration has been restored", deployErr)
	}
	config.Targets[targetIdx].ActiveRevision = id
	config.Revisions[idx].AppliedAt, config.Revisions[idx].AppliedBy = time.Now().Unix(), ctx.UserID
	config.PendingRevision = ""
	config.PendingImages = nil
	config.Version++
	stack.Status = portainer.StackStatusActive
	stack.UpdateDate = time.Now().Unix()
	if ctx.User != nil {
		stack.UpdatedBy = ctx.User.Username
	}
	appendConfigEvent(config, id, action, ctx.UserID, true)
	if err := h.DataStore.Stack().Update(stack.ID, stack); err != nil {
		return httperror.InternalServerError("Deployment succeeded but its receipt could not be saved; administrator recovery is required", err)
	}
	return nil
}

func (h *Handler) stackConfigRecover(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stack, endpoint, control, ctx, herr := h.loadControlledStack(r)
	if herr != nil {
		return herr
	}
	if !ctx.IsAdmin {
		return httperror.Forbidden("Only administrators may recover an interrupted deployment", nil)
	}
	if herr := configurationReady(stack, endpoint, control); herr != nil {
		return herr
	}
	config := stack.Configuration
	if config == nil || config.PendingRevision == "" {
		return httperror.Conflict("No configuration deployment needs recovery", nil)
	}
	idx := slices.IndexFunc(config.Revisions, func(rev portainer.StackConfigRevision) bool { return rev.ID == config.PendingRevision })
	if idx < 0 {
		return httperror.Conflict("Pending revision is missing", nil)
	}
	targetIdx := slices.IndexFunc(config.Targets, func(t portainer.StackConfigTarget) bool { return t.ID == config.Revisions[idx].TargetID })
	if targetIdx < 0 {
		return httperror.Conflict("Pending target is missing", nil)
	}
	images := config.PendingImages
	if len(images) != 1 || images[config.Targets[targetIdx].Service] == "" {
		return httperror.Conflict("Pending service image is missing", nil)
	}
	if err := h.deployConfiguration(stack, endpoint, images); err != nil {
		return httperror.InternalServerError("Configuration recovery failed", err)
	}
	appendConfigEvent(config, config.PendingRevision, "recover", ctx.UserID, true)
	config.PendingRevision = ""
	config.PendingImages = nil
	config.Version++
	stack.Status = portainer.StackStatusActive
	if err := h.DataStore.Stack().Update(stack.ID, stack); err != nil {
		return httperror.InternalServerError("Unable to save recovery receipt", err)
	}
	return response.JSON(w, config)
}

func (h *Handler) deployConfiguration(stack *portainer.Stack, endpoint *portainer.Endpoint, images map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return h.ComposeStackManager.Up(ctx, stack, endpoint, portainer.ComposeUpOptions{ConfigOnly: true, ServiceImages: images, ForceRecreate: true})
}

func (h *Handler) runningServiceImages(ctx context.Context, stack *portainer.Stack, endpoint *portainer.Endpoint, service string) (map[string]string, error) {
	client, err := h.DockerClientFactory.CreateClient(endpoint, "", nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	containers, err := client.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+stack.Name), filters.Arg("label", "com.docker.compose.service="+service), filters.Arg("label", "com.docker.compose.oneoff=False"))})
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, errors.New("service has no existing containers; deploy the stack first")
	}
	image := containers[0].ImageID
	for _, c := range containers {
		if c.ImageID != image {
			return nil, errors.New("service replicas use different images")
		}
	}
	if image == "" {
		return nil, errors.New("service image ID is missing")
	}
	return map[string]string{service: image}, nil
}

func appendConfigEvent(config *portainer.StackConfiguration, revision, action string, user portainer.UserID, success bool) {
	config.Events = append(config.Events, portainer.StackConfigEvent{RevisionID: revision, Action: action, UserID: user, Time: time.Now().Unix(), Success: success})
	if len(config.Events) > 200 {
		config.Events = config.Events[len(config.Events)-200:]
	}
}
