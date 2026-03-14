package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
)

// AuthService handles authentication logic
type AuthService struct {
	userRepo      *repository.UserRepository
	sessionRepo   *repository.SessionRepository
	sessionSecret string
}

// NewAuthService creates a new auth service
func NewAuthService(userRepo *repository.UserRepository, sessionRepo *repository.SessionRepository, sessionSecret string) *AuthService {
	return &AuthService{
		userRepo:      userRepo,
		sessionRepo:   sessionRepo,
		sessionSecret: sessionSecret,
	}
}

// Register creates a new user account
func (s *AuthService) Register(ctx context.Context, req models.RegisterRequest) (*models.AuthResponse, error) {
	// Check if email already exists
	_, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err == nil {
		return nil, ErrEmailTaken
	}
	if !errors.Is(err, repository.ErrUserNotFound) {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Create user
	user := &models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Preferences:  models.NewUserPreferences(),
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrEmailExists) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	// Create session
	token, err := s.createSession(ctx, user.ID, "", "")
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

// Login authenticates a user and creates a session
func (s *AuthService) Login(ctx context.Context, req models.LoginRequest, ipAddress, userAgent string) (*models.AuthResponse, error) {
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if errors.Is(err, repository.ErrUserNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Create session
	token, err := s.createSession(ctx, user.ID, ipAddress, userAgent)
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

// Logout invalidates a session
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.sessionRepo.DeleteByToken(ctx, token)
}

// ValidateSession checks if a session token is valid
func (s *AuthService) ValidateSession(ctx context.Context, token string) (*models.User, *models.Session, error) {
	session, err := s.sessionRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}

	return user, session, nil
}

// ChangePassword updates a user's password
func (s *AuthService) ChangePassword(ctx context.Context, userID string, currentPassword, newPassword string) error {
	user, err := s.userRepo.FindByEmail(ctx, userID)
	if err != nil {
		return err
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.userRepo.UpdatePassword(ctx, user.ID, string(hashedPassword))
}

// createSession generates a new session token
func (s *AuthService) createSession(ctx context.Context, userID interface{}, ipAddress, userAgent string) (string, error) {
	token, err := generateToken(32)
	if err != nil {
		return "", err
	}

	// Convert userID to primitive.ObjectID
	var oid primitive.ObjectID
	switch v := userID.(type) {
	case primitive.ObjectID:
		oid = v
	case string:
		var err error
		oid, err = primitive.ObjectIDFromHex(v)
		if err != nil {
			return "", fmt.Errorf("invalid user ID: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported user ID type: %T", userID)
	}

	session := &models.Session{
		UserID:    oid,
		Token:     token,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour), // 7 days
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return "", err
	}

	return token, nil
}

// generateToken creates a random hex token
func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
