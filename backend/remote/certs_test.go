package remote

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureSelfSignedCert_GeneratesAndCaches(t *testing.T) {
	dir := t.TempDir()

	first, err := EnsureSelfSignedCert(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, first.Leaf, "Leaf is unset until parsed; parse manually below")

	// Re-load and confirm cache hit (same serial).
	second, err := EnsureSelfSignedCert(dir, nil)
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
	cert, err := EnsureSelfSignedCert(dir, nil)
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

func TestEnsureSelfSignedCert_RegeneratesWhenLANIPRequested(t *testing.T) {
	dir := t.TempDir()

	first, err := EnsureSelfSignedCert(dir, nil)
	require.NoError(t, err)

	// Same dir, but now ask for a LAN IP we didn't have before. Cache must
	// be invalidated and a new cert minted that covers the requested IP.
	lanIP := net.ParseIP("192.168.1.42")
	second, err := EnsureSelfSignedCert(dir, []net.IP{lanIP})
	require.NoError(t, err)

	parsedFirst, err := x509.ParseCertificate(first.Certificate[0])
	require.NoError(t, err)
	parsedSecond, err := x509.ParseCertificate(second.Certificate[0])
	require.NoError(t, err)

	require.NotEqual(t, parsedFirst.SerialNumber.String(), parsedSecond.SerialNumber.String(),
		"requesting a new IP must regenerate the cert")
	ipStrings := make([]string, 0, len(parsedSecond.IPAddresses))
	for _, ip := range parsedSecond.IPAddresses {
		ipStrings = append(ipStrings, ip.String())
	}
	require.Contains(t, ipStrings, "192.168.1.42")
	require.Contains(t, ipStrings, "127.0.0.1") // loopback still covered
}

func TestEnsureSelfSignedCert_WritesPEMFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := EnsureSelfSignedCert(dir, nil)
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
