package handlers

import (
	"net/http"
	"strconv"
	"text/template"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
	"github.com/th0rn0/thournament/internal/models"
	"github.com/th0rn0/thournament/internal/tournament"
)

type AdminHandler struct {
	pool     *pgxpool.Pool
	tmpls    map[string]*template.Template
	brokers  *BracketBrokerMap
	maxParts int
	devLogin bool
}

func NewAdminHandler(pool *pgxpool.Pool, tmpls map[string]*template.Template, brokers *BracketBrokerMap, maxParticipants int, devLogin bool) *AdminHandler {
	return &AdminHandler{pool: pool, tmpls: tmpls, brokers: brokers, maxParts: maxParticipants, devLogin: devLogin}
}

// GET /admin
func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.id, t.name, t.description, t.format, t.team_size,
		       t.max_participants, t.status, t.created_by, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM participants p WHERE p.tournament_id = t.id) AS participant_count
		FROM tournaments t
		ORDER BY t.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TournamentRow struct {
		models.Tournament
		ParticipantCount int
	}
	var tournaments []TournamentRow
	for rows.Next() {
		var tr TournamentRow
		if err := rows.Scan(
			&tr.ID, &tr.Name, &tr.Description, &tr.Format, &tr.TeamSize,
			&tr.MaxParticipants, &tr.Status, &tr.CreatedBy, &tr.CreatedAt, &tr.UpdatedAt,
			&tr.ParticipantCount,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		tournaments = append(tournaments, tr)
	}

	render(w, r, h.tmpls, "admin_dashboard.html", map[string]interface{}{
		"Tournaments": tournaments,
	})
}

// GET /admin/tournaments/new
func (h *AdminHandler) NewTournamentForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, h.tmpls, "admin_tournament_new.html", map[string]interface{}{
		"MaxParticipants": h.maxParts,
	})
}

// POST /admin/tournaments
func (h *AdminHandler) CreateTournament(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	description := r.FormValue("description")
	format := r.FormValue("format")
	teamSizeStr := r.FormValue("team_size")
	maxPartsStr := r.FormValue("max_participants")

	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if format != string(models.FormatSingleElim) && format != string(models.FormatDoubleElim) {
		http.Error(w, "invalid format", http.StatusBadRequest)
		return
	}

	teamSize, _ := strconv.Atoi(teamSizeStr)
	if teamSize < 1 {
		teamSize = 1
	}

	maxParts, _ := strconv.Atoi(maxPartsStr)
	if maxParts < 2 {
		maxParts = h.maxParts
	}
	if maxParts > 256 {
		maxParts = 256
	}

	creatorID, _ := auth.UserIDFromContext(r.Context())

	var id int64
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO tournaments (name, description, format, team_size, max_participants, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, name, description, format, teamSize, maxParts, creatorID).Scan(&id)
	if err != nil {
		http.Error(w, "failed to create tournament", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/tournaments/{id}/bracket-generate
func (h *AdminHandler) GenerateBracket(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Check tournament is in registration
	var status models.TournamentStatus
	if err := h.pool.QueryRow(r.Context(), `SELECT status FROM tournaments WHERE id = $1`, id).Scan(&status); err != nil {
		http.Error(w, "tournament not found", http.StatusNotFound)
		return
	}
	if status != models.StatusRegistration {
		http.Error(w, "tournament bracket already generated", http.StatusBadRequest)
		return
	}

	bracketID, err := tournament.GenerateBracket(r.Context(), h.pool, id, h.maxParts)
	if err != nil {
		http.Error(w, "failed to generate bracket: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = bracketID

	// Broadcast update
	h.brokers.BroadcastBracketUpdate(id)

	http.Redirect(w, r, "/tournaments/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// POST /admin/matches/{id}/result
func (h *AdminHandler) OverrideResult(w http.ResponseWriter, r *http.Request) {
	matchID, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid match id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	winnerID, err := strconv.ParseInt(r.FormValue("winner_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid winner_id", http.StatusBadRequest)
		return
	}

	var scoreA, scoreB *int
	if sa := r.FormValue("score_a"); sa != "" {
		n, _ := strconv.Atoi(sa)
		scoreA = &n
	}
	if sb := r.FormValue("score_b"); sb != "" {
		n, _ := strconv.Atoi(sb)
		scoreB = &n
	}
	var scoreDisplay *string
	if sd := r.FormValue("score_display"); sd != "" {
		scoreDisplay = &sd
	}

	if err := tournament.SubmitResult(
		r.Context(), h.pool, h.brokers,
		matchID, winnerID, scoreA, scoreB, scoreDisplay,
		0, true, // isAdmin=true
	); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tournamentID int64
	_ = h.pool.QueryRow(r.Context(), `
		SELECT b.tournament_id FROM matches m
		JOIN brackets b ON b.id = m.bracket_id
		WHERE m.id = $1
	`, matchID).Scan(&tournamentID)

	http.Redirect(w, r, "/tournaments/"+strconv.FormatInt(tournamentID, 10), http.StatusSeeOther)
}

// GET /admin/tournaments/{id}
func (h *AdminHandler) TournamentDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var t models.Tournament
	err = h.pool.QueryRow(r.Context(), `
		SELECT id, name, description, format, team_size, max_participants,
		       status, created_by, created_at, updated_at
		FROM tournaments WHERE id = $1
	`, id).Scan(
		&t.ID, &t.Name, &t.Description, &t.Format, &t.TeamSize, &t.MaxParticipants,
		&t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		http.Error(w, "tournament not found", http.StatusNotFound)
		return
	}

	// Load participants
	rows, _ := h.pool.Query(r.Context(), `
		SELECT p.id, p.tournament_id, p.user_id, p.team_id, p.seed, p.registered_at,
		       COALESCE(u.username, '') AS user_username,
		       COALESCE(u.avatar, '') AS user_avatar,
		       COALESCE(u.discord_id, '') AS user_discord_id,
		       COALESCE(te.name, '') AS team_name
		FROM participants p
		LEFT JOIN users u ON u.id = p.user_id
		LEFT JOIN teams te ON te.id = p.team_id
		WHERE p.tournament_id = $1
		ORDER BY p.seed, p.registered_at
	`, id)
	var participants []*models.Participant
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			p := &models.Participant{}
			if err := rows.Scan(
				&p.ID, &p.TournamentID, &p.UserID, &p.TeamID, &p.Seed, &p.RegisteredAt,
				&p.UserUsername, &p.UserAvatar, &p.UserDiscordID, &p.TeamName,
			); err != nil {
				continue
			}
			participants = append(participants, p)
		}
	}

	// Load matches if bracket exists
	var bracketID *int64
	_ = h.pool.QueryRow(r.Context(), `SELECT id FROM brackets WHERE tournament_id = $1`, id).Scan(&bracketID)
	var matches []*models.Match
	if bracketID != nil {
		matches, _ = tournament.LoadMatchesForBracket(r.Context(), h.pool, *bracketID)
	}

	render(w, r, h.tmpls, "admin_tournament_detail.html", map[string]interface{}{
		"Tournament":   t,
		"Participants": participants,
		"Matches":      matches,
		"BracketID":    bracketID,
		"DevLogin":     h.devLogin,
	})
}

// POST /admin/tournaments/{id}/cancel
func (h *AdminHandler) CancelTournament(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	_, _ = h.pool.Exec(r.Context(), `
		UPDATE tournaments SET status = 'cancelled', updated_at = NOW() WHERE id = $1
	`, id)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
