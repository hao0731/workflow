# MongoDB Auth Env Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add optional MongoDB username/password environment-variable authentication across all four services, while preserving URI-only behavior and failing fast when only one credential is provided.

**Architecture:** Centralize MongoDB client option construction in `internal/config` so every service follows the same startup behavior. The config layer will always apply `MONGO_URI`, conditionally add `Auth` only when both credentials are present, and return a validation error when exactly one credential is set. Each `cmd/*` entrypoint will consume this shared helper and stop on validation errors before attempting a Mongo connection.

**Tech Stack:** Go 1.25+, MongoDB Go Driver (`go.mongodb.org/mongo-driver`), Echo v4 services, standard `testing` package (table-driven tests).

### Task 1: Add failing tests for Mongo option building and fail-fast validation

**Files:**
- Create: `internal/config/mongo_options_test.go`
- Test: `internal/config/mongo_options_test.go`

**Step 1: Write the failing test**

```go
package config

import "testing"

func TestConfigMongoClientOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *Config
		wantErr     bool
		wantAuth    bool
		wantUser    string
		wantPass    string
	}{
		{
			name: "uri only",
			cfg: &Config{MongoURI: "mongodb://localhost:27017"},
			wantErr: false,
			wantAuth: false,
		},
		{
			name: "username and password",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoUsername: "workflow_user",
				MongoPassword: "workflow_pass",
			},
			wantErr:  false,
			wantAuth: true,
			wantUser: "workflow_user",
			wantPass: "workflow_pass",
		},
		{
			name: "username only fails",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoUsername: "workflow_user",
			},
			wantErr: true,
		},
		{
			name: "password only fails",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoPassword: "workflow_pass",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := tt.cfg.MongoClientOptions()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if opts == nil {
				t.Fatalf("expected options, got nil")
			}
			if (opts.Auth != nil) != tt.wantAuth {
				t.Fatalf("unexpected auth presence: got %v want %v", opts.Auth != nil, tt.wantAuth)
			}
			if tt.wantAuth {
				if opts.Auth.Username != tt.wantUser || opts.Auth.Password != tt.wantPass {
					t.Fatalf("unexpected auth values: got (%s, %s)", opts.Auth.Username, opts.Auth.Password)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestConfigMongoClientOptions -v`
Expected: FAIL because `MongoUsername`, `MongoPassword`, and `MongoClientOptions` do not exist yet.

**Step 3: Commit failing test**

```bash
git add internal/config/mongo_options_test.go
git commit -m "test(config): add mongo auth option coverage"
```

### Task 2: Implement config fields and Mongo options helper

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/mongo_options.go`
- Test: `internal/config/mongo_options_test.go`

**Step 1: Write minimal implementation in config**

```go
// in Config
MongoUsername string
MongoPassword string

// in Load()
MongoUsername: getEnv("MONGO_USERNAME", ""),
MongoPassword: getEnv("MONGO_PASSWORD", ""),
```

```go
// internal/config/mongo_options.go
package config

import (
	"fmt"

	"go.mongodb.org/mongo-driver/mongo/options"
)

func (c *Config) MongoClientOptions() (*options.ClientOptions, error) {
	hasUser := c.MongoUsername != ""
	hasPass := c.MongoPassword != ""

	if hasUser != hasPass {
		return nil, fmt.Errorf("mongo auth misconfigured: MONGO_USERNAME and MONGO_PASSWORD must both be set or both be empty")
	}

	opts := options.Client().ApplyURI(c.MongoURI)
	if hasUser {
		opts.SetAuth(options.Credential{
			Username: c.MongoUsername,
			Password: c.MongoPassword,
		})
	}

	return opts, nil
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/config -run TestConfigMongoClientOptions -v`
Expected: PASS.

**Step 3: Commit implementation**

```bash
git add internal/config/config.go internal/config/mongo_options.go internal/config/mongo_options_test.go
git commit -m "feat(config): support optional mongo username/password auth"
```

### Task 3: Integrate shared Mongo options in all four services

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `cmd/engine/main.go`
- Modify: `cmd/registry/main.go`
- Modify: `cmd/workflow-api/main.go`

**Step 1: Replace direct `ApplyURI` usage with config helper**

```go
mongoOpts, err := cfg.MongoClientOptions()
if err != nil {
	logger.Error("invalid MongoDB configuration", slog.Any("error", err))
	os.Exit(1)
}

client, err := mongo.Connect(context.Background(), mongoOpts)
```

For `cmd/workflow-api/main.go`, keep the existing timeout context, but still use `cfg.MongoClientOptions()`.

**Step 2: Align workflow-api with shared DB config**

```go
// before: db := mongoClient.Database("orchestration")
db := mongoClient.Database(cfg.MongoDatabase)
```

Also remove its local `os.Getenv("MONGO_URI")` fallback block so all services use `config.Load()` consistently.

**Step 3: Run package build tests**

Run: `go test ./cmd/api ./cmd/engine ./cmd/registry ./cmd/workflow-api`
Expected: PASS (compile/build verification).

**Step 4: Commit integration changes**

```bash
git add cmd/api/main.go cmd/engine/main.go cmd/registry/main.go cmd/workflow-api/main.go
git commit -m "refactor(cmd): unify mongo client initialization"
```

### Task 4: Document new environment variables

**Files:**
- Modify: `.env.example`
- Modify: `docs/technical-design.md`

**Step 1: Add env keys to `.env.example`**

```dotenv
# MongoDB
MONGO_URI=mongodb://localhost:27017
MONGO_DATABASE=orchestration
MONGO_USERNAME=
MONGO_PASSWORD=
```

**Step 2: Update technical design env table**

Add rows for:
- `MONGO_DATABASE` (default `orchestration`)
- `MONGO_USERNAME` (default empty)
- `MONGO_PASSWORD` (default empty)

Include note: startup fails when only one of `MONGO_USERNAME` / `MONGO_PASSWORD` is provided.

**Step 3: Commit docs updates**

```bash
git add .env.example docs/technical-design.md
git commit -m "docs: add mongodb auth env configuration"
```

### Task 5: Final verification and handoff

**Files:**
- Verify only: repository-wide

**Step 1: Run focused unit tests**

Run: `go test ./internal/config ./internal/eventstore ./internal/dsl/...`
Expected: PASS for local unit coverage relevant to Mongo usage.

**Step 2: Run full test suite (optional but recommended)**

Run: `go test ./...`
Expected: PASS, or capture environment-dependent failures explicitly.

**Step 3: Sanity-check startup behavior manually**

Run (example):
- `MONGO_USERNAME=user MONGO_PASSWORD=pass go run ./cmd/engine` -> should continue past config validation
- `MONGO_USERNAME=user go run ./cmd/engine` -> should exit with config validation error
- `MONGO_PASSWORD=pass go run ./cmd/engine` -> should exit with config validation error

**Step 4: Final commit (if needed)**

```bash
git status
git log --oneline -n 5
```

Ensure commit set is clean and messages clearly map to config/test/integration/docs scope.

## Notes

- Keep logic DRY: no per-service credential branching.
- Do not log plaintext credentials.
- Preserve current behavior when both `MONGO_USERNAME` and `MONGO_PASSWORD` are empty.
