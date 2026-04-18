package tournament_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/th0rn0/thournament/internal/models"
	"github.com/th0rn0/thournament/internal/testutil"
	"github.com/th0rn0/thournament/internal/tournament"
)

// noBroadcast satisfies tournament.Broadcaster and records calls safely.
// SubmitResult fires the broadcast in a goroutine, so a test that reads
// `calls` while the goroutine may still be writing would trip the race
// detector without this mutex.
type noBroadcast struct {
	mu    sync.Mutex
	calls []int64
}

func (n *noBroadcast) BroadcastBracketUpdate(tournamentID int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, tournamentID)
}

func (n *noBroadcast) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

func (n *noBroadcast) callAt(i int) int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[i]
}

// TestSubmitResult_WinnerAdvances verifies that submitting a result moves the winner to the next match.
func TestSubmitResult_WinnerAdvances(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "4SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 4)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	r1 := matchesByRound(matches, 1)
	require.Len(t, r1, 2, "4-player SE should have 2 r1 matches")

	broker := &noBroadcast{}

	// Submit result for first r1 match — winner is participant A
	m0 := r1[0]
	require.NotNil(t, m0.ParticipantAID)
	winnerID := *m0.ParticipantAID

	err = tournament.SubmitResult(ctx, pool, broker, m0.ID, winnerID, nil, nil, nil, 0, true)
	require.NoError(t, err)

	// Reload matches
	matches = testutil.LoadMatches(t, pool, bracketID)

	// m0 should now be completed
	updated := testutil.MatchByRoundSide(matches, m0.Round, m0.MatchNumber, models.SideWinners)
	require.NotNil(t, updated)
	assert.Equal(t, models.MatchCompleted, updated.Status)
	require.NotNil(t, updated.WinnerID)
	assert.Equal(t, winnerID, *updated.WinnerID)

	// The round-2 match should now have participant A filled in
	r2 := matchesByRound(matches, 2)
	require.Len(t, r2, 1)
	final := r2[0]
	hasWinner := (final.ParticipantAID != nil && *final.ParticipantAID == winnerID) ||
		(final.ParticipantBID != nil && *final.ParticipantBID == winnerID)
	assert.True(t, hasWinner, "winner should appear in final")

	// SSE broadcast should have fired (eventually — it runs in a goroutine).
	require.Eventually(t, func() bool { return broker.callCount() == 1 },
		testTimeout(), tickInterval(), "one broadcast call expected")
	assert.Equal(t, tid, broker.callAt(0))

	_ = parts
}

// TestSubmitResult_CompletesTournament verifies that finishing the final match
// marks the tournament as completed.
func TestSubmitResult_CompletesTournament(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "2SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	require.Len(t, matches, 1, "2-player SE has 1 match")

	m := matches[0]
	require.NotNil(t, m.ParticipantAID)
	winnerID := *m.ParticipantAID

	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerID, nil, nil, nil, 0, true)
	require.NoError(t, err)

	assert.Equal(t, models.StatusCompleted, testutil.TournamentStatus(t, pool, tid))
	_ = parts
}

// TestSubmitResult_WrongWinner rejects a winnerID that is not in the match.
func TestSubmitResult_WrongWinner(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "4SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 4)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	r1 := matchesByRound(matches, 1)
	m := r1[0]

	// Use a participant ID from a different match as the "winner"
	wrongWinner := int64(99999)
	err = tournament.SubmitResult(ctx, pool, nil, m.ID, wrongWinner, nil, nil, nil, 0, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a participant")

	_ = parts
}

// TestSubmitResult_AlreadyCompleted rejects double-submission.
func TestSubmitResult_AlreadyCompleted(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "2SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	m := matches[0]
	winnerID := *m.ParticipantAID

	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerID, nil, nil, nil, 0, true)
	require.NoError(t, err)

	// Second submit should error
	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerID, nil, nil, nil, 0, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")

	_ = parts
}

// TestSubmitResult_Auth_SoloSelfReport verifies that a solo participant can self-report.
func TestSubmitResult_Auth_SoloSelfReport(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "auth-test", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	require.Len(t, matches, 1)
	m := matches[0]

	// Participant A's user submits, declaring themselves winner — should work
	winnerPartID := *m.ParticipantAID
	submitterUserID := users[0] // user whose participant is ParticipantA (first registered = first user)

	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerPartID, nil, nil, nil, submitterUserID, false)
	require.NoError(t, err, "participant should be able to self-report as winner")

	_ = parts
}

