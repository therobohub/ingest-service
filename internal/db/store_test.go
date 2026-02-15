package db

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Integration test - requires DATABASE_URL env var to be set
// Run with: DATABASE_URL="postgres://..." go test -v ./internal/db

func setupTestDB(t *testing.T) (*gorm.DB, func()) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	gormDB, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	// Run migrations
	err = gormDB.AutoMigrate(&Repo{}, &Build{}, &Image{}, &BuildImage{})
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	cleanup := func() {
		// Clean up test data
		gormDB.Exec("DELETE FROM build_images WHERE build_id IN (SELECT id FROM builds WHERE build_id LIKE 'test-%')")
		gormDB.Exec("DELETE FROM builds WHERE build_id LIKE 'test-%'")
		gormDB.Exec("DELETE FROM images WHERE digest LIKE 'sha256:test%'")
		gormDB.Exec("DELETE FROM repos WHERE full_name LIKE 'test/%'")
	}

	return gormDB, cleanup
}

func TestIntegration_UpsertBuild(t *testing.T) {
	gormDB, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	store := NewGormStore(gormDB)

	// Test 1: Create new build
	t.Run("create new build", func(t *testing.T) {
		repoID, err := store.UpsertRepo(ctx, "test/repo1", "https://github.com/test/repo1")
		if err != nil {
			t.Fatalf("failed to upsert repo: %v", err)
		}

		result, err := store.UpsertBuild(ctx, &UpsertBuildRequest{
			RepoID:      repoID,
			BuildID:     "test-build-1",
			Provider:    "github_actions",
			CommitSHA:   "abc123",
			Branch:      "main",
			RunURL:      "https://github.com/test/repo1/actions/runs/1",
			Workflow:    "CI",
			Status:      "success",
			Timestamp:   time.Now(),
			PayloadHash: "hash1",
		})

		if err != nil {
			t.Fatalf("failed to upsert build: %v", err)
		}

		if result.Idempotent {
			t.Error("expected idempotent to be false for new build")
		}

		if result.ID == 0 {
			t.Error("expected non-zero build ID")
		}
	})

	// Test 2: Idempotent insert with same hash
	t.Run("idempotent insert", func(t *testing.T) {
		repoID, _ := store.UpsertRepo(ctx, "test/repo2", "https://github.com/test/repo2")

		// First insert
		req := &UpsertBuildRequest{
			RepoID:      repoID,
			BuildID:     "test-build-2",
			Provider:    "github_actions",
			CommitSHA:   "def456",
			Branch:      "main",
			RunURL:      "https://github.com/test/repo2/actions/runs/2",
			Workflow:    "CI",
			Status:      "success",
			Timestamp:   time.Now(),
			PayloadHash: "hash2",
		}
		_, err := store.UpsertBuild(ctx, req)
		if err != nil {
			t.Fatalf("failed first insert: %v", err)
		}

		// Second insert with same hash
		result, err := store.UpsertBuild(ctx, req)
		if err != nil {
			t.Fatalf("failed second insert: %v", err)
		}

		if !result.Idempotent {
			t.Error("expected idempotent to be true for duplicate")
		}
	})

	// Test 3: Conflict with different hash
	t.Run("conflict detection", func(t *testing.T) {
		repoID, _ := store.UpsertRepo(ctx, "test/repo3", "https://github.com/test/repo3")

		// First insert
		req1 := &UpsertBuildRequest{
			RepoID:      repoID,
			BuildID:     "test-build-3",
			Provider:    "github_actions",
			CommitSHA:   "ghi789",
			Branch:      "main",
			RunURL:      "https://github.com/test/repo3/actions/runs/3",
			Workflow:    "CI",
			Status:      "success",
			Timestamp:   time.Now(),
			PayloadHash: "hash3",
		}
		_, err := store.UpsertBuild(ctx, req1)
		if err != nil {
			t.Fatalf("failed first insert: %v", err)
		}

		// Second insert with different hash
		req2 := &UpsertBuildRequest{
			RepoID:      repoID,
			BuildID:     "test-build-3",
			Provider:    "github_actions",
			CommitSHA:   "different",
			Branch:      "main",
			RunURL:      "https://github.com/test/repo3/actions/runs/3",
			Workflow:    "CI",
			Status:      "success",
			Timestamp:   time.Now(),
			PayloadHash: "hash3-different",
		}
		result, err := store.UpsertBuild(ctx, req2)
		if err != nil {
			t.Fatalf("failed second insert: %v", err)
		}

		if result.Idempotent {
			t.Error("expected idempotent to be false for conflict")
		}

		if result.ExistingHash != "hash3" {
			t.Errorf("expected existing hash 'hash3', got %s", result.ExistingHash)
		}
	})

	// Test 4: Full flow with image
	t.Run("full flow with image", func(t *testing.T) {
		repoID, err := store.UpsertRepo(ctx, "test/repo4", "https://github.com/test/repo4")
		if err != nil {
			t.Fatalf("failed to upsert repo: %v", err)
		}

		buildResult, err := store.UpsertBuild(ctx, &UpsertBuildRequest{
			RepoID:      repoID,
			BuildID:     "test-build-4",
			Provider:    "github_actions",
			CommitSHA:   "jkl012",
			Branch:      "main",
			RunURL:      "https://github.com/test/repo4/actions/runs/4",
			Workflow:    "CI",
			Status:      "success",
			Timestamp:   time.Now(),
			PayloadHash: "hash4",
		})
		if err != nil {
			t.Fatalf("failed to upsert build: %v", err)
		}

		imageID, err := store.UpsertImage(ctx, "sha256:test1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab", "ghcr.io/test/image")
		if err != nil {
			t.Fatalf("failed to upsert image: %v", err)
		}

		err = store.UpsertBuildImage(ctx, buildResult.ID, imageID, "perception", []string{"main", "latest"})
		if err != nil {
			t.Fatalf("failed to upsert build-image: %v", err)
		}

		// Verify we can retrieve it
		detail, err := store.GetBuildByBuildID(ctx, "test-build-4")
		if err != nil {
			t.Fatalf("failed to get build: %v", err)
		}

		if detail.BuildID != "test-build-4" {
			t.Errorf("expected build_id 'test-build-4', got %s", detail.BuildID)
		}

		if len(detail.Images) != 1 {
			t.Fatalf("expected 1 image, got %d", len(detail.Images))
		}

		if detail.Images[0].Component != "perception" {
			t.Errorf("expected component 'perception', got %s", detail.Images[0].Component)
		}
	})
}
