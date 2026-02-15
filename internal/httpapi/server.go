package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/robohub/ingest-service/internal/auth"
	"github.com/robohub/ingest-service/internal/config"
	"github.com/robohub/ingest-service/internal/db"
	"github.com/robohub/ingest-service/internal/hash"
	"github.com/robohub/ingest-service/internal/ratelimit"
	"github.com/robohub/ingest-service/internal/types"
	"gorm.io/gorm"
)

var digestRegex = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Server represents the HTTP server
type Server struct {
	store     db.Store
	validator *auth.Validator
	limiter   *ratelimit.Limiter
	cfg       *config.Config
}

// NewServer creates a new HTTP server
func NewServer(store db.Store, cfg *config.Config) *Server {
	return &Server{
		store:     store,
		validator: auth.NewValidator(cfg),
		limiter:   ratelimit.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		cfg:       cfg,
	}
}

// Routes sets up the HTTP routes
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Health endpoints (no auth)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	// Protected endpoints
	r.Route("/", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Use(s.rateLimitMiddleware)

		r.Post("/ingest/build", s.handleIngestBuild)
		r.Get("/repos/{repo}/builds", s.handleListBuilds)
		r.Get("/builds/{build_id}", s.handleGetBuild)
		r.Get("/images/{digest}", s.handleGetImage)
	})

	return r
}

// authMiddleware validates JWT and extracts claims
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.respondError(w, http.StatusUnauthorized, "missing_auth", "Authorization header required", nil)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			s.respondError(w, http.StatusUnauthorized, "invalid_auth", "Invalid authorization header format", nil)
			return
		}

		claims, err := s.validator.ValidateToken(parts[1])
		if err != nil {
			code := "invalid_token"
			if errors.Is(err, auth.ErrTokenExpired) {
				code = "token_expired"
			} else if errors.Is(err, auth.ErrInvalidAudience) {
				code = "invalid_audience"
			} else if errors.Is(err, auth.ErrInvalidIssuer) {
				code = "invalid_issuer"
			}
			s.respondError(w, http.StatusUnauthorized, code, err.Error(), nil)
			return
		}

		// Store claims in context
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimitMiddleware enforces rate limiting per repo
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := getClaimsFromContext(r.Context())
		if claims == nil {
			// Should not happen after auth middleware
			s.respondError(w, http.StatusUnauthorized, "no_claims", "No claims in context", nil)
			return
		}

		if !s.limiter.Allow(claims.Repo) {
			s.respondError(w, http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded", map[string]interface{}{
				"repo": claims.Repo,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleIngestBuild handles POST /ingest/build
func (s *Server) handleIngestBuild(w http.ResponseWriter, r *http.Request) {
	// Enforce max body size
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)

	var req types.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// Validate required fields
	if err := s.validateIngestRequest(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
		return
	}

	// Get claims and enforce repo match
	claims := getClaimsFromContext(r.Context())
	if claims.Repo != req.Repo {
		s.respondError(w, http.StatusForbidden, "repo_mismatch", "Token repo does not match request repo", map[string]interface{}{
			"token_repo":   claims.Repo,
			"request_repo": req.Repo,
		})
		return
	}

	// Compute payload hash
	payloadHash, err := hash.ComputePayloadHash(&req)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "hash_error", "Failed to compute payload hash", nil)
		return
	}

	ctx := r.Context()

	// Upsert repo
	repoID, err := s.store.UpsertRepo(ctx, req.Repo, req.RepoURL)
	if err != nil {
		slog.Error("failed to upsert repo", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to store repo", nil)
		return
	}

	// Upsert build
	buildResult, err := s.store.UpsertBuild(ctx, &db.UpsertBuildRequest{
		RepoID:      repoID,
		BuildID:     req.BuildID,
		Provider:    req.Provider,
		CommitSHA:   req.CommitSHA,
		Branch:      req.Branch,
		RunURL:      req.RunURL,
		Workflow:    req.Workflow,
		Status:      req.Status,
		Timestamp:   req.Timestamp,
		PayloadHash: payloadHash,
	})
	if err != nil {
		slog.Error("failed to upsert build", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to store build", nil)
		return
	}

	// Check for conflict (same build ID but different payload)
	if !buildResult.Idempotent && buildResult.ExistingHash != "" {
		s.respondError(w, http.StatusConflict, "payload_conflict", "Build ID exists with different payload", map[string]interface{}{
			"existing_hash": buildResult.ExistingHash,
			"new_hash":      payloadHash,
		})
		return
	}

	// Upsert image
	imageID, err := s.store.UpsertImage(ctx, req.Image.Digest, req.Image.Name)
	if err != nil {
		slog.Error("failed to upsert image", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to store image", nil)
		return
	}

	// Upsert build-image relationship
	if err := s.store.UpsertBuildImage(ctx, buildResult.ID, imageID, req.Image.Component, req.Image.Tags); err != nil {
		slog.Error("failed to upsert build-image", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to store build-image relationship", nil)
		return
	}

	// Success response
	s.respondJSON(w, http.StatusOK, types.IngestResponse{
		OK:          true,
		Idempotent:  buildResult.Idempotent,
		Repo:        req.Repo,
		BuildID:     req.BuildID,
		ImageDigest: req.Image.Digest,
		StoredAt:    time.Now(),
	})
}

// handleListBuilds handles GET /repos/{repo}/builds
func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if repo == "" {
		s.respondError(w, http.StatusBadRequest, "missing_param", "Missing repo parameter", nil)
		return
	}

	// Decode repo from URL encoding
	repo = strings.ReplaceAll(repo, "%2F", "/")

	// Enforce repo access
	claims := getClaimsFromContext(r.Context())
	if claims.Repo != repo {
		s.respondError(w, http.StatusForbidden, "repo_mismatch", "Token repo does not match requested repo", nil)
		return
	}

	// Get query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil {
			limit = 50 // Reset to default on parse error
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cursor := r.URL.Query().Get("cursor")
	status := r.URL.Query().Get("status")

	// Validate status if provided
	if status != "" && status != "success" && status != "failure" {
		s.respondError(w, http.StatusBadRequest, "invalid_status", "Status must be 'success' or 'failure'", nil)
		return
	}

	// Get repo from database
	repoRecord, err := s.store.GetRepoByFullName(r.Context(), repo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.respondJSON(w, http.StatusOK, types.BuildListResponse{
				Repo:  repo,
				Items: []types.BuildItem{},
			})
			return
		}
		slog.Error("failed to get repo", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to query repo", nil)
		return
	}

	// List builds
	builds, nextCursor, err := s.store.ListBuilds(r.Context(), uint(repoRecord.ID), limit, cursor, status)
	if err != nil {
		slog.Error("failed to list builds", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to list builds", nil)
		return
	}

	// Convert to response format
	items := make([]types.BuildItem, len(builds))
	for i, b := range builds {
		items[i] = types.BuildItem{
			BuildID:   b.BuildID,
			CommitSHA: b.CommitSHA,
			Status:    b.Status,
			Timestamp: b.Timestamp,
			RunURL:    b.RunURL,
			Image:     b.Image,
		}
	}

	s.respondJSON(w, http.StatusOK, types.BuildListResponse{
		Repo:       repo,
		Items:      items,
		NextCursor: nextCursor,
	})
}

// handleGetBuild handles GET /builds/{build_id}
func (s *Server) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "build_id")
	if buildID == "" {
		s.respondError(w, http.StatusBadRequest, "missing_param", "Missing build_id parameter", nil)
		return
	}

	detail, err := s.store.GetBuildByBuildID(r.Context(), buildID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.respondError(w, http.StatusNotFound, "not_found", "Build not found", nil)
			return
		}
		slog.Error("failed to get build", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to query build", nil)
		return
	}

	// Enforce repo access
	claims := getClaimsFromContext(r.Context())
	if claims.Repo != detail.Repo {
		s.respondError(w, http.StatusForbidden, "repo_mismatch", "Token repo does not match build repo", nil)
		return
	}

	s.respondJSON(w, http.StatusOK, types.BuildDetailResponse{
		Repo:      detail.Repo,
		BuildID:   detail.BuildID,
		CommitSHA: detail.CommitSHA,
		Status:    detail.Status,
		Timestamp: detail.Timestamp,
		RunURL:    detail.RunURL,
		Workflow:  detail.Workflow,
		Images:    detail.Images,
	})
}

// handleGetImage handles GET /images/{digest}
func (s *Server) handleGetImage(w http.ResponseWriter, r *http.Request) {
	digest := chi.URLParam(r, "digest")
	if digest == "" {
		s.respondError(w, http.StatusBadRequest, "missing_param", "Missing digest parameter", nil)
		return
	}

	// Add sha256: prefix if not present
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}

	detail, err := s.store.GetImageByDigest(r.Context(), digest)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.respondError(w, http.StatusNotFound, "not_found", "Image not found", nil)
			return
		}
		slog.Error("failed to get image", "error", err)
		s.respondError(w, http.StatusInternalServerError, "database_error", "Failed to query image", nil)
		return
	}

	// Check if token repo has access to at least one build for this image
	claims := getClaimsFromContext(r.Context())
	hasAccess := false
	for _, repo := range detail.Repos {
		if repo == claims.Repo {
			hasAccess = true
			break
		}
	}
	if !hasAccess {
		s.respondError(w, http.StatusForbidden, "repo_mismatch", "Token repo does not have access to this image", nil)
		return
	}

	s.respondJSON(w, http.StatusOK, types.ImageDetailResponse{
		Digest:       detail.Digest,
		Name:         detail.Name,
		Components:   detail.Components,
		Tags:         detail.Tags,
		Repos:        detail.Repos,
		RecentBuilds: detail.RecentBuilds,
	})
}

