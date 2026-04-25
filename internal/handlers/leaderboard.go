package handlers

import (
	"fmt"
	"net/http"
	"text/template"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/models"
)

type LeaderboardHandler struct {
	pool  *pgxpool.Pool
	tmpls map[string]*template.Template
}

func NewLeaderboardHandler(pool *pgxpool.Pool, tmpls map[string]*template.Template) *LeaderboardHandler {
	return &LeaderboardHandler{pool: pool, tmpls: tmpls}
}

type LeaderboardRow struct {
	Rank           int
	Username       string
	AvatarURL      string
	Tournaments    int
	TournamentWins int
	MatchWins      int
	MatchLosses    int
	WinPct         string
}

// GET /leaderboard
func (h *LeaderboardHandler) Show(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		WITH player_stats AS (
			SELECT
				u.id,
				u.username,
				u.discord_id,
				u.avatar,
				COUNT(DISTINCT p.tournament_id)                                                     AS tournaments,
				COUNT(DISTINCT CASE
					WHEN m_win.winner_id = p.id
					 AND m_win.next_match_id IS NULL
					 AND m_win.status = 'completed'
					 AND t_win.status = 'completed'
					THEN p.tournament_id
				END)                                                                                AS tournament_wins,
				COUNT(DISTINCT CASE WHEN m.winner_id = p.id THEN m.id END)                         AS match_wins,
				COUNT(DISTINCT CASE WHEN m.loser_id  = p.id THEN m.id END)                         AS match_losses
			FROM users u
			JOIN participants p ON p.user_id = u.id
			LEFT JOIN matches m
				ON (m.participant_a_id = p.id OR m.participant_b_id = p.id)
				AND m.status = 'completed'
			LEFT JOIN matches m_win
				ON m_win.winner_id = p.id
			LEFT JOIN brackets b_win ON b_win.id = m_win.bracket_id
			LEFT JOIN tournaments t_win ON t_win.id = b_win.tournament_id
			GROUP BY u.id, u.username, u.discord_id, u.avatar
		)
		SELECT username, discord_id, avatar,
		       tournaments, tournament_wins, match_wins, match_losses
		FROM player_stats
		WHERE tournaments > 0
		ORDER BY tournament_wins DESC, match_wins DESC, match_losses ASC, username ASC
		LIMIT 100
	`)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var leaderboard []LeaderboardRow
	rank := 0
	for rows.Next() {
		var username, discordID, avatar string
		var tournaments, tournamentWins, matchWins, matchLosses int
		if err := rows.Scan(
			&username, &discordID, &avatar,
			&tournaments, &tournamentWins, &matchWins, &matchLosses,
		); err != nil {
			continue
		}
		rank++
		winPct := "—"
		total := matchWins + matchLosses
		if total > 0 {
			winPct = fmt.Sprintf("%.0f%%", float64(matchWins)/float64(total)*100)
		}
		leaderboard = append(leaderboard, LeaderboardRow{
			Rank:           rank,
			Username:       username,
			AvatarURL:      models.AvatarURLFor(discordID, avatar, username),
			Tournaments:    tournaments,
			TournamentWins: tournamentWins,
			MatchWins:      matchWins,
			MatchLosses:    matchLosses,
			WinPct:         winPct,
		})
	}

	render(w, r, h.tmpls, "leaderboard.html", map[string]interface{}{
		"Leaderboard": leaderboard,
	})
}
