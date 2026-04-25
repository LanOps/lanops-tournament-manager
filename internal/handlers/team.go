package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"text/template"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/auth"
	"github.com/th0rn0/thournament/internal/models"
)

type TeamHandler struct {
	pool    *pgxpool.Pool
	brokers *BracketBrokerMap
	tmpls   map[string]*template.Template
}

func NewTeamHandler(pool *pgxpool.Pool, brokers *BracketBrokerMap, tmpls map[string]*template.Template) *TeamHandler {
	return &TeamHandler{pool: pool, brokers: brokers, tmpls: tmpls}
}

func randomToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// POST /tournaments/{id}/teams — create a team (player_created mode only)
func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	// Verify tournament is team+player_created+registration
	var t models.Tournament
	if err := h.pool.QueryRow(r.Context(), `
		SELECT id, team_mode, team_size, status FROM tournaments WHERE id = $1
	`, tournamentID).Scan(&t.ID, &t.TeamMode, &t.TeamSize, &t.Status); err != nil {
		http.Error(w, "tournament not found", http.StatusNotFound)
		return
	}
	if t.TeamMode != models.TeamModePlayerCreated {
		http.Error(w, "teams are admin-assigned for this tournament", http.StatusForbidden)
		return
	}
	if t.Status != models.StatusRegistration {
		http.Error(w, "tournament is not open for registration", http.StatusForbidden)
		return
	}

	name := r.FormValue("team_name")
	if name == "" {
		http.Error(w, "team name is required", http.StatusBadRequest)
		return
	}
	password := r.FormValue("join_password")
	open := r.FormValue("open") == "1"
	if password != "" {
		open = false
	}
	token := randomToken()

	var teamID int64
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO teams (tournament_id, name, captain_id, open, join_password, invite_token)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, tournamentID, name, userID, open, password, token).Scan(&teamID)
	if err != nil {
		http.Error(w, "failed to create team (name may already be taken)", http.StatusBadRequest)
		return
	}

	// Add captain as first member
	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, teamID, userID)

	http.Redirect(w, r, "/tournaments/"+strconv.FormatInt(tournamentID, 10), http.StatusSeeOther)
}

// POST /tournaments/{id}/teams/{team_id}/join
func (h *TeamHandler) Join(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	teamID, err := strconv.ParseInt(chi.URLParam(r, "team_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	// Load team + tournament
	var team models.Team
	var tournStatus models.TournamentStatus
	var teamMode models.TournamentTeamMode
	if err := h.pool.QueryRow(r.Context(), `
		SELECT te.id, te.open, te.join_password, te.invite_token, t.status, t.team_mode
		FROM teams te
		JOIN tournaments t ON t.id = te.tournament_id
		WHERE te.id = $1 AND te.tournament_id = $2
	`, teamID, tournamentID).Scan(
		&team.ID, &team.Open, &team.JoinPassword, &team.InviteToken,
		&tournStatus, &teamMode,
	); err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}
	if tournStatus != models.StatusRegistration {
		http.Error(w, "tournament is not open for registration", http.StatusForbidden)
		return
	}
	if teamMode != models.TeamModePlayerCreated {
		http.Error(w, "teams are admin-assigned", http.StatusForbidden)
		return
	}

	// Check invite token or password
	token := r.URL.Query().Get("token")
	password := r.FormValue("join_password")
	if !team.Open {
		if token != "" && token != team.InviteToken {
			http.Error(w, "invalid invite link", http.StatusForbidden)
			return
		}
		if token == "" && password != team.JoinPassword {
			http.Error(w, "incorrect password", http.StatusForbidden)
			return
		}
	}

	// Check user isn't already in a team for this tournament
	var existing int
	_ = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM team_members tm
		JOIN teams te ON te.id = tm.team_id
		WHERE te.tournament_id = $1 AND tm.user_id = $2
	`, tournamentID, userID).Scan(&existing)
	if existing > 0 {
		http.Error(w, "you are already in a team for this tournament", http.StatusBadRequest)
		return
	}

	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, teamID, userID)

	http.Redirect(w, r, "/tournaments/"+strconv.FormatInt(tournamentID, 10), http.StatusSeeOther)
}

// POST /tournaments/{id}/teams/{team_id}/leave
func (h *TeamHandler) Leave(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	teamID, err := strconv.ParseInt(chi.URLParam(r, "team_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	_, _ = h.pool.Exec(r.Context(), `
		DELETE FROM team_members WHERE team_id = $1 AND user_id = $2
	`, teamID, userID)

	http.Redirect(w, r, "/tournaments/"+strconv.FormatInt(tournamentID, 10), http.StatusSeeOther)
}
