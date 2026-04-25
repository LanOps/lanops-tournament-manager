package tournament

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/lanops-tournament-manager/internal/models"
)

// matchSeed holds the data for one match before DB IDs are assigned.
type matchSeed struct {
	round       int
	matchNumber int
	side        models.BracketSide
	status      models.MatchStatus
	// indices into the matchSeed slice for the next match slot
	nextIdx      int // -1 if none
	loserNextIdx int // -1 if none (double elim only)
	// for round 1: which seeded participants go here
	seedA int // 0-based seed index, -1 for bye
	seedB int
}

// GenerateBracket creates a bracket record plus all match rows for the given tournament.
// Participants must already be registered. Seeds are assigned by registration order if not set.
// Returns the bracket ID.
func GenerateBracket(ctx context.Context, pool *pgxpool.Pool, tournamentID int64, maxParticipants int) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Load participants
	rows, err := tx.Query(ctx, `
		SELECT id, seed
		FROM participants
		WHERE tournament_id = $1
		ORDER BY COALESCE(seed, 999), registered_at
	`, tournamentID)
	if err != nil {
		return 0, fmt.Errorf("load participants: %w", err)
	}
	defer rows.Close()

	type pEntry struct {
		id   int64
		seed *int
	}
	var parts []pEntry
	for rows.Next() {
		var p pEntry
		if err := rows.Scan(&p.id, &p.seed); err != nil {
			return 0, err
		}
		parts = append(parts, p)
	}
	rows.Close()

	n := len(parts)
	if n < 1 {
		return 0, fmt.Errorf("no participants registered")
	}
	if n > maxParticipants {
		return 0, fmt.Errorf("too many participants: %d > %d", n, maxParticipants)
	}
	if n == 1 {
		bracketID, err := generateSingleParticipantBracket(ctx, tx, tournamentID, parts[0].id)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit single-participant bracket: %w", err)
		}
		return bracketID, nil
	}

	// Load tournament format
	var format models.TournamentFormat
	if err := tx.QueryRow(ctx, `SELECT format FROM tournaments WHERE id = $1`, tournamentID).Scan(&format); err != nil {
		return 0, fmt.Errorf("load tournament format: %w", err)
	}

	// Create bracket row
	var bracketID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO brackets (tournament_id) VALUES ($1) RETURNING id`, tournamentID,
	).Scan(&bracketID); err != nil {
		return 0, fmt.Errorf("insert bracket: %w", err)
	}

	// Build participant ID array (seeded order)
	participantIDs := make([]int64, n)
	for i, p := range parts {
		participantIDs[i] = p.id
	}

	switch format {
	case models.FormatSingleElim:
		err = buildSingleElim(ctx, tx, bracketID, participantIDs)
	case models.FormatDoubleElim:
		err = buildDoubleElim(ctx, tx, bracketID, participantIDs)
	case models.FormatRoundRobin:
		err = buildRoundRobin(ctx, tx, bracketID, participantIDs)
	default:
		return 0, fmt.Errorf("unknown format: %s", format)
	}
	if err != nil {
		return 0, err
	}

	// Mark tournament as active
	if _, err := tx.Exec(ctx, `UPDATE tournaments SET status = 'active', updated_at = NOW() WHERE id = $1`, tournamentID); err != nil {
		return 0, fmt.Errorf("update tournament status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return bracketID, nil
}

// generateSingleParticipantBracket handles n=1 — auto-winner, no matches.
func generateSingleParticipantBracket(ctx context.Context, tx pgx.Tx, tournamentID, participantID int64) (int64, error) {
	var bracketID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO brackets (tournament_id) VALUES ($1) RETURNING id`, tournamentID,
	).Scan(&bracketID); err != nil {
		return 0, err
	}
	// No matches — tournament is immediately completed
	if _, err := tx.Exec(ctx, `UPDATE tournaments SET status = 'completed', updated_at = NOW() WHERE id = $1`, tournamentID); err != nil {
		return 0, err
	}
	return bracketID, nil
}

// nextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n int) int {
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

