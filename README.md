# RoboHub Ingest Service

A production-grade HTTP service for ingesting build metadata from CI systems (GitHub Actions), authenticating via JWT, and storing build/image data in PostgreSQL.

## Features

- **Build Metadata Ingestion**: POST endpoint for receiving build data from CI
- **JWT Authentication**: HS256 JWT validation with repo-based authorization
- **Idempotency**: Deterministic payload hashing prevents duplicate ingestion
- **Conflict Detection**: Detects when same build ID has different payloads
- **Query APIs**: Retrieve builds, images, and metadata
- **Rate Limiting**: Per-repo token bucket rate limiting
- **Health Checks**: `/healthz` and `/readyz` endpoints
- **Database Migrations**: Automatic schema migration on startup
- **Docker Support**: Full docker-compose setup for local development

## Architecture

```
cmd/robohub-ingest/     - Main application entry point
internal/
  ├── auth/             - JWT validation and claims extraction
  ├── config/           - Environment-based configuration
  ├── db/               - PostgreSQL store implementation
  ├── hash/             - Payload canonicalization and hashing
  ├── httpapi/          - HTTP handlers and routing
  ├── migrate/          - Database migration runner
  ├── ratelimit/        - Token bucket rate limiter
  └── types/            - Shared data types
migrations/             - SQL migration files
```

## Prerequisites

- Go 1.22+
- Docker & Docker Compose (for local development)
- PostgreSQL 15+ (if running outside Docker)

## Quick Start

### Using Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone <repo-url>
cd ingest-service
```

2. Start all services:
```bash
docker-compose up --build
```

The service will be available at `http://localhost:8081`.

### Manual Setup

1. Install dependencies:
```bash
go mod download
```

2. Set up PostgreSQL database:
```bash
createdb robohub_ingest
```

3. Configure environment variables:
```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/robohub_ingest?sslmode=disable"
export ROBOHUB_JWT_SECRET="your-secret-key"
```

4. Run the service:
```bash
go run cmd/robohub-ingest/main.go
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP server port |
| `DATABASE_URL` | `postgres://robohub:robohub@localhost:5432/robohub_ingest?sslmode=disable` | PostgreSQL connection string |
| `ROBOHUB_JWT_SECRET` | `dev-secret-change-in-production` | JWT signing secret (HS256) |
| `ROBOHUB_CLOCK_SKEW_SECONDS` | `60` | Allowed clock skew for JWT validation |
| `ROBOHUB_RATE_LIMIT_RPS` | `10` | Requests per second per repo |
| `ROBOHUB_RATE_LIMIT_BURST` | `20` | Burst capacity for rate limiter |
| `ROBOHUB_MAX_BODY_BYTES` | `262144` | Max request body size (256KB) |

## API Endpoints

### Health Checks

#### GET /healthz
Basic health check (always returns 200 if service is running).

**Response:**
```
ok
```

#### GET /readyz
Readiness check (returns 200 only if database is accessible).

**Response:**
```
ok
```

### Authenticated Endpoints

All endpoints below require `Authorization: Bearer <JWT>` header.

#### POST /ingest/build

Ingest build metadata from CI.

**Request:**
```json
{
  "schema_version": "1.0",
  "provider": "github_actions",
  "repo": "owner/repo",
  "repo_url": "https://github.com/owner/repo",
  "commit_sha": "abc123...",
  "branch": "main",
  "build_id": "gha-123-1",
  "run_url": "https://github.com/owner/repo/actions/runs/123",
  "workflow": "RoboHub CI",
  "status": "success",
  "timestamp": "2024-01-15T10:30:00Z",
  "image": {
    "component": "perception",
    "name": "ghcr.io/owner/perception",
    "digest": "sha256:1234567890abcdef...",
    "tags": ["main", "sha-abc123"]
  }
}
```

**Response (200 OK):**
```json
{
  "ok": true,
  "idempotent": false,
  "repo": "owner/repo",
  "build_id": "gha-123-1",
  "image_digest": "sha256:1234567890abcdef...",
  "stored_at": "2024-01-15T10:30:01Z"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid JSON or missing required fields
- `401 Unauthorized` - Missing or invalid JWT
- `403 Forbidden` - Token repo doesn't match request repo
- `409 Conflict` - Same build_id exists with different payload
- `413 Payload Too Large` - Body exceeds max size
- `429 Too Many Requests` - Rate limit exceeded

#### GET /repos/{repo}/builds?limit=50&cursor=<opaque>&status=success

List builds for a repository.

**Query Parameters:**
- `limit` (optional): Number of results (default 50, max 100)
- `cursor` (optional): Opaque pagination cursor
- `status` (optional): Filter by status (`success` or `failure`)

**Response:**
```json
{
  "repo": "owner/repo",
  "items": [
    {
      "build_id": "gha-123-1",
      "commit_sha": "abc123",
      "status": "success",
      "timestamp": "2024-01-15T10:30:00Z",
      "run_url": "https://github.com/owner/repo/actions/runs/123",
      "image": {
        "component": "perception",
        "name": "ghcr.io/owner/perception",
        "digest": "sha256:1234...",
        "tags": ["main"]
      }
    }
  ],
  "next_cursor": "base64encodedcursor"
}
```

#### GET /builds/{build_id}

Get detailed information about a specific build.

**Response:**
```json
{
  "repo": "owner/repo",
  "build_id": "gha-123-1",
  "commit_sha": "abc123",
  "status": "success",
  "timestamp": "2024-01-15T10:30:00Z",
  "run_url": "https://github.com/owner/repo/actions/runs/123",
  "workflow": "RoboHub CI",
  "images": [
    {
      "component": "perception",
      "name": "ghcr.io/owner/perception",
      "digest": "sha256:1234...",
      "tags": ["main"]
    }
  ]
}
```

#### GET /images/{digest}

Get information about an image (with or without `sha256:` prefix).

**Response:**
```json
{
  "digest": "sha256:1234...",
  "name": "ghcr.io/owner/perception",
  "components": ["perception"],
  "tags": ["main", "sha-abc123"],
  "repos": ["owner/repo"],
  "recent_builds": [
    {
      "repo": "owner/repo",
      "build_id": "gha-123-1",
      "timestamp": "2024-01-15T10:30:00Z"
    }
  ]
}
```

## JWT Authentication

The service validates JWT tokens with the following requirements:

- **Algorithm**: HS256
- **Issuer**: Must be `robohub-auth`
- **Audience**: Must include `robohub-api`
- **Custom Claims**:
  - `repo`: Repository full name (e.g., `owner/repo`)
- **Time Validation**: Checks `exp`, `iat`, `nbf` with configurable clock skew

### Generating Test Tokens

Example using `jwt-cli`:
```bash
jwt encode \
  --secret "your-secret" \
  --alg HS256 \
  --iss "robohub-auth" \
  --aud "robohub-api" \
  --exp "+1h" \
  '{"repo":"owner/repo"}'
