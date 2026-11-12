// Package hostguard keeps Mentat answering only to the names it is meant to have. The
// backend binds loopback; a browser pointed at it from another site (DNS rebinding) arrives
// with a Host the app does not serve, and is refused before any handler sees it.
//
// Served Hosts are loopback names and addresses, plus an operator list (mentat.allowed_hosts)
// for a local TLS proxy that forwards a public name. A request for a listed name is believed
// only when it comes from a loopback peer carrying exactly one X-Forwarded-Proto: https. Every
// other forwarding header is dropped, because none of them may choose the host, origin or
// caller identity, and X-Forwarded-Proto is passed on (for the identity module's trusted_proxy)
// only for such a validated request.
package hostguard

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

// Handler refuses requests for hosts that are not served and strips forwarding headers.
// allowed lists extra host names exactly as a client sends them in Host (lower case, with a
// port only when the client sends one): "mentat.example.test" does not admit
// "mentat.example.test:8443".
func Handler(allowed []string, next http.Handler) http.Handler {
	names := make([]string, len(allowed))
	for i, a := range allowed {
		names[i] = strings.ToLower(a)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied := false
		switch {
		case isLoopbackHost(r.Host):
		case slices.Contains(names, strings.ToLower(r.Host)):
			values := r.Header.Values("X-Forwarded-Proto")
			if !loopbackPeer(r.RemoteAddr) || len(values) != 1 || values[0] != "https" {
				http.Error(w, "Untrusted HTTPS proxy", http.StatusForbidden)
				return
			}
			proxied = true
		default:
			http.Error(w, "Invalid host", http.StatusForbidden)
			return
		}
		r = r.Clone(r.Context())
		for _, name := range []string{"Forwarded", "X-Forwarded-Host", "X-Forwarded-For", "X-Real-IP"} {
			r.Header.Del(name)
		}
		// X-Forwarded-Proto is the one hint the identity module may use (trusted_proxy). It
		// survives only when it was just validated, so a loopback request cannot claim https.
		if !proxied {
			r.Header.Del("X-Forwarded-Proto")
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func loopbackPeer(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackHost(host string) bool {
	name := host
	if strings.Contains(name, ":") {
		var err error
		name, _, err = net.SplitHostPort(name)
		if err != nil {
			// A bare IPv6 literal without brackets or port, such as ::1.
			name = strings.Trim(host, "[]")
		}
	}
	name = strings.Trim(name, "[]")
	ip := net.ParseIP(name)
	return name == "localhost" || (ip != nil && ip.IsLoopback())
}
