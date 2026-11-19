package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/database/boltdb"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/stacks/stackconfig"
)

const (
	stackConfigManifestName = "stack-config-backup.json"
	stackConfigFilesName    = "stack-config-files"
	stackConfigFormat       = 1
)

type stackConfigBackup struct {
	Version        int                     `json:"version"`
	DatabaseFile   string                  `json:"databaseFile"`
	DatabaseSHA256 string                  `json:"databaseSha256"`
	Revisions      []stackConfigBackupItem `json:"revisions"`
}

type stackConfigBackupItem struct {
	StackID portainer.StackID `json:"stackId"`
	ID      string            `json:"id"`
	SHA256  string            `json:"sha256"`
	Size    int               `json:"size"`
}

// backupStackConfigurations runs while the offline gate protects the database
// snapshot from concurrent configuration changes through the HTTP API.
func backupStackConfigurations(backupDir string, datastore dataservices.DataStore) error {
	stacks, err := datastore.Stack().ReadAll()
	if err != nil {
		return fmt.Errorf("read stack metadata: %w", err)
	}

	manifest := stackConfigBackup{
		Version:      stackConfigFormat,
		DatabaseFile: datastore.Connection().GetDatabaseFileName(),
		Revisions:    []stackConfigBackupItem{},
	}
	seen := make(map[string]bool)
	for _, stack := range stacks {
		if stack.Configuration == nil {
			continue
		}
		for _, revision := range stack.Configuration.Revisions {
			item := stackConfigBackupItem{StackID: stack.ID, ID: revision.ID, SHA256: revision.SHA256, Size: revision.Size}
			name, err := item.path()
			if err != nil {
				return fmt.Errorf("stack %d revision %q: %w", stack.ID, revision.ID, err)
			}
			if seen[name] {
				return fmt.Errorf("duplicate stack configuration revision %s", name)
			}
			seen[name] = true
			manifest.Revisions = append(manifest.Revisions, item)
		}
	}

	manifest.DatabaseSHA256, err = fileSHA256(filepath.Join(backupDir, manifest.DatabaseFile))
	if err != nil {
		return fmt.Errorf("hash backup database: %w", err)
	}

	if len(manifest.Revisions) > 0 {
		rootPath, err := stackconfig.Root()
		if err != nil {
			return err
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return fmt.Errorf("open stack configuration root: %w", err)
		}
		defer root.Close()
		for _, item := range manifest.Revisions {
			content, err := readBackupRevision(root, item)
			if err != nil {
				return fmt.Errorf("read stack %d revision %s: %w", item.StackID, item.ID, err)
			}
			name, _ := item.path()
			destination := filepath.Join(backupDir, stackConfigFilesName, name)
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(destination, content, 0o600); err != nil {
				return err
			}
		}
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(backupDir, stackConfigManifestName), data, 0o600)
}

func (item stackConfigBackupItem) path() (string, error) {
	if item.StackID <= 0 {
		return "", errors.New("invalid stack ID")
	}
	if _, err := uuid.Parse(item.ID); err != nil || len(item.ID) != 36 {
		return "", errors.New("invalid revision ID")
	}
	if item.Size <= 0 || item.Size > stackconfig.MaxFileSize {
		return "", errors.New("invalid revision size")
	}
	hash, err := hex.DecodeString(item.SHA256)
	if err != nil || len(hash) != sha256.Size {
		return "", errors.New("invalid revision hash")
	}
	return filepath.Join(fmt.Sprint(item.StackID), item.ID), nil
}

// readBackupRevision does not follow symlinks in the managed revision path.
func readBackupRevision(root *os.Root, item stackConfigBackupItem) ([]byte, error) {
	name, err := item.path()
	if err != nil {
		return nil, err
	}
	dir, err := root.Lstat(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	if !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("stack configuration directory is not a regular directory")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("stack configuration revision is not a regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("stack configuration revision is not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(f, stackconfig.MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	if len(content) != item.Size || hex.EncodeToString(sum[:]) != item.SHA256 {
		return nil, errors.New("stack configuration revision integrity check failed")
	}
	return content, nil
}

func fileSHA256(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("database is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// restoreStackConfigurations verifies every staged file and every existing
// target before installing missing revisions. Legacy archives have no manifest.
func restoreStackConfigurations(restoreDir string) error {
	data, err := os.ReadFile(filepath.Join(restoreDir, stackConfigManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var manifest stackConfigBackup
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("invalid stack configuration backup manifest: %w", err)
	}
	if manifest.Version != stackConfigFormat {
		return fmt.Errorf("unsupported stack configuration backup version %d", manifest.Version)
	}
	if manifest.DatabaseFile != boltdb.DatabaseFileName && manifest.DatabaseFile != boltdb.EncryptedDatabaseFileName {
		return errors.New("invalid database filename in stack configuration backup")
	}
	databaseHash, err := fileSHA256(filepath.Join(restoreDir, manifest.DatabaseFile))
	if err != nil {
		return fmt.Errorf("read backup database: %w", err)
	}
	if databaseHash != manifest.DatabaseSHA256 {
		return errors.New("backup database does not match the stack configuration manifest")
	}
	if len(manifest.Revisions) == 0 {
		return nil
	}

	source, err := os.OpenRoot(filepath.Join(restoreDir, stackConfigFilesName))
	if err != nil {
		return fmt.Errorf("open archived stack configuration revisions: %w", err)
	}
	defer source.Close()
	seen := make(map[string]bool)
	for _, item := range manifest.Revisions {
		name, err := item.path()
		if err != nil {
			return fmt.Errorf("invalid stack configuration revision: %w", err)
		}
		if seen[name] {
			return fmt.Errorf("duplicate stack configuration revision %s", name)
		}
		seen[name] = true
		if _, err := readBackupRevision(source, item); err != nil {
			return fmt.Errorf("verify archived stack %d revision %s: %w", item.StackID, item.ID, err)
		}
	}

	rootPath, err := stackconfig.Root()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return fmt.Errorf("prepare stack configuration root: %w", err)
	}
	target, err := os.OpenRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open stack configuration root: %w", err)
	}
	defer target.Close()
	for _, item := range manifest.Revisions {
		name, _ := item.path()
		dir, err := target.Lstat(filepath.Dir(name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("stack configuration directory %s is not a regular directory", filepath.Dir(name))
		}
		if _, err := target.Lstat(name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := readBackupRevision(target, item); err != nil {
			return fmt.Errorf("existing stack %d revision %s conflicts with the backup: %w", item.StackID, item.ID, err)
		}
	}

	created := make([]string, 0)
	installed := false
	defer func() {
		if !installed {
			for _, name := range created {
				_ = target.Remove(name)
			}
		}
	}()
	for _, item := range manifest.Revisions {
		name, _ := item.path()
		if dir, err := target.Lstat(filepath.Dir(name)); err == nil {
			if !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("stack configuration directory %s is not a regular directory", filepath.Dir(name))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := target.Lstat(name); err == nil {
			if _, err := readBackupRevision(target, item); err != nil {
				return fmt.Errorf("existing stack %d revision %s conflicts with the backup: %w", item.StackID, item.ID, err)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		content, err := readBackupRevision(source, item)
		if err != nil {
			return fmt.Errorf("read archived stack %d revision %s: %w", item.StackID, item.ID, err)
		}
		if _, err := stackconfig.WriteRevision(item.StackID, item.ID, content); err != nil {
			return fmt.Errorf("restore stack %d revision %s: %w", item.StackID, item.ID, err)
		}
		created = append(created, name)
	}
	installed = true
	return nil
}
