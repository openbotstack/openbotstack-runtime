# =============================================================================
# OpenBotStack Runtime - Multi-stage Docker Build
# =============================================================================
#
# Build from the PARENT directory (openbotstack/):
#   docker build -t openbotstack:latest -f openbotstack-runtime/Dockerfile .
#
# For development without Docker, use: make binary && ./build/openbotstack
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build User SPA
# ---------------------------------------------------------------------------
FROM node:22-alpine AS user-spa

WORKDIR /app
COPY openbotstack-runtime/web/user/package*.json ./
RUN npm ci --ignore-scripts
COPY openbotstack-runtime/web/user/ ./
RUN OUTDIR=/app/dist npm run build

# ---------------------------------------------------------------------------
# Stage 2: Build Admin SPA
# ---------------------------------------------------------------------------
FROM node:22-alpine AS admin-spa

WORKDIR /app
COPY openbotstack-runtime/web/admin/package*.json ./
RUN npm ci --ignore-scripts
COPY openbotstack-runtime/web/admin/ ./
RUN OUTDIR=/app/dist npm run build

# ---------------------------------------------------------------------------
# Stage 3: Build Go binary
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS backend

WORKDIR /build
COPY openbotstack-core/ openbotstack-core/
COPY openbotstack-runtime/ openbotstack-runtime/

WORKDIR /build/openbotstack-runtime
RUN go mod download

# Copy frontend builds into embedded locations
COPY --from=user-spa /app/dist ./web/webui/user/dist
COPY --from=admin-spa /app/dist ./web/webui/admin/dist

ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /openbotstack ./cmd/openbotstack

# ---------------------------------------------------------------------------
# Stage 4: Runtime image
# ---------------------------------------------------------------------------
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

COPY --from=backend /openbotstack /usr/local/bin/openbotstack
COPY openbotstack-runtime/config.yaml /etc/openbotstack/config.yaml

RUN mkdir -p /app/data /app/examples
COPY --from=backend /build/openbotstack-runtime/examples/skills/ /app/examples/skills/

EXPOSE 8080
VOLUME ["/app/data", "/app/openbotstack.db"]

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["openbotstack"]
CMD ["--config", "/etc/openbotstack/config.yaml"]
