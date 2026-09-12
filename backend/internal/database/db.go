package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"url-shortener/internal/config"
)

type DB struct {
	*sql.DB
}

func NewDB(cfg *config.Config) (*DB, error) {
	var connStr string
	if cfg.DBPassword != "" {
		connStr = fmt.Sprintf("host=%s port=%s user=%s password='%s' dbname=%s sslmode=%s",
			cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)
	} else {
		connStr = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s",
			cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBName, cfg.DBSSLMode)
	}

	var db *sql.DB
	var err error

	// Retry database connection — it might still be starting up in Docker Compose.
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break
			}
		}
		slog.Info("Waiting for database to be ready", "attempt", i+1, "max", 10)
		time.Sleep(3 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Connection pool tuning — critical for 10k concurrent users.
	// MaxOpenConns prevents overwhelming Postgres; MaxIdleConns keeps warm connections ready;
	// ConnMaxLifetime prevents stale connections on managed DBs that rotate credentials.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("Database connected", "host", cfg.DBHost, "db", cfg.DBName)
	return &DB{db}, nil
}

// RunMigrations applies all pending up migrations from the given directory.
// Uses golang-migrate for reversible, versioned SQL files (000001_*.up.sql / 000001_*.down.sql).
func (db *DB) RunMigrations(migrationsPath string) error {
	driver, err := postgres.WithInstance(db.DB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", migrationsPath),
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize migrations: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, _ := m.Version()
	slog.Info("Database migrations applied", "version", version, "dirty", dirty)
	return nil
}

