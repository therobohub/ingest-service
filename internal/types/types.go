package types

import "time"

// IngestRequest represents the incoming build metadata payload
type IngestRequest struct {
	SchemaVersion string    `json:"schema_version"`
	Provider      string    `json:"provider"`
	Repo          string    `json:"repo"`
	RepoURL       string    `json:"repo_url"`
	CommitSHA     string    `json:"commit_sha"`
	Branch        string    `json:"branch"`
	BuildID       string    `json:"build_id"`
	RunURL        string    `json:"run_url"`
	Workflow      string    `json:"workflow"`
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
	Image         Image     `json:"image"`
}

// Image represents container image metadata
type Image struct {
	Component string   `json:"component"`
	Name      string   `json:"name"`
	Digest    string   `json:"digest"`
	Tags      []string `json:"tags"`
}

// IngestResponse is the response for successful ingest
type IngestResponse struct {
	OK          bool      `json:"ok"`
	Idempotent  bool      `json:"idempotent"`
	Repo        string    `json:"repo"`
	BuildID     string    `json:"build_id"`
	ImageDigest string    `json:"image_digest"`
	StoredAt    time.Time `json:"stored_at"`
}

// BuildListResponse for GET /repos/{repo}/builds
type BuildListResponse struct {
	Repo       string      `json:"repo"`
	Items      []BuildItem `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// BuildItem represents a build in the list
type BuildItem struct {
	BuildID   string    `json:"build_id"`
	CommitSHA string    `json:"commit_sha"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	RunURL    string    `json:"run_url"`
	Image     Image     `json:"image"`
}

// BuildDetailResponse for GET /builds/{build_id}
type BuildDetailResponse struct {
	Repo      string    `json:"repo"`
	BuildID   string    `json:"build_id"`
	CommitSHA string    `json:"commit_sha"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	RunURL    string    `json:"run_url"`
	Workflow  string    `json:"workflow"`
	Images    []Image   `json:"images"`
}

// ImageDetailResponse for GET /images/{digest}
type ImageDetailResponse struct {
	Digest       string              `json:"digest"`
	Name         string              `json:"name"`
	Components   []string            `json:"components"`
	Tags         []string            `json:"tags"`
	Repos        []string            `json:"repos"`
	RecentBuilds []ImageRecentBuild  `json:"recent_builds"`
}

// ImageRecentBuild represents a build that references an image
type ImageRecentBuild struct {
	Repo      string    `json:"repo"`
	BuildID   string    `json:"build_id"`
	Timestamp time.Time `json:"timestamp"`
}

// ErrorResponse is the standard error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Build represents a build record from database
type Build struct {
	ID          int64
	RepoID      int64
	BuildID     string
	Provider    string
	CommitSHA   string
	Branch      string
	RunURL      string
	Workflow    string
	Status      string
	Timestamp   time.Time
	PayloadHash string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repo represents a repository record
type Repo struct {
	ID        int64
	FullName  string
	URL       string
	CreatedAt time.Time
}

// ImageRecord represents an image record from database
type ImageRecord struct {
	ID        int64
	Digest    string
	Name      string
	CreatedAt time.Time
}

// BuildImage represents the many-to-many relationship
type BuildImage struct {
	BuildID   int64
	ImageID   int64
	Component string
	Tags      []string
}
