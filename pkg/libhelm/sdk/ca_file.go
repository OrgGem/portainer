package sdk

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/portainer/portainer/api/crypto"
	"github.com/portainer/portainer/pkg/libhttp/ssrf"
)

var (
	caFileMu   sync.RWMutex
	caFilePath string
)

// SetCAFile sets the path to the additional CA certificate file used for HTTPS
// connections to Helm repositories.
func SetCAFile(path string) {
	caFileMu.Lock()
	defer caFileMu.Unlock()
	caFilePath = path
}

// GetCAFile returns the configured additional CA certificate file path.
func GetCAFile() string {
	caFileMu.RLock()
	defer caFileMu.RUnlock()
	return caFilePath
}

// NewRepoTransport returns an SSRF-protected HTTP transport configured with the
// additional Helm CA certificate file (appended to the system cert pool) if set.
func NewRepoTransport() (*http.Transport, error) {
	caPath := GetCAFile()
	if caPath == "" {
		return ssrf.NewTransport(nil), nil
	}

	return newTransportWithAdditionalCA(caPath)
}

func newTransportWithAdditionalCA(caPath string) (*http.Transport, error) {
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Helm CA file %q: %w", caPath, err)
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if !rootCAs.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse certificates from Helm CA file %q", caPath)
	}

	tlsConfig := crypto.CreateTLSConfiguration(false)
	tlsConfig.RootCAs = rootCAs

	return ssrf.NewTransport(tlsConfig), nil
}
