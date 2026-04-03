package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
)

type AuthHandler struct {
	discord *auth.Discord
	store   sessions.Store
	pool    *pgxpool.Pool
}

func NewAuthHandler(discord *auth.Discord, store sessions.Store, pool *pgxpool.Pool) *AuthHandler {
	return &AuthHandler{discord: discord, store: store, pool: pool}
}

// GET /auth/discord — redirect to Discord OAuth2
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sess, _ := h.store.Get(r, auth.SessionName)
	sess.Values["oauth_state"] = state
	if err := sess.Save(r, w); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.discord.AuthCodeURL(state), http.StatusTemporaryRedirect)
}

// GET /auth/callback — handle Discord OAuth2 callback
func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, auth.SessionName)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// Validate state
	expectedState, _ := sess.Values["oauth_state"].(string)
	if expectedState == "" || r.URL.Query().Get("state") != expectedState {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	delete(sess.Values, "oauth_state")

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	token, err := h.discord.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "OAuth exchange failed", http.StatusInternalServerError)
		return
	}

	discordUser, err := h.discord.GetUser(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}

	// Upsert user in DB
	var userID int64
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO users (discord_id, username, discriminator, avatar)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (discord_id) DO UPDATE
		SET username = EXCLUDED.username,
		    discriminator = EXCLUDED.discriminator,
		    avatar = EXCLUDED.avatar,
		    updated_at = NOW()
		RETURNING id
	`, discordUser.ID, discordUser.Username, discordUser.Discriminator, discordUser.Avatar).Scan(&userID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	sess.Values[auth.SessionUserID] = userID
	sess.Values[auth.SessionDiscordID] = discordUser.ID
	if err := sess.Save(r, w); err != nil {
		http.Error(w, "session save error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, auth.SessionName)
	if err == nil {
		sess.Options.MaxAge = -1
		_ = sess.Save(r, w)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
