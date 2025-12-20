# AI Agent Instructions for Golang Project

This document outlines the coding standards, architectural patterns, and libraries that must be used when generating code for this project.

## 1. Tech Stack & Versioning

* **Language:** Go (Golang)
* **Version:** `1.25` (or newer). Use modern idioms (Generics, `min`/`max` built-ins, `cmp` package, `iter` package if applicable).
* **Web Framework:** [Echo v4](https://github.com/labstack/echo) (`github.com/labstack/echo/v4`).
* **Configuration:** Environment variables + `.env` file support.
* **Logging:** Standard library `log/slog`.

## 2. Coding Style (Uber Go Style Guide)

Strictly adhere to the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md). Key rules to enforce:

* **Group Imports:** Separate imports into groups: Standard Library, Third Party, Local Packages.
* **Interface Compliance:** Verify interface compliance at compile time:
    ```go
    var _ MyInterface = (*MyImplementation)(nil)
    ```
* **Pointers:**
    * Use pointers for mutability or large structs.
    * Use values for small structs, slices, maps, and channels.
    * Avoid pointers to interfaces.
* **Error Handling:**
    * Check errors immediately.
    * Wrap errors with `fmt.Errorf("%w", err)` for context.
    * Handle `defer` errors (do not ignore them silently).
* **Zero Values:** Use zero values for initialization where possible (`var s sync.Mutex` instead of `s := sync.Mutex{}`).
* **Naming:**
    * PascalCase for exported, camelCase for unexported.
    * Short variable names (e.g., `c` for `echo.Context`, `l` for logger) are acceptable in short scopes, but be descriptive in larger scopes.
* **Functional Options:** Use the Functional Options pattern for complex struct initialization.

## 3. Architecture & Patterns

### 3.1 Project Structure

Organize code by domain or functionality, keeping main execution entry points in `cmd/`.

```
/cmd
  /server      # Main entry point
/internal
  /config      # Configuration logic
  /handlers    # Echo handlers
  /middleware  # Custom middleware
  /models      # Domain models
  /pkg         # Public library code (if any)
```

### 3.2 Configuration Strategy

Define a strong-typed `Config` struct in `internal/config`.

*   Load values only from Environment Variables.
*   Use `github.com/joho/godotenv` to load a `.env` file if present (useful for local dev).
*   Do not hardcode config values.

#### Implementation Pattern

```go
package config

import (
    "os"
    "strconv"
    "time"

    "github.com/joho/godotenv"
)

type Config struct {
    Port        string
    Env         string
    ReadTimeout time.Duration
    Debug       bool
}

func Load() *Config {
    _ = godotenv.Load() // Ignore error, env vars might be set by OS

    return &Config{
        Port:        getEnv("PORT", "8080"),
        Env:         getEnv("APP_ENV", "development"),
        ReadTimeout: getDurationEnv("READ_TIMEOUT", 5*time.Second),
        Debug:       getBoolEnv("DEBUG", false),
    }
}

// Helper functions
func getEnv(key, fallback string) string {
    if value, exists := os.LookupEnv(key); exists {
        return value
    }
    return fallback
}

func getIntEnv(key string, fallback int) int {
    if value, exists := os.LookupEnv(key); exists {
        if i, err := strconv.Atoi(value); err == nil {
            return i
        }
    }
    return fallback
}

func getBoolEnv(key string, fallback bool) bool {
    if value, exists := os.LookupEnv(key); exists {
        if b, err := strconv.ParseBool(value); err == nil {
            return b
        }
    }
    return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
    if value, exists := os.LookupEnv(key); exists {
        if d, err := time.ParseDuration(value); err == nil {
            return d
        }
    }
    return fallback
}
```

### 3.3 Logging Strategy (log/slog)

Use `log/slog` exclusively.

*   **Environment Aware:**
    *   **development:** Use `slog.NewTextHandler` (human-readable).
    *   **production:** Use `slog.NewJSONHandler` (structured parsing).
*   **Context:** Pass `context.Context` to logging methods when possible, or inject the logger into the Echo context.

#### Setup Pattern

```go
func SetupLogger(env string) *slog.Logger {
    var handler slog.Handler
    opts := &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }
    
    if env == "development" {
        opts.Level = slog.LevelDebug
        handler = slog.NewTextHandler(os.Stdout, opts)
    } else {
        handler = slog.NewJSONHandler(os.Stdout, opts)
    }
    
    logger := slog.New(handler)
    slog.SetDefault(logger)
    return logger
}
```

### 3.4 Web Server (Echo)

*   **Context:** Use `echo.Context` (c) strictly for HTTP request/response parsing.
*   **Graceful Shutdown:** Ensure the server listens for SIGINT/SIGTERM and shuts down gracefully with a timeout context.
*   **Handlers:** Handlers should call a Service layer; they should not contain heavy business logic.

#### Example Echo Handler

```go
func (h *Handler) GetItem(c echo.Context) error {
    ctx := c.Request().Context()
    id := c.Param("id")

    item, err := h.service.GetItem(ctx, id)
    if err != nil {
        slog.ErrorContext(ctx, "failed to fetch item", "error", err, "id", id)
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
    }
    
    return c.JSON(http.StatusOK, item)
}
```

## 4. Testing

*   Use the standard `testing` package.
*   Use Table-Driven Tests (`t.Run`) for all logic tests.
*   Use `net/http/httptest` for testing Echo handlers.
*   **Mocks:** Use the [mockery](https://github.com/vektra/mockery) package to generate mocks for interfaces. Mocking external dependencies is mandatory for unit tests.

## 5. Infrastructure & Development Environment

To ensure a seamless developer onboarding experience:

*   **Docker:** If the project requires external dependency services (e.g., PostgreSQL, Redis, NATS), a `docker-compose.yaml` file **must** be provided to spin up these dependencies locally.
*   **Dockerfile:** Provide a multi-stage `Dockerfile` for building and running the application in a containerized environment.
*   **Local Setup:** Include a `Makefile` or setup script if there are complex initialization steps required beyond `docker-compose up`.