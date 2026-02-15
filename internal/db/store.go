package db

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/robohub/ingest-service/internal/types"
)

// GORM Models
type Repo struct {
	ID        uint   `gorm:"primaryKey"`
	FullName  string `gorm:"uniqueIndex;not null"`
	URL       string
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type Build struct {
	ID          uint   `gorm:"primaryKey"`
	RepoID      uint   `gorm:"not null;index:idx_builds_repo_ts,priority:1;index:idx_builds_repo_status_ts,priority:1"`
	BuildID     string `gorm:"not null;index:idx_builds_build_id"`
	Provider    string `gorm:"not null"`
	CommitSHA   string `gorm:"column:commit_sha;not null"`
	Branch      string
	RunURL      string `gorm:"column:run_url"`
	Workflow    string
	Status      string    `gorm:"not null;index:idx_builds_repo_status_ts,priority:2"`
	Ts          time.Time `gorm:"not null;index:idx_builds_repo_ts,priority:2,sort:desc;index:idx_builds_repo_status_ts,priority:3,sort:desc"`
	PayloadHash string    `gorm:"not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Repo        Repo         `gorm:"foreignKey:RepoID"`
	BuildImages []BuildImage `gorm:"foreignKey:BuildID"`
}

func (Build) TableName() string {
	return "builds"
}

type Image struct {
	ID        uint      `gorm:"primaryKey"`
	Digest    string    `gorm:"uniqueIndex;not null"`
	Name      string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`

	BuildImages []BuildImage `gorm:"foreignKey:ImageID"`
}

type BuildImage struct {
	BuildID   uint `gorm:"primaryKey;not null"`
	ImageID   uint `gorm:"primaryKey;not null;index:idx_build_images_image"`
	Component string
	Tags      string `gorm:"type:text"` // Stored as comma-separated

	Build Build `gorm:"foreignKey:BuildID"`
	Image Image `gorm:"foreignKey:ImageID"`
}

// Store defines the interface for database operations
type Store interface {
	UpsertRepo(ctx context.Context, fullName, url string) (uint, error)
	UpsertBuild(ctx context.Context, req *UpsertBuildRequest) (*UpsertBuildResult, error)
	UpsertImage(ctx context.Context, digest, name string) (uint, error)
	UpsertBuildImage(ctx context.Context, buildID, imageID uint, component string, tags []string) error
	ListBuilds(ctx context.Context, repoID uint, limit int, cursor string, status string) ([]*BuildWithImage, string, error)
	GetBuildByBuildID(ctx context.Context, buildID string) (*BuildDetail, error)
	GetImageByDigest(ctx context.Context, digest string) (*ImageDetail, error)
	GetRepoByFullName(ctx context.Context, fullName string) (*types.Repo, error)
	Ping(ctx context.Context) error
}

// UpsertBuildRequest contains all data needed to upsert a build
type UpsertBuildRequest struct {
	RepoID      uint
	BuildID     string
	Provider    string
	CommitSHA   string
	Branch      string
	RunURL      string
	Workflow    string
	Status      string
	Timestamp   time.Time
	PayloadHash string
}

// UpsertBuildResult contains the result of a build upsert
type UpsertBuildResult struct {
	ID           uint
	Idempotent   bool
	ExistingHash string
}

// BuildWithImage represents a build with its image for listing
type BuildWithImage struct {
	Repo      string
	BuildID   string
	CommitSHA string
	Status    string
	Timestamp time.Time
	RunURL    string
	Image     types.Image
}

// BuildDetail contains full build details with all images
type BuildDetail struct {
	Repo      string
	BuildID   string
	CommitSHA string
	Status    string
	Timestamp time.Time
	RunURL    string
	Workflow  string
	Images    []types.Image
}

// ImageDetail contains image details with recent builds
type ImageDetail struct {
	Digest       string
	Name         string
	Components   []string
	Tags         []string
	Repos        []string
	RecentBuilds []types.ImageRecentBuild
}

// GormStore implements Store using GORM
type GormStore struct {
	db *gorm.DB
}

// NewGormStore creates a new GORM store
func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

// UpsertRepo inserts or returns existing repo
func (s *GormStore) UpsertRepo(ctx context.Context, fullName, url string) (uint, error) {
	var repo Repo
	err := s.db.WithContext(ctx).Where("full_name = ?", fullName).FirstOrCreate(&repo, Repo{
		FullName: fullName,
		URL:      url,
	}).Error

	if err != nil {
		return 0, err
	}

	// Update URL if changed
	if repo.URL != url {
		s.db.WithContext(ctx).Model(&repo).Update("url", url)
	}

	return repo.ID, nil
}

// UpsertBuild inserts or updates a build, checking for idempotency
func (s *GormStore) UpsertBuild(ctx context.Context, req *UpsertBuildRequest) (*UpsertBuildResult, error) {
	var existing Build
	err := s.db.WithContext(ctx).Where("repo_id = ? AND build_id = ?", req.RepoID, req.BuildID).First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// Insert new build
		newBuild := Build{
			RepoID:      req.RepoID,
			BuildID:     req.BuildID,
			Provider:    req.Provider,
			CommitSHA:   req.CommitSHA,
			Branch:      req.Branch,
			RunURL:      req.RunURL,
			Workflow:    req.Workflow,
			Status:      req.Status,
			Ts:          req.Timestamp,
			PayloadHash: req.PayloadHash,
		}

		if err := s.db.WithContext(ctx).Create(&newBuild).Error; err != nil {
			return nil, fmt.Errorf("insert build: %w", err)
		}

		return &UpsertBuildResult{
			ID:         newBuild.ID,
			Idempotent: false,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("check existing build: %w", err)
	}

	// Build exists - check if idempotent
	if existing.PayloadHash == req.PayloadHash {
		return &UpsertBuildResult{
			ID:         existing.ID,
			Idempotent: true,
		}, nil
	}

	// Conflict - different payload
	return &UpsertBuildResult{
		ID:           existing.ID,
		Idempotent:   false,
		ExistingHash: existing.PayloadHash,
	}, nil
}

// UpsertImage inserts or returns existing image
func (s *GormStore) UpsertImage(ctx context.Context, digest, name string) (uint, error) {
	var image Image
	err := s.db.WithContext(ctx).Where("digest = ?", digest).FirstOrCreate(&image, Image{
		Digest: digest,
		Name:   name,
	}).Error

	if err != nil {
		return 0, err
	}

	// Update name if changed
	if image.Name != name {
		s.db.WithContext(ctx).Model(&image).Update("name", name)
	}

	return image.ID, nil
}

// UpsertBuildImage inserts or updates the build-image relationship
func (s *GormStore) UpsertBuildImage(ctx context.Context, buildID, imageID uint, component string, tags []string) error {
	tagsStr := strings.Join(tags, ",")

	buildImage := BuildImage{
		BuildID:   buildID,
		ImageID:   imageID,
		Component: component,
		Tags:      tagsStr,
	}

	err := s.db.WithContext(ctx).Where("build_id = ? AND image_id = ?", buildID, imageID).
		Assign(BuildImage{Component: component, Tags: tagsStr}).
		FirstOrCreate(&buildImage).Error

	return err
}

// ListBuilds returns builds for a repo with pagination
func (s *GormStore) ListBuilds(ctx context.Context, repoID uint, limit int, cursor string, status string) ([]*BuildWithImage, string, error) {
	var afterTS time.Time
	var afterID uint

	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		parts := strings.Split(string(decoded), "|")
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid cursor format")
		}
		afterTS, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor timestamp: %w", err)
		}
		id, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor id: %w", err)
		}
		afterID = uint(id)
	}

	query := s.db.WithContext(ctx).
		Table("builds").
		Select("repos.full_name as repo, builds.build_id, builds.commit_sha, builds.status, builds.ts, builds.run_url, "+
			"build_images.component, images.name as image_name, images.digest, build_images.tags, builds.id").
		Joins("JOIN repos ON builds.repo_id = repos.id").
		Joins("LEFT JOIN build_images ON builds.id = build_images.build_id").
		Joins("LEFT JOIN images ON build_images.image_id = images.id").
		Where("builds.repo_id = ?", repoID)

	if status != "" {
		query = query.Where("builds.status = ?", status)
	}

	if cursor != "" {
		query = query.Where("(builds.ts < ? OR (builds.ts = ? AND builds.id < ?))", afterTS, afterTS, afterID)
	}

	query = query.Order("builds.ts DESC, builds.id DESC").Limit(limit + 1)

	type result struct {
		Repo      string
		BuildID   string
		CommitSHA string
		Status    string
		Ts        time.Time
		RunURL    string
		Component *string
		ImageName *string
		Digest    *string
		Tags      string
		ID        uint
	}

	var rows []result
	if err := query.Find(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("query builds: %w", err)
	}

	var results []*BuildWithImage
	var lastID uint
	for _, row := range rows {
		build := &BuildWithImage{
			Repo:      row.Repo,
			BuildID:   row.BuildID,
			CommitSHA: row.CommitSHA,
			Status:    row.Status,
			Timestamp: row.Ts,
			RunURL:    row.RunURL,
		}

		if row.ImageName != nil && row.Digest != nil {
			var tags []string
			if row.Tags != "" {
				tags = strings.Split(row.Tags, ",")
			}
			build.Image = types.Image{
				Component: stringValue(row.Component),
				Name:      *row.ImageName,
				Digest:    *row.Digest,
				Tags:      tags,
			}
		}

		lastID = row.ID
		results = append(results, build)
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[len(results)-1]
		cursorStr := fmt.Sprintf("%s|%d", last.Timestamp.Format(time.RFC3339Nano), lastID)
		nextCursor = base64.StdEncoding.EncodeToString([]byte(cursorStr))
	}

	return results, nextCursor, nil
}

