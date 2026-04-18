package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/models"
)

// CreateUser inserts a user and returns their DB id.
func CreateUser(t *testing.T, pool *pgxpool.Pool, discordID, username string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (discord_id, username, discriminator, avatar)
		VALUES ($1, $2, '', '')
		RETURNING id
	`, discordID, username).Scan(&id)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return id
}

// CreateTournament inserts a tournament and returns its id.
func CreateTournament(t *testing.T, pool *pgxpool.Pool, name string, format models.TournamentFormat, createdBy int64) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tournaments (name, format, max_participants, created_by)
		VALUES ($1, $2, 64, $3)
		RETURNING id
	`, name, format, createdBy).Scan(&id)
	if err != nil {
		t.Fatalf("CreateTournament(%s): %v", name, err)
	}
	return id
}

// RegisterParticipants registers n users in a tournament and returns their participant IDs (in order).
func RegisterParticipants(t *testing.T, pool *pgxpool.Pool, tournamentID int64, userIDs []int64) []int64 {
	t.Helper()
	ids := make([]int64, len(userIDs))
	for i, uid := range userIDs {
		var pid int64
		err := pool.QueryRow(context.Background(), `
			INSERT INTO participants (tournament_id, user_id)
			VALUES ($1, $2)
			RETURNING id
		`, tournamentID, uid).Scan(&pid)
		if err != nil {
			t.Fatalf("RegisterParticipant user %d: %v", uid, err)
		}
		ids[i] = pid
	}
	return ids
}

// CreateUsers inserts n users and returns their DB ids.
func CreateUsers(t *testing.T, pool *pgxpool.Pool, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = CreateUser(t, pool, fmt.Sprintf("discord_%d", i), fmt.Sprintf("user%d", i))
	}
	return ids
}

// LoadMatches returns all non-pending-reset matches for a bracket, ordered by side/round/match_number.
func LoadMatches(t *testing.T, pool *pgxpool.Pool, bracketID int64) []MatchRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, bracket_id, round, match_number, bracket_side,
		       participant_a_id, participant_b_id, winner_id,
		       status, next_match_id, loser_next_match_id
		FROM matches
		WHERE bracket_id = $1 AND status != 'pending_reset'
		ORDER BY bracket_side, round, match_number
	`, bracketID)
	if err != nil {
		t.Fatalf("LoadMatches: %v", err)
	}
	defer rows.Close()

	var result []MatchRow
	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(
			&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
			&m.ParticipantAID, &m.ParticipantBID, &m.WinnerID,
			&m.Status, &m.NextMatchID, &m.LoserNextMatchID,
		); err != nil {
			t.Fatalf("LoadMatches scan: %v", err)
		}
		result = append(result, m)
	}
	return result
}

// LoadAllMatches returns ALL matches including pending_reset.
func LoadAllMatches(t *testing.T, pool *pgxpool.Pool, bracketID int64) []MatchRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, bracket_id, round, match_number, bracket_side,
		       participant_a_id, participant_b_id, winner_id,
		       status, next_match_id, loser_next_match_id
		FROM matches
		WHERE bracket_id = $1
		ORDER BY bracket_side, round, match_number
	`, bracketID)
	if err != nil {
		t.Fatalf("LoadAllMatches: %v", err)
	}
	defer rows.Close()

	var result []MatchRow
	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(
			&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
			&m.ParticipantAID, &m.ParticipantBID, &m.WinnerID,
			&m.Status, &m.NextMatchID, &m.LoserNextMatchID,
		); err != nil {
			t.Fatalf("LoadAllMatches scan: %v", err)
		}
		result = append(result, m)
	}
	return result
}

type MatchRow struct {
	ID              int64
	BracketID       int64
	Round           int
	MatchNumber     int
	BracketSide     models.BracketSide
	ParticipantAID  *int64
	ParticipantBID  *int64
	WinnerID        *int64
	Status          models.MatchStatus
	NextMatchID     *int64
	LoserNextMatchID *int64
}

// TournamentStatus returns the current status of a tournament.
func TournamentStatus(t *testing.T, pool *pgxpool.Pool, tournamentID int64) models.TournamentStatus {
	t.Helper()
	var status models.TournamentStatus
	_ = pool.QueryRow(context.Background(), `SELECT status FROM tournaments WHERE id = $1`, tournamentID).Scan(&status)
	return status
}

// BracketID returns the bracket id for a tournament, or 0 if none.
func BracketIDForTournament(t *testing.T, pool *pgxpool.Pool, tournamentID int64) int64 {
	t.Helper()
	var id int64
	_ = pool.QueryRow(context.Background(), `SELECT id FROM brackets WHERE tournament_id = $1`, tournamentID).Scan(&id)
	return id
}

// MatchByRoundSide finds a specific match by round, match_number, and bracket_side.
func MatchByRoundSide(matches []MatchRow, round, matchNum int, side models.BracketSide) *MatchRow {
	for i := range matches {
		m := &matches[i]
		if m.Round == round && m.MatchNumber == matchNum && m.BracketSide == side {
			return m
		}
	}
	return nil
}

// MatchesBySide filters matches by bracket side.
func MatchesBySide(matches []MatchRow, side models.BracketSide) []MatchRow {
	var out []MatchRow
	for _, m := range matches {
		if m.BracketSide == side {
			out = append(out, m)
		}
	}
	return out
}
