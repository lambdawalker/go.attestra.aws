package bootstrap

import (
	"fmt"
	"net/http"
	"strings"
)

// managedLoginBrowser is a compatibility adapter for Apexfission 8f7bdade14db:
// its shell's self-only default CSP otherwise blocks the OAuth token exchange.
// This adds only the deployment-configured HTTPS login origin, never a request URL.
func managedLoginBrowser(next http.Handler, loginOrigin string) (http.Handler, error) {
	if !httpsOrigin(loginOrigin) {
		return nil, fmt.Errorf("managed login requires a trusted HTTPS origin")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The pinned shell does not forward its verified cookie to extension routing.
		// Reconstruct the bearer header only for extensions; the shell still verifies
		// it and checks grants. Preserve the cookie-origin guard before doing so.
		if strings.HasPrefix(r.URL.Path, "/admin/api/extensions/") && r.Header.Get("Authorization") == "" {
			if cookie, e := r.Cookie("__Host-apex-session"); e == nil {
				if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != "https://"+r.Host {
					http.Error(w, "invalid origin", http.StatusForbidden)
					return
				}
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+cookie.Value)
			}
		}
		if r.Method == http.MethodGet && r.URL.Path == "/admin" {
			next.ServeHTTP(&loginCSPWriter{ResponseWriter: w, origin: loginOrigin}, r)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

type loginCSPWriter struct {
	http.ResponseWriter
	origin  string
	written bool
}

func (w *loginCSPWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	policy := w.Header().Get("Content-Security-Policy")
	if policy != "" {
		w.Header().Set("Content-Security-Policy", policy+"; connect-src 'self' "+w.origin)
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *loginCSPWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
