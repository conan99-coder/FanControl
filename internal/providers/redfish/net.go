package redfish

import (
	"crypto/tls"
	"net/http"
	"strings"
)

// insecureTransport returns an HTTP transport that accepts the BMC's
// self-signed certificate. Required because the Gigabyte BMC ships with a
// self-signed cert.
func insecureTransport() http.RoundTripper {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

// normalizeURL resolves a possibly absolute or root-relative action target into
// a path relative to the BMC base URL.
func normalizeURL(base, target string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		// Absolute URL: extract path + query.
		idx := strings.Index(target, "//")
		rest := target[idx+2:]
		slash := strings.Index(rest, "/")
		if slash >= 0 {
			return rest[slash:]
		}
		return "/"
	}
	if strings.HasPrefix(target, "/") {
		return target
	}
	return "/" + target
}
