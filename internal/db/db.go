package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	pgdriver "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq" // postgres driver for database/sql, used by MigrateToSchema
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func Migrate(databaseURL string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// MigrateToSchema runs migrations into a specific, already-created Postgres
// schema. Used by the integration test harness to isolate each test.
//
// migrate/v4's postgres driver keys migration DDL execution to the session's
// search_path, not to Config.SchemaName (SchemaName only affects the
// schema_migrations bookkeeping table). Encoding search_path as a libpq
// `options=-c search_path=X` startup param ensures every physical connection
// the sql.DB hands out starts with the right schema on top of its path.
func MigrateToSchema(databaseURL, schema string) error {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse db url: %w", err)
	}
	q := u.Query()
	q.Set("options", fmt.Sprintf("-c search_path=%s", schema))
	u.RawQuery = q.Encode()

	sqlDB, err := sql.Open("postgres", u.String())
	if err != nil {
		return fmt.Errorf("open sql db: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	driver, err := pgdriver.WithInstance(sqlDB, &pgdriver.Config{
		SchemaName:      schema,
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("create postgres driver: %w", err)
	}

	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	// Don't m.Close() — the driver would close sqlDB, but we manage that above.

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
