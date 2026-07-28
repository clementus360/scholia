# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Stage 1: build the API binary
# ---------------------------------------------------------------------------
FROM golang:1.26-bookworm AS build

WORKDIR /src

# Dependencies first so the module cache survives source-only changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# CGO stays off: the SQLite driver is a WASM build, so the binary is fully
# static and can run on a distroless base.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# ---------------------------------------------------------------------------
# Stage 2: build the Bible corpus
# ---------------------------------------------------------------------------
# The corpus is generated at image build time and baked into the final image.
# It is immutable, versioned with the image, and needs no volume — which is the
# whole point of keeping it in SQLite while user data lives in Postgres.
#
# If the seed inputs under data/ are large or slow to process, build this stage
# once and push it to a registry, then swap the FROM below for that tag.
FROM golang:1.26-bookworm AS corpus

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# cmd/seed finishes by checkpointing the WAL and switching to rollback
# journalling, leaving a single self-contained file. That step is required: a
# WAL-mode database cannot be opened from a read-only image layer, because even
# a pure reader must write the -shm sidecar.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go run ./cmd/seed

# Fail the build rather than shipping a corpus the API will refuse to open.
RUN test -f ./data/bible.db \
    && ! test -f ./data/bible.db-wal \
    && ! test -f ./data/bible.db-shm

# ---------------------------------------------------------------------------
# Stage 3: runtime
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=corpus /src/data/bible.db /app/data/bible.db

ENV SCHOLIA_DB_PATH=/app/data/bible.db \
    PORT=8080

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/api"]
