# Builder image for the AI Frontend Builder (see .specs/features/ai-frontend-builder/design.md, section 5).
#
# Stages:
#   gobuild      compiles cmd/zeep-builder (static, CGO off)
#   runtime-base Node LTS + git + non-root user + /work + default template
#   runtime      runtime-base + the Go binary (default/last stage)
#
# NOTE: cmd/zeep-builder is delivered by task T07. Until it lands, the default
# target (`runtime`) cannot build. The runtime part is verifiable on its own:
#   docker build --target runtime-base -f Dockerfile.builder -t zeep-builder-base .
# BUILDER_BINARY_SRC lets CI/devs point the Go stage at another main package.
#
# The image bakes NO secrets and NO npm registry: the builder daemon receives
# BUILDER_SHARED_SECRET and npm_config_registry / npm_config_cache at runtime
# (compose/Helm). Generated code is untrusted: run it as uid 1000 with a
# read-only root filesystem, tmpfs /tmp, a writable /work volume, no
# docker.sock and no extra capabilities.

ARG NODE_IMAGE=node:22-bookworm-slim
ARG GO_IMAGE=golang:1.26-alpine

# --- Go build -----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS gobuild

ARG TARGETARCH
ARG BUILDER_BINARY_SRC=./cmd/zeep-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/zeep-builder ${BUILDER_BINARY_SRC}

# --- Runtime base (Node + git + template) ---------------------------------------
FROM ${NODE_IMAGE} AS runtime-base

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    # Commits made by the daemon need an identity; no remote/credentials involved.
    && git config --system user.name "Orbit Builder" \
    && git config --system user.email "builder@orbit.invalid" \
    && git config --system init.defaultBranch main

# The node base image already ships user "node" with uid/gid 1000. /work holds
# per-session working copies and the shared npm cache; it is a volume at runtime.
RUN mkdir -p /work /opt/templates \
    && chown 1000:1000 /work

# Default template, root-owned and read-only for the builder user. The daemon
# copies it into /work/<session>/ on Init.
COPY builder/templates/default /opt/templates/default

WORKDIR /work
VOLUME ["/work"]

USER 1000:1000

# --- Final image ----------------------------------------------------------------
FROM runtime-base AS runtime

COPY --from=gobuild /out/zeep-builder /usr/local/bin/zeep-builder

# HTTP API of the builder daemon (every route needs X-Builder-Token, including
# /v1/health, so no HEALTHCHECK is declared here; probe it from compose/Helm
# with a signed token or a TCP check on this port).
EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/zeep-builder"]
