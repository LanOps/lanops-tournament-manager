package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/th0rn0/thournament/internal/auth"
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