// GetBuildByBuildID returns full build details including all images
func (s *GormStore) GetBuildByBuildID(ctx context.Context, buildID string) (*BuildDetail, error) {
	type result struct {
		Repo      string
		BuildID   string
		CommitSHA string
		Status    string
		Ts        time.Time
		RunURL    string
		Workflow  string
		Component *string
		ImageName *string
		Digest    *string
		Tags      string
	}

	var rows []result
	err := s.db.WithContext(ctx).
		Table("builds").
		Select("repos.full_name as repo, builds.build_id, builds.commit_sha, builds.status, "+
			"builds.ts, builds.run_url, builds.workflow, "+
			"build_images.component, images.name as image_name, images.digest, build_images.tags").
		Joins("JOIN repos ON builds.repo_id = repos.id").
		Joins("LEFT JOIN build_images ON builds.id = build_images.build_id").
		Joins("LEFT JOIN images ON build_images.image_id = images.id").
		Where("builds.build_id = ?", buildID).
		Find(&rows).Error

	if err != nil {
		return nil, fmt.Errorf("query build: %w", err)
	}

	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	detail := &BuildDetail{
		Repo:      rows[0].Repo,
		BuildID:   rows[0].BuildID,
		CommitSHA: rows[0].CommitSHA,
		Status:    rows[0].Status,
		Timestamp: rows[0].Ts,
		RunURL:    rows[0].RunURL,
		Workflow:  rows[0].Workflow,
	}

	for _, row := range rows {
		if row.ImageName != nil && row.Digest != nil {
			var tags []string
			if row.Tags != "" {
				tags = strings.Split(row.Tags, ",")
			}
			detail.Images = append(detail.Images, types.Image{
				Component: stringValue(row.Component),
				Name:      *row.ImageName,
				Digest:    *row.Digest,
				Tags:      tags,
			})
		}
	}

	return detail, nil
}

