# sofalog ships as one binary with the SPA embedded. Three stages: build the
# frontend, compile the Go binary around it, then copy only the binary into a
# minimal runtime image.

# ---- Stage 1: build the Svelte SPA -------------------------------------------
FROM node:22-alpine AS frontend

WORKDIR /src/frontend
# Copy the manifests first so a dependency-free source change reuses the
# npm ci layer.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
# vite.config.js writes to ../server/web/dist with emptyOutDir: false, so the
# committed stub index.html must exist for the build to layer on top of.
COPY server/web/dist/ /src/server/web/dist/
RUN npm run build

# ---- Stage 2: compile the Go binary with the SPA embedded --------------------
FROM golang:1.26-alpine AS backend

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
# Stage 1's output overwrites the committed stub. go:embed fails to compile
# against a missing directory, which is why dist/ is copied and not generated.
COPY --from=frontend /src/server/web/dist/ ./web/dist/

# modernc.org/sqlite is pure Go, so the binary is static and needs no libc in
# the runtime image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sofalog .

# ---- Stage 3: runtime --------------------------------------------------------
FROM alpine:3.21

# ca-certificates is not optional: without it every HTTPS call to TMDB, Open
# Library and Steam fails with an x509 error, and the only symptom is a
# provider that appears permanently unreachable.
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 sofalog \
    && adduser -S -u 10001 -G sofalog sofalog

COPY --from=backend /out/sofalog /usr/local/bin/sofalog

# The runtime volume: SQLite database, downloaded artwork, provider cache.
ENV DATA_DIR=/data
RUN mkdir -p /data && chown sofalog:sofalog /data
VOLUME /data

USER sofalog:sofalog
EXPOSE 8080

# BASE_URL has no sensible default and is required — the container exits with a
# message naming it if unset. TLS is the reverse proxy's job; this listener is
# plain HTTP.
ENTRYPOINT ["/usr/local/bin/sofalog"]
