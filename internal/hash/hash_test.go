package hash

import (
	"testing"
	"time"

	"github.com/robohub/ingest-service/internal/types"
)

func TestComputePayloadHash(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")

	tests := []struct {
		name     string
		req1     *types.IngestRequest
		req2     *types.IngestRequest
		wantSame bool
	}{
		{
			name: "identical requests produce same hash",
			req1: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main", "sha-abc123"},
				},
			},
			req2: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main", "sha-abc123"},
				},
			},
			wantSame: true,
		},
		{
			name: "tags in different order produce same hash",
			req1: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main", "sha-abc123", "latest"},
				},
			},
			req2: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"latest", "sha-abc123", "main"},
				},
			},
			wantSame: true,
		},
		{
			name: "different commit SHA produces different hash",
			req1: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main"},
				},
			},
			req2: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "def456",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main"},
				},
			},
			wantSame: false,
		},
		{
			name: "different status produces different hash",
			req1: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main"},
				},
			},
			req2: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "failure",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main"},
				},
			},
			wantSame: false,
		},
		{
			name: "different image digest produces different hash",
			req1: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Tags:      []string{"main"},
				},
			},
			req2: &types.IngestRequest{
				Repo:      "owner/repo",
				BuildID:   "build-1",
				Provider:  "github_actions",
				CommitSHA: "abc123",
				Branch:    "main",
				RunURL:    "https://github.com/owner/repo/actions/runs/1",
				Workflow:  "CI",
				Status:    "success",
				Timestamp: ts,
				Image: types.Image{
					Component: "perception",
					Name:      "ghcr.io/owner/perception",
					Digest:    "sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
					Tags:      []string{"main"},
				},
			},
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err1 := ComputePayloadHash(tt.req1)
			hash2, err2 := ComputePayloadHash(tt.req2)

			if err1 != nil || err2 != nil {
				t.Fatalf("unexpected error: %v, %v", err1, err2)
			}

			if tt.wantSame && hash1 != hash2 {
				t.Errorf("expected same hashes but got different:\nhash1: %s\nhash2: %s", hash1, hash2)
			}

			if !tt.wantSame && hash1 == hash2 {
				t.Errorf("expected different hashes but got same: %s", hash1)
			}
		})
	}
}

func TestComputePayloadHash_Deterministic(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")

	req := &types.IngestRequest{
		Repo:      "owner/repo",
		BuildID:   "build-1",
		Provider:  "github_actions",
		CommitSHA: "abc123",
		Branch:    "main",
		RunURL:    "https://github.com/owner/repo/actions/runs/1",
		Workflow:  "CI",
		Status:    "success",
		Timestamp: ts,
		Image: types.Image{
			Component: "perception",
			Name:      "ghcr.io/owner/perception",
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Tags:      []string{"c", "a", "b"},
		},
	}

	// Compute hash multiple times
	hashes := make([]string, 10)
	for i := 0; i < 10; i++ {
		hash, err := ComputePayloadHash(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hashes[i] = hash
	}

	// All hashes should be identical
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("hash not deterministic: hashes[0]=%s, hashes[%d]=%s", hashes[0], i, hashes[i])
		}
	}
}
