package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	// database/sql driver for Postgres. Registered as "pgx".
	_ "github.com/jackc/pgx/v5/stdlib"
)

// InitUserDB opens the Supabase Postgres connection that holds all durable user
// data. The DSN comes from DATABASE_URL.
//
// Supabase exposes several connection endpoints and the choice matters:
//
//   - Direct (:5432) — one real Postgres backend per connection. IPv6-only on
//     the free tier, which most container platforms cannot reach.
//   - Session pooler (:5432 via the pooler host) — behaves like a direct
//     connection, supports prepared statements. Good default for a long-lived
//     Go process.
//   - Transaction pooler (:6543) — hands a different backend to each
//     transaction, so server-side prepared statements break. If you use this,
//     the DSN must carry default_query_exec_mode=simple_protocol.
//
// We warn rather than fail on the transaction pooler, since the correct setting
// is expressible in the DSN itself.
func InitUserDB(ctx context.Context) (*sql.DB, error) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set: the API cannot start without the Supabase Postgres connection string")
	}

	if strings.Contains(dsn, ":6543") && !strings.Contains(dsn, "default_query_exec_mode") {
		return nil, fmt.Errorf(
			"DATABASE_URL points at the Supabase transaction pooler (:6543) but does not set default_query_exec_mode=simple_protocol; " +
				"prepared statements will fail intermittently. Append it to the DSN, or use the session pooler instead")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Supabase enforces per-project connection ceilings and a pooler in front of
	// them. An unbounded Go pool will exhaust that budget as soon as more than
	// one container is running, so keep this conservative and let it be tuned.
	db.SetMaxOpenConns(envInt("SCHOLIA_DB_MAX_OPEN_CONNS", 10))
	db.SetMaxIdleConns(envInt("SCHOLIA_DB_MAX_IDLE_CONNS", 5))
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	return db, nil
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
