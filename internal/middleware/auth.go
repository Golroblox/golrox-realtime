package middleware

import (
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golroblox/golrox-realtime/internal/config"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
)

// JWTClaims represents the JWT payload structure
type JWTClaims struct {
	ID    string `json:"userId"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// AuthResult holds authentication result
type AuthResult struct {
	UserID    *string
	Email     *string
	Anonymous bool
}

// Authenticator handles JWT authentication for WebSocket connections
type Authenticator struct {
	cfg    *config.Config
	logger *logger.Logger
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(cfg *config.Config, logger *logger.Logger) *Authenticator {
	return &Authenticator{
		cfg:    cfg,
		logger: logger,
	}
}

// Authenticate validates the token and returns auth result
// - No token: anonymous connection (allowed)
// - Valid token: authenticated user
// - Invalid token: error (connection should be rejected)
func (a *Authenticator) Authenticate(token string) (*AuthResult, error) {
	// No token = anonymous connection (allowed)
	if token == "" {
		a.logger.Debugw("Anonymous connection (no token)")
		return &AuthResult{
			UserID:    nil,
			Email:     nil,
			Anonymous: true,
		}, nil
	}

	// Remove "Bearer " prefix if present
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimSpace(token)

	// If JWT secret not configured, reject all token-based auth
	if a.cfg.JWTSecret == "" {
		a.logger.Warnw("JWT secret not configured, rejecting token auth")
		return nil, errors.New("authentication failed: server not configured for token auth")
	}

	// Parse and validate token
	claims := &JWTClaims{}
	parsedToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.cfg.JWTSecret), nil
	})

	if err != nil {
		a.logger.Warnw("JWT validation failed", "error", err.Error())
		return nil, errors.New("authentication failed: invalid token")
	}

	if !parsedToken.Valid {
		return nil, errors.New("authentication failed: token invalid")
	}

	if claims.ID == "" {
		a.logger.Warnw("JWT token missing user ID claim")
		return nil, errors.New("authentication failed: missing user id")
	}

	// Token is valid, return authenticated user
	a.logger.Debugw("Authenticated user",
		"userId", claims.ID,
		"email", claims.Email,
	)

	return &AuthResult{
		UserID:    &claims.ID,
		Email:     &claims.Email,
		Anonymous: false,
	}, nil
}