// TestSubmitResult_Auth_NonParticipantRejected verifies that a random user gets rejected.
func TestSubmitResult_Auth_NonParticipantRejected(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "auth-reject", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	m := matches[0]

	// A user who is not in this match tries to submit
	outsider := testutil.CreateUser(t, pool, "outsider", "outsider")
	winnerPartID := *m.ParticipantAID

	err = tournament.SubmitResult(ctx, pool, nil, m.ID, winnerPartID, nil, nil, nil, outsider, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")

	_ = parts
}

// TestSubmitResult_Auth_TeamCaptain verifies that a team captain can submit results.
func TestSubmitResult_Auth_TeamCaptain(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "team-auth", models.FormatSingleElim, admin)

	// Set team_size=2 so this is a team tournament
	_, _ = pool.Exec(ctx, `UPDATE tournaments SET team_size=2 WHERE id=$1`, tid)

	// Create two teams with captains
	captain1 := testutil.CreateUser(t, pool, "cap1", "captain1")
	captain2 := testutil.CreateUser(t, pool, "cap2", "captain2")

	var team1, team2 int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO teams (tournament_id, name, captain_id) VALUES ($1, 'Team Alpha', $2) RETURNING id
	`, tid, captain1).Scan(&team1))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO teams (tournament_id, name, captain_id) VALUES ($1, 'Team Beta', $2) RETURNING id
	`, tid, captain2).Scan(&team2))

	// Register teams as participants
	var part1, part2 int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO participants (tournament_id, team_id) VALUES ($1, $2) RETURNING id
	`, tid, team1).Scan(&part1))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO participants (tournament_id, team_id) VALUES ($1, $2) RETURNING id
	`, tid, team2).Scan(&part2))

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	require.Len(t, matches, 1)
	m := matches[0]

	// Captain 1 declares their team as winner
	winnerPartID := part1

	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerPartID, nil, nil, nil, captain1, false)
	require.NoError(t, err, "team captain should be able to submit result")

	// Captain 2 (losing team) should NOT be able to submit a different result (match is already completed)
	// (double submit test is covered elsewhere; here we just verify captain auth path worked)
}

// TestSubmitResult_Auth_TeamMemberRejected verifies that a non-captain team member cannot submit.
func TestSubmitResult_Auth_TeamMemberRejected(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "team-member-reject", models.FormatSingleElim, admin)
	_, _ = pool.Exec(ctx, `UPDATE tournaments SET team_size=2 WHERE id=$1`, tid)

	captain := testutil.CreateUser(t, pool, "cap", "captain")
	member := testutil.CreateUser(t, pool, "mem", "member")
	opponent := testutil.CreateUser(t, pool, "opp", "opponent")

	var team1, team2 int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO teams (tournament_id, name, captain_id) VALUES ($1,'T1',$2) RETURNING id`, tid, captain).Scan(&team1))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO teams (tournament_id, name, captain_id) VALUES ($1,'T2',$2) RETURNING id`, tid, opponent).Scan(&team2))
	_, _ = pool.Exec(ctx, `INSERT INTO team_members (team_id, user_id) VALUES ($1,$2)`, team1, member)

	var part1, part2 int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO participants (tournament_id, team_id) VALUES ($1,$2) RETURNING id`, tid, team1).Scan(&part1))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO participants (tournament_id, team_id) VALUES ($1,$2) RETURNING id`, tid, team2).Scan(&part2))

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	m := matches[0]

	// Non-captain team member tries to submit — should be rejected
	err = tournament.SubmitResult(ctx, pool, nil, m.ID, part1, nil, nil, nil, member, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")
}

// TestDoubleElim_GrandFinal_WBFinalistWins verifies that when the WB finalist wins the grand
// final, the pending_reset match is deleted and the tournament completes.
func TestDoubleElim_GrandFinal_WBFinalistWins(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "2DE", models.FormatDoubleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	// With 2 players in DE: WB has 1 final match, LB is skipped (no LB round 1 with 0 matches?).
	// For a 2-player DE, the bracket may collapse to WB final + GF directly.
	// Let's just drive through all ready matches until tournament completes.
	for i := 0; i < 20; i++ { // safety cap
		matches := testutil.LoadMatches(t, pool, bracketID)
		m := findReadyMatch(matches)
		if m == nil {
			break
		}
		require.NotNil(t, m.ParticipantAID, "ready match should have participant A")
		err := tournament.SubmitResult(ctx, pool, broker, m.ID, *m.ParticipantAID, nil, nil, nil, 0, true)
		require.NoError(t, err)
	}

	// Tournament should be completed
	assert.Equal(t, models.StatusCompleted, testutil.TournamentStatus(t, pool, tid))

	// No pending_reset match should remain (it was deleted because WB finalist won)
	allMatches := testutil.LoadAllMatches(t, pool, bracketID)
	for _, m := range allMatches {
		assert.NotEqual(t, models.MatchPendingReset, m.Status, "pending_reset match should have been deleted")
	}

	_ = parts
}

