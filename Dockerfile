# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application with static linking
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o robohub-ingest ./cmd/robohub-ingest

# Make binary executable (important for multi-stage builds)
RUN chmod +x robohub-ingest

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser

WORKDIR /app

# Copy binary from builder with explicit permissions
COPY --from=builder --chmod=0755 /app/robohub-ingest .

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

EXPOSE 8081

CMD ["./robohub-ingest"]
