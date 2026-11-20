package sdk

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
)

func generateTestCAPEM(t *testing.T) []byte {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Helm CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestCAFileTransport(t *testing.T) {
	t.Cleanup(func() { SetCAFile("") })

	SetCAFile("")
	transport, err := NewRepoTransport()
	require.NoError(t, err)
	require.NotNil(t, transport)

	caPath := filepath.Join(t.TempDir(), "ca-cert.pem")
	require.NoError(t, os.WriteFile(caPath, generateTestCAPEM(t), 0o600))

	SetCAFile(caPath)
	require.Equal(t, caPath, GetCAFile())

	transport, err = NewRepoTransport()
	require.NoError(t, err)
	require.NotNil(t, transport)
	require.NotNil(t, transport.TLSClientConfig)
	require.NotNil(t, transport.TLSClientConfig.RootCAs)

	chartTr, err := chartTransport(&action.ChartPathOptions{})
	require.NoError(t, err)
	require.NotNil(t, chartTr.TLSClientConfig)
	require.NotNil(t, chartTr.TLSClientConfig.RootCAs)
}

func TestCAFileTransportInvalidPEM(t *testing.T) {
	t.Cleanup(func() { SetCAFile("") })

	badPath := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(badPath, []byte("not a cert"), 0o600))

	SetCAFile(badPath)
	_, err := NewRepoTransport()
	require.Error(t, err)
}