// standardSeedPairs returns round-1 match pairings (0-indexed seeds) for a
// single-elimination bracket of the given slot count (must be a power of 2).
// Uses standard tournament seeding: top seed faces bottom seed, and halves are
// balanced at every level so top seeds can only meet in the final.
//
// Algorithm: start with [1], and at each doubling step, interleave each
// position p with its mirror (size+1-p). Produces the canonical bracket order,
// e.g. for 8 slots: [1, 8, 4, 5, 2, 7, 3, 6] → matches 1v8, 4v5, 2v7, 3v6.
func standardSeedPairs(slots int) [][2]int {
	positions := []int{1}
	for size := 2; size <= slots; size *= 2 {
		next := make([]int, 0, size)
		for _, p := range positions {
			next = append(next, p, size+1-p)
		}
		positions = next
	}
	pairs := make([][2]int, slots/2)
	for i := 0; i < slots/2; i++ {
		pairs[i] = [2]int{positions[i*2] - 1, positions[i*2+1] - 1}
	}
	return pairs
}

// buildSingleElim inserts all matches for a single-elimination bracket using two-pass insert.
func buildSingleElim(ctx context.Context, tx pgx.Tx, bracketID int64, participantIDs []int64) error {
	n := len(participantIDs)
	slots := nextPowerOf2(n)
	rounds := int(math.Log2(float64(slots)))

	// Build match seeds (round 1 through final)
	// Total matches in single elim: slots - 1
	seeds := make([]matchSeed, 0, slots-1)

	// Round 1: slots/2 matches
	round1Pairs := standardSeedPairs(slots)
	for i, pair := range round1Pairs {
		ms := matchSeed{
			round:       1,
			matchNumber: i + 1,
			side:        models.SideWinners,
			seedA:       pair[0],
			seedB:       pair[1],
			nextIdx:     -1,
			loserNextIdx: -1,
		}
		seeds = append(seeds, ms)
	}

	// Subsequent rounds: each match feeds winners to the next round
	prevRoundStart := 0
	prevRoundCount := slots / 2
	for r := 2; r <= rounds; r++ {
		matchesInRound := prevRoundCount / 2
		roundStart := len(seeds)
		for i := 0; i < matchesInRound; i++ {
			ms := matchSeed{
				round:        r,
				matchNumber:  i + 1,
				side:         models.SideWinners,
				seedA:        -1,
				seedB:        -1,
				nextIdx:      -1,
				loserNextIdx: -1,
			}
			seeds = append(seeds, ms)
			// Wire the two feeder matches to point here
			feederA := prevRoundStart + i*2
			feederB := prevRoundStart + i*2 + 1
			seeds[feederA].nextIdx = roundStart + i
			seeds[feederB].nextIdx = roundStart + i
		}
		prevRoundStart = roundStart
		prevRoundCount = matchesInRound
	}

	// Determine per-match status and participants for round 1
	for i := range seeds {
		if seeds[i].round != 1 {
			continue
		}
		sA := seeds[i].seedA
		sB := seeds[i].seedB
		hasA := sA < n
		hasB := sB < n
		if hasA && hasB {
			seeds[i].status = models.MatchReady
		} else if hasA && !hasB {
			// Bye for A
			seeds[i].status = models.MatchBye
		} else {
			seeds[i].status = models.MatchPending
		}
	}
	// Later rounds start as pending
	for i := range seeds {
		if seeds[i].round > 1 {
			seeds[i].status = models.MatchPending
		}
	}

	return twoPassInsert(ctx, tx, bracketID, seeds, participantIDs)
}

