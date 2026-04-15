# =============================================================================
# OpenBotStack Runtime - Multi-stage Docker Build
# =============================================================================
#
# Build context MUST include both openbotstack-runtime and openbotstack-core
# because go.mod uses a replace directive:
#   replace github.com/openbotstack/openbotstack-core => ../openbotstack-core
#
# Build from the PARENT directory (openbotstack/):
#   docker build -t openbotstack:latest -f openbotstack-runtime/Dockerfile .
#
# Or from this directory with a custom context:
#   docker build -t openbotstack:latest - < Dockerfile --build-context=..
#
# For development without Docker, use: make binary && ./build/openbotstack
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build frontend (React + Vite)
# ---------------------------------------------------------------------------
FROM node:22-alpine AS frontend

WORKDIR /build/web

COPY openbotstack-runtime/web/package*.json ./
RUN npm ci --ignore-scripts

COPY openbotstack-runtime/web/ ./
RUN npm run build
# Output: /build/web/webui/dist/

# ---------------------------------------------------------------------------
# Stage 2: Build Go binary
# ---------------------------------------------------------------------------
FROM golang:1.23-alpine AS backend

WORKDIR /build

# Copy Go module files from both repos (replace directive needs core)
COPY openbotstack-core/ openbotstack-core/
COPY openbotstack-runtime/ openbotstack-runtime/

# Set working directory to the runtime module
WORKDIR /build/openbotstack-runtime

# Download dependencies
RUN go mod download

# Copy frontend build output into the embedded location
COPY --from=frontend /build/web/webui/dist ./web/webui/dist

# Build the binary with version info and stripped symbols
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT}" \
    -o /openbotstack ./cmd/openbotstack

# ---------------------------------------------------------------------------
# Stage 3: Minimal runtime image
# ---------------------------------------------------------------------------
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary
COPY --from=backend /openbotstack /usr/local/bin/openbotstack

# Copy default config
COPY openbotstack-runtime/config.yaml /etc/openbotstack/config.yaml

# Create data directory for SQLite and markdown memory
RUN mkdir -p /app/data /app/examples

# Copy example skills (optional, useful for first run)
COPY --from=backend /build/openbotstack-runtime/examples/skills/ /app/examples/skills/

EXPOSE 8080

# Persistent volumes: database + memory data + config override
VOLUME ["/app/data", "/app/openbotstack.db"]

# Health check using wget (available in Alpine)
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["openbotstack"]
CMD ["--config", "/etc/openbotstack/config.yaml"]
