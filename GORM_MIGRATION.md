# Migration to GORM - Summary

## 🔄 Changes Made

The RoboHub Ingest Service has been successfully migrated from **pgx** to **GORM** with automatic database migrations.

### Key Changes

#### 1. **Dependency Updates**
- ✅ Replaced `github.com/jackc/pgx/v5` with `gorm.io/gorm` and `gorm.io/driver/postgres`
- ✅ Updated `go.mod` and `go.sum`
- ✅ Removed `testcontainers-go` dependency

#### 2. **Database Layer (`internal/db/store.go`)**
- ✅ **GORM Models**: Defined struct-based models with tags:
  - `Repo` - Repository information
  - `Build` - Build records with indexes
  - `Image` - Container images
  - `BuildImage` - Many-to-many relationship
- ✅ **Automatic Indexes**: GORM creates indexes from struct tags
- ✅ **Store Interface**: Kept the same interface (no breaking changes to HTTP handlers)
- ✅ **GormStore Implementation**: New implementation using GORM methods
- ✅ **Tag Storage**: Changed from PostgreSQL array to comma-separated strings (simpler with GORM)

#### 3. **Main Application (`cmd/robohub-ingest/main.go`)**
- ✅ **GORM Connection**: Uses `gorm.Open()` with PostgreSQL driver
- ✅ **Connection Pooling**: Configured via `sqlDB.SetMaxIdleConns()`, etc.
- ✅ **AutoMigrate**: Replaces manual SQL migrations
  ```go
  gormDB.AutoMigrate(&db.Repo{}, &db.Build{}, &db.Image{}, &db.BuildImage{})
  ```
- ✅ **Automatic on Startup**: Migrations run every time the service starts

#### 4. **Removed Files**
- ❌ `internal/migrate/migrate.go` - No longer needed (GORM AutoMigrate replaces it)
- ❌ `migrations/001_init.up.sql` - Schema defined in Go structs
- ❌ `migrations/001_init.down.sql` - Not needed with AutoMigrate

#### 5. **HTTP API (`internal/httpapi/server.go`)**
- ✅ Changed error handling from `pgx.ErrNoRows` to `gorm.ErrRecordNotFound`
- ✅ Updated type signatures (`int64` → `uint` for IDs)
- ✅ All endpoints continue to work the same way

#### 6. **Tests**
- ✅ Updated `server_test.go` to use `gorm.ErrRecordNotFound`
- ✅ Updated `store_test.go` for GORM integration tests
- ✅ Tests use GORM's `AutoMigrate` for test database setup

## 🎯 Benefits of GORM

### Advantages
1. **Automatic Migrations**: Schema changes happen automatically from Go structs
2. **Type Safety**: Compile-time checks for model fields
3. **Less Boilerplate**: No manual SQL for CRUD operations
4. **Easier Testing**: Simple `AutoMigrate()` for test databases
5. **Relationships**: Built-in support for foreign keys and associations
6. **Query Builder**: Chainable query methods
7. **Hooks**: BeforeCreate, AfterUpdate, etc. (if needed later)

### Trade-offs
- **Slightly Less Control**: Can't fine-tune every SQL query
- **Learning Curve**: Different API from pgx
- **Tag Storage**: Changed from PostgreSQL arrays to comma-separated (simpler but less elegant)

## 📋 Database Schema

GORM creates this schema automatically:

```go
type Repo struct {
    ID        uint      `gorm:"primaryKey"`
    FullName  string    `gorm:"uniqueIndex;not null"`
    URL       string
    CreatedAt time.Time `gorm:"autoCreateTime"`
}

type Build struct {
    ID          uint      `gorm:"primaryKey"`
    RepoID      uint      `gorm:"not null;index"`
    BuildID     string    `gorm:"not null;index"`
    Provider    string    `gorm:"not null"`
    CommitSHA   string    `gorm:"column:commit_sha;not null"`
    Branch      string
    RunURL      string    `gorm:"column:run_url"`
    Workflow    string
    Status      string    `gorm:"not null;index"`
    Ts          time.Time `gorm:"not null;index"`
    PayloadHash string    `gorm:"not null"`
    CreatedAt   time.Time `gorm:"autoCreateTime"`
    UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

type Image struct {
    ID        uint      `gorm:"primaryKey"`
    Digest    string    `gorm:"uniqueIndex;not null"`
    Name      string    `gorm:"not null"`
    CreatedAt time.Time `gorm:"autoCreateTime"`
}

type BuildImage struct {
    BuildID   uint   `gorm:"primaryKey;not null"`
    ImageID   uint   `gorm:"primaryKey;not null;index"`
    Component string
    Tags      string `gorm:"type:text"` // Comma-separated
}
```