// twoPassInsert inserts all match seeds, collects IDs, then updates next_match_id links.
func twoPassInsert(ctx context.Context, tx pgx.Tx, bracketID int64, seeds []matchSeed, participantIDs []int64) error {
	ids := make([]int64, len(seeds))

	for i, ms := range seeds {
		var pA, pB *int64
		if ms.round == 1 {
			if ms.seedA >= 0 && ms.seedA < len(participantIDs) {
				id := participantIDs[ms.seedA]
				pA = &id
			}
			if ms.seedB >= 0 && ms.seedB < len(participantIDs) {
				id := participantIDs[ms.seedB]
				pB = &id
			}
		}

		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO matches (
				bracket_id, round, match_number, bracket_side,
				participant_a_id, participant_b_id, status
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`, bracketID, ms.round, ms.matchNumber, ms.side, pA, pB, ms.status).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert match r%d m%d: %w", ms.round, ms.matchNumber, err)
		}
		ids[i] = id
	}

	// Pass 2: update next_match_id links
	for i, ms := range seeds {
		if ms.nextIdx < 0 && ms.loserNextIdx < 0 {
			continue
		}
		var nextID *int64
		if ms.nextIdx >= 0 {
			id := ids[ms.nextIdx]
			nextID = &id
		}
		var loserNextID *int64
		if ms.loserNextIdx >= 0 {
			id := ids[ms.loserNextIdx]
			loserNextID = &id
		}
		if _, err := tx.Exec(ctx,
			`UPDATE matches SET next_match_id = $1, loser_next_match_id = $2 WHERE id = $3`,
			nextID, loserNextID, ids[i],
		); err != nil {
			return fmt.Errorf("link match %d: %w", ids[i], err)
		}
	}

	// Auto-advance bye matches
	return autoAdvanceByes(ctx, tx, bracketID, ids, seeds, participantIDs)
}

// autoAdvanceByes advances winners from bye matches immediately.
func autoAdvanceByes(ctx context.Context, tx pgx.Tx, bracketID int64, ids []int64, seeds []matchSeed, participantIDs []int64) error {
	for i, ms := range seeds {
		if ms.status != models.MatchBye {
			continue
		}
		// ParticipantA is the winner (only real participant)
		var winnerID int64
		if ms.seedA >= 0 && ms.seedA < len(participantIDs) {
			winnerID = participantIDs[ms.seedA]
		} else {
			continue
		}

		if _, err := tx.Exec(ctx, `
			UPDATE matches
			SET status = 'completed', winner_id = $1, played_at = NOW(), updated_at = NOW()
			WHERE id = $2
		`, winnerID, ids[i]); err != nil {
			return fmt.Errorf("complete bye match: %w", err)
		}

		// Place winner in next match
		if ms.nextIdx >= 0 {
			nextID := ids[ms.nextIdx]
			if err := placeParticipantInMatch(ctx, tx, nextID, winnerID); err != nil {
				return err
			}
		}
	}
	return nil
}

// placeParticipantInMatch fills participant_a_id or participant_b_id in the target match,
// then sets status to 'ready' when both slots are filled.
func placeParticipantInMatch(ctx context.Context, tx pgx.Tx, matchID, participantID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE matches
		SET
			participant_a_id = CASE
				WHEN participant_a_id IS NULL THEN $1
				ELSE participant_a_id
			END,
			participant_b_id = CASE
				WHEN participant_a_id IS NOT NULL AND participant_b_id IS NULL THEN $1
				ELSE participant_b_id
			END,
			status = CASE
				WHEN participant_a_id IS NOT NULL AND participant_b_id IS NULL THEN 'ready'::match_status
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND status != 'completed'
	`, participantID, matchID)
	if err != nil {
		return fmt.Errorf("place participant %d in match %d: %w", participantID, matchID, err)
	}
	return nil
}

// buildDoubleElim inserts all matches for a double-elimination bracket.
// Structure: Winners Bracket (WB) + Losers Bracket (LB) + Grand Final + optional Reset.
func buildDoubleElim(ctx context.Context, tx pgx.Tx, bracketID int64, participantIDs []int64) error {
	n := len(participantIDs)
	slots := nextPowerOf2(n)
	wbRounds := int(math.Log2(float64(slots))) // WB rounds including final

	// We'll collect all seeds and link them up, then do a single two-pass insert.
	var seeds []matchSeed

	// --- Winners Bracket ---
	// WB Round 1
	round1Pairs := standardSeedPairs(slots)
	wbRound1Start := len(seeds)
	for i, pair := range round1Pairs {
		ms := matchSeed{
			round:        1,
			matchNumber:  i + 1,
			side:         models.SideWinners,
			seedA:        pair[0],
			seedB:        pair[1],
			nextIdx:      -1,
			loserNextIdx: -1,
		}
		seeds = append(seeds, ms)
	}

	// WB Rounds 2..wbRounds
	wbRoundStarts := []int{wbRound1Start}
	prevStart := wbRound1Start
	prevCount := slots / 2
	for r := 2; r <= wbRounds; r++ {
		matchesInRound := prevCount / 2
		if matchesInRound == 0 {
			matchesInRound = 1
		}
		thisStart := len(seeds)
		wbRoundStarts = append(wbRoundStarts, thisStart)
		for i := 0; i < matchesInRound; i++ {
			ms := matchSeed{
				round:        r,
				matchNumber:  i + 1,
				side:         models.SideWinners,
				seedA:        -1,
				seedB:        -1,
				nextIdx:      -1,
				loserNextIdx: -1,
			}
			seeds = append(seeds, ms)
			// Wire feeders
			if r > 1 {
				feederA := prevStart + i*2
				feederB := prevStart + i*2 + 1
				if feederA < thisStart {
					seeds[feederA].nextIdx = thisStart + i
				}
				if feederB < thisStart {
					seeds[feederB].nextIdx = thisStart + i
				}
			}
		}
		prevStart = thisStart
		prevCount = matchesInRound
	}

	// WB Final index (last WB match)
	wbFinalIdx := len(seeds) - 1

	// --- Losers Bracket ---
	// For a 2-player DE (wbRounds==1), there is no losers bracket: the WB
	// match's loser is the de-facto LB finalist and goes straight to GF.
	// For wbRounds>=2 we build 2*(wbRounds-1) LB rounds.
	lbRoundCounts := computeLBRoundCounts(wbRounds)
	lbRounds := len(lbRoundCounts) - 1 // 1-indexed, index 0 unused; 0 when wbRounds==1
	lbRoundStarts := make([]int, lbRounds+1)

	for r := 1; r <= lbRounds; r++ {
		lbRoundStarts[r] = len(seeds)
		for i := 0; i < lbRoundCounts[r]; i++ {
			ms := matchSeed{
				round:        r,
				matchNumber:  i + 1,
				side:         models.SideLosers,
				seedA:        -1,
				seedB:        -1,
				nextIdx:      -1,
				loserNextIdx: -1,
				status:       models.MatchPending,
			}
			seeds = append(seeds, ms)
		}
	}

	// Wire LB internal progression.
	//
	// Standard DE losers-bracket structure (for any wbRounds >= 2):
	//   odd  round r: same size as previous; minor "continuation" step — each
	//                 match is a LB survivor against a new WB drop-in.
	//   even round r: halves the size; major step — the minor-round winners
	//                 pair up and cut the field in half.
	// So: odd → r+1 is 1-to-1 (sizes stay equal), even → r+1 pairs up (sizes
	// halve). The previous implementation had these swapped, which in bigger
	// brackets caused two different LB matches to both target the same next
	// slot — and the downstream WB-loser drop compounded the overfill so the
	// 3rd placement errored out with "next match already has both participants".
	for r := 1; r < lbRounds; r++ {
		for i := 0; i < lbRoundCounts[r]; i++ {
			src := lbRoundStarts[r] + i
			if r%2 == 0 {
				dst := lbRoundStarts[r+1] + i/2
				seeds[src].nextIdx = dst
			} else {
				dst := lbRoundStarts[r+1] + i
				seeds[src].nextIdx = dst
			}
		}
	}

	// Wire WB losers into LB drop-in rounds (only when LB exists).
	if lbRounds >= 1 {
		// WB round 1 losers → LB round 1. All slots/2 WB R1 matches feed
		// into LB R1 paired up: matches 0,1 → LB R1 match 0, etc.
		wbR1Count := slots / 2
		for i := 0; i < wbR1Count; i++ {
			wbMatchIdx := wbRound1Start + i
			lbMatchIdx := lbRoundStarts[1] + i/2
			if lbMatchIdx >= len(seeds) {
				break
			}
			seeds[wbMatchIdx].loserNextIdx = lbMatchIdx
		}

		// WB rounds 2..wbRounds losers drop into even LB rounds 1:1. Include
		// the WB final (wbR == wbRounds) — its loser is the LB final's WB-side
		// participant. Skipping it used to leave the LB final with only one
		// player and the bracket would hang there.
		for wbR := 2; wbR <= wbRounds; wbR++ {
			lbDropRound := 2 * (wbR - 1)
			if lbDropRound > lbRounds {
				break
			}
			wbRStart := wbRoundStarts[wbR-1]
			for i := 0; i < lbRoundCounts[lbDropRound]; i++ {
				if wbRStart+i < len(seeds) {
					seeds[wbRStart+i].loserNextIdx = lbRoundStarts[lbDropRound] + i
				}
			}
		}
	}

	// --- Grand Final ---
	gfIdx := len(seeds)
	seeds = append(seeds, matchSeed{
		round:        1,
		matchNumber:  1,
		side:         models.SideGrandFinal,
		seedA:        -1,
		seedB:        -1,
		nextIdx:      -1,
		loserNextIdx: -1,
		status:       models.MatchPending,
	})

	// Wire WB final -> GF. LB finalist -> GF when LB exists; for 2-player DE,
	// the WB match's loser is itself the "LB finalist" and advances to GF.
	seeds[wbFinalIdx].nextIdx = gfIdx
	if lbRounds >= 1 {
		seeds[lbRoundStarts[lbRounds]].nextIdx = gfIdx
	} else {
		seeds[wbFinalIdx].loserNextIdx = gfIdx
	}

	// --- Reset Match (pre-generated, status=pending_reset) ---
	resetIdx := len(seeds)
	seeds = append(seeds, matchSeed{
		round:        2,
		matchNumber:  1,
		side:         models.SideReset,
		seedA:        -1,
		seedB:        -1,
		nextIdx:      -1,
		loserNextIdx: -1,
		status:       models.MatchPendingReset,
	})

	// Grand Final loser route -> reset (LB finalist wins GF -> reset activates)
	seeds[gfIdx].loserNextIdx = resetIdx
	_ = resetIdx

	// Set status for all WB matches
	for i := range seeds {
		if seeds[i].side != models.SideWinners {
			continue
		}
		if seeds[i].round == 1 {
			sA := seeds[i].seedA
			sB := seeds[i].seedB
			hasA := sA >= 0 && sA < n
			hasB := sB >= 0 && sB < n
			if hasA && hasB {
				seeds[i].status = models.MatchReady
			} else if hasA && !hasB {
				seeds[i].status = models.MatchBye
			} else {
				seeds[i].status = models.MatchPending
			}
		} else {
			seeds[i].status = models.MatchPending
		}
	}

	return twoPassInsert(ctx, tx, bracketID, seeds, participantIDs)
}

// computeLBRoundCounts returns LB match counts per round (1-indexed, [0] unused).
// For an n-slot WB (n=slots), LB has 2*(log2(n)-1) rounds.
func computeLBRoundCounts(wbRounds int) []int {
	// wbRounds = log2(slots)
	// LB total rounds = 2*(wbRounds-1)
	lbRounds := 2 * (wbRounds - 1)
	counts := make([]int, lbRounds+1) // index 0 unused

	// LB round 1: slots/4 matches (receiving WB round 1 losers in pairs)
	// Each subsequent pair of LB rounds halves the count
	size := int(math.Pow(2, float64(wbRounds-2)))
	if size < 1 {
		size = 1
	}

	for r := 1; r <= lbRounds; r++ {
		counts[r] = size
		if r%2 == 0 {
			// After even rounds, halve for the next odd round
			size = size / 2
			if size < 1 {
				size = 1
			}
		}
	}

	return counts
}

// roundRobinPairs returns the complete schedule for an n-player round robin,
// using the circle method so each player plays at most once per round.
//
// Output: [][2]int where each pair is a 0-indexed (a, b) match. The slice is
// ordered by round: for n even there are n-1 rounds of n/2 matches each; for
// n odd each player sits out one round (bye), so n rounds of (n-1)/2 matches
// each. Every unordered pair {i, j} appears exactly once.
//
// Standard algorithm: fix slot 0, rotate the rest. For odd n, append a bye
// phantom at slot n (skipped when emitting pairs).
func roundRobinPairs(n int) [][2]int {
	if n < 2 {
		return nil
	}
	slots := make([]int, 0, n+1)
	for i := 0; i < n; i++ {
		slots = append(slots, i)
	}
	if n%2 == 1 {
		slots = append(slots, -1) // bye phantom
	}
	m := len(slots)
	half := m / 2
	rounds := m - 1
	pairs := make([][2]int, 0, rounds*half)
	for r := 0; r < rounds; r++ {
		for i := 0; i < half; i++ {
			a, b := slots[i], slots[m-1-i]
			if a == -1 || b == -1 {
				continue
			}
			// Order within pair is cosmetic; keep lower id first for tests.
			if a > b {
				a, b = b, a
			}
			pairs = append(pairs, [2]int{a, b})
		}
		// Rotate slots[1..m-1] clockwise; slots[0] is the fixed vertex.
		last := slots[m-1]
		for i := m - 1; i > 1; i-- {
			slots[i] = slots[i-1]
		}
		slots[1] = last
	}
	return pairs
}

// buildRoundRobin inserts one match per unordered pair of participants, all
// status='ready' (no byes in the SE sense — every match has two players).
// match.round groups simultaneous matches; match_number orders within the
// round. There's no next_match_id wiring — round robin has no advancement.
func buildRoundRobin(ctx context.Context, tx pgx.Tx, bracketID int64, participantIDs []int64) error {
	n := len(participantIDs)
	if n < 2 {
		return fmt.Errorf("round robin needs at least 2 participants, got %d", n)
	}
	pairs := roundRobinPairs(n)

	// Figure out how many matches are in each round so we can number them.
	perRound := n / 2
	if n%2 == 1 {
		perRound = (n - 1) / 2
	}

	for i, pair := range pairs {
		round := i/perRound + 1
		matchNumber := i%perRound + 1
		a := participantIDs[pair[0]]
		b := participantIDs[pair[1]]
		if _, err := tx.Exec(ctx, `
			INSERT INTO matches (
				bracket_id, round, match_number, bracket_side,
				participant_a_id, participant_b_id, status
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, bracketID, round, matchNumber, models.SideWinners, a, b, models.MatchReady); err != nil {
			return fmt.Errorf("insert RR match r%d m%d: %w", round, matchNumber, err)
		}
	}
	return nil
}

// LoadMatchesForBracket returns all non-pending-reset matches for a bracket, with participant names.
func LoadMatchesForBracket(ctx context.Context, pool *pgxpool.Pool, bracketID int64) ([]*models.Match, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			m.id, m.bracket_id, m.round, m.match_number, m.bracket_side,
			m.participant_a_id, m.participant_b_id,
			m.winner_id, m.loser_id,
			m.score_a, m.score_b, m.score_display,
			m.status, m.next_match_id, m.loser_next_match_id,
			m.played_at, m.created_at, m.updated_at,
			COALESCE(pa_u.username, pa_t.name, '') AS participant_a_name,
			COALESCE(pb_u.username, pb_t.name, '') AS participant_b_name,
			COALESCE(w_u.username, w_t.name, '') AS winner_name,
			COALESCE(pa_u.discord_id, '') AS pa_discord_id,
			COALESCE(pa_u.avatar, '')     AS pa_avatar,
			COALESCE(pb_u.discord_id, '') AS pb_discord_id,
			COALESCE(pb_u.avatar, '')     AS pb_avatar
		FROM matches m
		LEFT JOIN participants pa ON pa.id = m.participant_a_id
		LEFT JOIN users pa_u ON pa_u.id = pa.user_id
		LEFT JOIN teams pa_t ON pa_t.id = pa.team_id
		LEFT JOIN participants pb ON pb.id = m.participant_b_id
		LEFT JOIN users pb_u ON pb_u.id = pb.user_id
		LEFT JOIN teams pb_t ON pb_t.id = pb.team_id
		LEFT JOIN participants wp ON wp.id = m.winner_id
		LEFT JOIN users w_u ON w_u.id = wp.user_id
		LEFT JOIN teams w_t ON w_t.id = wp.team_id
		WHERE m.bracket_id = $1 AND m.status != 'pending_reset'
		ORDER BY m.bracket_side, m.round, m.match_number
	`, bracketID)
	if err != nil {
		return nil, fmt.Errorf("load matches: %w", err)
	}
	defer rows.Close()

	var matches []*models.Match
	for rows.Next() {
		m := &models.Match{}
		var paDiscordID, paAvatar, pbDiscordID, pbAvatar string
		if err := rows.Scan(
			&m.ID, &m.BracketID, &m.Round, &m.MatchNumber, &m.BracketSide,
			&m.ParticipantAID, &m.ParticipantBID,
			&m.WinnerID, &m.LoserID,
			&m.ScoreA, &m.ScoreB, &m.ScoreDisplay,
			&m.Status, &m.NextMatchID, &m.LoserNextMatchID,
			&m.PlayedAt, &m.CreatedAt, &m.UpdatedAt,
			&m.ParticipantAName, &m.ParticipantBName, &m.WinnerName,
			&paDiscordID, &paAvatar, &pbDiscordID, &pbAvatar,
		); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		m.ParticipantAAvatarURL = models.AvatarURLFor(paDiscordID, paAvatar, m.ParticipantAName)
		m.ParticipantBAvatarURL = models.AvatarURLFor(pbDiscordID, pbAvatar, m.ParticipantBName)
		matches = append(matches, m)
	}
	return matches, nil
}
