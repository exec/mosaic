// Package remote implements the optional HTTPS+WebSocket interface that lets a
// browser run the same SolidJS UI as the embedded Wails shell. This file owns
// the on-disk self-signed certificate used when the user binds beyond loopback.
package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureSelfSignedCert returns a tls.Certificate, generating a fresh ECDSA P-256
// self-signed certificate (10-year validity) under dir/cert.pem + key.pem if not
// already present OR the cached cert's SAN list doesn't cover the requested
// extraIPs. The cert always covers localhost + 127.0.0.1 + ::1; extraIPs is
// the set of bound LAN-interface addresses appended on top so a browser
// hitting the box at its local-network address doesn't trip a TLS-name-
// mismatch warning.
//
// The cache is invalidated (and the cert regenerated) when extraIPs adds an
// address the cached cert doesn't already cover — typically because the
// user's box got a new DHCP lease, plugged in a different VPN, or moved
// networks. Removing an old IP doesn't trigger regen; over-permissive SANs
// for unreachable interfaces are harmless.
func EnsureSelfSignedCert(dir string, extraIPs []net.IP) (tls.Certificate, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	want := append([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, extraIPs...)

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
				if certCoversIPs(cert, want) {
					return cert, nil
				}
			}
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Mosaic local"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           dedupIPs(want),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// certCoversIPs reports whether every IP in want is present in the loaded
// certificate's IPAddresses SAN list. Used by EnsureSelfSignedCert to decide
// when a regeneration is needed.
func certCoversIPs(cert tls.Certificate, want []net.IP) bool {
	if len(cert.Certificate) == 0 {
		return false
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false
	}
	have := make(map[string]struct{}, len(parsed.IPAddresses))
	for _, ip := range parsed.IPAddresses {
		have[ip.String()] = struct{}{}
	}
	for _, ip := range want {
		if _, ok := have[ip.String()]; !ok {
			return false
		}
	}
	return true
}

// dedupIPs returns a copy of in with stable ordering and duplicates removed.
// Interface enumeration can return the same address twice (e.g. an IPv4 on
// both eth0 and a Docker bridge) — duplicate SAN entries are legal but ugly
// and a few Linux openssl tooling versions warn on them.
func dedupIPs(in []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(in))
	out := make([]net.IP, 0, len(in))
	for _, ip := range in {
		key := ip.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ip)
	}
	return out
}

// LocalInterfaceIPs returns every non-loopback unicast IP on the host, used
// to populate the cert SAN list when binding 0.0.0.0. We can't know which
// interface the user's browser will reach us from, so we cover all of them.
// Returns an empty slice on enumeration failure — caller falls back to the
// loopback-only SAN set, the same behavior as before this function existed.
func LocalInterfaceIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		out = append(out, ipnet.IP)
	}
	return out
}
