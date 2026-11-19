package backup

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pkg/errors"
	"github.com/portainer/portainer/api/archive"
	"github.com/portainer/portainer/api/crypto"
	"github.com/portainer/portainer/api/database/boltdb"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/logs"

	"github.com/rs/zerolog/log"
)

var filesToRestore = append(filesToBackup, "portainer.db")

// Restores system state from backup archive, will trigger system shutdown, when finished.
func RestoreArchive(archive io.Reader, password string, filestorePath string, gate *offlinegate.OfflineGate, datastore dataservices.DataStore, shutdownTrigger context.CancelFunc) error {
	restoreParent := filesystem.JoinPaths(filestorePath, "restore")
	if err := os.MkdirAll(restoreParent, 0o700); err != nil {
		return errors.Wrap(err, "failed to prepare restore directory")
	}
	restorePath, err := os.MkdirTemp(restoreParent, "restore-")
	if err != nil {
		return errors.Wrap(err, "failed to create restore directory")
	}
	defer func() {
		if err := os.RemoveAll(restorePath); err != nil {
			log.Warn().Err(err).Msg("failed to clean up restore files")
		}
	}()

	if password != "" {
		// Finish decrypting the input before extraction, including authentication
		// of every GCM block. Legacy OFB backups have no authentication.
		decrypted, err := os.CreateTemp(restorePath, "decrypted-*.tar.gz")
		if err != nil {
			return errors.Wrap(err, "failed to prepare decrypted backup file")
		}
		defer logs.CloseAndLogErr(decrypted)
		if err := crypto.AesDecryptTo(archive, decrypted, []byte(password)); err != nil {
			return errors.Wrap(err, "failed to decrypt the archive. Please ensure the password is correct and try again")
		}
		if _, err := decrypted.Seek(0, io.SeekStart); err != nil {
			return errors.Wrap(err, "failed to read decrypted backup file")
		}
		archive = decrypted
	}

	extractPath := filesystem.JoinPaths(restorePath, "contents")
	if err := extractArchive(archive, extractPath); err != nil {
		return errors.Wrap(err, "cannot extract files from the archive. Please ensure the password is correct and try again")
	}

	unlock := gate.Lock()
	defer unlock()

	// At some point, backups were created containing a subdirectory, now we need to handle both
	restoreSourcePath, err := getRestoreSourcePath(extractPath)
	if err != nil {
		return errors.Wrap(err, "failed to restore from backup. Portainer database missing from backup file")
	}
	if err := validateRestoreDatabase(restoreSourcePath); err != nil {
		return errors.Wrap(err, "failed to restore from backup")
	}
	if err := restoreStackConfigurations(restoreSourcePath); err != nil {
		return errors.Wrap(err, "failed to restore stack configuration revisions")
	}
	if err := datastore.Close(); err != nil {
		return errors.Wrap(err, "Failed to stop db")
	}

	if err := restoreFiles(restoreSourcePath, filestorePath); err != nil {
		return errors.Wrap(err, "failed to restore the system state")
	}

	shutdownTrigger()
	return nil
}

func validateRestoreDatabase(dir string) error {
	found := 0
	for _, name := range []string{boltdb.DatabaseFileName, boltdb.EncryptedDatabaseFileName} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("backup database is not a regular file")
		}
		found++
	}
	if found != 1 {
		return errors.New("backup must contain exactly one Portainer database")
	}
	return nil
}

func extractArchive(r io.Reader, destinationDirPath string) error {
	return archive.ExtractTarGz(r, destinationDirPath)
}

func getRestoreSourcePath(dir string) (string, error) {
	// find portainer.db or portainer.edb file. Return the parent directory
	var portainerdbRegex = regexp.MustCompile(`^portainer.e?db$`)

	backupDirPath := dir
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if portainerdbRegex.MatchString(d.Name()) {
			backupDirPath = filepath.Dir(path)
			return filepath.SkipDir
		}
		return nil
	})

	return backupDirPath, err
}

func restoreFiles(srcDir string, destinationDir string) error {
	for _, filename := range filesToRestore {
		if err := filesystem.CopyPath(filesystem.JoinPaths(srcDir, filename), destinationDir); err != nil {
			return err
		}
	}

	// TODO:  This is very boltdb module specific once again due to the filename.  Move to bolt module? Refactor for another day

	// Prevent the possibility of having both databases.  Remove any default new instance
	if err := os.Remove(filesystem.JoinPaths(destinationDir, boltdb.DatabaseFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := os.Remove(filesystem.JoinPaths(destinationDir, boltdb.EncryptedDatabaseFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Now copy the database.  It'll be either portainer.db or portainer.edb

	// Note: CopyPath does not return an error if the source file doesn't exist
	if err := filesystem.CopyPath(filesystem.JoinPaths(srcDir, boltdb.EncryptedDatabaseFileName), destinationDir); err != nil {
		return err
	}

	return filesystem.CopyPath(filesystem.JoinPaths(srcDir, boltdb.DatabaseFileName), destinationDir)
}
