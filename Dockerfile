# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

# Dependencies resolve in their own layer, so editing Go source doesn't
# re-download the module graph on every rebuild.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# What the settings page reports and what the updater compares against. Left at
# "dev" every image looks alike, which makes "which version is actually running
# on the server?" unanswerable -- so the compose file passes the real tag.
ARG VERSION=dev

# CGO_ENABLED=0 because modernc.org/sqlite is pure Go: no libsqlite3, no libc,
# nothing to link against. That is what lets the runtime stage below be a base
# image with no operating system in it at all.
#
# -trimpath matches .github/workflows/release.yml, so a container built here is
# reproducible against the published release binary rather than merely similar.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /binbash .

# The runtime image has no shell, so /data has to be built here and copied in.
# It has to exist in the image at all because Docker creates a missing volume
# mountpoint owned by root, which a non-root container then cannot write to.
# The ownership below is what a fresh named volume inherits.
RUN mkdir -p /newdata

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
# distroless/static carries exactly what binbash needs and nothing more: CA
# certificates (the AI provider and the update check are both HTTPS), tzdata
# (so TZ below is honoured), /etc/passwd, and /tmp. There is no shell, no
# package manager and no libc, so the usual container attack surface is
# missing rather than merely unused.
#
# :nonroot pins uid 65532. See `user:` in compose.yaml -- with a bind mount you
# want this overridden to whoever owns the directory on the host.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /binbash /app/binbash
# COPY normalizes the mode to 0755 and there is no portable way to set it here
# (BuildKit's --chmod is not honoured by every builder). Harmless: db.Open
# tightens the directory to 0700 on first start and logs one line saying so.
COPY --from=build --chown=65532:65532 /newdata /data

ENV BINBASH_DB_PATH=/data/binbash.db

# Kept deliberately, as a floor under `docker rm` on a container someone forgot
# to give a mount: the inventory survives in an anonymous volume instead of
# vanishing with the container's writable layer. An explicit -v or compose
# `volumes:` entry overrides it, which is the normal case.
VOLUME ["/data"]

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/binbash"]
