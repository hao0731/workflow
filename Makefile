.PHONY: dev deps up down test build clean mocks lint lint-fix

# Start dependencies (MongoDB + NATS)
up:
	docker-compose up -d

# Stop dependencies
down:
	docker-compose down

# Install Go dependencies
deps:
	go mod download
	go mod tidy

# Generate mocks
mocks:
	@export PATH=$$PATH:$$(go env GOPATH)/bin && mockery

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-cover:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Build the binary
build:
	go build -o bin/workflow-engine ./cmd/engine

# Run locally (requires deps running)
dev: up
	@sleep 2
	go run ./cmd/engine

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Setup local development environment
setup:
	cp -n .env.example .env || true
	$(MAKE) deps
	$(MAKE) up
	@echo "✅ Development environment ready!"
	@echo "   MongoDB: localhost:27017"
	@echo "   NATS: localhost:4222 (HTTP: localhost:8222)"

# Run linter
lint:
	golangci-lint run ./...

# Run linter with auto-fix
lint-fix:
	golangci-lint run --fix ./...

