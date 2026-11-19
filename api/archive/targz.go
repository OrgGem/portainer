package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/logs"
)

// TarGzDir creates a tar.gz archive inside the given directory.
func TarGzDir(absolutePath string) (string, error) {
	targzPath := filesystem.JoinPaths(absolutePath, filepath.Base(absolutePath)+".tar.gz")
	outFile, err := os.Create(targzPath)
	if err != nil {
		return "", err
	}
	zipWriter := gzip.NewWriter(outFile)
	tarWriter := tar.NewWriter(zipWriter)

	walkErr := filepath.Walk(absolutePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == targzPath {
			return nil // skip archive file
		}

		pathInArchive := filepath.Clean(strings.TrimPrefix(path, absolutePath))
		if pathInArchive == "" {
			return nil // skip root dir
		}

		return addToArchive(tarWriter, pathInArchive, path, info)
	})

	// Closing the writers completes the archive and must succeed for a backup
	// to be reported as valid.
	closeErr := errors.Join(tarWriter.Close(), zipWriter.Close(), outFile.Close())
	return targzPath, errors.Join(walkErr, closeErr)
}

func addToArchive(tarWriter *tar.Writer, pathInArchive string, path string, info os.FileInfo) (err error) {
	if info.IsDir() {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(stat, stat.Name())
	if err != nil {
		return err
	}
	header.Name = pathInArchive // use relative paths in archive

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return nil
	}

	_, err = io.Copy(tarWriter, file)
	return err
}

// ExtractTarGz reads a .tar.gz archive from the reader and extracts it into outputDirPath directory
func ExtractTarGz(r io.Reader, outputDirPath string) error {
	zipReader, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer logs.CloseAndLogErr(zipReader)

	tarReader := tar.NewReader(zipReader)

	for {
		header, err := tarReader.Next()

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// skip, dir will be created with a file
		case tar.TypeReg:
			p := filesystem.JoinPaths(outputDirPath, header.Name)
			if err := os.MkdirAll(filepath.Dir(p), 0o744); err != nil {
				return fmt.Errorf("Failed to extract dir %s", filepath.Dir(p))
			}
			outFile, err := os.Create(p)
			if err != nil {
				return fmt.Errorf("Failed to create file %s", header.Name)
			}
			_, copyErr := io.Copy(outFile, tarReader)
			closeErr := outFile.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("tar: unknown type: %v in %s",
				header.Typeflag,
				header.Name)
		}
	}

	return nil
}
