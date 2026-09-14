package repository

import (
	"context"
	"database/sql"
	"fmt"

	"url-shortener/internal/database"
	"url-shortener/internal/model"
)

// URLRepository defines all persistence operations for shortened URLs.
type URLRepository interface {
	Create(ctx context.Context, url *model.URL) error
	GetByCode(ctx context.Context, code string) (*model.URL, error)
	GetByUserID(ctx context.Context, userID string) ([]*model.URL, error)
	ClaimURL(ctx context.Context, code, userID string) error
	IncrementClickCount(ctx context.Context, code string) error
	BulkIncrementClicks(ctx context.Context, clickMap map[string]int) error
	AliasExists(ctx context.Context, code string) (bool, error)
}

type postgresURLRepo struct {
	db *database.DB
}

// NewURLRepository returns a Postgres-backed URLRepository.
func NewURLRepository(db *database.DB) URLRepository {
	return &postgresURLRepo{db: db}
}

func (r *postgresURLRepo) Create(ctx context.Context, url *model.URL) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO urls (id, short_code, long_url, is_custom, expires_at, user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, url.ID, url.ShortCode, url.LongURL, url.IsCustom, url.ExpiresAt, url.UserID)
	if err != nil {
		return fmt.Errorf("url_repo.Create: %w", err)
	}
	return nil
}

func (r *postgresURLRepo) GetByCode(ctx context.Context, code string) (*model.URL, error) {
	var u model.URL
	err := r.db.QueryRowContext(ctx, `
		SELECT id, short_code, long_url, is_custom, created_at, expires_at, user_id, click_count
		FROM urls WHERE short_code = $1
	`, code).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.IsCustom,
		&u.CreatedAt, &u.ExpiresAt, &u.UserID, &u.ClickCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("url_repo.GetByCode: %w", err)
	}
	return &u, nil
}

func (r *postgresURLRepo) GetByUserID(ctx context.Context, userID string) ([]*model.URL, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, short_code, long_url, is_custom, created_at, expires_at, user_id, click_count
		FROM urls WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 100
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("url_repo.GetByUserID: %w", err)
	}
	defer rows.Close()

	var urls []*model.URL
	for rows.Next() {
		var u model.URL
		if err := rows.Scan(
			&u.ID, &u.ShortCode, &u.LongURL, &u.IsCustom,
			&u.CreatedAt, &u.ExpiresAt, &u.UserID, &u.ClickCount,
		); err != nil {
			return nil, fmt.Errorf("url_repo.GetByUserID scan: %w", err)
		}
		urls = append(urls, &u)
	}
	return urls, nil
}

// ClaimURL sets the owner of an anonymous link. Silently no-ops if already owned.
func (r *postgresURLRepo) ClaimURL(ctx context.Context, code, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE urls SET user_id = $1 WHERE short_code = $2 AND user_id IS NULL`,
		userID, code,
	)
	return err
}

// IncrementClickCount bumps the denormalised counter on the urls row.
func (r *postgresURLRepo) IncrementClickCount(ctx context.Context, code string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE urls SET click_count = click_count + 1 WHERE short_code = $1`, code,
	)
	return err
}

// BulkIncrementClicks performs a single batched UPDATE for multiple short codes.
func (r *postgresURLRepo) BulkIncrementClicks(ctx context.Context, clickMap map[string]int) error {
	if len(clickMap) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("url_repo.BulkIncrementClicks begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE urls SET click_count = click_count + $1 WHERE short_code = $2`)
	if err != nil {
		return fmt.Errorf("url_repo.BulkIncrementClicks prepare: %w", err)
	}
	defer stmt.Close()

	for code, count := range clickMap {
		if _, err := stmt.ExecContext(ctx, count, code); err != nil {
			return fmt.Errorf("url_repo.BulkIncrementClicks exec: %w", err)
		}
	}

	return tx.Commit()
}

// AliasExists reports whether a short code is already taken.
func (r *postgresURLRepo) AliasExists(ctx context.Context, code string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM urls WHERE short_code = $1`, code).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
