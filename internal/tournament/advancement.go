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
		       participant_a_id, participant_b_id, winner_id, loser_id, status,
		       next_match_id, loser_next_match_id
		FROM matches WHERE id = $1
	`, matchID).Scan(
		&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
		&m.ParticipantAID, &m.ParticipantBID, &m.WinnerID, &m.LoserID, &m.Status,
		&m.NextMatchID, &m.LoserNextMatchID,
	)
	if err != nil {
		return fmt.Errorf("load match: %w", err)
	}

	// An "edit" submission updates a completed match in-place. Allowed only in
	// the winners/losers brackets (not GF/reset, whose downstream logic is
	// wrapped up in tournament completion state), and only while every match
	// downstream is still un-completed. Editing with a completed downstream
	// would silently invalidate its already-recorded result.
	isEdit := m.Status == models.MatchCompleted
	if isEdit {
		editable, err := isMatchEditable(ctx, tx, m)
		if err != nil {
			return fmt.Errorf("check editable: %w", err)
		}
		if !editable {
			return fmt.Errorf("match cannot be edited: either downstream match is already completed, or this is a grand-final/reset match")
		}
	} else if m.Status != models.MatchReady && m.Status != models.MatchInProgress {
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

	// Remember what the match was before the write so an edit can propagate
	// the swap into already-populated downstream slots.
	var oldWinner, oldLoser int64
	if m.WinnerID != nil {
		oldWinner = *m.WinnerID
	}
	if m.LoserID != nil {
		oldLoser = *m.LoserID
	}

	// Write the match itself — same statement for first submit and edit.
	if _, err := tx.Exec(ctx, `
		UPDATE matches
		SET status = 'completed', winner_id = $1, loser_id = $2,
		    score_a = $3, score_b = $4, score_display = $5,
		    played_at = $6, updated_at = NOW()
		WHERE id = $7
	`, winnerID, loserID, scoreA, scoreB, scoreDisplay, now, matchID); err != nil {
		return fmt.Errorf("mark match completed: %w", err)
	}

	if isEdit {
		// Swap old → new in any populated downstream slot. If the winner didn't
		// actually change (scores-only edit), this is a no-op.
		if m.NextMatchID != nil && oldWinner != winnerID {
			if err := swapParticipant(ctx, tx, *m.NextMatchID, oldWinner, winnerID); err != nil {
				return fmt.Errorf("update winner's next match: %w", err)
			}
		}
		if m.LoserNextMatchID != nil && oldLoser != loserID && oldLoser != 0 {
			if err := swapParticipant(ctx, tx, *m.LoserNextMatchID, oldLoser, loserID); err != nil {
				return fmt.Errorf("update loser's next match: %w", err)
			}
		}
	} else {
		// First-time submission — advance participants into downstream slots.
		switch m.BracketSide {
		case models.SideGrandFinal:
			// Who won the Grand Final decides whether the reset match activates:
			//   WB finalist wins → tournament over, delete the reset row.
			//   LB finalist wins → reset activates for a winner-take-all match.
			// Identify the WB finalist by looking up the winners-bracket match
			// whose next_match_id points at this GF. Its winner_id IS the WB
			// finalist.
			var wbFinalistID int64
			_ = tx.QueryRow(ctx, `
				SELECT winner_id FROM matches
				WHERE bracket_id = $1 AND next_match_id = $2 AND bracket_side = 'winners'
			`, m.BracketID, m.ID).Scan(&wbFinalistID)

			if m.LoserNextMatchID != nil && wbFinalistID != winnerID {
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
			// Reset match complete — bracket is finished; admin completes manually.
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
			// Tournament completion:
			//   SE: the winners-bracket final has no next match — completing
			//       it ends the tournament.
			//   RR: every match has no next match; end only when all of them
			//       are done.
			// DE's terminal flow is handled in the SideGrandFinal/SideReset
			// branches above and never reaches here.
			// No auto-completion — admin completes the tournament manually.
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
	// WB finalist won GF → no reset needed, delete the pending_reset match.
	// Null out any loser_next_match_id references first to avoid the FK
	// constraint on matches(loser_next_match_id) → matches(id).
	if _, err := tx.Exec(ctx, `
		UPDATE matches SET loser_next_match_id = NULL
		WHERE bracket_id = $1 AND loser_next_match_id IN (
			SELECT id FROM matches WHERE bracket_id = $1 AND status = 'pending_reset'
		)
	`, bracketID); err != nil {
		return fmt.Errorf("unlink reset match: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM matches WHERE bracket_id = $1 AND status = 'pending_reset'
	`, bracketID); err != nil {
		return fmt.Errorf("delete reset match: %w", err)
	}
	return nil
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