// handleHealthz handles GET /healthz
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Error("failed to write healthz response", "error", err)
	}
}

// handleReadyz handles GET /readyz
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		slog.Error("readiness check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("database unavailable")); err != nil {
			slog.Error("failed to write readyz error response", "error", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Error("failed to write readyz response", "error", err)
	}
}

// validateIngestRequest validates the ingest request
func (s *Server) validateIngestRequest(req *types.IngestRequest) error {
	if req.Repo == "" {
		return fmt.Errorf("repo is required")
	}
	if req.BuildID == "" {
		return fmt.Errorf("build_id is required")
	}
	if req.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if req.CommitSHA == "" {
		return fmt.Errorf("commit_sha is required")
	}
	if req.Status != "success" && req.Status != "failure" {
		return fmt.Errorf("status must be 'success' or 'failure'")
	}
	if req.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	if req.Image.Digest == "" {
		return fmt.Errorf("image.digest is required")
	}
	if !digestRegex.MatchString(req.Image.Digest) {
		return fmt.Errorf("image.digest must be in format sha256:64hexchars")
	}
	if req.Image.Name == "" {
		return fmt.Errorf("image.name is required")
	}
	return nil
}

// respondJSON writes a JSON response
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// respondError writes an error response
func (s *Server) respondError(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := types.ErrorResponse{
		Error: types.ErrorDetail{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}

type contextKey string

const claimsKey contextKey = "claims"

func getClaimsFromContext(ctx context.Context) *auth.Claims {
	claims, ok := ctx.Value(claimsKey).(*auth.Claims)
	if !ok {
		return nil
	}
	return claims
}