```

## Database Schema

### Tables

- **repos**: Repository metadata
- **builds**: Build records with payload hashing for idempotency
- **images**: Container image metadata
- **build_images**: Many-to-many relationship between builds and images
- **schema_migrations**: Migration tracking

### Idempotency

Idempotency is enforced via payload hash:
1. Extract material fields (repo, build_id, commit_sha, status, image, etc.)
2. Sort image tags deterministically
3. JSON-marshal canonical struct
4. Compute SHA256 hash
5. Compare with existing record's hash

Same hash = idempotent operation.
Different hash = 409 Conflict.

## Testing

### Run Unit Tests

```bash
go test ./...
```

### Run Integration Tests

Integration tests require a PostgreSQL database:

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/test_db?sslmode=disable"
go test -v ./internal/db
```

### Test Coverage

```bash
go test -cover ./...
```

## Development

### Project Structure

```
.
├── cmd/
│   └── robohub-ingest/
│       └── main.go
├── internal/
│   ├── auth/
│   │   ├── jwt.go
│   │   └── jwt_test.go
│   ├── config/
│   │   └── config.go
│   ├── db/
│   │   ├── store.go
│   │   └── store_test.go
│   ├── hash/
│   │   ├── hash.go
│   │   └── hash_test.go
│   ├── httpapi/
│   │   ├── server.go
│   │   └── server_test.go
│   ├── migrate/
│   │   └── migrate.go
│   ├── ratelimit/
│   │   └── ratelimit.go
│   └── types/
│       └── types.go
├── migrations/
│   ├── 001_init.up.sql
│   └── 001_init.down.sql
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

### Adding Migrations

1. Create new migration files in `migrations/`:
   - `NNN_name.up.sql` - Apply migration
   - `NNN_name.down.sql` - Rollback migration

2. Migrations run automatically on service startup.

### Running Locally

1. Start dependencies:
```bash
docker-compose up -d postgres
```

2. Run service:
```bash
go run cmd/robohub-ingest/main.go
```

3. Send test request:
```bash
curl -X POST http://localhost:8081/ingest/build \
  -H "Authorization: Bearer <your-jwt>" \
  -H "Content-Type: application/json" \
  -d @test-payload.json
```

## Deployment

### Docker

Build image:
```bash
docker build -t robohub-ingest:latest .
```

Run container:
```bash
docker run -p 8081:8081 \
  -e DATABASE_URL="postgres://..." \
  -e ROBOHUB_JWT_SECRET="..." \
  robohub-ingest:latest
```

### Production Considerations

1. **Security**:
   - Always set `ROBOHUB_JWT_SECRET` to a strong random value
   - Use TLS/HTTPS in production
   - Enable PostgreSQL SSL (`sslmode=require`)
   - Run as non-root user

2. **Scalability**:
   - Service is stateless and can be horizontally scaled
   - Consider connection pooling for PostgreSQL
   - Monitor rate limiter effectiveness

3. **Monitoring**:
   - Service logs JSON-formatted logs to stdout
   - Monitor `/readyz` for health checks
   - Set up alerts on 5xx errors and high latency

4. **Database**:
   - Regular backups of PostgreSQL
   - Monitor index performance
   - Consider partitioning builds table by date for very high volumes

## Troubleshooting

### Database Connection Issues

Check that PostgreSQL is running:
```bash
docker-compose ps
```

Test connection:
```bash
psql $DATABASE_URL -c "SELECT 1"
```

### JWT Validation Errors

Common issues:
- Wrong secret key
- Token expired (check `exp` claim)
- Wrong issuer or audience
- Missing `repo` claim

### Rate Limiting

If hitting rate limits, adjust environment variables:
```bash
export ROBOHUB_RATE_LIMIT_RPS=50
export ROBOHUB_RATE_LIMIT_BURST=100
```

## License

[Your License Here]

## Contributing

[Your Contributing Guidelines Here]
