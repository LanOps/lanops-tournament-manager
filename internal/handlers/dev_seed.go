package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
	"github.com/th0rn0/thournament/internal/models"
)

type DevSeedHandler struct {
	pool *pgxpool.Pool
}

func NewDevSeedHandler(pool *pgxpool.Pool) *DevSeedHandler {
	return &DevSeedHandler{pool: pool}
}

var (
	seedAdjectives = []string{"Swift", "Turbo", "Dark", "Epic", "Pro", "Silver", "Shadow", "Thunder", "Iron", "Crimson", "Lunar", "Solar", "Frost", "Wild", "Royal"}
	seedNouns      = []string{"Knight", "Ace", "Ninja", "Dragon", "Wolf", "Phoenix", "Hawk", "Tiger", "Falcon", "Viper", "Ranger", "Rogue", "Mage", "Titan", "Specter"}
)

// POST /dev/tournaments/{id}/seed
func (h *DevSeedHandler) Seed(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	count, err := strconv.Atoi(r.FormValue("count"))
	if err != nil || count < 1 {
		http.Error(w, "count must be a positive integer", http.StatusBadRequest)
		return
	}
	if count > 256 {
		count = 256
	}

	var status models.TournamentStatus
	var maxParts, existing int
	err = h.pool.QueryRow(r.Context(), `
		SELECT t.status, t.max_participants,
		       (SELECT COUNT(*) FROM participants p WHERE p.tournament_id = t.id)
		FROM tournaments t WHERE t.id = $1
	`, id).Scan(&status, &maxParts, &existing)
	if err != nil {
		http.Error(w, "tournament not found", http.StatusNotFound)
		return
	}
	if status != models.StatusRegistration {
		http.Error(w, "tournament not accepting registrations", http.StatusBadRequest)
		return
	}

	room := maxParts - existing
	if room <= 0 {
		http.Error(w, "tournament is full", http.StatusBadRequest)
		return
	}
	if count > room {
		count = room
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	for i := 0; i < count; i++ {
		suffix, err := randHex(4)
		if err != nil {
			http.Error(w, "rng error", http.StatusInternalServerError)
			return
		}
		discordID := auth.DevPrefixUser + "fake-" + suffix
		username := fmt.Sprintf("%s%s%d",
			seedAdjectives[byteMod(suffix[0], len(seedAdjectives))],
			seedNouns[byteMod(suffix[1], len(seedNouns))],
			byteMod(suffix[2], 90)+10,
		)

		var userID int64
		err = tx.QueryRow(r.Context(), `
			INSERT INTO users (discord_id, username, discriminator, avatar)
			VALUES ($1, $2, '', '')
			RETURNING id
		`, discordID, username).Scan(&userID)
		if err != nil {
			http.Error(w, "failed to create fake user: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(r.Context(), `
			INSERT INTO participants (tournament_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, id, userID)
		if err != nil {
			http.Error(w, "failed to add participant: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "commit error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/tournaments/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func byteMod(c byte, n int) int {
	return int(c) % n
}
