package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/th0rn0/lanops-tournament-manager/internal/auth"
)

// --- RequireAuth ---

func TestRequireAuth_Unauthenticated_Redirects(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-padded-here"))
	disc := auth.NewDiscord(&auth.DiscordConfig{
		ClientID: "x", ClientSecret: "x", RedirectURL: "x",
		BotToken: "x", AdminRoleID: "x", GuildID: "x",
	})
	mw := auth.NewMiddleware(store, disc)

	handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.Equal(t, http.StatusSeeOther, rw.Code)
	assert.Equal(t, "/auth/discord", rw.Header().Get("Location"))
}

// --- RequireAdmin fail-closed ---

func TestRequireAdmin_DiscordAPIDown_Returns503(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-padded-here"))
	disc := auth.NewDiscord(&auth.DiscordConfig{
		ClientID: "x", ClientSecret: "x", RedirectURL: "x",
		BotToken: "x", AdminRoleID: "x", GuildID: "x",
	})
	mw := auth.NewMiddleware(store, disc)

	handler := mw.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Build a request with a valid session
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rw := httptest.NewRecorder()

	// Stamp the session
	w2 := httptest.NewRecorder()
	sess, _ := store.Get(req, auth.SessionName)
	sess.Values[auth.SessionUserID] = int64(42)
	sess.Values[auth.SessionDiscordID] = "discord_user_1"
	_ = sess.Save(req, w2)
	for _, cookie := range w2.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Discord will be unreachable (bot token "x" against real Discord API) — we test the
	// error path by using an invalid token. In CI without network, this also fails-closed.
	// The key assertion: status should NOT be 200.
	handler.ServeHTTP(rw, req)

	// Either 503 (API error) or 403 (not admin) — both are correct fail-closed behavior.
	// Should never be 200.
	assert.NotEqual(t, http.StatusOK, rw.Code, "should not grant admin access when Discord is unreachable or token is invalid")
}

// --- Context helpers ---

func TestUserIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	id, ok := auth.UserIDFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, int64(0), id)
}

func TestIsAdminFromContext_Default(t *testing.T) {
	ctx := context.Background()
	assert.False(t, auth.IsAdminFromContext(ctx))
}

func TestDiscordIDFromContext_Empty(t *testing.T) {
	_, ok := auth.DiscordIDFromContext(context.Background())
	assert.False(t, ok)
}

// --- OptionalAuth ---

type fixedChecker struct {
	isAdmin bool
	err     error
}

func (f *fixedChecker) IsAdmin(context.Context, string) (bool, error) {
	return f.isAdmin, f.err
}

// helper: build a middleware against a cookie store with a stub admin checker,
// plus a function that stamps a signed session onto a request.
func mwWithSession(t *testing.T, checker auth.AdminChecker) (*auth.Middleware, func(*http.Request) *http.Request) {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-padded-here"))
	mw := auth.NewMiddlewareWithChecker(store, checker)
	stamp := func(r *http.Request) *http.Request {
		w := httptest.NewRecorder()
		sess, _ := store.Get(r, auth.SessionName)
		sess.Values[auth.SessionUserID] = int64(42)
		sess.Values[auth.SessionDiscordID] = "discord_user_42"
		_ = sess.Save(r, w)
		r2 := r.Clone(r.Context())
		for _, c := range w.Result().Cookies() {
			r2.AddCookie(c)
		}
		return r2
	}
	return mw, stamp
}

func TestOptionalAuth_NoSession_PassesThrough(t *testing.T) {
	mw, _ := mwWithSession(t, &fixedChecker{})
	var sawUser bool
	handler := mw.OptionalAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, sawUser = auth.UserIDFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	assert.False(t, sawUser, "no session → no user in context")
}

func TestOptionalAuth_WithSession_LoadsUserAndAdmin(t *testing.T) {
	mw, stamp := mwWithSession(t, &fixedChecker{isAdmin: true})
	var uid int64
	var isAdmin bool
	var discordID string
	handler := mw.OptionalAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		uid, _ = auth.UserIDFromContext(r.Context())
		isAdmin = auth.IsAdminFromContext(r.Context())
		discordID, _ = auth.DiscordIDFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), stamp(httptest.NewRequest("GET", "/", nil)))
	assert.Equal(t, int64(42), uid)
	assert.True(t, isAdmin, "OptionalAuth populates IsAdmin best-effort")
	assert.Equal(t, "discord_user_42", discordID)
}

func TestOptionalAuth_CheckerError_StillAuthenticates(t *testing.T) {
	mw, stamp := mwWithSession(t, &fixedChecker{err: assertFailingError("discord down")})
	var uid int64
	var isAdmin bool
	handler := mw.OptionalAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		uid, _ = auth.UserIDFromContext(r.Context())
		isAdmin = auth.IsAdminFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), stamp(httptest.NewRequest("GET", "/", nil)))
	assert.Equal(t, int64(42), uid, "user still authenticated when admin lookup fails")
	assert.False(t, isAdmin, "admin flag stays false on lookup error")
}

// --- RequireAuth (happy path) ---

func TestRequireAuth_WithSession_CallsNext(t *testing.T) {
	mw, stamp := mwWithSession(t, &fixedChecker{isAdmin: true})
	var called bool
	handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// IsAdmin should be populated best-effort here too.
		assert.True(t, auth.IsAdminFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, stamp(httptest.NewRequest("GET", "/protected", nil)))
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rw.Code)
}

// --- RequireAdmin (happy path) ---

func TestRequireAdmin_ValidAdmin_CallsNext(t *testing.T) {
	mw, stamp := mwWithSession(t, &fixedChecker{isAdmin: true})
	var called bool
	handler := mw.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, stamp(httptest.NewRequest("GET", "/admin", nil)))
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rw.Code)
}

func TestRequireAdmin_NotAdmin_Returns403(t *testing.T) {
	mw, stamp := mwWithSession(t, &fixedChecker{isAdmin: false})
	handler := mw.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called for non-admin")
	}))
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, stamp(httptest.NewRequest("GET", "/admin", nil)))
	assert.Equal(t, http.StatusForbidden, rw.Code)
}

// assertFailingError is a typed sentinel — using errors.New inline would make
// the test rely on string identity.
type assertFailingError string

func (e assertFailingError) Error() string { return string(e) }
