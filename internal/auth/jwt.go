package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/robohub/ingest-service/internal/config"
)

var (
	ErrMissingToken      = errors.New("missing authorization token")
	ErrInvalidToken      = errors.New("invalid token")
	ErrInvalidSignature  = errors.New("invalid token signature")
	ErrInvalidIssuer     = errors.New("invalid token issuer")
	ErrInvalidAudience   = errors.New("invalid token audience")
	ErrTokenExpired      = errors.New("token expired")
	ErrMissingRepoClaim  = errors.New("missing repo claim in token")
)

// Claims represents the JWT claims we expect
type Claims struct {
	Repo string `json:"repo"`
	jwt.RegisteredClaims
}

// Validator validates JWT tokens
type Validator struct {
	secret    []byte
	clockSkew time.Duration
}

// NewValidator creates a new JWT validator
func NewValidator(cfg *config.Config) *Validator {
	return &Validator{
		secret:    []byte(cfg.JWTSecret),
		clockSkew: cfg.ClockSkew(),
	}
}

// ValidateToken validates a JWT token and returns the claims
func (v *Validator) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.secret, nil
	}, jwt.WithLeeway(v.clockSkew))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if errors.Is(err, jwt.ErrSignatureInvalid) {
			return nil, ErrInvalidSignature
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}

	// Validate issuer
	if claims.Issuer != "robohub-auth" {
		return nil, ErrInvalidIssuer
	}

	// Validate audience
	audiences := claims.Audience
	hasValidAudience := false
	for _, aud := range audiences {
		if aud == "robohub-api" {
			hasValidAudience = true
			break
		}
	}
	if !hasValidAudience {
		return nil, ErrInvalidAudience
	}

	// Validate repo claim exists
	if claims.Repo == "" {
		return nil, ErrMissingRepoClaim
	}

	return claims, nil
}

// ExtractRepo extracts the repo claim from validated claims
func ExtractRepo(claims *Claims) string {
	return claims.Repo
}
