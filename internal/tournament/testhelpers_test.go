package tournament_test

import "time"

func testTimeout() time.Duration  { return 2 * time.Second }
func tickInterval() time.Duration { return 10 * time.Millisecond }
