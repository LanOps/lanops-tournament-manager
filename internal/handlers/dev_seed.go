package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
	"github.com/th0rn0/thournament/internal/models"
	"github.com/th0rn0/thournament/internal/tournament"
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

// POST /dev/seed-all — wipes all tournament data, creates one tournament of each
// format (single elim, double elim, round robin) with participants and brackets.
func (h *DevSeedHandler) SeedAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	// Delete brackets first so match FK refs to participants are removed before
	// the tournaments→participants cascade fires.
	if _, err := h.pool.Exec(ctx, `DELETE FROM brackets`); err != nil {
		http.Error(w, "failed to clear brackets: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM tournaments`); err != nil {
		http.Error(w, "failed to clear tournaments: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM users WHERE discord_id LIKE 'dev-user-fake-%'`); err != nil {
		http.Error(w, "failed to clear fake users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type seedSpec struct {
		name   string
		game   string
		format models.TournamentFormat
		count  int
		max    int
	}
	specs := []seedSpec{
		{"LanOps Singles Cup", "Counter-Strike 2", models.FormatSingleElim, 8, 16},
		{"LanOps Challengers Cup", "Valorant", models.FormatDoubleElim, 8, 16},
		{"LanOps Round Robin", "Rocket League", models.FormatRoundRobin, 6, 8},
	}

	for _, spec := range specs {
		var id int64
		err := h.pool.QueryRow(ctx, `
			INSERT INTO tournaments (name, game, format, team_size, max_participants, created_by)
			VALUES ($1, $2, $3, 1, $4, $5)
			RETURNING id
		`, spec.name, spec.game, spec.format, spec.max, adminID).Scan(&id)
		if err != nil {
			http.Error(w, "failed to create tournament "+spec.name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := h.addFakeParticipants(ctx, id, spec.count); err != nil {
			http.Error(w, "failed to seed "+spec.name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := tournament.GenerateBracket(ctx, h.pool, id, spec.max); err != nil {
			http.Error(w, "failed to generate bracket for "+spec.name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/tournaments", http.StatusSeeOther)
}

func (h *DevSeedHandler) addFakeParticipants(ctx context.Context, tournamentID int64, count int) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i := 0; i < count; i++ {
		suffix, err := randHex(4)
		if err != nil {
			return err
		}
		discordID := auth.DevPrefixUser + "fake-" + suffix
		username := fmt.Sprintf("%s%s%d",
			seedAdjectives[byteMod(suffix[0], len(seedAdjectives))],
			seedNouns[byteMod(suffix[1], len(seedNouns))],
			byteMod(suffix[2], 90)+10,
		)
		var userID int64
		if err = tx.QueryRow(ctx, `
			INSERT INTO users (discord_id, username, discriminator, avatar)
			VALUES ($1, $2, '', '')
			RETURNING id
		`, discordID, username).Scan(&userID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO participants (tournament_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, tournamentID, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
