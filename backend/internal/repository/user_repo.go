package repository

import (
	"context"
	"database/sql"
	"fmt"

	"url-shortener/internal/database"
	"url-shortener/internal/model"
)

// UserRepository defines all persistence operations for users.
type UserRepository interface {
	Upsert(ctx context.Context, firebaseUID, email string) (*model.User, error)
	GetByFirebaseUID(ctx context.Context, firebaseUID string) (*model.User, error)
	GetByID(ctx context.Context, id string) (*model.User, error)
	UpdatePlan(ctx context.Context, userID string, plan model.Plan) error
}

type postgresUserRepo struct {
	db *database.DB
}

// NewUserRepository returns a Postgres-backed UserRepository.
func NewUserRepository(db *database.DB) UserRepository {
	return &postgresUserRepo{db: db}
}

// Upsert creates a user on first login, or updates their email if they already exist.
// Called every time a valid Firebase ID token is verified.
func (r *postgresUserRepo) Upsert(ctx context.Context, firebaseUID, email string) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (firebase_uid, email)
		VALUES ($1, $2)
		ON CONFLICT (firebase_uid) DO UPDATE SET email = EXCLUDED.email
		RETURNING id, firebase_uid, email, plan, created_at
	`, firebaseUID, email).Scan(&u.ID, &u.FirebaseUID, &u.Email, &u.Plan, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("user_repo.Upsert: %w", err)
	}
	return &u, nil
}

func (r *postgresUserRepo) GetByFirebaseUID(ctx context.Context, firebaseUID string) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, firebase_uid, email, plan, created_at FROM users WHERE firebase_uid = $1
	`, firebaseUID).Scan(&u.ID, &u.FirebaseUID, &u.Email, &u.Plan, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user_repo.GetByFirebaseUID: %w", err)
	}
	return &u, nil
}

func (r *postgresUserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, firebase_uid, email, plan, created_at FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.FirebaseUID, &u.Email, &u.Plan, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user_repo.GetByID: %w", err)
	}
	return &u, nil
}

// UpdatePlan changes a user's subscription plan (free ↔ pro).
func (r *postgresUserRepo) UpdatePlan(ctx context.Context, userID string, plan model.Plan) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET plan = $1 WHERE id = $2`, plan, userID,
	)
	return err
}
