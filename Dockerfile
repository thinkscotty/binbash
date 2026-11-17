# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
# --platform=$BUILDPLATFORM pins this stage to the machine doing the building,
# not the machine the image is for. Without it, `buildx --platform
# linux/amd64,linux/arm64` would run the arm64 half of the build inside QEMU on
# an amd64 runner: minutes instead of seconds, and emulated toolchains are a
# well-known source of flaky, hard-to-reproduce build failures.
#
# Go doesn't need emulation. CGO is already off (see the build command below),
# so setting GOARCH is the entire cross-compilation story -- both architectures
# are built natively, side by side, at full speed.
#
# golang:1-alpine is the newest Go release, so a rebuild picks up new Go
# versions and their security fixes without anyone editing this file -- the
# same Go that `go-version: stable` gives .github/workflows/release.yml. The go
# line in go.mod is the floor: the image sets GOTOOLCHAIN=local, so a stale
# cached image older than that fails the build rather than quietly shipping an
# unpatched Go.
FROM --platform=$BUILDPLATFORM golang:1-alpine AS build
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

# Filled in by BuildKit, once per entry in --platform: linux/amd64 gives amd64,
# linux/arm64 gives arm64. There is no TARGETOS here because there is nowhere
# for it to go -- the runtime stage is a Linux base image, so Linux is the only
# thing this Dockerfile can produce.
#
# It is empty under the legacy (non-BuildKit) builder, which is harmless: Go
# treats an empty GOARCH as unset and builds for the host, which is exactly what
# a plain local `docker build` wants anyway.
ARG TARGETARCH

# CGO_ENABLED=0 because modernc.org/sqlite is pure Go: no libsqlite3, no libc,
# nothing to link against. That is what lets the runtime stage below be a base
# image with no operating system in it at all -- and, since there is no C
# toolchain in the picture, what makes GOARCH alone enough to cross-compile.
#
# -trimpath matches .github/workflows/release.yml, so a container built here is
# reproducible against the published release binary rather than merely similar.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /binbash .

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
#
# debian13, not debian12: distroless only rebuilds images for the Debian
# releases it lists as current, and a stale image means a stale CA bundle --
# which is what decides whose certificates binbash trusts.
FROM gcr.io/distroless/static-debian13:nonroot

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
