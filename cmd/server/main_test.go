package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// sentinel handler that always returns 200 OK — used to focus assertions on
// response headers set by securityHeaders rather than any business logic.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestSecurityHeaders_AlwaysPresent(t *testing.T) {
	for _, secureCookies := range []bool{false, true} {
		secureCookies := secureCookies
		t.Run("secureCookies="+boolStr(secureCookies), func(t *testing.T) {
			h := securityHeaders(secureCookies)(okHandler)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			h.ServeHTTP(rw, req)

			assertHeader(t, rw, "X-Frame-Options", "DENY")
			assertHeader(t, rw, "X-Content-Type-Options", "nosniff")
			assertHeader(t, rw, "Referrer-Policy", "strict-origin-when-cross-origin")
			assertHeader(t, rw, "Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		})
	}
}

func TestSecurityHeaders_HSTSOnlyWhenSecureCookies(t *testing.T) {
	t.Run("secureCookies=true sets HSTS", func(t *testing.T) {
		h := securityHeaders(true)(okHandler)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

		got := rw.Header().Get("Strict-Transport-Security")
		if got == "" {
			t.Fatal("expected Strict-Transport-Security to be set when secureCookies=true, got empty string")
		}
		const want = "max-age=31536000; includeSubDomains"
		if got != want {
			t.Errorf("Strict-Transport-Security = %q, want %q", got, want)
		}
	})

	t.Run("secureCookies=false omits HSTS", func(t *testing.T) {
		h := securityHeaders(false)(okHandler)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

		got := rw.Header().Get("Strict-Transport-Security")
		if got != "" {
			t.Errorf("Strict-Transport-Security should be absent when secureCookies=false, got %q", got)
		}
	})
}

func TestSecurityHeaders_PassesThrough(t *testing.T) {
	// Verify the middleware calls next and status code is preserved.
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	h := securityHeaders(false)(notFound)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404 passthrough, got %d", rw.Code)
	}
}

// assertHeader is a small helper so failures name the header clearly.
func assertHeader(t *testing.T, rw *httptest.ResponseRecorder, header, want string) {
	t.Helper()
	got := rw.Header().Get(header)
	if got != want {
		t.Errorf("header %s = %q, want %q", header, got, want)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
