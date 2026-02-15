package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/robohub/ingest-service/internal/config"
)

const testSecret = "test-secret-key"

func TestValidateToken_Success(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
	}
	validator := NewValidator(cfg)

	// Create a valid token
	token := createTestToken(t, testSecret, "owner/repo", "robohub-auth", []string{"robohub-api"}, time.Now().Add(10*time.Minute))

	claims, err := validator.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if claims.Repo != "owner/repo" {
		t.Errorf("expected repo 'owner/repo', got %s", claims.Repo)
	}

	if claims.Issuer != "robohub-auth" {
		t.Errorf("expected issuer 'robohub-auth', got %s", claims.Issuer)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
	}
	validator := NewValidator(cfg)

	// Create token with different secret
	token := createTestToken(t, "wrong-secret", "owner/repo", "robohub-auth", []string{"robohub-api"}, time.Now().Add(10*time.Minute))

	_, err := validator.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for invalid signature, got none")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 1,
	}
	validator := NewValidator(cfg)

	// Create expired token
	token := createTestToken(t, testSecret, "owner/repo", "robohub-auth", []string{"robohub-api"}, time.Now().Add(-10*time.Minute))

	_, err := validator.ValidateToken(token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestValidateToken_InvalidIssuer(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
	}
	validator := NewValidator(cfg)

	// Create token with wrong issuer
	token := createTestToken(t, testSecret, "owner/repo", "wrong-issuer", []string{"robohub-api"}, time.Now().Add(10*time.Minute))

	_, err := validator.ValidateToken(token)
	if err != ErrInvalidIssuer {
		t.Errorf("expected ErrInvalidIssuer, got %v", err)
	}
}

func TestValidateToken_InvalidAudience(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
	}
	validator := NewValidator(cfg)

	// Create token with wrong audience
	token := createTestToken(t, testSecret, "owner/repo", "robohub-auth", []string{"wrong-audience"}, time.Now().Add(10*time.Minute))

	_, err := validator.ValidateToken(token)
	if err != ErrInvalidAudience {
		t.Errorf("expected ErrInvalidAudience, got %v", err)
	}
}

func TestValidateToken_MissingRepoClaim(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
	}
	validator := NewValidator(cfg)

	// Create token without repo claim
	token := createTestToken(t, testSecret, "", "robohub-auth", []string{"robohub-api"}, time.Now().Add(10*time.Minute))

	_, err := validator.ValidateToken(token)
	if err != ErrMissingRepoClaim {
		t.Errorf("expected ErrMissingRepoClaim, got %v", err)
	}
}

func TestValidateToken_ClockSkew(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 120, // 2 minutes skew
	}
	validator := NewValidator(cfg)

	// Create token that expired 30 seconds ago (within skew)
	token := createTestToken(t, testSecret, "owner/repo", "robohub-auth", []string{"robohub-api"}, time.Now().Add(-30*time.Second))

	_, err := validator.ValidateToken(token)
	if err != nil {
		t.Errorf("expected no error with clock skew, got %v", err)
	}
}

func createTestToken(t *testing.T, secret, repo, issuer string, audience []string, expiresAt time.Time) string {
	claims := &Claims{
		Repo: repo,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  audience,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to create test token: %v", err)
	}

	return tokenString
}
