package backup

import (
	"mime/multipart"
	"net/http"

	"github.com/pkg/errors"
	operations "github.com/portainer/portainer/api/backup"
	"github.com/portainer/portainer/api/http/security/setuptoken"
	"github.com/portainer/portainer/api/logs"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/rs/zerolog/log"
)

type restorePayload struct {
	FileContent []byte
	FileName    string
	Password    string
}

// @id Restore
// @summary Triggers a system restore using provided backup file
// @description Triggers a system restore using provided backup file
// @description **Access policy**: public (requires the X-Setup-Token header on an uninitialized instance unless --no-setup-token is set)
// @tags backup
// @accept json
// @param X-Setup-Token header string false "Setup token (required when instance is uninitialized and --no-setup-token is not set)"
// @param restorePayload body restorePayload true "Restore request payload"
// @success 200 "Success"
// @failure 400 "Invalid request"
// @failure 403 "Access denied - invalid or missing setup token"
// @failure 500 "Server error"
// @router /restore [post]
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	defer func() {
		if r.MultipartForm != nil {
			if err := r.MultipartForm.RemoveAll(); err != nil {
				log.Warn().Err(err).Msg("failed to remove restore upload temporary files")
			}
		}
	}()

	if err := setuptoken.Validate(r, h.SetupToken); err != nil {
		return err
	}

	initialized, err := h.adminMonitor.WasInitialized()
	if err != nil {
		return httperror.InternalServerError("Failed to check system initialization", err)
	}
	if initialized {
		return httperror.BadRequest("Cannot restore already initialized instance", errors.New("system already initialized"))
	}
	h.adminMonitor.Stop()

	archive, password, err := decodeForm(r)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}
	defer logs.CloseAndLogErr(archive)

	err = operations.RestoreArchive(archive, password, h.filestorePath, h.gate, h.dataStore, h.shutdownTrigger)
	if err != nil {
		return httperror.InternalServerError("Failed to restore the backup", err)
	}

	return nil
}

func decodeForm(r *http.Request) (multipart.File, string, error) {
	// Keep uploaded archives on disk so memory use does not scale with file size.
	// Parsing the form first accepts the password before or after the file part.
	if err := r.ParseMultipartForm(0); err != nil {
		return nil, "", err
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "", err
	}
	return file, r.FormValue("password"), nil
}
