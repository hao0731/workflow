# Orchestration Engine

A workflow orchestration engine built with Go, using a CQRS/event-sourced architecture with Cassandra for event writes, MongoDB for read models, and NATS JetStream for event streaming.

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose

## Architecture

The project uses two separate Docker Compose files:

| File | Purpose | Services |
|------|---------|----------|
| `docker-compose.yaml` | Infrastructure (long-lived) | MongoDB, NATS, Cassandra |
| `docker-compose.app.yaml` | Applications (rebuild frequently) | engine, api, workflow-api |

Infrastructure and application services communicate over a shared Docker network (`orchestration`). This split lets you rebuild and restart apps without touching infra.

## Getting Started

### 1. Start Infrastructure Services

```bash
docker compose up -d
```

This starts three services on a shared Docker network (`orchestration`):

| Service   | Image             | Purpose                     | Host Port |
|-----------|-------------------|-----------------------------|-----------|
| MongoDB   | mongo:7.0         | Read models, workflow store | 27018     |
| NATS      | nats:2.10-alpine  | Event streaming (JetStream) | 4222 (client), 8222 (monitoring) |
| Cassandra | cassandra:4.1     | Event write store           | -         |

- **MongoDB** is exposed on `localhost:27018` for tools like [MongoDB Compass](https://www.mongodb.com/products/compass).
- **NATS** client connections on `localhost:4222`, monitoring dashboard at `http://localhost:8222`.
- **Cassandra** is only accessible within the Docker network.

### 2. Wait for Cassandra to be Ready

Cassandra takes about 30-60 seconds to start. Check its health:

```bash
docker compose ps
```

Wait until the cassandra service shows `healthy` status.

### 3. Initialize Cassandra Schema

```bash
docker exec -i orchestration-cassandra cqlsh < scripts/cassandra-init.cql
```

This creates the `orchestration` keyspace and the `events` table.

### 4. Configure Environment

```bash
cp .env.example .env
```

Edit `.env` as needed. The defaults work for local development with the Docker Compose setup.

### 5. Run Application Services

**Option A: Docker (recommended)**

Make sure infrastructure is running first (`docker compose ps`), then:

```bash
# Build and start all app services (engine, api, workflow-api)
docker compose -f docker-compose.app.yaml up --build -d

# View logs
docker compose -f docker-compose.app.yaml logs -f
```

| Service        | Host Port | Health Check |
|----------------|-----------|--------------|
| `api`          | 8081      | `curl http://localhost:8081/api/workflows` |
| `workflow-api` | 8083      | `curl http://localhost:8083/health` |
| `engine`       | —         | Check logs |

The `workflow-api` service exposes workflow CRUD endpoints, including `GET /api/workflows/:id/source` for persisted DSL source and definition metadata.

**Option B: Run on host with Go**

```bash
# Build all services
go build ./...

# Run the workflow engine
go run ./cmd/engine

# Run the workflow API (in a separate terminal)
go run ./cmd/workflow-api

# Run the API server (in a separate terminal)
go run ./cmd/api
```

## Configuration

Configuration is loaded from environment variables (or `.env` file). Key settings:

| Variable            | Default                      | Description                          |
|---------------------|------------------------------|--------------------------------------|
| `APP_ENV`           | `development`                | Application environment              |
| `PORT`              | `8081`                       | HTTP server port                     |
| `MONGO_URI`         | `mongodb://localhost:27018`  | MongoDB connection string            |
| `MONGO_DATABASE`    | `orchestration`              | MongoDB database name                |
| `NATS_URL`          | `nats://localhost:4222`      | NATS server URL                      |
| `CASSANDRA_HOSTS`   | `localhost`                  | Cassandra cluster hosts (comma-separated) |
| `CASSANDRA_KEYSPACE`| `orchestration`              | Cassandra keyspace                   |
| `CASSANDRA_PORT`    | `9042`                       | Cassandra CQL port                   |
| `MIGRATION_PHASE`   | `shadow`                     | Event store migration phase          |

## Running Tests

```bash
# Run all tests (MongoDB/Cassandra tests skip if services are unavailable)
go test ./...

# Run with verbose output
go test ./... -v
```

## Stopping Services

```bash
# Stop app services only (infra stays running)
docker compose -f docker-compose.app.yaml down

# Restart apps (rebuild and start fresh)
docker compose -f docker-compose.app.yaml up --build -d

# Stop infra services (data persists in volumes)
docker compose down

# Stop infra and delete all data
docker compose down -v
```
