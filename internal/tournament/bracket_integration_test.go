package tournament_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/th0rn0/thournament/internal/models"
	"github.com/th0rn0/thournament/internal/testutil"
	"github.com/th0rn0/thournament/internal/tournament"
)

// TestGenerateBracket_SingleElim verifies match counts and next_match_id links for various
// participant counts in single elimination format.
func TestGenerateBracket_SingleElim(t *testing.T) {
	cases := []struct {
		n              int
		wantMatches    int // non-pending-reset matches
		wantRound1Ready int // matches in round 1 with status=ready (both participants present)
		wantByes       int // round 1 bye matches
	}{
		{n: 1, wantMatches: 0, wantRound1Ready: 0, wantByes: 0}, // auto-winner, no matches
		{n: 2, wantMatches: 1, wantRound1Ready: 1, wantByes: 0},
		{n: 4, wantMatches: 3, wantRound1Ready: 2, wantByes: 0},
		{n: 5, wantMatches: 7, wantRound1Ready: 1, wantByes: 3}, // 8-slot: 4 r1 matches, 3 byes
		{n: 6, wantMatches: 7, wantRound1Ready: 2, wantByes: 2},
		{n: 7, wantMatches: 7, wantRound1Ready: 3, wantByes: 1},
		{n: 8, wantMatches: 7, wantRound1Ready: 4, wantByes: 0},
	}

	for _, c := range cases {
		c := c
		t.Run("n="+itoa(c.n), func(t *testing.T) {
			t.Parallel()
			pool := testutil.NewDB(t)
			ctx := context.Background()

			admin := testutil.CreateUser(t, pool, "admin", "admin")
			tid := testutil.CreateTournament(t, pool, "Test", models.FormatSingleElim, admin)
			users := testutil.CreateUsers(t, pool, c.n)
			testutil.RegisterParticipants(t, pool, tid, users)

			bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
			require.NoError(t, err)

			if c.n == 1 {
				assert.Greater(t, bracketID, int64(0), "n=1 still creates a bracket row")
				// Tournament should be completed immediately — no matches to play
				assert.Equal(t, models.StatusCompleted, testutil.TournamentStatus(t, pool, tid))
				matches := testutil.LoadMatches(t, pool, bracketID)
				assert.Empty(t, matches, "n=1 bracket has no matches")
				return
			}

			assert.Greater(t, bracketID, int64(0))
			assert.Equal(t, models.StatusActive, testutil.TournamentStatus(t, pool, tid))

			matches := testutil.LoadMatches(t, pool, bracketID)
			assert.Len(t, matches, c.wantMatches, "total match count")

			// Byes are auto-advanced to status=completed during generation (see
			// TestGenerateBracket_ByeAutoAdvance). Identify them structurally: a
			// round-1 match with exactly one participant filled.
			var ready, byes int
			for _, m := range matches {
				if m.Round != 1 || m.BracketSide != models.SideWinners {
					continue
				}
				if m.Status == models.MatchReady {
					ready++
				}
				aFilled := m.ParticipantAID != nil
				bFilled := m.ParticipantBID != nil
				if aFilled != bFilled { // exactly one side filled
					byes++
				}
			}
			assert.Equal(t, c.wantRound1Ready, ready, "ready matches in round 1")
			assert.Equal(t, c.wantByes, byes, "bye matches in round 1")
		})
	}
}

// TestGenerateBracket_8Players_NextMatchLinks verifies the winner advancement graph
// for an 8-player single elimination tournament.
// Expected: 7 matches, each round-1 match links to a round-2 match, which links to the final.
func TestGenerateBracket_8Players_NextMatchLinks(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "8-player SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 8)
	testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)
	require.Len(t, matches, 7)

	// Round 1: 4 matches, all status=ready, all have next_match_id pointing to a round-2 match
	r1 := matchesByRound(matches, 1)
	require.Len(t, r1, 4)
	for _, m := range r1 {
		assert.Equal(t, models.MatchReady, m.Status, "r1 match %d should be ready", m.MatchNumber)
		assert.NotNil(t, m.NextMatchID, "r1 match %d missing next_match_id", m.MatchNumber)
		assert.NotNil(t, m.ParticipantAID, "r1 match %d missing participant A", m.MatchNumber)
		assert.NotNil(t, m.ParticipantBID, "r1 match %d missing participant B", m.MatchNumber)
	}

	// Round 2: 2 matches, both pending (no participants yet), both have next_match_id (final)
	r2 := matchesByRound(matches, 2)
	require.Len(t, r2, 2)
	for _, m := range r2 {
		assert.Equal(t, models.MatchPending, m.Status, "r2 match %d should be pending", m.MatchNumber)
		assert.NotNil(t, m.NextMatchID, "r2 match %d missing next_match_id", m.MatchNumber)
	}

	// Round 3: 1 match (final), pending, no next_match_id
	r3 := matchesByRound(matches, 3)
	require.Len(t, r3, 1)
	assert.Equal(t, models.MatchPending, r3[0].Status)
	assert.Nil(t, r3[0].NextMatchID, "final should have no next_match_id")

	// Each pair of r1 matches feeds the same r2 match
	r2IDs := map[int64]int{}
	for _, m := range r1 {
		r2IDs[*m.NextMatchID]++
	}
	assert.Len(t, r2IDs, 2, "4 r1 matches should feed exactly 2 distinct r2 matches")
	for id, count := range r2IDs {
		assert.Equal(t, 2, count, "r2 match %d should be fed by exactly 2 r1 matches", id)
	}
}

