//go:build linux

package archive

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_addToArchiveClosesSourceFilesOnSuccessAndWriteError(t *testing.T) {
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)

	baselineFDs, err := openFileDescriptorCount()
	require.NoError(t, err)

	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	for i := 0; i < 256; i++ {
		name := fmt.Sprintf("source-%03d", i)
		path := filepath.Join(t.TempDir(), name)
		require.NoError(t, os.WriteFile(path, []byte("payload"), 0600))
		info, err := os.Stat(path)
		require.NoError(t, err)

		require.NoError(t, addToArchive(tarWriter, name, path, info))
		currentFDs, err := openFileDescriptorCount()
		require.NoError(t, err)
		require.Equal(t, baselineFDs, currentFDs, "source descriptor must close before the next tar entry")
	}
	require.NoError(t, tarWriter.Close())

	path := filepath.Join(t.TempDir(), "write-error-source")
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	writeErr := errors.New("forced tar output failure")
	failedWriter := tar.NewWriter(&failAfterBytesWriter{remaining: 512, err: writeErr})
	err = addToArchive(failedWriter, "write-error-source", path, info)
	require.ErrorIs(t, err, writeErr)

	currentFDs, err := openFileDescriptorCount()
	require.NoError(t, err)
	require.Equal(t, baselineFDs, currentFDs, "source descriptor must close when writing the tar entry fails")
}

func openFileDescriptorCount() (int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

type failAfterBytesWriter struct {
	remaining int
	err       error
}

func (writer *failAfterBytesWriter) Write(p []byte) (int, error) {
	if writer.remaining <= 0 {
		return 0, writer.err
	}
	if len(p) > writer.remaining {
		written := writer.remaining
		writer.remaining = 0
		return written, writer.err
	}
	writer.remaining -= len(p)
	return len(p), nil
}
