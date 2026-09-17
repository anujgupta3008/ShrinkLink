package repository

import (
	"context"
	"fmt"
	"sync"

	"url-shortener/internal/database"
	"url-shortener/internal/model"
)

// AnalyticsRepository defines persistence operations for click analytics.
type AnalyticsRepository interface {
	BulkInsert(ctx context.Context, records []model.AnalyticsRecord) error
	GetByCode(ctx context.Context, code string) (*model.URLAnalyticsResponse, error)
}

type postgresAnalyticsRepo struct {
	db *database.DB
}

// NewAnalyticsRepository returns a Postgres-backed AnalyticsRepository.
func NewAnalyticsRepository(db *database.DB) AnalyticsRepository {
	return &postgresAnalyticsRepo{db: db}
}

// BulkInsert writes a batch of analytics records in a single transaction.
func (r *postgresAnalyticsRepo) BulkInsert(ctx context.Context, records []model.AnalyticsRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics_repo.BulkInsert begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO analytics (short_code, click_time, ip_address, user_agent, referrer, country, browser, os, device)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("analytics_repo.BulkInsert prepare: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		if _, err := stmt.ExecContext(ctx,
			rec.ShortCode, rec.ClickTime, rec.IPAddress,
			rec.UserAgent, rec.Referrer, rec.Country, rec.Browser, rec.OS, rec.Device,
		); err != nil {
			return fmt.Errorf("analytics_repo.BulkInsert exec: %w", err)
		}
	}
	return tx.Commit()
}

// GetByCode runs 6 analytics queries concurrently and returns the aggregated result.
func (r *postgresAnalyticsRepo) GetByCode(ctx context.Context, code string) (*model.URLAnalyticsResponse, error) {
	var (
		wg             sync.WaitGroup
		mu             sync.Mutex
		firstErr       error
		totalClicks    int
		clicksOverTime []model.ClickStats
		referrers      []model.StatBreakdown
		browsers       []model.StatBreakdown
		osList         []model.StatBreakdown
		countries      []model.StatBreakdown
	)

	setErr := func(e error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = e
		}
		mu.Unlock()
	}

	// Q1: Total clicks
	wg.Add(1)
	go func() {
		defer wg.Done()
		var n int
		if e := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics WHERE short_code = $1`, code).Scan(&n); e != nil {
			setErr(e)
			return
		}
		mu.Lock()
		totalClicks = n
		mu.Unlock()
	}()

	// Q2: Clicks over time (last 7 days, hourly)
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT TO_CHAR(click_time, 'YYYY-MM-DD HH24:00') as period, COUNT(*) as clicks
			FROM analytics
			WHERE short_code = $1 AND click_time >= NOW() - INTERVAL '7 days'
			GROUP BY period ORDER BY period ASC
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.ClickStats
		for rows.Next() {
			var cs model.ClickStats
			if e := rows.Scan(&cs.Period, &cs.Clicks); e == nil {
				result = append(result, cs)
			}
		}
		mu.Lock()
		clicksOverTime = result
		mu.Unlock()
	}()

	// Q3: Top referrers
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(referrer, ''), 'Direct') as name, COUNT(*) as count
			FROM analytics WHERE short_code = $1
			GROUP BY name ORDER BY count DESC LIMIT 5
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.StatBreakdown
		for rows.Next() {
			var sb model.StatBreakdown
			if e := rows.Scan(&sb.Name, &sb.Count); e == nil {
				result = append(result, sb)
			}
		}
		mu.Lock()
		referrers = result
		mu.Unlock()
	}()

	// Q4: Browser breakdown
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(browser, ''), 'Unknown') as name, COUNT(*) as count
			FROM analytics WHERE short_code = $1
			GROUP BY name ORDER BY count DESC LIMIT 5
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.StatBreakdown
		for rows.Next() {
			var sb model.StatBreakdown
			if e := rows.Scan(&sb.Name, &sb.Count); e == nil {
				result = append(result, sb)
			}
		}
		mu.Lock()
		browsers = result
		mu.Unlock()
	}()

	// Q5: OS breakdown
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(os, ''), 'Unknown') as name, COUNT(*) as count
			FROM analytics WHERE short_code = $1
			GROUP BY name ORDER BY count DESC LIMIT 5
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.StatBreakdown
		for rows.Next() {
			var sb model.StatBreakdown
			if e := rows.Scan(&sb.Name, &sb.Count); e == nil {
				result = append(result, sb)
			}
		}
		mu.Lock()
		osList = result
		mu.Unlock()
	}()

	// Q6: Country breakdown
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(country, ''), 'Unknown') as name, COUNT(*) as count
			FROM analytics WHERE short_code = $1
			GROUP BY name ORDER BY count DESC LIMIT 5
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.StatBreakdown
		for rows.Next() {
			var sb model.StatBreakdown
			if e := rows.Scan(&sb.Name, &sb.Count); e == nil {
				result = append(result, sb)
			}
		}
		mu.Lock()
		countries = result
		mu.Unlock()
	}()

	// Q7: Device breakdown (Level 9)
	var devices []model.StatBreakdown
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, e := r.db.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(device, ''), 'Unknown') as name, COUNT(*) as count
			FROM analytics WHERE short_code = $1
			GROUP BY name ORDER BY count DESC LIMIT 5
		`, code)
		if e != nil {
			setErr(e)
			return
		}
		defer rows.Close()
		var result []model.StatBreakdown
		for rows.Next() {
			var sb model.StatBreakdown
			if e := rows.Scan(&sb.Name, &sb.Count); e == nil {
				result = append(result, sb)
			}
		}
		mu.Lock()
		devices = result
		mu.Unlock()
	}()

	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("analytics_repo.GetByCode: %w", firstErr)
	}

	return &model.URLAnalyticsResponse{
		ShortCode:      code,
		TotalClicks:    totalClicks,
		ClicksOverTime: clicksOverTime,
		Referrers:      referrers,
		Browsers:       browsers,
		OS:             osList,
		Countries:      countries,
		Devices:        devices,
	}, nil
}
