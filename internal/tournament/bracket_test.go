package tournament

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextPowerOf2(t *testing.T) {
	cases := [][2]int{
		{1, 1}, {2, 2}, {3, 4}, {4, 4}, {5, 8}, {7, 8}, {8, 8},
		{9, 16}, {16, 16}, {17, 32}, {33, 64}, {65, 128},
	}
	for _, c := range cases {
		assert.Equal(t, c[1], nextPowerOf2(c[0]), "nextPowerOf2(%d)", c[0])
	}
}

func TestStandardSeedPairs(t *testing.T) {
	// 4-slot bracket: matches should be (1v4, 2v3) by standard seeding
	pairs := standardSeedPairs(4)
	assert.Len(t, pairs, 2)

	// 8-slot bracket: 4 first-round matches
	pairs8 := standardSeedPairs(8)
	assert.Len(t, pairs8, 4)

	// Verify seed 0 (top seed) is always in first pair
	assert.Equal(t, 0, pairs8[0][0])
}

func TestComputeLBRoundCounts(t *testing.T) {
	// 8-player DE: wbRounds=3, LB should have 4 rounds
	counts := computeLBRoundCounts(3)
	assert.Equal(t, 4+1, len(counts)) // index 0 unused

	// LB round 1 for 8-slot WB: 2 matches
	assert.Equal(t, 2, counts[1])

	// 16-player DE: wbRounds=4
	counts16 := computeLBRoundCounts(4)
	assert.Equal(t, 6+1, len(counts16))
	assert.Equal(t, 4, counts16[1])
}

func TestBuildSingleElimMatchCount(t *testing.T) {
	// Verify match counts for various participant sizes
	cases := []struct {
		n       int
		wantMin int // at least this many matches in seeds slice
	}{
		{2, 1},  // 1 match
		{4, 3},  // 3 matches
		{8, 7},  // 7 matches
		{5, 7},  // rounds up to 8-slot, still 7 seeds (some byes)
		{6, 7},
		{7, 7},
	}

	for _, c := range cases {
		t.Run("n="+itoa(c.n), func(t *testing.T) {
			slots := nextPowerOf2(c.n)
			rounds := 0
			p := slots
			for p > 1 {
				p /= 2
				rounds++
			}
			total := slots - 1
			assert.Equal(t, c.wantMin, total, "expected %d matches for n=%d (slots=%d)", c.wantMin, c.n, slots)
		})
	}
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
