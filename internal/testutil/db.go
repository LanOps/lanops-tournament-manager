// Package testutil provides helpers for integration tests.
// Tests that use DB helpers require TEST_DATABASE_URL to be set.
// Each test gets an isolated schema that is dropped on cleanup.
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/db"
)

// NewDB creates a fresh Postgres schema for one test and returns a pool pointing at it.
// The schema is dropped when t.Cleanup runs.
// Skips the test if TEST_DATABASE_URL is not set.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_%s", sanitize(t.Name()))

	// Connect to base DB to create the schema
	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect to base DB: %v", err)
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %q", schema)); err != nil {
		t.Fatalf("create schema %q: %v", schema, err)
	}

	// Point the test pool at the new schema via search_path
	var testURL string
	if containsQuery(baseURL) {
		testURL = baseURL + "&search_path=" + schema
	} else {
		testURL = baseURL + "?search_path=" + schema
	}

	testPool, err := db.Connect(ctx, testURL)
	if err != nil {
		t.Fatalf("connect to test schema: %v", err)
	}

	// Run migrations inside this schema
	if err := db.Migrate(testURL); err != nil {
		testPool.Close()
		t.Fatalf("migrate test schema: %v", err)
	}

	t.Cleanup(func() {
		testPool.Close()
		cleanupPool, _ := pgxpool.New(ctx, baseURL)
		if cleanupPool != nil {
			cleanupPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
			cleanupPool.Close()
		}
	})

	return testPool
}

// sanitize converts a test name to a valid Postgres identifier fragment.
func sanitize(name string) string {
	out := make([]byte, 0, len(name))
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			out = append(out, byte(c))
		} else {
			out = append(out, '_')
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return string(out)
}

func containsQuery(url string) bool {
	for _, c := range url {
		if c == '?' {
			return true
		}
	}
	return false
}
