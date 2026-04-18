package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
)

type DevLoginHandler struct {
	store sessions.Store
	pool  *pgxpool.Pool
}

func NewDevLoginHandler(store sessions.Store, pool *pgxpool.Pool) *DevLoginHandler {
	return &DevLoginHandler{store: store, pool: pool}
}

var devUsernameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

const devLoginForm = `<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Dev Login</title>
<link rel="stylesheet" href="/static/css/main.css"></head>
<body><main class="container">
<h1>Dev Login</h1>
<p style="color:#b45309">Development-only. Bypasses Discord OAuth.</p>
<form method="POST" action="/dev/login">
<input type="hidden" name="gorilla.csrf.Token" value="{{CSRF}}">
<p><label>Username: <input name="username" required pattern="[a-zA-Z0-9_-]{1,32}" value="devuser"></label></p>
<p><label><input type="checkbox" name="admin" value="1"> Log in as admin</label></p>
<p><button type="submit">Log in</button></p>
</form></main></body></html>`

// GET /dev/login
func (h *DevLoginHandler) Form(w http.ResponseWriter, r *http.Request) {
	body := strings.Replace(devLoginForm, "{{CSRF}}", csrf.Token(r), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// POST /dev/login
func (h *DevLoginHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	if !devUsernameRE.MatchString(username) {
		http.Error(w, "username must match [a-zA-Z0-9_-]{1,32}", http.StatusBadRequest)
		return
	}
	isAdmin := r.FormValue("admin") != ""

	discordID := auth.DevPrefixUser + username
	if isAdmin {
		discordID = auth.DevPrefixAdmin + username
	}

	var userID int64
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO users (discord_id, username, discriminator, avatar)
		VALUES ($1, $2, '', '')
		ON CONFLICT (discord_id) DO UPDATE
		SET username = EXCLUDED.username, updated_at = NOW()
		RETURNING id
	`, discordID, username).Scan(&userID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	sess, _ := h.store.Get(r, auth.SessionName)
	sess.Values[auth.SessionUserID] = userID
	sess.Values[auth.SessionDiscordID] = discordID
	if err := sess.Save(r, w); err != nil {
		http.Error(w, "session save error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
