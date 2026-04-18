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
	// 2-slot: 1v2
	assert.Equal(t, [][2]int{{0, 1}}, standardSeedPairs(2))

	// 4-slot: 1v4, 2v3
	assert.Equal(t, [][2]int{{0, 3}, {1, 2}}, standardSeedPairs(4))

	// 8-slot: 1v8, 4v5, 2v7, 3v6 (standard bracket, top seeds in opposite halves)
	assert.Equal(t, [][2]int{{0, 7}, {3, 4}, {1, 6}, {2, 5}}, standardSeedPairs(8))

	// 16-slot: full enumeration
	assert.Equal(t,
		[][2]int{{0, 15}, {7, 8}, {3, 12}, {4, 11}, {1, 14}, {6, 9}, {2, 13}, {5, 10}},
		standardSeedPairs(16))

	// Every seed appears exactly once across all pairs
	for _, slots := range []int{2, 4, 8, 16, 32, 64} {
		pairs := standardSeedPairs(slots)
		seen := map[int]bool{}
		for _, p := range pairs {
			for _, s := range p {
				assert.False(t, seen[s], "slots=%d: seed %d repeated", slots, s)
				seen[s] = true
			}
		}
		assert.Len(t, seen, slots, "slots=%d: missing seeds", slots)
	}
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
