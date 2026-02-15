package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/robohub/ingest-service/internal/auth"
	"github.com/robohub/ingest-service/internal/config"
	"github.com/robohub/ingest-service/internal/db"
	"github.com/robohub/ingest-service/internal/types"
	"gorm.io/gorm"
)

const testSecret = "test-secret-key"

// mockStore is a fake store implementation for testing
type mockStore struct {
	repos        map[string]*types.Repo
	builds       map[string]*db.BuildDetail
	images       map[string]*db.ImageDetail
	upsertResult *db.UpsertBuildResult
	shouldError  bool
}

func newMockStore() *mockStore {
	return &mockStore{
		repos:  make(map[string]*types.Repo),
		builds: make(map[string]*db.BuildDetail),
		images: make(map[string]*db.ImageDetail),
	}
}

func (m *mockStore) UpsertRepo(ctx context.Context, fullName, url string) (uint, error) {
	if m.shouldError {
		return 0, gorm.ErrRecordNotFound
	}
	m.repos[fullName] = &types.Repo{ID: 1, FullName: fullName, URL: url}
	return 1, nil
}

func (m *mockStore) UpsertBuild(ctx context.Context, req *db.UpsertBuildRequest) (*db.UpsertBuildResult, error) {
	if m.shouldError {
		return nil, gorm.ErrRecordNotFound
	}
	return m.upsertResult, nil
}

func (m *mockStore) UpsertImage(ctx context.Context, digest, name string) (uint, error) {
	if m.shouldError {
		return 0, gorm.ErrRecordNotFound
	}
	return 1, nil
}

func (m *mockStore) UpsertBuildImage(ctx context.Context, buildID, imageID uint, component string, tags []string) error {
	if m.shouldError {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (m *mockStore) ListBuilds(ctx context.Context, repoID uint, limit int, cursor string, status string) ([]*db.BuildWithImage, string, error) {
	if m.shouldError {
		return nil, "", gorm.ErrRecordNotFound
	}
	return []*db.BuildWithImage{}, "", nil
}

func (m *mockStore) GetBuildByBuildID(ctx context.Context, buildID string) (*db.BuildDetail, error) {
	if m.shouldError {
		return nil, gorm.ErrRecordNotFound
	}
	detail, ok := m.builds[buildID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return detail, nil
}

func (m *mockStore) GetImageByDigest(ctx context.Context, digest string) (*db.ImageDetail, error) {
	if m.shouldError {
		return nil, gorm.ErrRecordNotFound
	}
	detail, ok := m.images[digest]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return detail, nil
}

func (m *mockStore) GetRepoByFullName(ctx context.Context, fullName string) (*types.Repo, error) {
	if m.shouldError {
		return nil, gorm.ErrRecordNotFound
	}
	repo, ok := m.repos[fullName]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return repo, nil
}

func (m *mockStore) Ping(ctx context.Context) error {
	if m.shouldError {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func TestHandleIngestBuild_Success(t *testing.T) {
	store := newMockStore()
	store.upsertResult = &db.UpsertBuildResult{
		ID:         1,
		Idempotent: false,
	}

	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	reqBody := types.IngestRequest{
		SchemaVersion: "1.0",
		Provider:      "github_actions",
		Repo:          "owner/repo",
		RepoURL:       "https://github.com/owner/repo",
		CommitSHA:     "abc123",
		Branch:        "main",
		BuildID:       "build-1",
		RunURL:        "https://github.com/owner/repo/actions/runs/1",
		Workflow:      "CI",
		Status:        "success",
		Timestamp:     time.Now(),
		Image: types.Image{
			Component: "perception",
			Name:      "ghcr.io/owner/perception",
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Tags:      []string{"main"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader(bodyBytes))
	token := createTestToken(t, testSecret, "owner/repo")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp types.IngestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.OK {
		t.Error("expected OK to be true")
	}

	if resp.Repo != "owner/repo" {
		t.Errorf("expected repo 'owner/repo', got %s", resp.Repo)
	}
}

func TestHandleIngestBuild_MissingAuth(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	reqBody := types.IngestRequest{
		Repo:      "owner/repo",
		BuildID:   "build-1",
		Provider:  "github_actions",
		CommitSHA: "abc123",
		Status:    "success",
		Timestamp: time.Now(),
		Image: types.Image{
			Digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Name:   "ghcr.io/owner/perception",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestHandleIngestBuild_InvalidJSON(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader([]byte("invalid json")))
	token := createTestToken(t, testSecret, "owner/repo")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleIngestBuild_RepoMismatch(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	reqBody := types.IngestRequest{
		Repo:      "owner/different-repo",
		BuildID:   "build-1",
		Provider:  "github_actions",
		CommitSHA: "abc123",
		Status:    "success",
		Timestamp: time.Now(),
		Image: types.Image{
			Digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Name:   "ghcr.io/owner/perception",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader(bodyBytes))
	token := createTestToken(t, testSecret, "owner/repo") // Token for different repo
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestHandleIngestBuild_Conflict(t *testing.T) {
	store := newMockStore()
	store.upsertResult = &db.UpsertBuildResult{
		ID:           1,
		Idempotent:   false,
		ExistingHash: "existing-hash",
	}

	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	reqBody := types.IngestRequest{
		Repo:      "owner/repo",
		BuildID:   "build-1",
		Provider:  "github_actions",
		CommitSHA: "abc123",
		Status:    "success",
		Timestamp: time.Now(),
		Image: types.Image{
			Digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Name:   "ghcr.io/owner/perception",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader(bodyBytes))
	token := createTestToken(t, testSecret, "owner/repo")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}
}

func TestHandleIngestBuild_Idempotent(t *testing.T) {
	store := newMockStore()
	store.upsertResult = &db.UpsertBuildResult{
		ID:         1,
		Idempotent: true,
	}

	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		MaxBodyBytes:     262144,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	reqBody := types.IngestRequest{
		Repo:      "owner/repo",
		BuildID:   "build-1",
		Provider:  "github_actions",
		CommitSHA: "abc123",
		Status:    "success",
		Timestamp: time.Now(),
		Image: types.Image{
			Digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Name:   "ghcr.io/owner/perception",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/ingest/build", bytes.NewReader(bodyBytes))
	token := createTestToken(t, testSecret, "owner/repo")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp types.IngestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Idempotent {
		t.Error("expected Idempotent to be true")
	}
}

func TestHandleHealthz(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if w.Body.String() != "ok" {
		t.Errorf("expected 'ok', got %s", w.Body.String())
	}
}

func TestHandleReadyz_Success(t *testing.T) {
	store := newMockStore()
	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleReadyz_DatabaseUnavailable(t *testing.T) {
	store := newMockStore()
	store.shouldError = true

	cfg := &config.Config{
		JWTSecret:        testSecret,
		ClockSkewSeconds: 60,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
	}

	server := NewServer(store, cfg)
	handler := server.Routes()

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func createTestToken(t *testing.T, secret, repo string) string {
	claims := &auth.Claims{
		Repo: repo,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "robohub-auth",
			Audience:  []string{"robohub-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
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
