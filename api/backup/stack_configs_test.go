package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/archive"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	"github.com/stretchr/testify/require"
)

func createStackConfigurationArchive(t *testing.T, revisions map[string]string) string {
	t.Helper()

	stackRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", stackRoot)
	_, store := datastore.MustNewTestStore(t, true, false)

	configuration := &portainer.StackConfiguration{Revisions: make([]portainer.StackConfigRevision, 0, len(revisions))}
	for id, content := range revisions {
		sum := sha256.Sum256([]byte(content))
		_, err := stackconfig.WriteRevision(1, id, []byte(content))
		require.NoError(t, err)
		configuration.Revisions = append(configuration.Revisions, portainer.StackConfigRevision{
			ID:     id,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   len(content),
		})
	}
	require.NoError(t, store.Stack().Create(&portainer.Stack{
		ID:            1,
		Name:          "test-stack",
		Configuration: configuration,
	}))

	archivePath, err := CreateBackupArchive("", offlinegate.NewOfflineGate(), store, store.Connection().GetStorePath())
	require.NoError(t, err)
	return archivePath
}

func extractStackConfigurationArchive(t *testing.T, archivePath string) string {
	t.Helper()

	archiveFile, err := os.Open(archivePath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archiveFile.Close()) })

	extracted := t.TempDir()
	require.NoError(t, archive.ExtractTarGz(archiveFile, extracted))
	return extracted
}

func TestStackConfigurationBackup_RestoresVerifiedRevisions(t *testing.T) {
	id := "9e8b3a1a-9513-4b85-8e88-77a9f38e657d"
	archivePath := createStackConfigurationArchive(t, map[string]string{id: "server { listen 8080; }"})
	extracted := extractStackConfigurationArchive(t, archivePath)

	manifestBytes, err := os.ReadFile(filepath.Join(extracted, stackConfigManifestName))
	require.NoError(t, err)
	var manifest stackConfigBackup
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	require.Equal(t, stackConfigFormat, manifest.Version)
	require.Len(t, manifest.Revisions, 1)
	require.Equal(t, int64(1), int64(manifest.Revisions[0].StackID))
	require.Equal(t, id, manifest.Revisions[0].ID)

	archivedContent, err := os.ReadFile(filepath.Join(extracted, stackConfigFilesName, "1", id))
	require.NoError(t, err)
	require.Equal(t, "server { listen 8080; }", string(archivedContent))

	restoredRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", restoredRoot)
	require.NoError(t, restoreStackConfigurations(extracted))
	require.NoError(t, restoreStackConfigurations(extracted), "restoring matching immutable revisions should be idempotent")

	restored, err := stackconfig.ReadRevision(1, portainer.StackConfigRevision{
		ID:     id,
		SHA256: manifest.Revisions[0].SHA256,
		Size:   manifest.Revisions[0].Size,
	})
	require.NoError(t, err)
	require.Equal(t, archivedContent, restored)
}

func TestRestoreArchive_RestoresStackRevisionAndDatabaseMetadata(t *testing.T) {
	id := "9e8b3a1a-9513-4b85-8e88-77a9f38e657d"
	archivePath := createStackConfigurationArchive(t, map[string]string{id: "restored stack config"})
	archiveBytes, err := os.ReadFile(archivePath)
	require.NoError(t, err)

	_, targetStore := datastore.MustNewTestStore(t, true, false)
	targetRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", targetRoot)
	shutdown := false
	err = RestoreArchive(
		bytes.NewReader(archiveBytes),
		"",
		targetStore.Connection().GetStorePath(),
		offlinegate.NewOfflineGate(),
		targetStore,
		func() { shutdown = true },
	)
	require.NoError(t, err)
	require.True(t, shutdown)

	_, err = targetStore.Open()
	require.NoError(t, err)
	stack, err := targetStore.Stack().Read(1)
	require.NoError(t, err)
	require.NotNil(t, stack.Configuration)
	require.Len(t, stack.Configuration.Revisions, 1)
	require.Equal(t, id, stack.Configuration.Revisions[0].ID)

	content, err := stackconfig.ReadRevision(1, stack.Configuration.Revisions[0])
	require.NoError(t, err)
	require.Equal(t, "restored stack config", string(content))
}

func TestStackConfigurationBackup_RejectsCorruptArchiveBeforeWritingAnyRevision(t *testing.T) {
	firstID := "9e8b3a1a-9513-4b85-8e88-77a9f38e657d"
	secondID := "e2e4bb9d-f912-43cc-aab9-2e4bc2b88572"
	archivePath := createStackConfigurationArchive(t, map[string]string{
		firstID:  "first revision",
		secondID: "second revision",
	})
	extracted := extractStackConfigurationArchive(t, archivePath)

	corruptPath := filepath.Join(extracted, stackConfigFilesName, "1", secondID)
	require.NoError(t, os.Chmod(corruptPath, 0o600))
	require.NoError(t, os.WriteFile(corruptPath, []byte("modified revision"), 0o600))

	restoredRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", restoredRoot)
	err := restoreStackConfigurations(extracted)
	require.ErrorContains(t, err, "integrity check failed")
	_, err = os.Stat(filepath.Join(restoredRoot, "1", firstID))
	require.ErrorIs(t, err, os.ErrNotExist, "preflight must fail before installing other revisions")
}

func TestStackConfigurationBackup_RejectsConflictingDestinationRevision(t *testing.T) {
	id := "9e8b3a1a-9513-4b85-8e88-77a9f38e657d"
	archivePath := createStackConfigurationArchive(t, map[string]string{id: "backup content"})
	extracted := extractStackConfigurationArchive(t, archivePath)

	restoredRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", restoredRoot)
	conflictPath := filepath.Join(restoredRoot, "1", id)
	require.NoError(t, os.MkdirAll(filepath.Dir(conflictPath), 0o700))
	require.NoError(t, os.WriteFile(conflictPath, []byte("different content"), 0o600))

	err := restoreStackConfigurations(extracted)
	require.ErrorContains(t, err, "conflicts with the backup")
	conflict, err := os.ReadFile(conflictPath)
	require.NoError(t, err)
	require.Equal(t, "different content", string(conflict))
}

func TestStackConfigurationBackup_FailsWhenSourceRevisionDoesNotMatchMetadata(t *testing.T) {
	id := "9e8b3a1a-9513-4b85-8e88-77a9f38e657d"
	stackRoot := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", stackRoot)
	_, store := datastore.MustNewTestStore(t, true, false)

	content := []byte("original revision")
	sum := sha256.Sum256(content)
	_, err := stackconfig.WriteRevision(1, id, content)
	require.NoError(t, err)
	require.NoError(t, store.Stack().Create(&portainer.Stack{
		ID:   1,
		Name: "test-stack",
		Configuration: &portainer.StackConfiguration{Revisions: []portainer.StackConfigRevision{{
			ID: id, SHA256: hex.EncodeToString(sum[:]), Size: len(content),
		}}},
	}))
	storedRevision := filepath.Join(stackRoot, "1", id)
	require.NoError(t, os.Chmod(storedRevision, 0o600))
	require.NoError(t, os.WriteFile(storedRevision, []byte("changed revision"), 0o600))

	_, err = CreateBackupArchive("", offlinegate.NewOfflineGate(), store, store.Connection().GetStorePath())
	require.ErrorContains(t, err, "integrity check failed")
}

func TestStackConfigurationBackup_LegacyArchiveWithoutManifestRemainsSupported(t *testing.T) {
	legacy := t.TempDir()
	require.NoError(t, restoreStackConfigurations(legacy))
}
