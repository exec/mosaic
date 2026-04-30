package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestValidateFetchURL_Reject(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"empty string", ""},
		{"file scheme", "file:///etc/passwd"},
		{"ftp scheme", "ftp://example.com/x"},
		{"gopher scheme", "gopher://example.com/x"},
		{"javascript scheme", "javascript:alert(1)"},
		{"data scheme", "data:text/plain,hello"},
		{"http no host", "http://"},
		{"malformed triple-colon", ":::malformed"},
		{"localhost lowercase", "http://localhost"},
		{"localhost uppercase", "https://LOCALHOST/path"},
		{"localhost mixed case", "http://LocalHost:8080/"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := validateFetchURL(tc.raw); err == nil {
				t.Fatalf("validateFetchURL(%q) = nil err, want rejection", tc.raw)
			}
		})
	}
}

func TestValidateFetchURL_Accept(t *testing.T) {
	t.Parallel()

	cases := []string{
		"http://example.com",
		"https://example.com:8443/feeds/torrents.xml",
		"http://example.com/path?query=1",
		"https://sub.example.org:443/x",
	}

	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			u, err := validateFetchURL(raw)
			if err != nil {
				t.Fatalf("validateFetchURL(%q) = %v, want nil", raw, err)
			}
			if u == nil {
				t.Fatalf("validateFetchURL(%q) returned nil URL", raw)
			}
		})
	}
}

func TestValidateFetchURL_DoesNotLeakResolvedIP(t *testing.T) {
	t.Parallel()
	// localhost rejection must not include any IP information.
	_, err := validateFetchURL("http://localhost/x")
	if err == nil {
		t.Fatal("expected rejection")
	}
	msg := err.Error()
	if strings.Contains(msg, "127.") || strings.Contains(msg, "::1") {
		t.Errorf("error message leaks IP: %q", msg)
	}
}

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		blocked bool
		why     string
	}{
		{"127.0.0.1", true, "loopback v4"},
		{"127.255.255.254", true, "loopback v4 high"},
		{"10.0.0.1", true, "RFC1918 10/8"},
		{"10.255.255.255", true, "RFC1918 10/8 edge"},
		{"172.16.0.1", true, "RFC1918 172.16/12"},
		{"172.31.255.255", true, "RFC1918 172.16/12 edge"},
		{"192.168.1.1", true, "RFC1918 192.168/16"},
		{"169.254.169.254", true, "AWS metadata link-local"},
		{"169.254.0.1", true, "link-local v4"},
		{"::1", true, "loopback v6"},
		{"fe80::1", true, "link-local v6"},
		{"fc00::1", true, "ULA RFC4193"},
		{"fd00::1", true, "ULA RFC4193 alt"},
		{"0.0.0.0", true, "unspecified v4"},
		{"::", true, "unspecified v6"},
		{"224.0.0.1", true, "multicast v4"},
		{"239.255.255.255", true, "multicast v4 edge"},
		{"ff02::1", true, "multicast v6"},

		{"8.8.8.8", false, "Google DNS"},
		{"1.1.1.1", false, "Cloudflare DNS"},
		{"2001:4860:4860::8888", false, "Google DNS v6"},
		{"172.15.0.1", false, "just below RFC1918"},
		{"172.32.0.1", false, "just above RFC1918"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			addr, err := netip.ParseAddr(tc.raw)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			got := isBlockedIP(addr)
			if got != tc.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v (%s)", tc.raw, got, tc.blocked, tc.why)
			}
		})
	}
}

func TestIsBlockedIP_Invalid(t *testing.T) {
	t.Parallel()
	var zero netip.Addr // zero value is invalid
	if !isBlockedIP(zero) {
		t.Error("invalid addr should be treated as blocked")
	}
}

func TestSafeHTTPClient_RefusesLocalhostServer(t *testing.T) {
	t.Parallel()
	// httptest.NewServer binds to 127.0.0.1, which the safe dialer must refuse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := safeHTTPClient(5 * time.Second)
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected error fetching %s, got nil", srv.URL)
	}
	// Must not leak the resolved IP — the URL itself contains 127.0.0.1
	// already, but the *additional* error string should be the generic one.
	if !strings.Contains(err.Error(), "non-public address") {
		t.Errorf("error %q does not look like the generic blocked-address message", err.Error())
	}
}

func TestSafeHTTPClient_RefusesRedirectToLocalhost(t *testing.T) {
	t.Parallel()

	// Inner server: a victim service on localhost.
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("secret"))
	}))
	defer inner.Close()

	// Outer server: also on localhost (so we can't actually reach it from
	// the safe client either — but the test for redirect rejection works
	// at the CheckRedirect layer, which fires before the dialer for the
	// next hop). We instead simulate the redirect path with a plain client
	// that hands the safe client a public-looking URL... but we have no
	// public URLs in tests. So instead, test redirect validation directly
	// via the CheckRedirect on the safe client: we'll patch by using
	// the server's URL as the redirect target and a same-host outer
	// hop. Both ends are blocked, so we just verify the request errors
	// with a redirect-related or address-related error and never returns 200.
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL, http.StatusFound)
	}))
	defer redir.Close()

	client := safeHTTPClient(5 * time.Second)
	resp, err := client.Get(redir.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected error, got 200 — redirect to localhost was followed")
	}
	// The dial to redir itself should fail (loopback). That's fine: it
	// means the redirect was never followed, which is the property we want.
	// If for some reason the dialer let redir through, CheckRedirect would
	// catch the localhost hop. Either way, no successful fetch.
}

func TestSafeHTTPClient_CheckRedirectRejectsLocalhost(t *testing.T) {
	t.Parallel()
	// Directly exercise the CheckRedirect predicate, which must reject a
	// localhost hop independent of the dialer.
	client := safeHTTPClient(5 * time.Second)
	if client.CheckRedirect == nil {
		t.Fatal("safeHTTPClient must set CheckRedirect")
	}
	req, _ := http.NewRequest("GET", "http://localhost/admin", nil)
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Error("CheckRedirect should reject http://localhost")
	}
	req2, _ := http.NewRequest("GET", "file:///etc/passwd", nil)
	if err := client.CheckRedirect(req2, nil); err == nil {
		t.Error("CheckRedirect should reject file:// scheme")
	}
	req3, _ := http.NewRequest("GET", "https://example.com/feed", nil)
	if err := client.CheckRedirect(req3, nil); err != nil {
		t.Errorf("CheckRedirect should accept public https URL, got %v", err)
	}
}

func TestSafeHTTPClient_RejectsTooManyRedirects(t *testing.T) {
	t.Parallel()
	client := safeHTTPClient(5 * time.Second)
	// Build a slice of 5 prior hops; the 6th should be rejected.
	via := make([]*http.Request, 5)
	for i := range via {
		via[i], _ = http.NewRequest("GET", "https://example.com", nil)
	}
	req, _ := http.NewRequest("GET", "https://example.com/next", nil)
	if err := client.CheckRedirect(req, via); err == nil {
		t.Error("CheckRedirect should reject the 6th redirect")
	}
}
