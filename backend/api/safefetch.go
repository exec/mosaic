package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// SSRF defense for outbound HTTP fetches that take user-controlled URLs
// (RSS feeds, IP blocklist URLs).
//
// Two-layer defense:
//
//  1. validateFetchURL — fail-fast at write time. Rejects non-http(s) schemes,
//     empty hosts, and the literal "localhost" hostname.
//
//  2. safeHTTPClient — defense in depth at fetch time. The custom DialContext
//     resolves the hostname AND dials inside the same call (so an attacker
//     cannot win a DNS-rebind race by validating against one address and
//     dialing another). Every resolved IP is checked; if any is loopback,
//     private (RFC1918/RFC4193), link-local, multicast, or unspecified, the
//     dial is refused. The dial then targets the resolved IP literal directly,
//     so re-resolution between checks and dial cannot redirect the connection.
//     Redirects are likewise re-validated through validateFetchURL.
//
// Error messages deliberately do NOT include the resolved IP — that would turn
// a refused dial into a blind exfiltration channel for internal-network probes.

var errBlockedAddress = errors.New("host resolves to a non-public address")

// validateFetchURL parses rawURL and rejects it if the scheme is not http(s),
// the host is empty, or the hostname is the literal "localhost" (any case).
// It does NOT resolve DNS — that happens in the dialer to defeat rebind.
func validateFetchURL(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, errors.New("URL is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("URL scheme must be http or https")
	}
	if u.Host == "" {
		return nil, errors.New("URL has no host")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return nil, errBlockedAddress
	}
	return u, nil
}

// isBlockedIP returns true if dialing this address would reach a non-public
// destination. It adopts an allow-global-only baseline: anything that is not a
// global-unicast address is blocked (this covers loopback, link-local,
// multicast, and unspecified). On top of that it explicitly blocks RFC1918/ULA
// private space, the RFC 6598 CGNAT range 100.64.0.0/10 for IPv4, and the
// IPv6 transition ranges 6to4 2002::/16 and Teredo 2001::/32 — Go's
// IsGlobalUnicast reports those last two as global, so they must be checked
// explicitly. Pulled out of the dialer so it can be unit-tested directly.
func isBlockedIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	// Unmap so an IPv4-in-IPv6 address (::ffff:127.0.0.1) is checked as IPv4.
	addr = addr.Unmap()

	// Allow-global-only baseline. Anything that isn't a routable global
	// unicast address (loopback, link-local, multicast, unspecified) is
	// refused.
	if !addr.IsGlobalUnicast() {
		return true
	}

	// Explicitly block RFC1918 (IPv4) / RFC4193 ULA (IPv6) private space.
	if addr.IsPrivate() {
		return true
	}

	// RFC 6598 carrier-grade NAT: 100.64.0.0/10. Only applies to IPv4.
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1]&0xc0 == 64 {
			return true
		}
	}

	// Non-global IPv6 transition ranges that Go still classifies as global
	// unicast: 6to4 (2002::/16) embeds an arbitrary IPv4 destination and
	// Teredo (2001:0::/32) tunnels to one, so either can be used to reach an
	// otherwise-blocked v4 target. Block them outright.
	if addr.Is6() {
		b := addr.As16()
		// 6to4: 2002::/16.
		if b[0] == 0x20 && b[1] == 0x02 {
			return true
		}
		// Teredo: 2001:0000::/32 (the first 32 bits are 2001:0000).
		if b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x00 && b[3] == 0x00 {
			return true
		}
	}

	return false
}

// safeHTTPClient returns an *http.Client whose Transport refuses to connect
// to non-public IPs (loopback / private / link-local / multicast /
// unspecified) and re-validates redirect targets.
func safeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	resolver := &net.Resolver{}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}

			// Resolve and dial inside the same call to defeat DNS rebinding.
			// An attacker who can control DNS for their hostname cannot get
			// us to validate one IP and then dial a different one.
			ips, err := resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errBlockedAddress
			}
			for _, ip := range ips {
				if isBlockedIP(ip) {
					// Generic message — never leak which IP was resolved,
					// to avoid blind SSRF / internal-network probing.
					return nil, errBlockedAddress
				}
			}

			// Dial the resolved IP literal so re-resolution between this
			// check and the actual TCP connect cannot redirect us.
			ip := ips[0].Unmap()
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, err := validateFetchURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}