// isMatchEditable reports whether a completed match can be re-submitted in
// place. Editable iff every downstream match (winner-path and, in DE, the
// loser-path) is still un-completed, so the swap can't invalidate a result
// that's already been recorded.
//
// Grand-final and reset matches are never editable through this path — their
// completion flow is entangled with tournament-complete state and the reset
// activation logic. Edit those by cancelling and regenerating the bracket.
func isMatchEditable(ctx context.Context, tx pgx.Tx, m models.Match) (bool, error) {
	if m.Status != models.MatchCompleted {
		return false, nil
	}
	if m.BracketSide != models.SideWinners && m.BracketSide != models.SideLosers {
		return false, nil
	}
	check := func(mid *int64) (bool, error) {
		if mid == nil {
			return true, nil
		}
		var s models.MatchStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM matches WHERE id = $1`, *mid).Scan(&s); err != nil {
			return false, err
		}
		return s != models.MatchCompleted, nil
	}
	if ok, err := check(m.NextMatchID); err != nil || !ok {
		return ok, err
	}
	if ok, err := check(m.LoserNextMatchID); err != nil || !ok {
		return ok, err
	}
	return true, nil
}

// swapParticipant replaces `oldID` with `newID` in whichever slot of matchID
// currently holds it. No-op if neither slot matches. Used by the edit flow.
func swapParticipant(ctx context.Context, tx pgx.Tx, matchID, oldID, newID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE matches SET
			participant_a_id = CASE WHEN participant_a_id = $1 THEN $2 ELSE participant_a_id END,
			participant_b_id = CASE WHEN participant_b_id = $1 THEN $2 ELSE participant_b_id END,
			updated_at = NOW()
		WHERE id = $3
	`, oldID, newID, matchID)
	return err
}

// CanEditResult returns true if the match is a completed winners/losers-bracket
// match whose downstream matches are all still un-completed, AND the user is
// authorized to submit results for it. Used by handlers to decide whether to
// render the match as clickable for editing.
func CanEditResult(ctx context.Context, pool *pgxpool.Pool, matchID, userID int64) (bool, error) {
	var m models.Match
	err := pool.QueryRow(ctx, `
		SELECT id, bracket_id, round, match_number, bracket_side,
		       participant_a_id, participant_b_id, winner_id, loser_id, status,
		       next_match_id, loser_next_match_id
		FROM matches WHERE id = $1
	`, matchID).Scan(
		&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
		&m.ParticipantAID, &m.ParticipantBID, &m.WinnerID, &m.LoserID, &m.Status,
		&m.NextMatchID, &m.LoserNextMatchID,
	)
	if err != nil {
		return false, err
	}

	// Use the same gate as the write path, minus the tx (pool works here
	// because this is a read-only advisory check).
	if m.Status != models.MatchCompleted {
		return false, nil
	}
	if m.BracketSide != models.SideWinners && m.BracketSide != models.SideLosers {
		return false, nil
	}
	check := func(mid *int64) (bool, error) {
		if mid == nil {
			return true, nil
		}
		var s models.MatchStatus
		if err := pool.QueryRow(ctx, `SELECT status FROM matches WHERE id = $1`, *mid).Scan(&s); err != nil {
			return false, err
		}
		return s != models.MatchCompleted, nil
	}
	if ok, err := check(m.NextMatchID); err != nil || !ok {
		return ok, err
	}
	if ok, err := check(m.LoserNextMatchID); err != nil || !ok {
		return ok, err
	}

	// Authorization: same participant/captain check as CanSubmitResult.
	checkPart := func(partID int64) bool {
		var cnt int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM participants p
			LEFT JOIN teams t ON t.id = p.team_id
			WHERE p.id = $1 AND (p.user_id = $2 OR t.captain_id = $2)
		`, partID, userID).Scan(&cnt); err == nil && cnt > 0 {
			return true
		}
		return false
	}
	if m.ParticipantAID != nil && checkPart(*m.ParticipantAID) {
		return true, nil
	}
	if m.ParticipantBID != nil && checkPart(*m.ParticipantBID) {
		return true, nil
	}
	return false, nil
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