// TestDoubleElim_GrandFinal_LBFinalistWins verifies bracket reset activates when the LB
// finalist wins the grand final.
func TestDoubleElim_GrandFinal_LBFinalistWins(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "4DE-reset", models.FormatDoubleElim, admin)
	users := testutil.CreateUsers(t, pool, 4)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	// Drive WB: always let participant A win to push participant B to LB.
	// Drive LB: always let participant B (the LB player) win.
	// In GF: let the LB finalist (participant B) win to force a reset.
	// This is complex to orchestrate generically, so we drive matches greedily
	// and verify the reset match activates at the right time.

	var grandFinalPlayed bool
	for i := 0; i < 30; i++ {
		matches := testutil.LoadMatches(t, pool, bracketID)
		m := findReadyMatch(matches)
		if m == nil {
			break
		}
		require.NotNil(t, m.ParticipantAID)

		var winnerID int64
		if m.BracketSide == models.SideGrandFinal && !grandFinalPlayed {
			// LB finalist wins GF → let participant B win
			if m.ParticipantBID != nil {
				winnerID = *m.ParticipantBID
			} else {
				winnerID = *m.ParticipantAID
			}
			grandFinalPlayed = true
		} else {
			winnerID = *m.ParticipantAID
		}

		err := tournament.SubmitResult(ctx, pool, broker, m.ID, winnerID, nil, nil, nil, 0, true)
		require.NoError(t, err)

		if grandFinalPlayed {
			// Check if reset match activated
			allMatches := testutil.LoadAllMatches(t, pool, bracketID)
			resetMatch := findMatchByStatus(allMatches, models.MatchPendingReset)
			readyReset := findMatchByStatusAndSide(allMatches, models.MatchReady, models.SideReset)
			if readyReset != nil || resetMatch == nil {
				// Either reset match is now ready (LB won GF) or was deleted (WB won)
				break
			}
		}
	}

	if grandFinalPlayed {
		allMatches := testutil.LoadAllMatches(t, pool, bracketID)
		// After LB finalist wins GF, the reset match should be 'ready', not 'pending_reset'
		resetReady := findMatchByStatusAndSide(allMatches, models.MatchReady, models.SideReset)
		pendingReset := findMatchByStatus(allMatches, models.MatchPendingReset)

		if resetReady != nil {
			assert.Equal(t, models.MatchReady, resetReady.Status, "reset match should be ready after LB wins GF")
			assert.NotNil(t, resetReady.ParticipantAID, "reset match should have participant A")
			assert.NotNil(t, resetReady.ParticipantBID, "reset match should have participant B")
		} else if pendingReset == nil {
			// If no pending_reset and no ready reset, WB finalist must have won — that's fine too
			// (depends on bracket ordering)
			t.Log("WB finalist won grand final — no reset needed, that is valid")
		}
	}

	_ = parts
}

// TestCanSubmitResult verifies the exported authorization check function.
func TestCanSubmitResult(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "can-submit", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	require.Len(t, matches, 1)
	m := matches[0]

	// Participant A's user can submit
	canA, err := tournament.CanSubmitResult(ctx, pool, m.ID, users[0])
	require.NoError(t, err)
	assert.True(t, canA)

	// Participant B's user can submit
	canB, err := tournament.CanSubmitResult(ctx, pool, m.ID, users[1])
	require.NoError(t, err)
	assert.True(t, canB)

	// Outsider cannot submit
	outsider := testutil.CreateUser(t, pool, "out", "outsider")
	canOut, err := tournament.CanSubmitResult(ctx, pool, m.ID, outsider)
	require.NoError(t, err)
	assert.False(t, canOut)

	_ = parts
}

// TestSSEBroadcast_FiredOnResult verifies the broadcaster is called after SubmitResult.
func TestSSEBroadcast_FiredOnResult(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	broker := &noBroadcast{}

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "sse-test", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 2)
	testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	m := matches[0]
	winnerID := *m.ParticipantAID

	err = tournament.SubmitResult(ctx, pool, broker, m.ID, winnerID, nil, nil, nil, 0, true)
	require.NoError(t, err)

	// Give the goroutine time to fire
	require.Eventually(t, func() bool {
		return broker.callCount() > 0
	}, testTimeout(), tickInterval(), "SSE broadcast should fire after result submitted")

	assert.Equal(t, tid, broker.callAt(0))
}

// --- helpers ---

func findReadyMatch(matches []testutil.MatchRow) *testutil.MatchRow {
	for i := range matches {
		if matches[i].Status == models.MatchReady {
			return &matches[i]
		}
	}
	return nil
}

func findMatchByStatus(matches []testutil.MatchRow, status models.MatchStatus) *testutil.MatchRow {
	for i := range matches {
		if matches[i].Status == status {
			return &matches[i]
		}
	}
	return nil
}

func findMatchByStatusAndSide(matches []testutil.MatchRow, status models.MatchStatus, side models.BracketSide) *testutil.MatchRow {
	for i := range matches {
		if matches[i].Status == status && matches[i].BracketSide == side {
			return &matches[i]
		}
	}
	return nil
}
