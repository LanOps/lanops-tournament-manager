package tournament

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/models"
)

// Broadcaster is implemented by handlers.BracketBrokerMap.
type Broadcaster interface {
	BroadcastBracketUpdate(tournamentID int64)
}

// SubmitResult records a match result, advances winners/losers to the next match,
// and triggers SSE broadcast. Returns an error if authorization fails.
func SubmitResult(ctx context.Context, pool *pgxpool.Pool, broker Broadcaster, matchID, winnerID int64, scoreA, scoreB *int, scoreDisplay *string, submitterUserID int64, isAdmin bool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Load the match
	var m models.Match
	err = tx.QueryRow(ctx, `
		SELECT id, bracket_id, round, match_number, bracket_side,
		       participant_a_id, participant_b_id, winner_id, status,
		       next_match_id, loser_next_match_id
		FROM matches WHERE id = $1
	`, matchID).Scan(
		&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
		&m.ParticipantAID, &m.ParticipantBID, &m.WinnerID, &m.Status,
		&m.NextMatchID, &m.LoserNextMatchID,
	)
	if err != nil {
		return fmt.Errorf("load match: %w", err)
	}

	if m.Status == models.MatchCompleted {
		return fmt.Errorf("match already completed")
	}
	if m.Status != models.MatchReady && m.Status != models.MatchInProgress {
		return fmt.Errorf("match not ready (status: %s)", m.Status)
	}

	// Verify winnerID is a participant in this match
	if (m.ParticipantAID == nil || *m.ParticipantAID != winnerID) &&
		(m.ParticipantBID == nil || *m.ParticipantBID != winnerID) {
		return fmt.Errorf("winner %d is not a participant in match %d", winnerID, matchID)
	}

	// Authorization check
	if !isAdmin {
		if err := checkResultAuth(ctx, tx, m, submitterUserID); err != nil {
			return err
		}
	}

	// Determine loserID
	var loserID int64
	if m.ParticipantAID != nil && *m.ParticipantAID == winnerID {
		if m.ParticipantBID != nil {
			loserID = *m.ParticipantBID
		}
	} else if m.ParticipantAID != nil {
		loserID = *m.ParticipantAID
	}

	now := time.Now()

	// Mark match completed
	if _, err := tx.Exec(ctx, `
		UPDATE matches
		SET status = 'completed', winner_id = $1, loser_id = $2,
		    score_a = $3, score_b = $4, score_display = $5,
		    played_at = $6, updated_at = NOW()
		WHERE id = $7
	`, winnerID, loserID, scoreA, scoreB, scoreDisplay, now, matchID); err != nil {
		return fmt.Errorf("mark match completed: %w", err)
	}

	// Grand Final handling
	switch m.BracketSide {
	case models.SideGrandFinal:
		if m.LoserNextMatchID != nil && loserID != 0 {
			// LB finalist beat WB finalist → activate reset match
			if err := activateResetMatch(ctx, tx, m.BracketID, winnerID, loserID); err != nil {
				return err
			}
		} else {
			// WB finalist won GF → tournament over
			if err := handleGrandFinalWin(ctx, tx, m.BracketID, winnerID); err != nil {
				return err
			}
		}
	case models.SideReset:
		// Reset match complete — tournament over
		if err := completeTournamentByBracket(ctx, tx, m.BracketID); err != nil {
			return err
		}
	default:
		// Advance winner to next match
		if m.NextMatchID != nil {
			if err := placeInNextMatch(ctx, tx, *m.NextMatchID, winnerID); err != nil {
				return fmt.Errorf("advance winner: %w", err)
			}
		}
		// Advance loser to losers bracket (double elim only)
		if loserID != 0 && m.LoserNextMatchID != nil {
			if err := placeInNextMatch(ctx, tx, *m.LoserNextMatchID, loserID); err != nil {
				return fmt.Errorf("advance loser: %w", err)
			}
		}
	}

	// Load tournament ID for SSE broadcast
	var tournamentID int64
	if err := tx.QueryRow(ctx, `SELECT tournament_id FROM brackets WHERE id = $1`, m.BracketID).Scan(&tournamentID); err != nil {
		return fmt.Errorf("load tournament id: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Broadcast SSE update (non-blocking)
	if broker != nil {
		go broker.BroadcastBracketUpdate(tournamentID)
	}

	return nil
}

// checkResultAuth returns nil if submitterUserID is authorized to submit this result.
func checkResultAuth(ctx context.Context, tx pgx.Tx, m models.Match, submitterUserID int64) error {
	checkPart := func(partID int64) bool {
		var count int
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM participants p
			LEFT JOIN teams t ON t.id = p.team_id
			WHERE p.id = $1 AND (p.user_id = $2 OR t.captain_id = $2)
		`, partID, submitterUserID).Scan(&count)
		return err == nil && count > 0
	}

	if m.ParticipantAID != nil && checkPart(*m.ParticipantAID) {
		return nil
	}
	if m.ParticipantBID != nil && checkPart(*m.ParticipantBID) {
		return nil
	}
	return fmt.Errorf("not authorized to submit result for match %d", m.ID)
}

func placeInNextMatch(ctx context.Context, tx pgx.Tx, matchID, participantID int64) error {
	var pA, pB *int64
	err := tx.QueryRow(ctx, `
		SELECT participant_a_id, participant_b_id FROM matches WHERE id = $1
	`, matchID).Scan(&pA, &pB)
	if err != nil {
		return fmt.Errorf("load next match: %w", err)
	}

	if pA == nil {
		_, err = tx.Exec(ctx, `
			UPDATE matches
			SET participant_a_id = $1,
			    status = CASE WHEN participant_b_id IS NOT NULL THEN 'ready'::match_status ELSE status END,
			    updated_at = NOW()
			WHERE id = $2
		`, participantID, matchID)
	} else if pB == nil {
		_, err = tx.Exec(ctx, `
			UPDATE matches
			SET participant_b_id = $1,
			    status = CASE WHEN participant_a_id IS NOT NULL THEN 'ready'::match_status ELSE status END,
			    updated_at = NOW()
			WHERE id = $2
		`, participantID, matchID)
	} else {
		return fmt.Errorf("next match %d already has both participants", matchID)
	}
	return err
}

func handleGrandFinalWin(ctx context.Context, tx pgx.Tx, bracketID, winnerID int64) error {
	// WB finalist won GF → no reset needed, delete the pending_reset match
	if _, err := tx.Exec(ctx, `
		DELETE FROM matches WHERE bracket_id = $1 AND status = 'pending_reset'
	`, bracketID); err != nil {
		return fmt.Errorf("delete reset match: %w", err)
	}
	return completeTournamentByBracket(ctx, tx, bracketID)
}

func activateResetMatch(ctx context.Context, tx pgx.Tx, bracketID, wbFinalistID, lbFinalistID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE matches
		SET participant_a_id = $1, participant_b_id = $2,
		    status = 'ready', updated_at = NOW()
		WHERE bracket_id = $3 AND status = 'pending_reset'
	`, wbFinalistID, lbFinalistID, bracketID)
	return err
}

func completeTournamentByBracket(ctx context.Context, tx pgx.Tx, bracketID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE tournaments t
		SET status = 'completed', updated_at = NOW()
		FROM brackets b
		WHERE b.id = $1 AND t.id = b.tournament_id
	`, bracketID)
	return err
}

// CanSubmitResult returns true if userID is authorized to submit results for the given match.
func CanSubmitResult(ctx context.Context, pool *pgxpool.Pool, matchID, userID int64) (bool, error) {
	var pA, pB *int64
	var status models.MatchStatus
	err := pool.QueryRow(ctx, `
		SELECT participant_a_id, participant_b_id, status
		FROM matches WHERE id = $1
	`, matchID).Scan(&pA, &pB, &status)
	if err != nil {
		return false, err
	}

	if status != models.MatchReady && status != models.MatchInProgress {
		return false, nil
	}

	checkPart := func(partID int64) bool {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM participants p
			LEFT JOIN teams t ON t.id = p.team_id
			WHERE p.id = $1 AND (p.user_id = $2 OR t.captain_id = $2)
		`, partID, userID).Scan(&count); err == nil && count > 0 {
			return true
		}
		return false
	}

	if pA != nil && checkPart(*pA) {
		return true, nil
	}
	if pB != nil && checkPart(*pB) {
		return true, nil
	}
	return false, nil
}
