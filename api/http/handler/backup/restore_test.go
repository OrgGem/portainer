package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/adminmonitor"
	backupcrypto "github.com/portainer/portainer/api/crypto"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/http/security/setuptoken"
	"github.com/portainer/portainer/api/internal/testhelpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_restoreArchive_usingCombinationOfPasswords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		backupPassword  string
		restorePassword string
		fails           bool
	}{
		{
			name:            "empty password to both encrypt and decrypt",
			backupPassword:  "",
			restorePassword: "",
			fails:           false,
		},
		{
			name:            "same password to encrypt and decrypt",
			backupPassword:  "secret",
			restorePassword: "secret",
			fails:           false,
		},
		{
			name:            "different passwords to encrypt and decrypt",
			backupPassword:  "secret",
			restorePassword: "terces",
			fails:           true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			datastore := testhelpers.NewDatastore(
				testhelpers.WithUsers([]portainer.User{}),
				testhelpers.WithEdgeJobs([]portainer.EdgeJob{}),
				testhelpers.WithStacks(nil),
			)
			adminMonitor := adminmonitor.New(time.Hour, datastore)

			h := NewHandler(
				testhelpers.NewTestRequestBouncer(),
				datastore,
				offlinegate.NewOfflineGate(),
				prepareFilestorePath(t),
				func() {},
				adminMonitor,
			)

			// backup
			archive := backup(t, h, test.backupPassword)

			// restore
			w := httptest.NewRecorder()
			r := prepareMultipartRequest(t, test.restorePassword, archive)

			restoreErr := h.restore(w, r)
			assert.Equal(t, test.fails, restoreErr != nil, "Didn't meet expectation of failing restore handler")
		})
	}
}

func Test_restoreArchive_shouldFailIfSystemWasAlreadyInitialized(t *testing.T) {
	t.Parallel()
	admin := portainer.User{
		Role: portainer.AdministratorRole,
	}
	datastore := testhelpers.NewDatastore(
		testhelpers.WithUsers([]portainer.User{admin}),
		testhelpers.WithEdgeJobs([]portainer.EdgeJob{}),
		testhelpers.WithStacks(nil),
	)
	adminMonitor := adminmonitor.New(time.Hour, datastore)

	h := NewHandler(testhelpers.NewTestRequestBouncer(),
		datastore,
		offlinegate.NewOfflineGate(),
		prepareFilestorePath(t),
		func() {},
		adminMonitor,
	)

	// backup
	archive := backup(t, h, "password")

	// restore
	w := httptest.NewRecorder()
	r := prepareMultipartRequest(t, "password", archive)

	restoreErr := h.restore(w, r)
	assert.NotNil(t, restoreErr, "Should fail, because system it already initialized")
	assert.Equal(t, "Cannot restore already initialized instance", restoreErr.Message, "Should fail with certain error")
}

