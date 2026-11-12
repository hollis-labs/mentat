package hostguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const proxied = "mentat.example.test"

// The boundary table, ported from the pre-Substrate requestorigin package: which hosts are
// served, when a proxied name is believed, and what a downstream handler may still see.
func TestProxyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, peer, proto string
		listed                  bool
		accepted                bool
		keepsProto              bool
	}{
		{"local", "localhost:8080", "127.0.0.1:1234", "", true, true, false},
		{"loopback ip", "127.0.0.1:8080", "127.0.0.1:1234", "", false, true, false},
		{"ipv6", "[::1]:8080", "[::1]:1234", "", false, true, false},
		{"bare ipv6", "::1", "[::1]:1234", "", false, true, false},
		{"spoof local scheme", "localhost:8080", "127.0.0.1:1234", "https", true, true, false},
		{"proxy", proxied, "127.0.0.1:1234", "https", true, true, true},
		{"proxy ipv6", proxied, "[::1]:1234", "https", true, true, true},
		{"proxy mixed case", "Mentat.Example.Test", "127.0.0.1:1234", "https", true, true, true},
		{"not listed", proxied, "127.0.0.1:1234", "https", false, false, false},
		{"missing proto", proxied, "127.0.0.1:1234", "", true, false, false},
		{"plaintext", proxied, "127.0.0.1:1234", "http", true, false, false},
		{"ambiguous proto", proxied, "127.0.0.1:1234", "https,http", true, false, false},
		{"remote", proxied, "203.0.113.9:1234", "https", true, false, false},
		{"invalid peer", proxied, "localhost:1234", "https", true, false, false},
		{"alias", "mentat.alias.example.test", "127.0.0.1:1234", "https", true, false, false},
		{"foreign", "evil.example", "127.0.0.1:1234", "https", true, false, false},
		{"rebinding lookalike", "localhost.evil.example", "127.0.0.1:1234", "", true, false, false},
		{"private address", "192.0.2.5:8085", "127.0.0.1:1234", "", true, false, false},
		{"alternate port", proxied + ":443", "127.0.0.1:1234", "https", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/account", nil)
			r.Host = tc.host
			r.RemoteAddr = tc.peer
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			r.Header.Set("X-Forwarded-Host", proxied)
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
			r.Header.Set("X-Real-IP", "203.0.113.9")
			r.Header.Set("Forwarded", "host="+proxied+";proto=https")
			var allowed []string
			if tc.listed {
				allowed = []string{proxied}
			}
			called := false
			h := Handler(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				for _, name := range []string{"Forwarded", "X-Forwarded-Host", "X-Forwarded-For", "X-Real-Ip"} {
					if r.Header.Get(name) != "" {
						t.Fatalf("%s leaked downstream", name)
					}
				}
				if got := r.Header.Get("X-Forwarded-Proto") != ""; got != tc.keepsProto {
					t.Fatalf("X-Forwarded-Proto kept = %v, want %v", got, tc.keepsProto)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if called != tc.accepted || (!tc.accepted && w.Code != http.StatusForbidden) {
				t.Fatalf("accepted=%v status=%d", called, w.Code)
			}
			if tc.accepted && (w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Referrer-Policy") != "no-referrer") {
				t.Fatal("response hardening headers missing")
			}
		})
	}
}

func TestHostsAreCaseInsensitiveAndRefusalNeverReachesTheHandler(t *testing.T) {
	h := Handler(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler reached") }))
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "Evil.Test"
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal(w.Code)
	}
}