## 🚀 How It Works Now

### Startup Sequence
1. Service starts
2. Connects to PostgreSQL via GORM
3. **Runs `AutoMigrate`** - creates/updates tables automatically
4. Starts HTTP server

### AutoMigrate Behavior
- ✅ Creates tables if they don't exist
- ✅ Adds missing columns
- ✅ Creates indexes
- ✅ **Safe**: Does NOT delete existing data
- ✅ **Safe**: Does NOT drop columns
- ❌ Does NOT rename columns (requires manual migration)
- ❌ Does NOT change column types (requires manual migration)

### Tag Storage Change
**Before (pgx with PostgreSQL arrays):**
```sql
tags TEXT[] NOT NULL DEFAULT '{}'
```

**Now (GORM with comma-separated):**
```go
Tags string `gorm:"type:text"` // Stored as "tag1,tag2,tag3"
```

Split on read: `tags = strings.Split(row.Tags, ",")`

## ✅ Testing

### Unit Tests (No DB Required)
```bash
go test ./internal/hash ./internal/auth
```

### Integration Tests (Require DATABASE_URL)
```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/test_db?sslmode=disable"
go test -v ./internal/db
```

### All Tests
```bash
go test ./...
```

## 🔄 Running the Service

### Local Development
```bash
# Start PostgreSQL
docker-compose up -d postgres

# Run service (migrations automatic)
go run cmd/robohub-ingest/main.go
```

### Docker Compose
```bash
# Everything just works - migrations run on startup
docker-compose up --build
```

### First Time Setup
**No manual migrations needed!** GORM creates everything automatically.

## 📊 Migration Comparison

### Before (Manual SQL Migrations)
```go
// migrations/001_init.up.sql
CREATE TABLE repos (...);
CREATE TABLE builds (...);
CREATE INDEX idx_builds_repo_ts ON builds(...);

// internal/migrate/migrate.go
func (r *Runner) Up(ctx context.Context) error {
    // Read SQL files
    // Execute in transaction
    // Track in schema_migrations table
}
```

### After (GORM AutoMigrate)
```go
// internal/db/store.go
type Repo struct {
    ID        uint      `gorm:"primaryKey"`
    FullName  string    `gorm:"uniqueIndex;not null"`
    ...
}

// cmd/robohub-ingest/main.go
func runMigrations(gormDB *gorm.DB) error {
    return gormDB.AutoMigrate(
        &db.Repo{},
        &db.Build{},
        &db.Image{},
        &db.BuildImage{},
    )
}
```

**Result**: ~100 lines of migration code eliminated!

## 🎁 Bonus Features

GORM provides several features we can use in the future:

1. **Soft Deletes**: Add `gorm.DeletedAt` to enable soft deletes
2. **Hooks**: `BeforeCreate`, `AfterUpdate`, etc.
3. **Associations**: `Preload("Repo")` for eager loading
4. **Scopes**: Reusable query logic
5. **Transactions**: Built-in transaction support
6. **Migrations**: Can use `gorm.Migrator` for complex schema changes

## 🔐 Backward Compatibility

### API Endpoints
- ✅ **No Changes** - All endpoints work exactly the same
- ✅ Request/response formats identical
- ✅ Authentication unchanged
- ✅ Rate limiting unchanged

### Database
- ✅ **Compatible** - GORM creates same schema (minor differences in constraints)
- ✅ Existing data will work (if migrating from pgx version)
- ⚠️ **Tags**: If migrating from array storage, need to convert to comma-separated

## 📝 Developer Notes

### Adding New Fields
Just update the struct and restart - GORM adds the column:
```go
type Build struct {
    ...
    NewField string `gorm:"default:value"`
}
```

### Complex Migrations
For renames or type changes, use `gorm.Migrator`:
```go
gormDB.Migrator().RenameColumn(&Build{}, "old_name", "new_name")
```

### Query Examples
```go
// Find with conditions
var builds []Build
gormDB.Where("status = ? AND repo_id = ?", "success", repoID).Find(&builds)

// Join queries
gormDB.Table("builds").
    Joins("JOIN repos ON builds.repo_id = repos.id").
    Where("repos.full_name = ?", "owner/repo").
    Find(&builds)

// Preload associations
gormDB.Preload("Repo").Preload("BuildImages.Image").Find(&builds)
```

## ✅ Status

**Migration Complete!** ✅

- All code refactored
- Tests updated
- Documentation updated
- Service fully functional with automatic migrations
- No manual SQL migrations required anymore

## 🚀 Next Steps

The service is ready to use with GORM! Just run:

```bash
docker-compose up --build
```

Migrations happen automatically on startup. 🎉