func backup(t *testing.T, h *Handler, password string) []byte {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"password":"%s"}`, password)))
	w := httptest.NewRecorder()

	backupErr := h.backup(w, r)
	assert.Nil(t, backupErr, "Backup should not fail")

	response := w.Result()
	archive, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	err = response.Body.Close()
	require.NoError(t, err)

	return archive
}

func Test_restore_setupTokenGate(t *testing.T) {
	t.Parallel()
	datastore := testhelpers.NewDatastore(
		testhelpers.WithUsers([]portainer.User{}),
		testhelpers.WithEdgeJobs([]portainer.EdgeJob{}),
		testhelpers.WithStacks(nil),
	)
	adminMonitor := adminmonitor.New(time.Hour, datastore)
	h := NewHandler(
		testhelpers.NewTestRequestBouncer(),
		datastore,
		offlinegate.NewOfflineGate(),
		prepareFilestorePath(t),
		func() {},
		adminMonitor,
	)
	h.SetupToken = "secret-token"

	t.Run("403 without token header", func(t *testing.T) {
		err := h.restore(httptest.NewRecorder(), prepareMultipartRequest(t, "", []byte("x")))
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, err.StatusCode)
	})

	t.Run("403 with wrong token", func(t *testing.T) {
		r := prepareMultipartRequest(t, "", []byte("x"))
		r.Header.Set(setuptoken.HeaderName, "wrong")
		err := h.restore(httptest.NewRecorder(), r)
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, err.StatusCode)
	})

	t.Run("passes gate with correct token", func(t *testing.T) {
		archive := backup(t, h, "")
		r := prepareMultipartRequest(t, "", archive)
		r.Header.Set(setuptoken.HeaderName, "secret-token")
		require.Nil(t, h.restore(httptest.NewRecorder(), r))
	})
}

func prepareMultipartRequest(t *testing.T, password string, file []byte) *http.Request {
	var body bytes.Buffer

	w := multipart.NewWriter(&body)
	err := w.WriteField("password", password)
	require.NoError(t, err)

	fw, err := w.CreateFormFile("file", "filename")
	require.NoError(t, err)

	_, err = io.Copy(fw, bytes.NewReader(file))
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodPost, "http://localhost/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())

	err = w.Close()
	require.NoError(t, err)

	return r
}

func Test_restoreArchive_streamsLargeMultipartAndCleansTemporaryFiles(t *testing.T) {
	archivePath := createLargeRestoreArchive(t)
	corruptArchivePath := createCorruptEncryptedRestoreArchive(t, archivePath, "backup-password")
	tests := []struct {
		name      string
		archive   string
		fileFirst bool
		password  string
		wantError bool
	}{
		{name: "success with file part first", fileFirst: true},
		{name: "success with password part first"},
		{name: "restore error with file part first", fileFirst: true, password: "wrong-password", wantError: true},
		{name: "restore error with password part first", password: "wrong-password", wantError: true},
		{name: "corrupt later GCM block", archive: corruptArchivePath, fileFirst: true, password: "backup-password", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// ParseMultipartForm(0) must spool the archive to this directory.
			uploadTempDir := filepath.Join(t.TempDir(), "upload-temp")
			require.NoError(t, os.Mkdir(uploadTempDir, 0700))
			t.Setenv("TMPDIR", uploadTempDir)
			t.Setenv("TMP", uploadTempDir)
			t.Setenv("TEMP", uploadTempDir)

			datastore := testhelpers.NewDatastore(
				testhelpers.WithUsers([]portainer.User{}),
				testhelpers.WithEdgeJobs([]portainer.EdgeJob{}),
				testhelpers.WithStacks(nil),
			)
			shutdownCalled := false
			filestorePath := prepareFilestorePath(t)
			h := NewHandler(
				testhelpers.NewTestRequestBouncer(),
				datastore,
				offlinegate.NewOfflineGate(),
				filestorePath,
				func() { shutdownCalled = true },
				adminmonitor.New(time.Hour, datastore),
			)

			testArchive := test.archive
			if testArchive == "" {
				testArchive = archivePath
			}
			r, writerDone := prepareStreamingMultipartRequest(t, testArchive, test.password, test.fileFirst)
			require.NoError(t, r.ParseMultipartForm(0))
			require.NotNil(t, r.MultipartForm)
			require.Len(t, r.MultipartForm.File["file"], 1)
			spooledFile, err := r.MultipartForm.File["file"][0].Open()
			require.NoError(t, err)
			diskFile, diskBacked := spooledFile.(*os.File)
			require.True(t, diskBacked, "large uploads must be parsed into a disk-backed temporary file")
			tempFilePath := diskFile.Name()
			assert.Equal(t, filepath.Clean(uploadTempDir), filepath.Dir(tempFilePath))
			require.NoError(t, spooledFile.Close())

			restoreErr := h.restore(httptest.NewRecorder(), r)
			require.NoError(t, <-writerDone, "multipart upload should complete")

			if test.wantError {
				require.Error(t, restoreErr)
				assert.False(t, shutdownCalled)
				assert.NoFileExists(t, filepath.Join(filestorePath, "portainer.db"), "a failed restore must not install database files")
			} else {
				require.Nil(t, restoreErr)
				assert.True(t, shutdownCalled)
				assert.FileExists(t, filepath.Join(filestorePath, "portainer.db"))
			}

			uploadTempFiles, err := os.ReadDir(uploadTempDir)
			require.NoError(t, err)
			assert.Empty(t, uploadTempFiles, "multipart temporary files must be removed after success and failure")
			assert.NoFileExists(t, tempFilePath, "the disk-backed upload should be removed after restore")
			restoreTempFiles, err := os.ReadDir(filepath.Join(filestorePath, "restore"))
			require.NoError(t, err)
			assert.Empty(t, restoreTempFiles, "private restore workspaces must be removed after success and failure")
		})
	}
}

func createCorruptEncryptedRestoreArchive(t *testing.T, archivePath, password string) string {
	t.Helper()
	encryptedPath := filepath.Join(t.TempDir(), "corrupt-backup.enc")
	input, err := os.Open(archivePath)
	require.NoError(t, err)
	defer input.Close()
	output, err := os.Create(encryptedPath)
	require.NoError(t, err)
	require.NoError(t, backupcrypto.AesEncrypt(input, output, []byte(password)))
	require.NoError(t, output.Close())

	const firstCipherBlockOffset = int64(len("AES256-GCM") + 16 + 12 + (1024*1024 + 16))
	file, err := os.OpenFile(encryptedPath, os.O_RDWR, 0)
	require.NoError(t, err)
	var value [1]byte
	_, err = file.ReadAt(value[:], firstCipherBlockOffset)
	require.NoError(t, err)
	value[0] ^= 0x80
	_, err = file.WriteAt(value[:], firstCipherBlockOffset)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return encryptedPath
}

func createLargeRestoreArchive(t *testing.T) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "large-backup.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)

	gzipWriter := gzip.NewWriter(archiveFile)
	tarWriter := tar.NewWriter(gzipWriter)
	writeFile := func(name string, size int64) {
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0600,
			Size:     size,
			Typeflag: tar.TypeReg,
		}))
	}

	writeFile("portainer.db", 2)
	_, err = tarWriter.Write([]byte("db"))
	require.NoError(t, err)

	const largeMemberSize = 12 << 20
	writeFile("large.bin", largeMemberSize)
	_, err = io.CopyN(tarWriter, cryptorand.Reader, largeMemberSize)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, archiveFile.Close())
	return archivePath
}

func prepareStreamingMultipartRequest(t *testing.T, archivePath, password string, fileFirst bool) (*http.Request, <-chan error) {
	t.Helper()
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/", reader)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	done := make(chan error, 1)

	go func() {
		writePassword := func() error {
			return multipartWriter.WriteField("password", password)
		}
		writeFile := func() error {
			archiveFile, err := os.Open(archivePath)
			if err != nil {
				return err
			}
			defer archiveFile.Close()

			part, err := multipartWriter.CreateFormFile("file", "backup.tar.gz")
			if err != nil {
				return err
			}
			_, err = io.Copy(part, archiveFile)
			return err
		}

		var err error
		if fileFirst {
			err = writeFile()
			if err == nil {
				err = writePassword()
			}
		} else {
			err = writePassword()
			if err == nil {
				err = writeFile()
			}
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			_ = writer.Close()
		}
		done <- err
	}()

	return request, done
}