// GetImageByDigest returns image details with recent builds
func (s *GormStore) GetImageByDigest(ctx context.Context, digest string) (*ImageDetail, error) {
	var image Image
	if err := s.db.WithContext(ctx).Where("digest = ?", digest).First(&image).Error; err != nil {
		return nil, fmt.Errorf("query image: %w", err)
	}

	detail := &ImageDetail{
		Digest: image.Digest,
		Name:   image.Name,
	}

	// Get unique components, tags, and repos
	type aggregateResult struct {
		Components string
		Tags       string
		Repos      string
	}

	var agg aggregateResult
	err := s.db.WithContext(ctx).Raw(`
		SELECT 
			STRING_AGG(DISTINCT NULLIF(build_images.component, ''), ',') as components,
			STRING_AGG(DISTINCT build_images.tags, ',') as tags,
			STRING_AGG(DISTINCT repos.full_name, ',') as repos
		FROM images
		JOIN build_images ON images.id = build_images.image_id
		JOIN builds ON build_images.build_id = builds.id
		JOIN repos ON builds.repo_id = repos.id
		WHERE images.digest = ?
	`, digest).Scan(&agg).Error

	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("query aggregates: %w", err)
	}

	if agg.Components != "" {
		detail.Components = uniqueStrings(strings.Split(agg.Components, ","))
	}
	if agg.Tags != "" {
		// Split all tags and get unique
		allTags := []string{}
		for _, tagGroup := range strings.Split(agg.Tags, ",") {
			if tagGroup != "" {
				allTags = append(allTags, tagGroup)
			}
		}
		detail.Tags = uniqueStrings(allTags)
	}
	if agg.Repos != "" {
		detail.Repos = strings.Split(agg.Repos, ",")
	}

	// Get recent builds
	type recentBuild struct {
		Repo      string
		BuildID   string
		Timestamp time.Time
	}

	var recentBuilds []recentBuild
	err = s.db.WithContext(ctx).
		Table("images").
		Select("repos.full_name as repo, builds.build_id, builds.ts as timestamp").
		Joins("JOIN build_images ON images.id = build_images.image_id").
		Joins("JOIN builds ON build_images.build_id = builds.id").
		Joins("JOIN repos ON builds.repo_id = repos.id").
		Where("images.digest = ?", digest).
		Order("builds.ts DESC").
		Limit(10).
		Find(&recentBuilds).Error

	if err != nil {
		return nil, fmt.Errorf("query recent builds: %w", err)
	}

	for _, rb := range recentBuilds {
		detail.RecentBuilds = append(detail.RecentBuilds, types.ImageRecentBuild{
			Repo:      rb.Repo,
			BuildID:   rb.BuildID,
			Timestamp: rb.Timestamp,
		})
	}

	return detail, nil
}

// GetRepoByFullName returns a repo by its full name
func (s *GormStore) GetRepoByFullName(ctx context.Context, fullName string) (*types.Repo, error) {
	var repo Repo
	if err := s.db.WithContext(ctx).Where("full_name = ?", fullName).First(&repo).Error; err != nil {
		return nil, err
	}

	return &types.Repo{
		ID:        int64(repo.ID),
		FullName:  repo.FullName,
		URL:       repo.URL,
		CreatedAt: repo.CreatedAt,
	}, nil
}

// Ping checks database connectivity
func (s *GormStore) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
