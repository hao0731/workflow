# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/workflow-engine ./cmd/engine

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install CA certificates for HTTPS calls
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/workflow-engine .

# Create non-root user
RUN adduser -D -g '' appuser
USER appuser

EXPOSE 8080

ENTRYPOINT ["./workflow-engine"]
