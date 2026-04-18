// Package testutil provides helpers for integration tests.
// Tests that use DB helpers require TEST_DATABASE_URL to be set.
// Each test gets an isolated schema that is dropped on cleanup.
package testutil

import (
	"context"
	"fmt"
	"net/url"
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

	// Point the test pool at the new schema via libpq-compatible options.
	// Encoding search_path in `options=-c search_path=X` is honored by both
	// lib/pq and pgx (they treat unknown URL params as connection startup
	// params, and Postgres itself interprets `options=-c KEY=VAL` as SET).
	testURL, err := withSearchPath(baseURL, schema)
	if err != nil {
		t.Fatalf("build test url: %v", err)
	}

	testPool, err := db.Connect(ctx, testURL)
	if err != nil {
		t.Fatalf("connect to test schema: %v", err)
	}

	// Run migrations inside this schema. MigrateToSchema sets search_path
	// on the sql.DB connection migrate uses, so migration DDL lands here.
	if err := db.MigrateToSchema(baseURL, schema); err != nil {
		testPool.Close()
		t.Fatalf("migrate test schema: %v", err)
	}

	t.Cleanup(func() {
		testPool.Close()
		cleanupPool, _ := pgxpool.New(ctx, baseURL)
		if cleanupPool != nil {
			_, _ = cleanupPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
			cleanupPool.Close()
		}
	})

	return testPool
}

// sanitize converts a test name to a valid Postgres identifier fragment.
// Output is always lowercase so the schema name survives a round trip through
// `options=-c search_path=X`, which Postgres downcases unquoted identifiers in.
// (Quoting the value inside libpq's `options` string is possible but gnarly;
// lowercase sidesteps the whole issue.)
func sanitize(name string) string {
	out := make([]byte, 0, len(name))
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, byte(c))
		case c >= 'A' && c <= 'Z':
			out = append(out, byte(c-'A'+'a'))
		case c >= '0' && c <= '9':
			out = append(out, byte(c))
		default:
			out = append(out, '_')
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return string(out)
}

// withSearchPath returns the baseURL with `options=-c search_path=schema`
// merged into the query string. Overrides any existing `options` param.
func withSearchPath(baseURL, schema string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("options", fmt.Sprintf("-c search_path=%s", schema))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
