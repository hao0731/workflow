# Orchestration Engine

A workflow orchestration engine built with Go, MongoDB, Cassandra, and NATS JetStream.

## Supported Runtime Topology

The supported Workflow V2 runtime is split into five services:

| Service | Responsibility | Default Port |
|---|---|---|
| `workflow-api` | Workflow CRUD and execution entrypoint | `8083` |
| `orchestrator` | DAG traversal and runtime scheduling decisions | — |
| `scheduler` | Worker dispatch, node-result validation, and DLQ publishing | — |
| `worker-firstparty` | Repository-provided `StartNode`, `JoinNode`, and `PublishEvent` workers | — |
| `registry` | Node registration and WebSocket proxy | `8084` |

`cmd/api` and `cmd/engine` are deprecated compatibility wrappers and are not part of the supported topology.

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose

## Docker Compose Layout

The project uses two Docker Compose files:

| File | Purpose | Services |
|---|---|---|
| `docker-compose.yaml` | Infrastructure | MongoDB, NATS, Cassandra |
| `docker-compose.app.yaml` | Supported application services | `workflow-api`, `orchestrator`, `scheduler`, `worker-firstparty`, `registry` |

Infrastructure and application services communicate over the shared `orchestration` Docker network.

## Getting Started

### 1. Start Infrastructure

```bash
docker compose up -d
```

This starts MongoDB, NATS, and Cassandra.

### 2. Initialize Cassandra

Wait for Cassandra to become healthy, then run:

```bash
docker exec -i orchestration-cassandra cqlsh < scripts/cassandra-init.cql
```

### 3. Configure Environment

```bash
cp .env.example .env
```

The defaults work for the local Docker topology.

### 4. Start Supported Application Services

```bash
docker compose -f docker-compose.app.yaml up --build -d
docker compose -f docker-compose.app.yaml logs -f
```

| Service | Host Port | Health Check |
|---|---|---|
| `workflow-api` | `8083` | `curl http://localhost:8083/health` |
| `registry` | `8084` | `curl http://localhost:8084/health` |
| `orchestrator` | — | `docker compose -f docker-compose.app.yaml logs orchestrator` |
| `scheduler` | — | `docker compose -f docker-compose.app.yaml logs scheduler` |
| `worker-firstparty` | — | `docker compose -f docker-compose.app.yaml logs worker-firstparty` |

### 5. Run on Host Instead of Docker

```bash
go run ./cmd/workflow-api
go run ./cmd/orchestrator
go run ./cmd/scheduler
go run ./cmd/worker-firstparty
go run ./cmd/registry
```

## Configuration

Configuration is loaded from environment variables and optional `.env`.

| Variable | Default | Description |
|---|---|---|
| `APP_ENV` | `development` | Application environment |
| `PORT` | `8081` | HTTP port for services that use `PORT` |
| `WORKFLOW_API_PORT` | `8083` | HTTP port for `workflow-api` |
| `MONGO_URI` | `mongodb://localhost:27018` | MongoDB connection string |
| `MONGO_DATABASE` | `orchestration` | MongoDB database name |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `CASSANDRA_HOSTS` | `localhost` | Cassandra cluster hosts |
| `CASSANDRA_KEYSPACE` | `orchestration` | Cassandra keyspace |
| `CASSANDRA_PORT` | `9042` | Cassandra CQL port |
| `JWT_SECRET` | empty | Registry JWT signing secret |

## Verification

Use this sequence to verify the supported runtime topology:

```bash
go test ./...
go build ./cmd/workflow-api ./cmd/orchestrator ./cmd/scheduler ./cmd/worker-firstparty ./cmd/registry
docker compose -f docker-compose.app.yaml up --build -d
curl http://localhost:8083/health
curl http://localhost:8084/health
docker compose -f docker-compose.app.yaml ps
```

`docker compose -f docker-compose.app.yaml ps` should show exactly these application services: `workflow-api`, `orchestrator`, `scheduler`, `worker-firstparty`, and `registry`.

## Stopping Services

```bash
docker compose -f docker-compose.app.yaml down
docker compose down
docker compose down -v
```
