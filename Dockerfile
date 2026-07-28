# Deliberately plain Docker — no BuildKit-only syntax.
#
# Cache mounts (RUN --mount=type=cache) would speed up rebuilds, but they fail
# outright on builders without BuildKit enabled. Several hosted platforms build
# with plain Docker, and a Dockerfile that only works locally is worse than a
# slightly slower one that works everywhere.

# ---------------------------------------------------------------------------
# Stage 1: build the binary and the Bible corpus
# ---------------------------------------------------------------------------
# Both happen in one stage so the module download and source copy are paid for
# once rather than twice.
FROM golang:1.26-bookworm AS build

WORKDIR /src

# Dependencies first, so source-only changes reuse this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO stays off: the SQLite driver is a WASM build, so the binary is fully
# static and runs on a distroless base.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# Build the corpus at image-build time. This is the whole point of the Docker
# path: the running container starts serving immediately instead of spending
# its first minutes seeding, which on a small instance can exceed a platform's
# port-detection timeout and fail the deploy.
RUN go run ./cmd/seed

# Fail the build rather than shipping a corpus the API will refuse to open.
# cmd/seed checkpoints and leaves rollback-journal mode; a surviving -wal or
# -shm sidecar means that step did not complete, and a WAL-mode database cannot
# be opened from a read-only image layer at all.
RUN test -f ./data/bible.db \
    && ! test -f ./data/bible.db-wal \
    && ! test -f ./data/bible.db-shm

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
# Only the binary and the finished corpus are copied. The ~225MB of seed source
# files under data/ stay in the builder and never ship.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /src/data/bible.db /app/data/bible.db

# The corpus is baked in, so a missing one means the image is broken. Fail fast
# and loudly instead of silently rebuilding it at boot.
ENV SCHOLIA_DB_PATH=/app/data/bible.db \
    SCHOLIA_AUTO_SEED=false

# Documentation only. The server binds $PORT when the platform provides one
# (Render, Fly, Cloud Run all do) and falls back to 8080 otherwise.
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/api"]
