package auth

import (
	"context"
	"net/http"

	"github.com/gorilla/sessions"
)

type contextKey int

const (
	ctxUserID        contextKey = iota
	ctxDiscordUserID contextKey = iota
	ctxIsAdmin       contextKey = iota
)

const (
	SessionName    = "thournament_session"
	SessionUserID  = "user_id"
	SessionDiscordID = "discord_id"
)

// AdminChecker abstracts the Discord role check, making Middleware testable.
type AdminChecker interface {
	IsAdmin(ctx context.Context, discordUserID string) (bool, error)
}

type Middleware struct {
	store   sessions.Store
	checker AdminChecker
}

func NewMiddleware(store sessions.Store, discord *Discord) *Middleware {
	return &Middleware{store: store, checker: discord}
}

// NewMiddlewareWithChecker allows tests to inject a stub AdminChecker.
func NewMiddlewareWithChecker(store sessions.Store, checker AdminChecker) *Middleware {
	return &Middleware{store: store, checker: checker}
}

// RequireAuth rejects unauthenticated requests with a redirect to /auth/discord.
// Best-effort admin flag is populated for downstream handlers/templates; errors
// here are swallowed since auth itself already succeeded.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := m.store.Get(r, SessionName)
		if err != nil {
			http.Redirect(w, r, "/auth/discord", http.StatusSeeOther)
			return
		}

		userID, ok := sess.Values[SessionUserID].(int64)
		if !ok || userID == 0 {
			http.Redirect(w, r, "/auth/discord", http.StatusSeeOther)
			return
		}

		discordID, _ := sess.Values[SessionDiscordID].(string)

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxDiscordUserID, discordID)
		if discordID != "" {
			if isAdmin, err := m.checker.IsAdmin(r.Context(), discordID); err == nil && isAdmin {
				ctx = context.WithValue(ctx, ctxIsAdmin, true)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin rejects non-admin requests. Fail-closed: if Discord API is unreachable, returns 503.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := m.store.Get(r, SessionName)
		if err != nil {
			http.Redirect(w, r, "/auth/discord", http.StatusSeeOther)
			return
		}

		userID, ok := sess.Values[SessionUserID].(int64)
		if !ok || userID == 0 {
			http.Redirect(w, r, "/auth/discord", http.StatusSeeOther)
			return
		}

		discordID, _ := sess.Values[SessionDiscordID].(string)
		if discordID == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		isAdmin, err := m.checker.IsAdmin(r.Context(), discordID)
		if err != nil {
			// Fail-closed: Discord API unreachable
			http.Error(w, "Service unavailable: unable to verify admin role", http.StatusServiceUnavailable)
			return
		}

		if !isAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxDiscordUserID, discordID)
		ctx = context.WithValue(ctx, ctxIsAdmin, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserIDFromContext returns the authenticated user's DB ID.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxUserID).(int64)
	return id, ok
}

// DiscordIDFromContext returns the authenticated user's Discord ID.
func DiscordIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxDiscordUserID).(string)
	return id, ok
}

// IsAdminFromContext returns whether the current request is from an admin.
func IsAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxIsAdmin).(bool)
	return v
}

// OptionalAuth loads user info from session if present, does not reject unauthenticated requests.
// Best-effort admin flag so public pages can surface admin-only UI (e.g. Admin nav link).
func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := m.store.Get(r, SessionName)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		userID, ok := sess.Values[SessionUserID].(int64)
		if !ok || userID == 0 {
			next.ServeHTTP(w, r)
			return
		}

		discordID, _ := sess.Values[SessionDiscordID].(string)

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxDiscordUserID, discordID)
		if discordID != "" {
			if isAdmin, err := m.checker.IsAdmin(r.Context(), discordID); err == nil && isAdmin {
				ctx = context.WithValue(ctx, ctxIsAdmin, true)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
