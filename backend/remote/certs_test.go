package remote

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureSelfSignedCert_GeneratesAndCaches(t *testing.T) {
	dir := t.TempDir()

	first, err := EnsureSelfSignedCert(dir)
	require.NoError(t, err)
	require.NotNil(t, first.Leaf, "Leaf is unset until parsed; parse manually below")

	// Re-load and confirm cache hit (same serial).
	second, err := EnsureSelfSignedCert(dir)
	require.NoError(t, err)

	parsedFirst, err := x509.ParseCertificate(first.Certificate[0])
	require.NoError(t, err)
	parsedSecond, err := x509.ParseCertificate(second.Certificate[0])
	require.NoError(t, err)

	require.Equal(t, parsedFirst.SerialNumber.String(), parsedSecond.SerialNumber.String(),
		"second call should reuse existing cert (same serial)")
}

func TestEnsureSelfSignedCert_CoversLocalhost(t *testing.T) {
	dir := t.TempDir()
	cert, err := EnsureSelfSignedCert(dir)
	require.NoError(t, err)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)

	require.Contains(t, leaf.DNSNames, "localhost")

	ipStrings := make([]string, 0, len(leaf.IPAddresses))
	for _, ip := range leaf.IPAddresses {
		ipStrings = append(ipStrings, ip.String())
	}
	require.Contains(t, ipStrings, "127.0.0.1")
	require.Contains(t, ipStrings, "::1")
}

func TestEnsureSelfSignedCert_WritesPEMFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := EnsureSelfSignedCert(dir)
	require.NoError(t, err)

	certBytes, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
	require.NoError(t, err)
	block, _ := pem.Decode(certBytes)
	require.NotNil(t, block)
	require.Equal(t, "CERTIFICATE", block.Type)

	keyBytes, err := os.ReadFile(filepath.Join(dir, "key.pem"))
	require.NoError(t, err)
	keyBlock, _ := pem.Decode(keyBytes)
	require.NotNil(t, keyBlock)
	require.Equal(t, "EC PRIVATE KEY", keyBlock.Type)
}
