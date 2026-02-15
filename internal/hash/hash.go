package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/robohub/ingest-service/internal/types"
)

// CanonicalPayload represents the material fields for idempotency checking
type CanonicalPayload struct {
	Repo      string          `json:"repo"`
	BuildID   string          `json:"build_id"`
	Provider  string          `json:"provider"`
	CommitSHA string          `json:"commit_sha"`
	Branch    string          `json:"branch"`
	RunURL    string          `json:"run_url"`
	Workflow  string          `json:"workflow"`
	Status    string          `json:"status"`
	Timestamp time.Time       `json:"timestamp"`
	Image     CanonicalImage  `json:"image"`
}

// CanonicalImage represents the image with sorted tags
type CanonicalImage struct {
	Component string   `json:"component"`
	Name      string   `json:"name"`
	Digest    string   `json:"digest"`
	Tags      []string `json:"tags"`
}

// ComputePayloadHash computes a deterministic SHA256 hash of the material payload fields
func ComputePayloadHash(req *types.IngestRequest) (string, error) {
	// Create canonical representation with sorted tags
	sortedTags := make([]string, len(req.Image.Tags))
	copy(sortedTags, req.Image.Tags)
	sort.Strings(sortedTags)

	canonical := CanonicalPayload{
		Repo:      req.Repo,
		BuildID:   req.BuildID,
		Provider:  req.Provider,
		CommitSHA: req.CommitSHA,
		Branch:    req.Branch,
		RunURL:    req.RunURL,
		Workflow:  req.Workflow,
		Status:    req.Status,
		Timestamp: req.Timestamp,
		Image: CanonicalImage{
			Component: req.Image.Component,
			Name:      req.Image.Name,
			Digest:    req.Image.Digest,
			Tags:      sortedTags,
		},
	}

	// Marshal to JSON (Go's encoding/json maintains stable struct field order)
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}

	// Compute SHA256
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
