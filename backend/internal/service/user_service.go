package service

import (
	"context"
	"fmt"

	"url-shortener/internal/model"
	"url-shortener/internal/repository"
)

// UserService handles user creation and profile retrieval.
type UserService struct {
	userRepo repository.UserRepository
	quota    *QuotaService
}

func NewUserService(userRepo repository.UserRepository, quota *QuotaService) *UserService {
	return &UserService{userRepo: userRepo, quota: quota}
}

// GetOrCreate upserts the user record from a verified Firebase token.
// Called on every authenticated request to keep email in sync.
func (s *UserService) GetOrCreate(ctx context.Context, firebaseUID, email string) (*model.User, error) {
	user, err := s.userRepo.Upsert(ctx, firebaseUID, email)
	if err != nil {
		return nil, fmt.Errorf("user_service.GetOrCreate: %w", err)
	}
	return user, nil
}

// GetProfile returns a user's full profile including current-month quota usage.
func (s *UserService) GetProfile(ctx context.Context, userID string, plan model.Plan) (*model.MeResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user_service.GetProfile: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	used, limit, err := s.quota.GetUsage(ctx, userID, plan)
	if err != nil {
		return nil, err
	}

	return &model.MeResponse{
		UserID: user.ID,
		Email:  user.Email,
		Plan:   user.Plan,
		Usage:  model.UsageInfo{Used: used, Limit: limit},
	}, nil
}

// UpgradePlan promotes a user to Pro (or demotes back to Free).
func (s *UserService) UpgradePlan(ctx context.Context, userID string, plan model.Plan) error {
	return s.userRepo.UpdatePlan(ctx, userID, plan)
}