// TestGenerateBracket_ByeAutoAdvance verifies that bye matches are auto-completed
// and the winner is placed in the next match.
func TestGenerateBracket_ByeAutoAdvance(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	// 5 players → 8-slot bracket → 3 byes, 1 real match in round 1
	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "5-player SE", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 5)
	parts := testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	matches := testutil.LoadMatches(t, pool, bracketID)

	// All bye matches should be completed with a winner set
	for _, m := range matches {
		if m.Status == models.MatchBye {
			t.Errorf("bye match %d should be completed, not status=bye after generation", m.ID)
		}
	}

	// Round 1 bye matches should be completed
	byeCompleted := 0
	for _, m := range matches {
		if m.Round == 1 && m.Status == models.MatchCompleted {
			byeCompleted++
			assert.NotNil(t, m.WinnerID, "completed bye match should have winner")
		}
	}
	assert.Equal(t, 3, byeCompleted, "3 byes should be auto-completed")

	// Round 2 matches that received a bye winner should now have one participant filled
	// (the bye winner should have been placed in their round 2 match)
	r2 := matchesByRound(matches, 2)
	for _, m := range r2 {
		// At least one participant should be filled in all r2 matches
		// (bye winner placed there)
		hasAny := m.ParticipantAID != nil || m.ParticipantBID != nil
		assert.True(t, hasAny, "r2 match %d should have at least one participant after bye auto-advance", m.MatchNumber)
	}

	_ = parts // referenced to suppress unused warning
}

// TestGenerateBracket_DoubleElim_8Players verifies structure of a double elimination bracket.
func TestGenerateBracket_DoubleElim_8Players(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "8-player DE", models.FormatDoubleElim, admin)
	users := testutil.CreateUsers(t, pool, 8)
	testutil.RegisterParticipants(t, pool, tid, users)

	bracketID, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	allMatches := testutil.LoadAllMatches(t, pool, bracketID)
	visibleMatches := testutil.LoadMatches(t, pool, bracketID)

	// Should have a pending_reset match (hidden from normal view)
	resetMatches := 0
	for _, m := range allMatches {
		if m.Status == models.MatchPendingReset {
			resetMatches++
			assert.Equal(t, models.SideReset, m.BracketSide)
		}
	}
	assert.Equal(t, 1, resetMatches, "should have exactly 1 pending_reset match")

	// Winners bracket
	wbMatches := testutil.MatchesBySide(visibleMatches, models.SideWinners)
	assert.NotEmpty(t, wbMatches, "winners bracket should have matches")

	// 8-player WB = 7 matches (4 + 2 + 1)
	assert.Len(t, wbMatches, 7, "8-player WB should have 7 matches")

	// Losers bracket
	lbMatches := testutil.MatchesBySide(visibleMatches, models.SideLosers)
	assert.NotEmpty(t, lbMatches, "losers bracket should have matches")

	// Grand final
	gfMatches := testutil.MatchesBySide(visibleMatches, models.SideGrandFinal)
	assert.Len(t, gfMatches, 1, "should have exactly 1 grand final match")
	assert.Equal(t, models.MatchPending, gfMatches[0].Status)
	assert.NotNil(t, gfMatches[0].LoserNextMatchID, "grand final should have loser_next_match_id pointing to reset")

	// WB round 1: 4 matches, all ready, all have loser_next_match_id (drop to LB)
	wbR1 := matchesByRoundAndSide(wbMatches, 1, models.SideWinners)
	require.Len(t, wbR1, 4)
	for _, m := range wbR1 {
		assert.Equal(t, models.MatchReady, m.Status)
		assert.NotNil(t, m.LoserNextMatchID, "WB r1 match %d should have loser_next_match_id", m.MatchNumber)
	}
}

// TestGenerateBracket_AlreadyHasBracket verifies that generating a bracket twice fails.
func TestGenerateBracket_AlreadyHasBracket(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "double-gen", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 4)
	testutil.RegisterParticipants(t, pool, tid, users)

	_, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	require.NoError(t, err)

	// Second generate should fail (tournament status is now 'active', not 'registration')
	// The handler checks status before calling, but GenerateBracket itself will fail on
	// the UNIQUE constraint on brackets(tournament_id)
	_, err = tournament.GenerateBracket(ctx, pool, tid, 64)
	assert.Error(t, err, "second bracket generation should fail")
}

// TestGenerateBracket_NoParticipants verifies that generating with zero participants fails.
func TestGenerateBracket_NoParticipants(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "empty", models.FormatSingleElim, admin)

	_, err := tournament.GenerateBracket(ctx, pool, tid, 64)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no participants")
}

// TestGenerateBracket_TooManyParticipants verifies max_participants enforcement.
func TestGenerateBracket_TooManyParticipants(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	admin := testutil.CreateUser(t, pool, "admin", "admin")
	tid := testutil.CreateTournament(t, pool, "overflow", models.FormatSingleElim, admin)
	users := testutil.CreateUsers(t, pool, 5)
	testutil.RegisterParticipants(t, pool, tid, users)

	_, err := tournament.GenerateBracket(ctx, pool, tid, 4) // maxParticipants=4 but 5 registered
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many participants")
}

// --- helpers ---

func matchesByRound(matches []testutil.MatchRow, round int) []testutil.MatchRow {
	var out []testutil.MatchRow
	for _, m := range matches {
		if m.Round == round {
			out = append(out, m)
		}
	}
	return out
}

func matchesByRoundAndSide(matches []testutil.MatchRow, round int, side models.BracketSide) []testutil.MatchRow {
	var out []testutil.MatchRow
	for _, m := range matches {
		if m.Round == round && m.BracketSide == side {
			out = append(out, m)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
