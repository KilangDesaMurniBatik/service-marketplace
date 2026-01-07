# syntax=docker/dockerfile:1

# =============================================================================
# Go Service Dockerfile - Marketplace Service (Production Ready)
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: Build
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy lib-common module
COPY lib-common ./lib-common

# Copy service files
COPY service-marketplace ./service-marketplace

# Build lib-common dependencies first
WORKDIR /build/lib-common
ENV GOTOOLCHAIN=auto
RUN go mod download

# Now build service-marketplace
WORKDIR /build/service-marketplace
ENV GOTOOLCHAIN=auto
RUN go mod download && go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# -----------------------------------------------------------------------------
# Stage 2: Runtime
# -----------------------------------------------------------------------------
FROM alpine:3.19

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Set timezone
ENV TZ=Asia/Kuala_Lumpur

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server .

# Change ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8010

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8010/health || exit 1

# Run the application
CMD ["./server"]
