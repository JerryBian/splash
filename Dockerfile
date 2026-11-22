# syntax=docker/dockerfile:1
#
# splash — multi-stage build.
#
#   web     builds the React app (architecture-independent JavaScript)
#   build   compiles it into the Go binary via go:embed
#   runtime a static binary on a static base
#
# Both build stages are pinned to --platform=$BUILDPLATFORM, so they always run
# natively on the builder and cross-compile towards the target. That matters:
# without it, a multi-arch build runs the whole Vite build under QEMU emulation
# for every non-native architecture, which turns a two-minute build into a
# fifteen-minute one. Cross-compiling Go needs no emulation, and the frontend
# output is the same bytes everywhere, so nothing is lost.
#
# The result is one static binary with no libc, no shell and no package manager
# in the runtime image.

# --- Stage 1: frontend -------------------------------------------------------
# node:26 matches the "engines" range in web/package.json.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web

WORKDIR /src/web

# Dependencies are installed before the source is copied, so editing a .tsx file
# does not invalidate this layer.
COPY web/package.json web/pnpm-lock.yaml ./

# Node 25+ no longer ships corepack (`corepack enable` exits 127 on
# node:26-alpine), so install pnpm directly at the exact version pinned by the
# packageManager field — the same no-drift guarantee corepack used to give.
RUN npm install -g "pnpm@$(node -p 'require("./package.json").packageManager.split("@")[1]')"
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# --- Stage 2: binary ---------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

WORKDIR /src

# go.sum is globbed because a dependency-free module has none. When the first
# dependency lands, this line already picks it up and the cache still works.
COPY go.mod go.sum* ./
RUN --mount=type=cache,id=go,target=/go/pkg/mod \
    go mod download

COPY . .

# The Vite output lands where internal/web/web.go embeds it. This is a merge,
# not a replace, which is why .dockerignore keeps a stale host-side build out of
# the context.
COPY --from=web /src/web/dist/ /src/internal/web/dist/

# BuildKit sets TARGETOS/TARGETARCH for the platform being built; Go does the
# cross-compilation itself. CGO stays off so the binary is static and can run on
# the distroless base.
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# GOARM is ignored unless GOARCH=arm; it keeps the 32-bit ARM image on the
# ARMv7 baseline (the only arm variant this project publishes).
ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=7

RUN --mount=type=cache,id=go,target=/go/pkg/mod \
    --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w \
      -X github.com/JerryBian/splash/internal/version.version=${VERSION} \
      -X github.com/JerryBian/splash/internal/version.commit=${COMMIT} \
      -X github.com/JerryBian/splash/internal/version.date=${DATE}" \
    -o /out/splash ./cmd/server

# The SQLite directory is prepared here so the runtime stage can COPY it with
# the nonroot ownership already set — distroless has no shell to chown with.
RUN mkdir /out/data

# --- Stage 3: runtime --------------------------------------------------------
# A static binary needs no libc, so the distroless static base is enough. The
# nonroot variant runs as uid 65532 and ships no shell, which also means there
# is nothing to `docker exec` into.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="splash" \
      org.opencontainers.image.description="Mobile-first check-in habit tracker: one static binary with the embedded UI and SQLite." \
      org.opencontainers.image.source="https://github.com/JerryBian/splash" \
      org.opencontainers.image.licenses="MIT"

WORKDIR /

COPY --from=build /out/splash /usr/local/bin/splash
COPY --from=build --chown=65532:65532 /out/data /data

# 8080 is unprivileged, so binding it needs no root.
EXPOSE 8080

# The database lives on a volume so it survives container replacement. With a
# bind mount instead, the host directory must be writable by uid 65532.
VOLUME /data

ENV SPLASH_HTTP_ADDR=:8080 \
    SPLASH_ENV=production \
    SPLASH_LOG_FORMAT=json \
    SPLASH_DB_PATH=/data/splash.db

# Readiness, not liveness: a container that cannot serve traffic should be
# reported unhealthy even while the process is alive. There is no shell or curl
# here, so the binary probes itself — see probeHealth in cmd/server/main.go.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/splash", "-check", "127.0.0.1:8080"]

ENTRYPOINT ["/usr/local/bin/splash"]
