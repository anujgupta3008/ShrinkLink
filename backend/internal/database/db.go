package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

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

	// Retry database connection as it might still be starting up in Docker Compose
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break
			}
		}
		log.Printf("Waiting for database to be ready (attempt %d/10)...", i+1)
		time.Sleep(3 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	database := &DB{db}
	if err := database.runMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return database, nil
}

func (db *DB) runMigrations() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS urls (
			id BIGINT PRIMARY KEY,
			short_code VARCHAR(10) UNIQUE NOT NULL,
			long_url TEXT NOT NULL,
			is_custom BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP WITH TIME ZONE
		);`,
		`CREATE TABLE IF NOT EXISTS analytics (
			id BIGSERIAL PRIMARY KEY,
			short_code VARCHAR(10) NOT NULL,
			click_time TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			ip_address VARCHAR(45),
			user_agent TEXT,
			referrer TEXT,
			country VARCHAR(100),
			browser VARCHAR(50),
			os VARCHAR(50)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_urls_short_code ON urls(short_code);`,
		`CREATE INDEX IF NOT EXISTS idx_analytics_short_code ON analytics(short_code);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	log.Println("Database migrations completed successfully")
	return nil
}
