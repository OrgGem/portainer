package backup

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/portainer/portainer/api/backup/s3backup"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// @id BackupS3Settings
// @summary Get the scheduled S3 backup settings and last result
// @description **Access policy**: administrator
// @tags backup
// @security ApiKeyAuth
// @security jwt
// @produce json
// @success 200 {object} s3backup.Settings "Success"
// @failure 500 "Server error"
// @router /backup/s3/settings [get]
func (h *Handler) s3SettingsGet(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	settings, err := h.s3Backup.Get()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve S3 backup settings", nil)
	}
	return response.JSON(w, settings)
}

// @id BackupS3SettingsSave
// @summary Save a daily S3 backup schedule
// @description **Access policy**: administrator
// @tags backup
// @security ApiKeyAuth
// @security jwt
// @accept json
// @produce json
// @param body body s3backup.Configuration true "S3 destination and daily UTC schedule"
// @success 200 {object} s3backup.Settings "Success"
// @failure 400 "Invalid settings"
// @failure 500 "Server error"
// @router /backup/s3/settings [post]
func (h *Handler) s3SettingsSave(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload s3backup.Configuration
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return httperror.BadRequest("Invalid S3 backup settings", nil)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return httperror.BadRequest("Invalid S3 backup settings", nil)
	}
	if err := payload.Validate(); err != nil {
		return httperror.BadRequest("Invalid S3 backup settings", err)
	}
	settings, err := h.s3Backup.Save(payload)
	if err != nil {
		return httperror.InternalServerError("Unable to save S3 backup settings", nil)
	}
	return response.JSON(w, settings)
}

// @id BackupS3Execute
// @summary Back up Portainer to the configured S3 bucket now
// @description **Access policy**: administrator
// @tags backup
// @security ApiKeyAuth
// @security jwt
// @produce json
// @success 200 {object} s3backup.Run "Success"
// @failure 400 "Invalid settings"
// @failure 409 "Backup already running"
// @failure 500 "Backup failed"
// @router /backup/s3/execute [post]
func (h *Handler) s3Execute(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	result, err := h.s3Backup.Execute(r.Context())
	if errors.Is(err, s3backup.ErrBackupRunning) {
		return httperror.Conflict("A backup is already running", nil)
	}
	if errors.Is(err, s3backup.ErrInvalidSettings) {
		return httperror.BadRequest("Invalid S3 backup settings", err)
	}
	if err != nil {
		return httperror.InternalServerError("S3 backup failed", err)
	}
	return response.JSON(w, result)
}
