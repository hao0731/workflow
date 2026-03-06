# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build all service binaries
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/engine ./cmd/engine
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/workflow-api ./cmd/workflow-api
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/orchestrator ./cmd/orchestrator
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/scheduler ./cmd/scheduler
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-w -s" -o /app/worker-firstparty ./cmd/worker-firstparty

# Runtime stage
FROM alpine:3.19

WORKDIR /app

RUN apk --no-cache add ca-certificates

# Copy binaries from builder
COPY --from=builder /app/engine .
COPY --from=builder /app/api .
COPY --from=builder /app/workflow-api .
COPY --from=builder /app/orchestrator .
COPY --from=builder /app/scheduler .
COPY --from=builder /app/worker-firstparty .

# Copy scripts for schema init
COPY scripts/ ./scripts/

# Create non-root user and grant ownership
RUN adduser -D -g '' appuser && chown -R appuser:appuser /app
USER appuser

ENTRYPOINT ["./engine"]
